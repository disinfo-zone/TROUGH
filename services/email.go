package services

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/yourusername/trough/models"
)

type MailSender interface {
	Send(to, subject, body string) error
}

type Mailer struct {
	host string
	port int
	user string
	pass string
	tls  bool
	from string
}

func NewMailer(cfg *models.SiteSettings) *Mailer {
	// Sanitize host: strip http/https and any path
	host := strings.TrimSpace(cfg.SMTPHost)
	if strings.HasPrefix(strings.ToLower(host), "http://") || strings.HasPrefix(strings.ToLower(host), "https://") {
		if u, err := url.Parse(host); err == nil {
			host = u.Host
		}
	}
	// Strip any trailing slash and any embedded path
	if i := strings.Index(host, "/"); i >= 0 {
		host = host[:i]
	}
	// Use custom from email if set, otherwise fall back to username
	fromEmail := cfg.SMTPFromEmail
	if fromEmail == "" {
		fromEmail = cfg.SMTPUsername
	}
	return &Mailer{
		host: host,
		port: cfg.SMTPPort,
		user: cfg.SMTPUsername,
		pass: cfg.SMTPPassword,
		tls:  cfg.SMTPTLS,
		from: fromEmail,
	}
}

// Allows swapping in tests
var NewMailSender = func(cfg *models.SiteSettings) MailSender { return NewMailer(cfg) }

// HashToken computes a hex-encoded SHA-256 of an opaque token string. Use for storing verification/reset tokens at rest.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Mailer) Send(to, subject, body string) error {
	// Build dial address; net.Dial supports bracketed IPv6
	hostPort := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	msg := []byte("From: " + s.from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	// Common dialer with timeouts for non-implicit TLS path
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	// If TLS is enabled and using implicit TLS (commonly port 465), connect via TLS immediately
	if s.tls && s.port == 465 {
		// Implicit TLS (465): bound handshake with timeout
		d := &net.Dialer{Timeout: 10 * time.Second}
		conn, err := tls.DialWithDialer(d, "tcp", hostPort, &tls.Config{ServerName: s.host})
		if err != nil {
			return err
		}
		c, err := smtp.NewClient(conn, s.host)
		if err != nil {
			return err
		}
		defer c.Close()
		// Bound the overall SMTP interaction
		_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
		if err := c.Auth(auth); err != nil {
			return err
		}
		if err := c.Mail(s.from); err != nil {
			return err
		}
		if err := c.Rcpt(to); err != nil {
			return err
		}
		w, err := c.Data()
		if err != nil {
			return err
		}
		if _, err := w.Write(msg); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		return c.Quit()
	}

	// For STARTTLS (commonly port 587) or plain connection, dial then optionally upgrade
	conn, err := dialer.Dial("tcp", hostPort)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))

	if s.tls {
		// Attempt STARTTLS upgrade; fail if not supported when TLS is requested
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("server does not support STARTTLS")
		}
	}

	if err := c.Auth(auth); err != nil {
		return err
	}
	if err := c.Mail(s.from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// ---- Lightweight async mail queue ----

type queuedMail struct {
	to      string
	subject string
	body    string
}

var (
	mailQueueCh   chan queuedMail
	mailQueueOnce = func() {
		// default no-op; real init below
	}
)

// InitMailQueue starts a background goroutine to process emails asynchronously.
// It must be called once at startup when SMTP is configured.
func InitMailQueue(senderFactory func(*models.SiteSettings) MailSender, repo models.SiteSettingsRepositoryInterface) {
	if mailQueueCh != nil {
		return
	}
	mailQueueCh = make(chan queuedMail, 256)
	go func() {
		// Read settings once and create sender; refresh on failure every minute
		var sender MailSender
		var lastInit time.Time
		for msg := range mailQueueCh {
			// lazily init or re-init every 60s
			if sender == nil || time.Since(lastInit) > time.Minute {
				if repo != nil {
					if s, err := repo.Get(); err == nil && s != nil {
						sender = senderFactory(s)
						lastInit = time.Now()
					}
				}
			}
			if sender == nil {
				// drop silently when not configured
				continue
			}
			// Try with one retry on transient error
			if err := sender.Send(msg.to, msg.subject, msg.body); err != nil {
				time.Sleep(2 * time.Second)
				_ = sender.Send(msg.to, msg.subject, msg.body)
			}
		}
	}()
}

// EnqueueMail enqueues a message to be sent asynchronously; no-op if queue not initialized.
func EnqueueMail(to, subject, body string) {
	if mailQueueCh == nil {
		return
	}
	select {
	case mailQueueCh <- queuedMail{to: to, subject: subject, body: body}:
	default:
		// queue full: drop to avoid blocking request path
	}
}
