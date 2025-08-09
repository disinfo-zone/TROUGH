package services

import (
	"crypto/tls"
	"fmt"
	"net/smtp"

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
	return &Mailer{
		host: cfg.SMTPHost,
		port: cfg.SMTPPort,
		user: cfg.SMTPUsername,
		pass: cfg.SMTPPassword,
		tls:  cfg.SMTPTLS,
		from: cfg.SMTPUsername,
	}
}

// Allows swapping in tests
var NewMailSender = func(cfg *models.SiteSettings) MailSender { return NewMailer(cfg) }

func (s *Mailer) Send(to, subject, body string) error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	msg := []byte("To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body + "\r\n")
	auth := smtp.PlainAuth("", s.user, s.pass, s.host)
	if s.tls {
		conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.host})
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
	return smtp.SendMail(addr, auth, s.from, []string{to}, msg)
}
