package handlers

import (
	"os"
	"path/filepath"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

var checkAdmin = func(c *fiber.Ctx, repo models.UserRepositoryInterface) bool { return isAdmin(c, repo) }

type AdminHandler struct {
	settingsRepo  models.SiteSettingsRepositoryInterface
	userRepo      models.UserRepositoryInterface
	newMailSender func(*models.SiteSettings) services.MailSender
}

func NewAdminHandler(settingsRepo models.SiteSettingsRepositoryInterface, userRepo models.UserRepositoryInterface) *AdminHandler {
	return &AdminHandler{settingsRepo: settingsRepo, userRepo: userRepo, newMailSender: services.NewMailSender}
}

// For tests
func (h *AdminHandler) WithMailFactory(f func(*models.SiteSettings) services.MailSender) *AdminHandler {
	h.newMailSender = f
	return h
}

// Public site settings
func (h *AdminHandler) GetPublicSite(c *fiber.Ctx) error {
	set, _ := h.settingsRepo.Get()
	emailEnabled := set.SMTPHost != "" && set.SMTPPort > 0 && set.SMTPUsername != "" && set.SMTPPassword != ""
	return c.JSON(fiber.Map{
		"site_name":                  set.SiteName,
		"site_url":                   set.SiteURL,
		"seo_title":                  set.SEOTitle,
		"seo_description":            set.SEODescription,
		"social_image_url":           set.SocialImageURL,
		"favicon_path":               set.FaviconPath,
		"email_enabled":              emailEnabled,
		"require_email_verification": set.RequireEmailVerification,
	})
}

// Admin-only full settings
func (h *AdminHandler) GetSiteSettings(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	set, err := h.settingsRepo.Get()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to load settings"})
	}
	return c.JSON(set)
}

func (h *AdminHandler) UpdateSiteSettings(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	var body models.SiteSettings
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	body.UpdatedAt = time.Now()
	if err := h.settingsRepo.Upsert(&body); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save settings"})
	}
	return c.JSON(body)
}

func (h *AdminHandler) UploadFavicon(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	file, err := c.FormFile("favicon")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No favicon provided"})
	}
	if file.Size > 5*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "File too large"})
	}
	if err := os.MkdirAll(filepath.Join("uploads", "site"), 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to prepare upload directory"})
	}
	ext := filepath.Ext(file.Filename)
	if ext == "" {
		ext = ".ico"
	}
	path := filepath.Join("uploads", "site", "favicon"+ext)
	if err := c.SaveFile(file, path); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save favicon"})
	}
	public := "/" + path
	if err := h.settingsRepo.UpdateFavicon(public); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update settings"})
	}
	return c.JSON(fiber.Map{"favicon_path": public})
}

func (h *AdminHandler) UploadSocialImage(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No image provided"})
	}
	if file.Size > 20*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "File too large"})
	}
	if err := os.MkdirAll(filepath.Join("uploads", "site"), 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to prepare upload directory"})
	}
	ext := filepath.Ext(file.Filename)
	if ext == "" {
		ext = ".png"
	}
	path := filepath.Join("uploads", "site", "social-image"+ext)
	if err := c.SaveFile(file, path); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save image"})
	}
	public := "/" + path
	if err := h.settingsRepo.UpdateSocialImageURL(public); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update settings"})
	}
	return c.JSON(fiber.Map{"social_image_url": public})
}

func (h *AdminHandler) TestSMTP(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	type req struct {
		To string `json:"to"`
	}
	var r req
	if err := c.BodyParser(&r); err != nil || r.To == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Recipient required"})
	}
	set, _ := h.settingsRepo.Get()
	if set.SMTPHost == "" || set.SMTPPort == 0 || set.SMTPUsername == "" || set.SMTPPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP not configured"})
	}
	sender := h.newMailSender(set)
	if err := sender.Send(r.To, "SMTP test", "This is a test email from Trough."); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP send failed", "details": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}
