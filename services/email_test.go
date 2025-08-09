package services

import (
	"errors"
	"testing"

	"github.com/yourusername/trough/models"
)

type fakeSender struct {
	sent []struct{ to, sub, body string }
	fail error
}

func (f *fakeSender) Send(to, subject, body string) error {
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, struct{ to, sub, body string }{to, subject, body})
	return nil
}

func TestNewMailSenderFactory(t *testing.T) {
	set := &models.SiteSettings{SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: "noreply@example.com", SMTPPassword: "x", SMTPTLS: false}
	s := NewMailSender(set)
	if s == nil {
		t.Fatal("expected sender")
	}
}

func TestMailerConfig(t *testing.T) {
	set := &models.SiteSettings{SMTPHost: "smtp.example.com", SMTPPort: 465, SMTPUsername: "from@example.com", SMTPPassword: "secret", SMTPTLS: true}
	m := NewMailer(set)
	if m.host != set.SMTPHost || m.port != set.SMTPPort || m.user != set.SMTPUsername || m.pass != set.SMTPPassword || m.tls != set.SMTPTLS || m.from != set.SMTPUsername {
		t.Fatalf("mailer not configured from settings")
	}
}

func TestMailSenderMock(t *testing.T) {
	f := &fakeSender{}
	if err := f.Send("a@b.c", "sub", "body"); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 {
		t.Fatalf("expected 1 send, got %d", len(f.sent))
	}
	if f.sent[0].to != "a@b.c" {
		t.Fatalf("wrong to")
	}
	f.fail = errors.New("boom")
	if err := f.Send("x", "y", "z"); err == nil {
		t.Fatal("expected error")
	}
}
