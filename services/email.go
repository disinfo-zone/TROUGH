package services

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"mime"
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

// BuildVerificationEmail returns a subject and plain-text body for email verification.
// It is intentionally whimsical and text-only (UTF-8) to keep compatibility while feeling distinct.
func BuildVerificationEmail(siteName, siteURL, link string) (string, string) {
	if strings.TrimSpace(siteName) == "" {
		siteName = "TROUGH"
	}
	// Normalize siteURL for display
	siteURL = strings.TrimSpace(siteURL)
	// Subject keeps it short and eye-catching with unicode arrows and blocks.
	subject := "▣ Verify your email · " + siteName

	// Body: retro-cyber ASCII/Unicode style, no HTML.
	// Keep lines relatively short to render nicely in plain-text clients.
	body := "" +
		"┌──────────────────────────────────────────────┐\n" +
		"│   " + siteName + " — SIGNAL CONFIRMATION RITUAL   │\n" +
		"└──────────────────────────────────────────────┘\n\n" +
		"greetings operator,\n\n" +
		"to complete your account setup you must verify your email.\n" +
		"this proves you control this address and unlocks uploads.\n\n" +
		"→ verification link (valid ~24 hours)\n" +
		link + "\n\n" +
		"if the link is not clickable, copy + paste it into your browser.\n" +
		"keep this link secret; it works once.\n\n" +
		"site: " + siteURL + "\n" +
		"time: " + time.Now().Format(time.RFC1123) + "\n\n" +
		"— " + siteName + " // see you on the other side ✷\n"

	return subject, body
}

// HashToken computes a hex-encoded SHA-256 of an opaque token string. Use for storing verification/reset tokens at rest.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *Mailer) Send(to, subject, body string) error {
	headerSafe := func(v string) string {
		// Strip CR/LF to prevent header injection; headers must be single-line
		v = strings.ReplaceAll(v, "\r", "")
		v = strings.ReplaceAll(v, "\n", "")
		return v
	}
	encodeHeader := func(v string) string {
		// RFC 2047 encoded-word for non-ASCII
		return mime.QEncoding.Encode("utf-8", headerSafe(v))
	}
	// Build dial address; net.Dial supports bracketed IPv6
	hostPort := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	safeFrom := headerSafe(s.from)
	safeTo := headerSafe(to)
	msg := []byte("From: " + safeFrom + "\r\n" +
		"To: " + safeTo + "\r\n" +
		"Subject: " + encodeHeader(subject) + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n" + body + "\r\n")
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
