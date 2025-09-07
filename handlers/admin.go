package handlers

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yourusername/trough/middleware"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

// Precompiled regex validators for analytics settings
var (
	gaIDRe    = regexp.MustCompile(`^G-[A-Z0-9]{6,}`)
	uuidRe    = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	httpsJSRe = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(?::\d{2,5})?/.+\.js(\?.*)?$`)
	domainRe  = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)
)

var checkAdmin = func(c *fiber.Ctx, repo models.UserRepositoryInterface) bool { return isAdmin(c, repo) }

type AdminHandler struct {
	settingsRepo  models.SiteSettingsRepositoryInterface
	userRepo      models.UserRepositoryInterface
	imageRepo     models.ImageRepositoryInterface
	newMailSender func(*models.SiteSettings) services.MailSender
	storage       services.Storage
	inviteRepo    models.InviteRepositoryInterface
	pageRepo      models.PageRepositoryInterface
	rateLimiter   *services.RateLimiter
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

// WithInvites injects the invite repository.
func (h *AdminHandler) WithInvites(r models.InviteRepositoryInterface) *AdminHandler {
	h.inviteRepo = r
	return h
}

// WithPages injects the pages repository
func (h *AdminHandler) WithPages(r models.PageRepositoryInterface) *AdminHandler {
	h.pageRepo = r
	return h
}

// WithRateLimiter injects the rate limiter
func (h *AdminHandler) WithRateLimiter(rl *services.RateLimiter) *AdminHandler {
	h.rateLimiter = rl
	return h
}

// Public site settings
func (h *AdminHandler) GetPublicSite(c *fiber.Ctx) error {
	set, _ := h.settingsRepo.Get()
	emailEnabled := set.SMTPHost != "" && set.SMTPPort > 0 && set.SMTPUsername != "" && set.SMTPPassword != ""
	return c.JSON(fiber.Map{
		"site_name":                   set.SiteName,
		"site_url":                    set.SiteURL,
		"seo_title":                   set.SEOTitle,
		"seo_description":             set.SEODescription,
		"social_image_url":            set.SocialImageURL,
		"favicon_path":                set.FaviconPath,
		"email_enabled":               emailEnabled,
		"require_email_verification":  set.RequireEmailVerification,
		"public_registration_enabled": set.PublicRegistrationEnabled,
	})
}

// Admin endpoints for invite codes
// CreateInvite allows an admin to generate an invite with optional max uses and expiration.
func (h *AdminHandler) CreateInvite(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.inviteRepo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Invite repository not configured"})
	}
	type body struct {
		MaxUses   *int    `json:"max_uses"`
		Duration  *string `json:"duration"`   // e.g., "24h", "7d", "3h"
		ExpiresAt *string `json:"expires_at"` // ISO8601 optional alternative
	}
	var b body
	if err := c.BodyParser(&b); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	var expires *time.Time
	// Prefer explicit expires_at if provided
	if b.ExpiresAt != nil && strings.TrimSpace(*b.ExpiresAt) != "" {
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(*b.ExpiresAt)); err == nil {
			expires = &t
		} else {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid expires_at (use RFC3339)"})
		}
	} else if b.Duration != nil && strings.TrimSpace(*b.Duration) != "" {
		dstr := strings.ToLower(strings.TrimSpace(*b.Duration))
		// support Nx where x in h,m,s and also days via 'd'
		var d time.Duration
		var err error
		if strings.HasSuffix(dstr, "d") {
			num := strings.TrimSuffix(dstr, "d")
			// parse as integer days
			if num == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid duration"})
			}
			// simple parse
			var days int
			_, err = fmt.Sscanf(num, "%d", &days)
			if err == nil && days > 0 {
				d = time.Hour * 24 * time.Duration(days)
			} else {
				err = fmt.Errorf("invalid days")
			}
		} else {
			d, err = time.ParseDuration(dstr)
		}
		if err != nil || d <= 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid duration"})
		}
		t := time.Now().Add(d)
		expires = &t
	}
	// Sanitize max uses: nil => unlimited; 0 or negative => treat as 1
	if b.MaxUses != nil && *b.MaxUses <= 0 {
		one := 1
		b.MaxUses = &one
	}
	// creator id
	var creator *uuid.UUID
	uid := middleware.GetUserID(c)
	if uid != uuid.Nil {
		creator = &uid
	}
	inv, err := h.inviteRepo.Create(b.MaxUses, expires, creator)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create invite"})
	}
	// Build link based on configured site URL if available
	set, _ := h.settingsRepo.Get()
	base := strings.TrimRight(strings.TrimSpace(set.SiteURL), "/")
	var link string
	if base != "" {
		link = base + "/register?invite=" + inv.Code
	} else {
		link = "/register?invite=" + inv.Code
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"invite": inv, "link": link})
}

// ListInvites returns paginated invites for admins
func (h *AdminHandler) ListInvites(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.inviteRepo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Invite repository not configured"})
	}
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit < 1 {
		limit = 1
	} else if limit > 200 {
		limit = 200
	}
	list, total, err := h.inviteRepo.List(page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list invites"})
	}
	return c.JSON(fiber.Map{"invites": list, "page": page, "limit": limit, "total": total, "total_pages": (total + limit - 1) / limit})
}

// DeleteInvite removes an invite by id
func (h *AdminHandler) DeleteInvite(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.inviteRepo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Invite repository not configured"})
	}
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid id"})
	}
	if err := h.inviteRepo.Delete(id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// PruneInvites deletes all fully-used or expired invite codes. Unlimited/time-unlimited active codes are kept.
func (h *AdminHandler) PruneInvites(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.inviteRepo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Invite repository not configured"})
	}
	n, err := h.inviteRepo.DeleteUsedAndExpired()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to clear invites"})
	}
	return c.JSON(fiber.Map{"deleted": n})
}

// Admin-only full settings
func (h *AdminHandler) GetSiteSettings(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	// Mark admin session with a non-sensitive cookie to allow analytics suppression in SSR
	c.Cookie(&fiber.Cookie{Name: "trough_admin", Value: "1", Path: "/", HTTPOnly: true, Secure: true, SameSite: "Lax"})
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
	// Reinforce admin cookie on update
	c.Cookie(&fiber.Cookie{Name: "trough_admin", Value: "1", Path: "/", HTTPOnly: true, Secure: true, SameSite: "Lax"})
	var body models.SiteSettings
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	// Trim basic fields
	body.SiteName = strings.TrimSpace(body.SiteName)
	body.SiteURL = strings.TrimSpace(body.SiteURL)
	body.SEOTitle = strings.TrimSpace(body.SEOTitle)
	body.SEODescription = strings.TrimSpace(body.SEODescription)
	body.SocialImageURL = strings.TrimSpace(body.SocialImageURL)

	// Validate analytics config conservatively
	provider := strings.ToLower(strings.TrimSpace(body.AnalyticsProvider))
	if provider != "ga4" && provider != "umami" && provider != "plausible" {
		provider = ""
	}
	body.AnalyticsProvider = provider
	// If a provider is selected, validate its fields; otherwise allow enabled state to persist without error
	switch body.AnalyticsProvider {
	case "ga4":
		body.GA4MeasurementID = strings.ToUpper(strings.TrimSpace(body.GA4MeasurementID))
		if !gaIDRe.MatchString(body.GA4MeasurementID) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid GA4 measurement ID"})
		}
		// Clear other providers to prevent stale injection when switching providers
		body.UmamiSrc, body.UmamiWebsiteID, body.PlausibleSrc, body.PlausibleDomain = "", "", "", ""
	case "umami":
		body.UmamiSrc = strings.TrimSpace(body.UmamiSrc)
		body.UmamiWebsiteID = strings.TrimSpace(body.UmamiWebsiteID)
		if !httpsJSRe.MatchString(body.UmamiSrc) || !uuidRe.MatchString(body.UmamiWebsiteID) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid Umami src or website id"})
		}
		body.GA4MeasurementID, body.PlausibleSrc, body.PlausibleDomain = "", "", ""
	case "plausible":
		body.PlausibleSrc = strings.TrimSpace(body.PlausibleSrc)
		body.PlausibleDomain = strings.ToLower(strings.TrimSpace(body.PlausibleDomain))
		if !httpsJSRe.MatchString(body.PlausibleSrc) || !domainRe.MatchString(body.PlausibleDomain) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid Plausible src or domain"})
		}
		body.GA4MeasurementID, body.UmamiSrc, body.UmamiWebsiteID = "", "", ""
	default:
		// No provider selected: keep enabled flag as-is, but ensure no injection will occur
		// by keeping provider empty; do not erase any fields the admin may be editing.
		// Nothing to do.
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
	log.Printf("Admin: updating site settings: provider=%s, s3_endpoint=%s, bucket=%s, public_base=%s, smtp_host=%s, smtp_port=%d, tls=%v, analytics=%v/%s",
		strings.TrimSpace(body.StorageProvider), strings.TrimSpace(body.S3Endpoint), strings.TrimSpace(body.S3Bucket), strings.TrimSpace(body.PublicBaseURL), strings.TrimSpace(body.SMTPHost), body.SMTPPort, body.SMTPTLS,
		body.AnalyticsEnabled, body.AnalyticsProvider)
	if err := h.settingsRepo.Upsert(&body); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save settings"})
	}
	// Update in-memory settings cache immediately
	services.UpdateCachedSettings(body)
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

// ---- Backups ----

// AdminCreateBackup creates a new backup and returns it as a downloadable file (application/gzip).
func (h *AdminHandler) AdminCreateBackup(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	b, name, err := services.CreateBackup(c.Context(), models.DB())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create backup"})
	}
	c.Set("Content-Type", "application/gzip")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", name))
	return c.Send(b)
}

// AdminListBackups lists locally stored backup files (in backups/ directory).
func (h *AdminHandler) AdminListBackups(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	list, err := services.ListBackups("backups")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list backups"})
	}
	return c.JSON(fiber.Map{"backups": list})
}

// AdminSaveBackup writes a backup to server disk (backups/) and returns path metadata.
func (h *AdminHandler) AdminSaveBackup(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	path, err := services.SaveBackupFile(c.Context(), models.DB(), "backups")
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save backup"})
	}
	return c.JSON(fiber.Map{"path": path})
}

// AdminDeleteBackup deletes a named backup from server disk.
func (h *AdminHandler) AdminDeleteBackup(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	name := strings.TrimSpace(c.Params("name"))
	if name == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Name required"})
	}
	if err := services.DeleteBackup("backups", name); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Delete failed"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// AdminRestoreBackup restores from an uploaded backup file.
func (h *AdminHandler) AdminRestoreBackup(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No file provided"})
	}
	f, err := fileHeader.Open()
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to open file"})
	}
	defer f.Close()
	var r io.Reader = f
	if err := services.RestoreBackup(c.Context(), models.DB(), r); err != nil {
		log.Printf("Admin: restore failed: %v", err)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Restore failed", "details": err.Error()})
	}
	// Invalidate caches that may depend on DB
	services.InvalidateSettingsCache()
	return c.SendStatus(fiber.StatusNoContent)
}

// AdminDownloadSavedBackup streams a previously-saved backup file by name.
func (h *AdminHandler) AdminDownloadSavedBackup(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	name := strings.TrimSpace(c.Params("name"))
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid name"})
	}
	path := filepath.Join("backups", name)
	b, err := os.ReadFile(path)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Not found"})
	}
	c.Set("Content-Type", "application/gzip")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", name))
	return c.Send(b)
}

// AdminDiag returns quick sanity counts for core tables.
func (h *AdminHandler) AdminDiag(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	type stat struct {
		Count int `json:"count"`
	}
	db := models.DB()
	out := fiber.Map{}
	var v int
	// helpers
	count := func(table string) int {
		v = 0
		_ = db.Get(&v, "SELECT COUNT(*) FROM "+table)
		return v
	}
	out["users"] = count("users")
	out["images"] = count("images")
	out["collections"] = count("collections")
	out["likes"] = count("likes")
	out["pages"] = count("pages")
	// sample ids
	type row struct {
		ID        string    `db:"id" json:"id"`
		CreatedAt time.Time `db:"created_at" json:"created_at"`
	}
	var img row
	_ = db.Get(&img, `SELECT id, created_at FROM images ORDER BY created_at DESC LIMIT 1`)
	out["latest_image"] = img
	return c.JSON(out)
}

// AdminRateLimiterStats returns rate limiter statistics and metrics
func (h *AdminHandler) AdminRateLimiterStats(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	
	if h.rateLimiter == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Rate limiter not configured"})
	}
	
	stats := h.rateLimiter.GetStats()
	return c.JSON(stats)
}

// ---- CMS Pages (Admin) ----

// AdminListPages lists pages with pagination
func (h *AdminHandler) AdminListPages(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.pageRepo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Page repository not configured"})
	}
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit < 1 {
		limit = 50
	} else if limit > 200 {
		limit = 200
	}
	list, total, err := h.pageRepo.ListAll(page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed"})
	}
	return c.JSON(fiber.Map{"pages": list, "page": page, "limit": limit, "total": total, "total_pages": (total + limit - 1) / limit})
}

type pageUpsertBody struct {
	ID              *string `json:"id"`
	Slug            string  `json:"slug"`
	Title           string  `json:"title"`
	Markdown        string  `json:"markdown"`
	IsPublished     bool    `json:"is_published"`
	RedirectURL     *string `json:"redirect_url"`
	MetaTitle       *string `json:"meta_title"`
	MetaDescription *string `json:"meta_description"`
}

// AdminCreatePage creates a page
func (h *AdminHandler) AdminCreatePage(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.pageRepo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Page repository not configured"})
	}
	var b pageUpsertBody
	if err := c.BodyParser(&b); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	slug := strings.ToLower(strings.TrimSpace(b.Slug))
	if !regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$`).MatchString(slug) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid slug"})
	}
	// Disallow conflicting reserved routes
	reserved := map[string]bool{"api": true, "uploads": true, "assets": true, "@:username": true, "i": true, "register": true, "reset": true, "verify": true, "settings": true, "admin": true}
	if reserved[slug] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Slug reserved"})
	}
	// If redirect set, validate and store only redirect; else render markdown to HTML
	if b.RedirectURL != nil && strings.TrimSpace(*b.RedirectURL) != "" {
		u := strings.TrimSpace(*b.RedirectURL)
		if !(strings.HasPrefix(strings.ToLower(u), "http://") || strings.HasPrefix(strings.ToLower(u), "https://")) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Redirect must be http(s) URL"})
		}
		// force not published? allow published so it can be used
	}
	// Store as-is; HTML will be generated on the client from markdown
	p := &models.Page{Slug: slug, Title: strings.TrimSpace(b.Title), Markdown: b.Markdown, HTML: "", IsPublished: b.IsPublished, RedirectURL: b.RedirectURL, MetaTitle: b.MetaTitle, MetaDescription: b.MetaDescription}
	if err := h.pageRepo.Create(p); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Create failed"})
	}
	return c.Status(fiber.StatusCreated).JSON(p)
}

// AdminUpdatePage updates an existing page
func (h *AdminHandler) AdminUpdatePage(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.pageRepo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Page repository not configured"})
	}
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid id"})
	}
	var b pageUpsertBody
	if err := c.BodyParser(&b); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	slug := strings.ToLower(strings.TrimSpace(b.Slug))
	if !regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$`).MatchString(slug) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid slug"})
	}
	reserved := map[string]bool{"api": true, "uploads": true, "assets": true, "@:username": true, "i": true, "register": true, "reset": true, "verify": true, "settings": true, "admin": true}
	if reserved[slug] {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Slug reserved"})
	}
	if b.RedirectURL != nil && strings.TrimSpace(*b.RedirectURL) != "" {
		u := strings.TrimSpace(*b.RedirectURL)
		if !(strings.HasPrefix(strings.ToLower(u), "http://") || strings.HasPrefix(strings.ToLower(u), "https://")) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Redirect must be http(s) URL"})
		}
	}
	p := &models.Page{ID: id, Slug: slug, Title: strings.TrimSpace(b.Title), Markdown: b.Markdown, HTML: "", IsPublished: b.IsPublished, RedirectURL: b.RedirectURL, MetaTitle: b.MetaTitle, MetaDescription: b.MetaDescription}
	if err := h.pageRepo.Update(p); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Update failed"})
	}
	return c.JSON(p)
}

// AdminDeletePage deletes a page
func (h *AdminHandler) AdminDeletePage(c *fiber.Ctx) error {
	if !checkAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	if h.pageRepo == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"error": "Page repository not configured"})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid id"})
	}
	if err := h.pageRepo.Delete(id); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Delete failed"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}
