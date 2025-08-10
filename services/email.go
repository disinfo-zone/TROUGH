package services

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"

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

func (s *Mailer) Send(to, subject, body string) error {
	// Build dial address; net.Dial supports bracketed IPv6
	hostPort := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	msg := []byte("From: " + s.from + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)

	// If TLS is enabled and using implicit TLS (commonly port 465), connect via TLS immediately
	if s.tls && s.port == 465 {
		conn, err := tls.Dial("tcp", hostPort, &tls.Config{ServerName: s.host})
		if err != nil {
			return err
		}
		c, err := smtp.NewClient(conn, s.host)
		if err != nil {
			return err
		}
		defer c.Close()
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
	conn, err := net.Dial("tcp", hostPort)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return err
	}
	defer c.Close()

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
