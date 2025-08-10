package handlers

import (
	"bytes"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

var checkAdmin = func(c *fiber.Ctx, repo models.UserRepositoryInterface) bool { return isAdmin(c, repo) }

type AdminHandler struct {
	settingsRepo  models.SiteSettingsRepositoryInterface
	userRepo      models.UserRepositoryInterface
	imageRepo     models.ImageRepositoryInterface
	newMailSender func(*models.SiteSettings) services.MailSender
	storage       services.Storage
}

func NewAdminHandler(settingsRepo models.SiteSettingsRepositoryInterface, userRepo models.UserRepositoryInterface, imageRepo models.ImageRepositoryInterface) *AdminHandler {
	return &AdminHandler{settingsRepo: settingsRepo, userRepo: userRepo, imageRepo: imageRepo, newMailSender: services.NewMailSender}
}

// For tests
func (h *AdminHandler) WithMailFactory(f func(*models.SiteSettings) services.MailSender) *AdminHandler {
	h.newMailSender = f
	return h
}

// WithStorage injects a storage backend for saving admin assets.
func (h *AdminHandler) WithStorage(s services.Storage) *AdminHandler {
	h.storage = s
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
	// Redact secrets before returning
	redacted := *set
	if redacted.SMTPPassword != "" {
		redacted.SMTPPassword = "***"
	}
	if redacted.S3AccessKey != "" {
		redacted.S3AccessKey = "***"
	}
	if redacted.S3SecretKey != "" {
		redacted.S3SecretKey = "***"
	}
	return c.JSON(redacted)
}

func (h *AdminHandler) UpdateSiteSettings(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	var body models.SiteSettings
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	// If access/secret are masked or empty, preserve existing stored values
	existing, _ := h.settingsRepo.Get()
	if existing != nil {
		if body.S3AccessKey == "" || body.S3AccessKey == "***" {
			body.S3AccessKey = existing.S3AccessKey
		}
		if body.S3SecretKey == "" || body.S3SecretKey == "***" {
			body.S3SecretKey = existing.S3SecretKey
		}
		if body.SMTPPassword == "" || body.SMTPPassword == "***" {
			body.SMTPPassword = existing.SMTPPassword
		}
	}
	body.UpdatedAt = time.Now()
	log.Printf("Admin: updating site settings: provider=%s, s3_endpoint=%s, bucket=%s, public_base=%s, smtp_host=%s, smtp_port=%d, tls=%v",
		strings.TrimSpace(body.StorageProvider), strings.TrimSpace(body.S3Endpoint), strings.TrimSpace(body.S3Bucket), strings.TrimSpace(body.PublicBaseURL), strings.TrimSpace(body.SMTPHost), body.SMTPPort, body.SMTPTLS)
	if err := h.settingsRepo.Upsert(&body); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save settings"})
	}
	// If storage settings changed, rebuild the storage for subsequent requests
	if st, err := services.NewStorageFromSettings(body); err == nil {
		h.storage = st
		services.SetCurrentStorage(st)
	} else {
		log.Printf("Admin: storage rebuild failed: %v", err)
	}
	// Return redacted
	saved := body
	if saved.SMTPPassword != "" {
		saved.SMTPPassword = "***"
	}
	if saved.S3AccessKey != "" {
		saved.S3AccessKey = "***"
	}
	if saved.S3SecretKey != "" {
		saved.S3SecretKey = "***"
	}
	log.Printf("Admin: settings updated successfully: provider=%s", strings.TrimSpace(saved.StorageProvider))
	return c.JSON(saved)
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
	// Attempt to push to storage under site/
	b, _ := os.ReadFile(path)
	key := filepath.Join("site", "favicon"+ext)
	public := "/" + path
	if h.storage != nil {
		if _, err := h.storage.Save(c.Context(), key, bytes.NewReader(b), file.Header.Get("Content-Type")); err == nil {
			public = h.storage.PublicURL(key)
		}
	}
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
	b, _ := os.ReadFile(path)
	key := filepath.Join("site", "social-image"+ext)
	public := "/" + path
	if h.storage != nil {
		if _, err := h.storage.Save(c.Context(), key, bytes.NewReader(b), file.Header.Get("Content-Type")); err == nil {
			public = h.storage.PublicURL(key)
		}
	}
	if err := h.settingsRepo.UpdateSocialImageURL(public); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update settings"})
	}
	return c.JSON(fiber.Map{"social_image_url": public})
}

// ExportLocalUploadsToStorage migrates files from local storage to remote storage and updates database URLs.
// This is a comprehensive migration that uploads files, updates database records, and optionally cleans up local files.
func (h *AdminHandler) ExportLocalUploadsToStorage(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.storage == nil || h.storage.IsLocal() {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Remote storage not configured"})
	}

	type MigrationRequest struct {
		CleanupLocal bool `json:"cleanup_local"`
	}
	var req MigrationRequest
	c.BodyParser(&req) // Optional body

	// The imageRepo is now available as h.imageRepo
	
	// For now, let's migrate files and provide comprehensive feedback
	type MigrationResult struct {
		TotalFiles     int      `json:"total_files"`
		UploadedFiles  int      `json:"uploaded_files"`
		UpdatedRecords int      `json:"updated_records"`
		CleanedFiles   int      `json:"cleaned_files,omitempty"`
		Errors         []string `json:"errors,omitempty"`
		Success        bool     `json:"success"`
	}

	result := MigrationResult{
		Success: true,
		Errors:  []string{},
	}

	// Walk uploads dir and collect files
	root := "uploads"
	var filesToMigrate []string
	
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Skip subdirectories we don't want to migrate (like avatars/site)
		rel, _ := filepath.Rel(root, path)
		if strings.Contains(rel, string(filepath.Separator)) {
			return nil // Skip files in subdirectories
		}
		filesToMigrate = append(filesToMigrate, rel)
		return nil
	})
	
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to scan local files", "details": err.Error()})
	}

	result.TotalFiles = len(filesToMigrate)

	// Upload each file to remote storage
	var uploadedFiles []string
	for _, filename := range filesToMigrate {
		localPath := filepath.Join(root, filename)
		b, err := os.ReadFile(localPath)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to read %s: %v", filename, err))
			continue
		}

		// Determine content type
		ct := "application/octet-stream"
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".png":
			ct = "image/png"
		case ".webp":
			ct = "image/webp"
		}

		// Upload to remote storage
		publicURL, err := h.storage.Save(c.Context(), filename, bytes.NewReader(b), ct)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to upload %s: %v", filename, err))
			continue
		}

		uploadedFiles = append(uploadedFiles, filename)
		result.UploadedFiles++
		
		// Update database records for images with this filename
		images, err := h.imageRepo.GetImagesByFilename(filename)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to query database for %s: %v", filename, err))
			continue
		}

		// Update each image record with the new public URL
		for _, img := range images {
			err := h.imageRepo.UpdateFilename(img.ID, publicURL)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to update database for %s: %v", filename, err))
			} else {
				result.UpdatedRecords++
			}
		}
	}

	// If cleanup is requested and uploads were successful, remove local files
	if req.CleanupLocal && len(uploadedFiles) > 0 {
		for _, filename := range uploadedFiles {
			localPath := filepath.Join(root, filename)
			if err := os.Remove(localPath); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to cleanup %s: %v", filename, err))
			} else {
				result.CleanedFiles++
			}
		}
	}

	if len(result.Errors) > 0 {
		result.Success = false
	}

	return c.JSON(result)
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
		log.Printf("Admin: SMTP test failed: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP send failed", "details": err.Error()})
	}
	log.Printf("Admin: SMTP test sent to %s", r.To)
	return c.SendStatus(fiber.StatusNoContent)
}

// TestStorage attempts a small write/delete to verify the current storage configuration.
func (h *AdminHandler) TestStorage(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	// Build storage from latest settings
	set, _ := h.settingsRepo.Get()
	st := h.storage
	if st == nil {
		if s2, err := services.NewStorageFromSettings(*set); err == nil {
			st = s2
		}
	}
	if st == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Storage not configured"})
	}
	// Try save/delete a small object
	key := filepath.ToSlash(filepath.Join("health", time.Now().Format("20060102T150405.000000000")+".txt"))
	_, err := st.Save(c.Context(), key, bytes.NewReader([]byte("ok")), "text/plain")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Storage write failed", "details": err.Error()})
	}
	_ = st.Delete(c.Context(), key)
	return c.JSON(fiber.Map{
		"ok":              true,
		"provider":        set.StorageProvider,
		"is_local":        st.IsLocal(),
		"public":          st.PublicURL(""),
		"public_base_url": set.PublicBaseURL,
	})
}
