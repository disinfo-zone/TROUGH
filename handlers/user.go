package handlers

import (
	"database/sql"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"image/jpeg"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yourusername/trough/middleware"
	"github.com/yourusername/trough/models"
)

type UserHandler struct {
	userRepo  models.UserRepositoryInterface
	imageRepo models.ImageRepositoryInterface
}

func NewUserHandler(userRepo models.UserRepositoryInterface, imageRepo models.ImageRepositoryInterface) *UserHandler {
	return &UserHandler{
		userRepo:  userRepo,
		imageRepo: imageRepo,
	}
}

func (h *UserHandler) GetProfile(c *fiber.Ctx) error {
	username := c.Params("username")
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Username required",
		})
	}

	user, err := h.userRepo.GetByUsername(username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	return c.JSON(user.ToResponse())
}

func (h *UserHandler) GetUserImages(c *fiber.Ctx) error {
	username := c.Params("username")
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Username required",
		})
	}

	user, err := h.userRepo.GetByUsername(username)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "User not found",
		})
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}

	limit := 20

	images, total, err := h.imageRepo.GetUserImages(user.ID, page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch user images",
		})
	}

	return c.JSON(models.FeedResponse{
		Images: images,
		Page:   page,
		Total:  total,
	})
}

func (h *UserHandler) GetMyProfile(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	user, err := h.userRepo.GetByID(userID)
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

	// If changing username, ensure it is not taken and not reserved
	if req.Username != nil {
		uname := strings.ToLower(strings.TrimSpace(*req.Username))
		if uname == "" || len(uname) < 3 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Username too short"})
		}
		reserved := map[string]bool{"admin": true, "root": true, "system": true, "support": true, "moderator": true, "owner": true, "undefined": true, "null": true}
		if reserved[uname] {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "That username is reserved"})
		}
		if existing, err := h.userRepo.GetByUsername(uname); err == nil && existing != nil {
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
	// Conflict check
	if existing, err := h.userRepo.GetByEmail(body.Email); err == nil && existing != nil {
		if existing.ID != userID {
			return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Email already in use"})
		}
	}
	if err := h.userRepo.UpdateEmail(userID, body.Email); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update email"})
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
	if err := c.BodyParser(&body); err != nil || body.NewPassword == "" || len(body.NewPassword) < 6 || body.CurrentPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid password"})
	}
	user, err := h.userRepo.GetByID(userID)
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
	user, err := h.userRepo.GetByID(userID)
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
	ct := file.Header.Get("Content-Type")
	if !isValidAvatarType(ct) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid image format. Supported: JPEG, PNG, WebP"})
	}
	if file.Size > 5*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "File too large. Max 5MB"})
	}
	// Ensure directory
	avatarDir := filepath.Join("uploads", "avatars")
	if err := os.MkdirAll(avatarDir, 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create avatar directory"})
	}
	// Attempt to decode and center-crop 95%
	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to open avatar"})
	}
	defer src.Close()
	img, _, decErr := image.Decode(src)
	fname := uuid.New().String() + ".jpg"
	path := filepath.Join(avatarDir, fname)
	if decErr == nil {
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
			// Fallback copy
			dst := image.NewRGBA(image.Rect(0, 0, cw, ch))
			draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
			cropped = dst
		}
		// Save as JPEG
		out, err := os.Create(path)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save avatar"})
		}
		defer out.Close()
		if err := jpeg.Encode(out, cropped, &jpeg.Options{Quality: 90}); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to write avatar"})
		}
	} else {
		// Fallback: save original (e.g., webp)
		path = filepath.Join(avatarDir, uuid.New().String()+filepath.Ext(file.Filename))
		if err := c.SaveFile(file, path); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save avatar"})
		}
		// Normalize URL to this saved path
	}
	url := "/uploads/avatars/" + filepath.Base(path)
	if _, err := h.userRepo.UpdateProfile(userID, models.UpdateUserRequest{AvatarURL: &url}); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update profile"})
	}
	return c.JSON(fiber.Map{"avatar_url": url})
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
	if !isAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	limit := 50
	users, total, err := h.userRepo.ListUsers(page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to list users"})
	}
	// Map to responses
	resp := make([]models.UserResponse, len(users))
	for i := range users {
		resp[i] = users[i].ToResponse()
	}
	return c.JSON(fiber.Map{"users": resp, "page": page, "total": total})
}

func (h *UserHandler) AdminSetUserFlags(c *fiber.Ctx) error {
	if !isAdmin(c, h.userRepo) {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	userIDParam := c.Params("id")
	uid, err := uuid.Parse(userIDParam)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid user id"})
	}
	type body struct {
		IsAdmin    *bool `json:"is_admin"`
		IsDisabled *bool `json:"is_disabled"`
	}
	var b body
	if err := c.BodyParser(&b); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	if b.IsAdmin != nil {
		if err := h.userRepo.SetAdmin(uid, *b.IsAdmin); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to set admin"})
		}
	}
	if b.IsDisabled != nil {
		if err := h.userRepo.SetDisabled(uid, *b.IsDisabled); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to set disabled"})
		}
	}
	u, _ := h.userRepo.GetByID(uid)
	return c.JSON(fiber.Map{"user": u.ToResponse()})
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

func isAdmin(c *fiber.Ctx, repo models.UserRepositoryInterface) bool {
	uid := middleware.GetUserID(c)
	if uid == uuid.Nil {
		return false
	}
	u, err := repo.GetByID(uid)
	if err != nil {
		return false
	}
	return u.IsAdmin && !u.IsDisabled
}
