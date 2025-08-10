package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

type fakeSettingsRepo struct{ s *models.SiteSettings }

func (f *fakeSettingsRepo) Get() (*models.SiteSettings, error)     { return f.s, nil }
func (f *fakeSettingsRepo) Upsert(*models.SiteSettings) error      { return nil }
func (f *fakeSettingsRepo) UpdateFavicon(path string) error        { return nil }
func (f *fakeSettingsRepo) UpdateSocialImageURL(path string) error { return nil }

type fakeUserRepo struct{ models.UserRepositoryInterface }

type fakeImageRepo struct{ models.ImageRepositoryInterface }

type fakeSender struct {
	fail error
	sent int
}

func (f *fakeSender) Send(to, subject, body string) error {
	if f.fail != nil {
		return f.fail
	}
	f.sent++
	return nil
}

// override admin check
func init() { checkAdmin = func(*fiber.Ctx, models.UserRepositoryInterface) bool { return true } }

func TestAdminSMTP_NotConfigured(t *testing.T) {
	app := fiber.New()
	repo := &fakeSettingsRepo{s: &models.SiteSettings{}}
	h := NewAdminHandler(repo, &fakeUserRepo{}, &fakeImageRepo{})
	app.Post("/test", h.TestSMTP)
	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdminSMTP_Success(t *testing.T) {
	app := fiber.New()
	repo := &fakeSettingsRepo{s: &models.SiteSettings{SMTPHost: "smtp", SMTPPort: 25, SMTPUsername: "u", SMTPPassword: "p"}}
	h := NewAdminHandler(repo, &fakeUserRepo{}, &fakeImageRepo{}).WithMailFactory(func(*models.SiteSettings) services.MailSender { return &fakeSender{} })
	app.Post("/test", h.TestSMTP)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"to":"a@b.c"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
}

func TestAdminSMTP_Failure(t *testing.T) {
	app := fiber.New()
	repo := &fakeSettingsRepo{s: &models.SiteSettings{SMTPHost: "smtp", SMTPPort: 25, SMTPUsername: "u", SMTPPassword: "p"}}
	fs := &fakeSender{fail: errors.New("boom")}
	h := NewAdminHandler(repo, &fakeUserRepo{}, &fakeImageRepo{}).WithMailFactory(func(*models.SiteSettings) services.MailSender { return fs })
	app.Post("/test", h.TestSMTP)
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"to":"a@b.c"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, _ := app.Test(req)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}
