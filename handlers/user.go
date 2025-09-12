package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yourusername/trough/middleware"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

type UserHandler struct {
	userRepo      models.UserRepositoryInterface
	imageRepo     models.ImageRepositoryInterface
	collectRepo   models.CollectRepositoryInterface
	storage       services.Storage
	validator     *validator.Validate
	settingsRepo  models.SiteSettingsRepositoryInterface
	newMailSender func(*models.SiteSettings) services.MailSender
	pageRepo      models.PageRepositoryInterface
}

func NewUserHandler(userRepo models.UserRepositoryInterface, imageRepo models.ImageRepositoryInterface, storage services.Storage) *UserHandler {
	return &UserHandler{userRepo: userRepo, imageRepo: imageRepo, storage: storage, validator: validator.New()}
}

func (h *UserHandler) WithCollect(r models.CollectRepositoryInterface) *UserHandler {
	h.collectRepo = r
	return h
}

// WithSettings injects site settings repo and mail sender for email verification flows.
func (h *UserHandler) WithSettings(r models.SiteSettingsRepositoryInterface) *UserHandler {
	h.settingsRepo = r
	h.newMailSender = services.NewMailSender
	return h
}

func (h *UserHandler) WithPages(r models.PageRepositoryInterface) *UserHandler {
	h.pageRepo = r
	return h
}

// Public: list published pages for footer or navigation
func (h *UserHandler) ListPublicPages(c *fiber.Ctx) error {
	if h.pageRepo == nil {
		return c.JSON(fiber.Map{"pages": []any{}})
	}
	list, err := h.pageRepo.ListPublished()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed"})
	}
	out := make([]fiber.Map, 0, len(list))
	for _, p := range list {
		out = append(out, fiber.Map{"slug": p.Slug, "title": p.Title})
	}
	return c.JSON(fiber.Map{"pages": out})
}

func (h *UserHandler) GetProfile(c *fiber.Ctx) error {
	username := normalizeUsername(c.Params("username"))
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Username required",
		})
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	user, err := h.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	return c.JSON(user.ToResponse())
}

func (h *UserHandler) GetUserImages(c *fiber.Ctx) error {
	username := normalizeUsername(c.Params("username"))
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Username required",
		})
	}

	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()

	user, err := h.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	// Support both cursor and page for compatibility
	limit := 20
	if lq := strings.TrimSpace(c.Query("limit", "")); lq != "" {
		if v, err := strconv.Atoi(lq); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	cursor := strings.TrimSpace(c.Query("cursor", ""))
	if cursor != "" {
		images, next, err := h.imageRepo.GetUserImagesSeek(user.ID, limit, cursor)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch user images"})
		}
		return c.JSON(models.FeedResponse{Images: images, NextCursor: next})
	}
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	images, total, err := h.imageRepo.GetUserImages(user.ID, page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch user images"})
	}
	return c.JSON(models.FeedResponse{Images: images, Page: page, Total: total})
}

// GetUserCollections returns images that the user has collected (not their own uploads).
func (h *UserHandler) GetUserCollections(c *fiber.Ctx) error {
	username := normalizeUsername(c.Params("username"))
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Username required"})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	user, err := h.userRepo.GetByUsername(ctx, username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}
	if h.collectRepo == nil {
		h.collectRepo = models.NewCollectRepository(models.DB())
	}
	// Support cursor and page
	limit := 20
	if lq := strings.TrimSpace(c.Query("limit", "")); lq != "" {
		if v, err := strconv.Atoi(lq); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}
	cursor := strings.TrimSpace(c.Query("cursor", ""))
	if cursor != "" {
		images, next, err := h.collectRepo.GetUserCollectionsSeek(user.ID, limit, cursor)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch collections"})
		}
		return c.JSON(models.FeedResponse{Images: images, NextCursor: next})
	}
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	images, total, err := h.collectRepo.GetUserCollections(user.ID, page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch collections", "details": err.Error()})
	}
	return c.JSON(models.FeedResponse{Images: images, Page: page, Total: total})
}

func (h *UserHandler) GetMyProfile(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	user, err := h.userRepo.GetByID(ctx, userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}
	return c.JSON(user.ToResponse())
}

func (h *UserHandler) UpdateMyProfile(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	var req models.UpdateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}

	// If changing username, ensure it is valid, not reserved, and not taken
	if req.Username != nil {
		uname := normalizeUsername(*req.Username)
		if uname == "" || len(uname) < 3 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Username too short"})
		}
		if isReservedUsername(uname) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "That username is reserved"})
		}
		// Validate against struct tags (alphanum, max=30)
		if err := h.validator.Struct(models.UpdateUserRequest{Username: &uname}); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Validation failed", "details": err.Error()})
		}
		ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
		defer cancel()
		if existing, err := h.userRepo.GetByUsername(ctx, uname); err == nil && existing != nil {
			if existing.ID != userID {
				return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Username already taken"})
			}
		}
		req.Username = &uname
	}
	// Enforce sensible bio length
	if req.Bio != nil {
		trimmed := strings.TrimSpace(*req.Bio)
		if len(trimmed) > 500 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Bio too long (max 500 characters)"})
		}
		req.Bio = &trimmed
	}

	updated, err := h.userRepo.UpdateProfile(userID, req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update profile"})
	}
	return c.JSON(updated.ToResponse())
}

// Change email
func (h *UserHandler) UpdateEmail(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	type reqBody struct {
		Email string `json:"email"`
	}
	var body reqBody
	if err := c.BodyParser(&body); err != nil || body.Email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Email required"})
	}
	// Normalize email
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	// Validate email format to prevent invalid or header-injection values from entering DB/email headers
	if h.validator != nil {
		type e struct {
			Email string `validate:"required,email"`
		}
		if err := h.validator.Struct(e{Email: body.Email}); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid email address"})
		}
	}
	// Conflict check
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	if existing, err := h.userRepo.GetByEmail(ctx, body.Email); err == nil && existing != nil {
		if existing.ID != userID {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Email already in use"})
		}
	}
	if err := h.userRepo.UpdateEmail(userID, body.Email); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update email"})
	}
	// If email verification is required, mark unverified and send verification email
	set, _ := h.settingsRepo.Get()
	if set.RequireEmailVerification && (set.SMTPHost != "" && set.SMTPPort > 0 && set.SMTPUsername != "" && set.SMTPPassword != "") {
		_ = models.SetEmailVerified(userID, false)
		token := uuid.New().String()
		exp := time.Now().Add(24 * time.Hour)
		_ = models.CreateEmailVerification(userID, services.HashToken(token), exp)
		link := strings.TrimRight(set.SiteURL, "/") + "/verify?token=" + token
		subj, bodyTxt := services.BuildVerificationEmail(set.SiteName, set.SiteURL, link)
		// Send asynchronously via queue to avoid duplicate sends
		services.EnqueueMail(body.Email, subj, bodyTxt)
	}
	return c.JSON(fiber.Map{"email": body.Email})
}

// Change password (requires current password)
func (h *UserHandler) UpdatePassword(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	type reqBody struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	var body reqBody
	if err := c.BodyParser(&body); err != nil || body.NewPassword == "" || body.CurrentPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid password"})
	}
	if err := services.ValidatePassword(body.NewPassword); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	user, err := h.userRepo.GetByID(ctx, userID)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to load user"})
	}
	if !user.CheckPassword(body.CurrentPassword) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Current password incorrect"})
	}
	// Hash new password
	if err := user.HashPassword(body.NewPassword); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to process password"})
	}
	if err := h.userRepo.UpdatePassword(userID, user.PasswordHash); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update password"})
	}
	// Best-effort: issue short response; token invalidation cache refresh happens via DB read path
	return c.SendStatus(fiber.StatusNoContent)
}

// Delete my account
func (h *UserHandler) DeleteMyAccount(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	type reqBody struct {
		Confirm string `json:"confirm"`
	}
	var body reqBody
	_ = c.BodyParser(&body)
	if body.Confirm != "DELETE" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Confirmation required"})
	}
	if err := h.userRepo.DeleteUser(userID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete account"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandler) GetMyAccount(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	user, err := h.userRepo.GetByID(ctx, userID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}
	return c.JSON(fiber.Map{"email": user.Email})
}

// UploadAvatar allows the authenticated user to upload a profile avatar image
func (h *UserHandler) UploadAvatar(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	file, err := c.FormFile("avatar")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No avatar file provided"})
	}
	// Use comprehensive file validation for avatars
	fileValidator := services.NewFileValidator()
	
	// Set smaller size limit for avatars (5MB)
	fileValidator.MaxFileSize = 5 * 1024 * 1024
	
	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to open uploaded file"})
	}
	defer src.Close()
	
	// Read a small sample for validation
	sample := make([]byte, 512)
	n, err := src.Read(sample)
	if err != nil && err != io.EOF {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to read file for validation"})
	}
	
	// Validate file sample
	result, err := fileValidator.ValidateFile(file.Filename, bytes.NewReader(sample[:n]))
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to validate file"})
	}
	
	if !result.IsValid {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": result.ErrorMessage})
	}
	
	// Seek back to beginning for further processing
	src.Seek(0, 0)
	
	// Ensure directory
	avatarDir := filepath.Join("uploads", "avatars")
	if err := os.MkdirAll(avatarDir, 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create avatar directory"})
	}
	
	// Fetch current user to know old avatar URL
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	u, _ := h.userRepo.GetByID(ctx, userID)
	oldAvatar := ""
	if u != nil && u.AvatarURL != nil {
		oldAvatar = *u.AvatarURL
	}
	
	// Ensure file pointer is at beginning for image.Decode
	if _, err := src.Seek(0, 0); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to reset file pointer"})
	}
	
	// Debug: Check file size before decode
	size, _ := src.Seek(0, 2) // End position
	src.Seek(0, 0) // Reset to beginning
	
	// Attempt to decode and center-crop 95%
	var fname, path string
	var shouldProcess bool
	
	img, _, decErr := image.Decode(src)
	if decErr != nil {
		// For JPEG files that fail the final decode but passed validation,
		// try to save them anyway without processing
		if result.MIMEType == "image/jpeg" {
			// Skip processing and save the original file
			fname = uuid.New().String() + filepath.Ext(file.Filename)
			path = filepath.Join(avatarDir, fname)
			shouldProcess = false
		} else {
			// Provide more detailed error information for other formats
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": fmt.Sprintf("Failed to decode avatar image: %v (file size: %d bytes)", decErr, size)})
		}
	} else {
		// Normal processing path
		shouldProcess = true
		fname = uuid.New().String() + ".jpg"
		path = filepath.Join(avatarDir, fname)
	}
	
	if shouldProcess {
		b := img.Bounds()
		w := b.Dx()
		h := b.Dy()
		cropFactor := 0.95
		cw := int(float64(w) * cropFactor)
		ch := int(float64(h) * cropFactor)
		// Center crop rectangle
		x0 := b.Min.X + (w-cw)/2
		y0 := b.Min.Y + (h-ch)/2
		rect := image.Rect(x0, y0, x0+cw, y0+ch)
		var cropped image.Image
		if s, ok := img.(interface {
			SubImage(r image.Rectangle) image.Image
		}); ok {
			cropped = s.SubImage(rect)
		} else {
			dst := image.NewRGBA(image.Rect(0, 0, cw, ch))
			draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
			cropped = dst
		}
		out, err := os.Create(path)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save avatar"})
		}
		defer out.Close()
		if err := jpeg.Encode(out, cropped, &jpeg.Options{Quality: 90}); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to write avatar"})
		}
	} else {
		// For JPEG files that failed decoding but passed validation, save original
		if err := c.SaveFile(file, path); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save avatar file"})
		}
	}
	// Upload to storage (use current storage)
	data, _ := os.ReadFile(path)
	key := filepath.Join("avatars", filepath.Base(path))
	st := services.GetCurrentStorage()
	if st == nil {
		st = h.storage
	}
	if st == nil {
		st = services.NewLocalStorage("uploads")
	}
	publicURL := st.PublicURL(key)
	// Detect content type by extension
	var ct string = "image/jpeg"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		ct = "image/png"
	case ".webp":
		ct = "image/webp"
	case ".ico":
		ct = "image/x-icon"
	}
	if _, err := st.Save(c.Context(), key, bytes.NewReader(data), ct); err != nil {
		// fallback to local path
		publicURL = "/uploads/avatars/" + filepath.Base(path)
	}
	if _, err := h.userRepo.UpdateProfile(userID, models.UpdateUserRequest{AvatarURL: &publicURL}); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update profile"})
	}
	// Best-effort delete of previous avatar file if it was under /uploads/avatars/
	// Attempt to cleanup old avatar both locally and remote
	if oldAvatar != "" {
		// local cleanup
		if strings.HasPrefix(oldAvatar, "/uploads/avatars/") {
			oldPath := strings.TrimPrefix(oldAvatar, "/")
			_ = os.Remove(oldPath)
		} else {
			// assume last segment is file name
			parts := strings.Split(oldAvatar, "/")
			if len(parts) > 0 {
				_ = st.Delete(c.Context(), filepath.Join("avatars", parts[len(parts)-1]))
			}
		}
	}
	return c.JSON(fiber.Map{"avatar_url": publicURL})
}

func isValidAvatarType(contentType string) bool {
	valid := []string{"image/jpeg", "image/jpg", "image/png", "image/webp"}
	for _, v := range valid {
		if strings.EqualFold(contentType, v) {
			return true
		}
	}
	return false
}

// Admin endpoints (minimal, but complete)
func (h *UserHandler) AdminListUsers(c *fiber.Ctx) error {
	// Authorization check
	if !(isAdmin(c, h.userRepo) || isModerator(c, h.userRepo)) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	// Allow configurable page size with sane bounds
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if limit < 1 {
		limit = 1
	} else if limit > 200 {
		limit = 200
	}
	q := strings.TrimSpace(c.Query("q", ""))
	var (
		users []models.User
		total int
		err   error
	)
	if q != "" {
		users, total, err = h.userRepo.SearchUsers(q, page, limit)
	} else {
		users, total, err = h.userRepo.ListUsers(page, limit)
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list users"})
	}
	resp := make([]models.UserResponse, len(users))
	for i := range users {
		resp[i] = users[i].ToResponse()
	}
	totalPages := (total + limit - 1) / limit
	return c.JSON(fiber.Map{"users": resp, "page": page, "limit": limit, "total": total, "total_pages": totalPages})
}

func (h *UserHandler) AdminSetUserFlags(c *fiber.Ctx) error {
	isAdminUser := isAdmin(c, h.userRepo)
	isModUser := isModerator(c, h.userRepo)
	if !isAdminUser && !isModUser {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	userIDParam := c.Params("id")
	uid, err := uuid.Parse(userIDParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user id"})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	target, err := h.userRepo.GetByID(ctx, uid)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}
	type body struct {
		IsAdmin     *bool `json:"is_admin"`
		IsDisabled  *bool `json:"is_disabled"`
		IsModerator *bool `json:"is_moderator"`
	}
	var b body
	if err := c.BodyParser(&b); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	// Protect default admin from being demoted
	if b.IsAdmin != nil && !*b.IsAdmin {
		if target.Email != "" && strings.EqualFold(target.Email, os.Getenv("ADMIN_EMAIL")) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Default admin cannot be demoted"})
		}
	}
	// Mods may only toggle moderator status
	if isModUser && !isAdminUser {
		if b.IsModerator == nil || b.IsAdmin != nil || b.IsDisabled != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Moderators can only toggle moderator status"})
		}
	}
	if b.IsAdmin != nil {
		if !isAdminUser {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Only admins can change admin flag"})
		}
		if err := h.userRepo.SetAdmin(uid, *b.IsAdmin); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to set admin"})
		}
	}
	if b.IsDisabled != nil {
		if !isAdminUser {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Only admins can disable users"})
		}
		if err := h.userRepo.SetDisabled(uid, *b.IsDisabled); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to set disabled"})
		}
	}
	if b.IsModerator != nil {
		if err := h.userRepo.SetModerator(uid, *b.IsModerator); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to set moderator"})
		}
	}
	u, _ := h.userRepo.GetByID(ctx, uid)
	return c.JSON(fiber.Map{"user": u.ToResponse()})
}

// AdminCreateUser: admin only
func (h *UserHandler) AdminCreateUser(c *fiber.Ctx) error {
	if !isAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	var req struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		Password    string `json:"password"`
		IsModerator bool   `json:"is_moderator"`
	}
	if err := c.BodyParser(&req); err != nil || req.Username == "" || req.Email == "" || req.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	// Normalize
	req.Username = normalizeUsername(req.Username)
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	// Reserved usernames
	if isReservedUsername(req.Username) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "That username is reserved"})
	}

	// Validate using existing model rules
	if err := h.validator.Struct(models.CreateUserRequest{Username: req.Username, Email: req.Email, Password: req.Password}); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Validation failed", "details": err.Error()})
	}

	if err := services.ValidatePassword(req.Password); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	if _, err := h.userRepo.GetByEmail(ctx, req.Email); err == nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Email already in use"})
	}
	// Username conflict check (graceful instead of relying on DB constraint)
	if _, err := h.userRepo.GetByUsername(ctx, req.Username); err == nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Username already taken"})
	}

	u := &models.User{Username: req.Username, Email: req.Email}
	if err := u.HashPassword(req.Password); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to hash password"})
	}
	if err := h.userRepo.Create(u); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create user"})
	}
	_ = h.userRepo.SetModerator(u.ID, req.IsModerator)
	ctx, cancel = context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	u2, _ := h.userRepo.GetByID(ctx, u.ID)
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"user": u2.ToResponse()})
}

// AdminDeleteUser: admin only
func (h *UserHandler) AdminDeleteUser(c *fiber.Ctx) error {
	if !isAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	uid, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user id"})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	target, err := h.userRepo.GetByID(ctx, uid)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}
	if target.Email != "" && strings.EqualFold(target.Email, os.Getenv("ADMIN_EMAIL")) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Default admin cannot be deleted"})
	}
	if err := h.userRepo.DeleteUser(uid); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete user"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// AdminSetUserPassword: admin only
func (h *UserHandler) AdminSetUserPassword(c *fiber.Ctx) error {
	if !isAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	uid, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user id"})
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil || body.Password == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid password"})
	}
	if err := services.ValidatePassword(body.Password); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	u, err := h.userRepo.GetByID(ctx, uid)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}
	if err := u.HashPassword(body.Password); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to hash password"})
	}
	if err := h.userRepo.UpdatePassword(uid, u.PasswordHash); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update password"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandler) AdminDeleteImage(c *fiber.Ctx) error {
	if !isAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	imgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid image id"})
	}
	if err := h.imageRepo.Delete(imgID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete image"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandler) AdminSetImageNSFW(c *fiber.Ctx) error {
	if !isAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	imgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid image id"})
	}
	type body struct {
		IsNSFW bool `json:"is_nsfw"`
	}
	var b body
	if err := c.BodyParser(&b); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	if err := h.imageRepo.SetNSFW(imgID, b.IsNSFW); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update image"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *UserHandler) AdminSendVerification(c *fiber.Ctx) error {
	if !isAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid id"})
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	u, err := h.userRepo.GetByID(ctx, id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "User not found"})
	}
	setRepo := models.NewSiteSettingsRepository(models.DB())
	set, _ := setRepo.Get()
	if !(set.SMTPHost != "" && set.SMTPPort > 0 && set.SMTPUsername != "" && set.SMTPPassword != "") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP not configured"})
	}
	last, _ := models.LastVerificationSentAt(id)
	if time.Since(last) < 5*time.Minute {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "Please wait before sending again"})
	}
	token := uuid.New().String()
	exp := time.Now().Add(24 * time.Hour)
	if err := models.CreateEmailVerification(id, services.HashToken(token), exp); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed"})
	}
	link := strings.TrimRight(set.SiteURL, "/") + "/verify?token=" + token
	subj, bodyTxt := services.BuildVerificationEmail(set.SiteName, set.SiteURL, link)
	// Use async queue only to avoid duplicates
	services.EnqueueMail(u.Email, subj, bodyTxt)
	return c.SendStatus(fiber.StatusNoContent)
}

func isAdmin(c *fiber.Ctx, repo models.UserRepositoryInterface) bool {
	uid := middleware.GetUserID(c)
	if uid == uuid.Nil {
		return false
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	u, err := repo.GetByID(ctx, uid)
	if err != nil {
		return false
	}
	return u.IsAdmin && !u.IsDisabled
}

func isModerator(c *fiber.Ctx, repo models.UserRepositoryInterface) bool {
	uid := middleware.GetUserID(c)
	if uid == uuid.Nil {
		return false
	}
	ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
	defer cancel()
	u, err := repo.GetByID(ctx, uid)
	if err != nil {
		return false
	}
	return (u.IsModerator || u.IsAdmin) && !u.IsDisabled
}
