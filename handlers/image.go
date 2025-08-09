package handlers

import (
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yourusername/trough/middleware"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

type ImageHandler struct {
	imageRepo models.ImageRepositoryInterface
	likeRepo  models.LikeRepositoryInterface
	userRepo  models.UserRepositoryInterface
	config    services.Config
}

func NewImageHandler(imageRepo models.ImageRepositoryInterface, likeRepo models.LikeRepositoryInterface, userRepo models.UserRepositoryInterface, config services.Config) *ImageHandler {
	return &ImageHandler{
		imageRepo: imageRepo,
		likeRepo:  likeRepo,
		userRepo:  userRepo,
		config:    config,
	}
}

func (h *ImageHandler) Upload(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Authentication required"})
	}

	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No image file provided"})
	}

	title := strings.TrimSpace(c.FormValue("title"))
	isNSFW := strings.ToLower(strings.TrimSpace(c.FormValue("is_nsfw"))) == "true"
	caption := strings.TrimSpace(c.FormValue("caption"))

	if !h.isValidImageType(file.Header.Get("Content-Type")) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid image format. Supported: JPEG, PNG, WebP"})
	}
	if file.Size > 10*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "File too large. Maximum size: 10MB"})
	}

	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to open uploaded file"})
	}
	defer src.Close()

	// Decode image for processing
	img, _, err := image.Decode(src)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to decode image"})
	}
	// Compute meta
	// Rewind for ProcessImage which also decodes.
	src.Seek(0, 0)
	imageMeta, err := services.ProcessImage(src)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to process image"})
	}

	// Write out as JPEG with XMP preserved (if present)
	tmpPath := filepath.Join("uploads", uuid.New().String()+filepath.Ext(file.Filename))
	if err := os.MkdirAll("uploads", 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create upload directory"})
	}
	// Save original bytes to temp to scan for XMP/EXIF
	src.Seek(0, 0)
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save temp file"})
	}
	if _, err = io.Copy(tmpFile, src); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save temp file"})
	}
	tmpFile.Close()

	xmp := services.ExtractXMPXML(tmpPath)
	filename := uuid.New().String() + ".jpg"
	uploadPath := filepath.Join("uploads", filename)
	if err := services.WriteJPEGWithXMP(img, 90, uploadPath, xmp); err != nil {
		os.Remove(tmpPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save image"})
	}
	os.Remove(tmpPath)

	// Verify AI and extract EXIF JSON from the final file
	isAI, aiSignature := services.VerifyAIImage(uploadPath, h.config)
	exifFull := services.ExtractExifJSON(uploadPath)

	var exifData json.RawMessage
	if len(aiSignature) > 0 {
		data := map[string]interface{}{
			"ai_detected": true,
			"signature":   aiSignature,
			"exif":        json.RawMessage(exifFull),
		}
		exifData, _ = json.Marshal(data)
	} else {
		exifData = exifFull
		if len(exifData) == 0 {
			exifData = json.RawMessage("null")
		}
	}

	originalName := file.Filename
	fileSize := int(file.Size)

	imageModel := &models.Image{
		UserID:        userID,
		Filename:      filename,
		OriginalName:  &originalName,
		FileSize:      &fileSize,
		Width:         &imageMeta.Width,
		Height:        &imageMeta.Height,
		Blurhash:      &imageMeta.Blurhash,
		DominantColor: &imageMeta.DominantColor,
		IsNSFW:        isNSFW,
		AISignature:   nil,
		ExifData:      exifData,
	}
	if isAI {
		imageModel.AISignature = &aiSignature
	}
	if title != "" {
		imageModel.OriginalName = &title
	}
	if caption != "" {
		imageModel.Caption = &caption
	}

	if err := h.imageRepo.Create(imageModel); err != nil {
		os.Remove(uploadPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save image metadata"})
	}

	return c.Status(fiber.StatusCreated).JSON(imageModel.ToUploadResponse())
}

func (h *ImageHandler) GetFeed(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	limit := 20

	// Determine NSFW visibility based on user pref
	showNSFW := false
	uid := middleware.OptionalUserID(c)
	if uid != uuid.Nil {
		if user, err := h.userRepo.GetByID(uid); err == nil {
			showNSFW = user.ShowNSFW || strings.ToLower(strings.TrimSpace(user.NsfwPref)) != "hide"
		}
	}

	images, total, err := h.imageRepo.GetFeed(page, limit, showNSFW)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch images"})
	}
	return c.JSON(models.FeedResponse{Images: images, Page: page, Total: total})
}

func (h *ImageHandler) GetImage(c *fiber.Ctx) error {
	imageID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid image ID",
		})
	}

	image, err := h.imageRepo.GetByID(imageID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "Image not found",
		})
	}

	return c.JSON(image)
}

func (h *ImageHandler) LikeImage(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Authentication required",
		})
	}

	imageID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid image ID",
		})
	}

	existingLike, _ := h.likeRepo.GetByUser(userID, imageID)
	if existingLike != nil {
		if err := h.likeRepo.Delete(userID, imageID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to unlike image",
			})
		}
		return c.JSON(fiber.Map{
			"liked": false,
		})
	}

	if err := h.likeRepo.Create(userID, imageID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to like image",
		})
	}

	return c.JSON(fiber.Map{
		"liked": true,
	})
}

func (h *ImageHandler) UpdateImage(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	imgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid image id"})
	}
	img, err := h.imageRepo.GetByID(imgID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Image not found"})
	}
	// Owner, admin, or moderator
	isOwner := img.UserID == userID
	isPrivileged := false
	if !isOwner {
		u, err := h.userRepo.GetByID(userID)
		if err == nil {
			isPrivileged = (u.IsAdmin || u.IsModerator) && !u.IsDisabled
		}
	}
	if !isOwner && !isPrivileged {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	type body struct {
		Title   *string `json:"title"`
		Caption *string `json:"caption"`
		IsNSFW  *bool   `json:"is_nsfw"`
	}
	var b body
	if err := c.BodyParser(&b); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid body"})
	}
	// Map Title to OriginalName for now; trim and validate lengths
	if b.Title != nil {
		s := strings.TrimSpace(*b.Title)
		if len(s) > 120 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Title too long (max 120 characters)"})
		}
		b.Title = &s
	}
	if b.Caption != nil {
		s := strings.TrimSpace(*b.Caption)
		if len(s) > 2000 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Caption too long (max 2000 characters)"})
		}
		b.Caption = &s
	}
	if err := h.imageRepo.UpdateMeta(imgID, b.Title, b.Caption, b.IsNSFW); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to update image"})
	}
	updated, _ := h.imageRepo.GetByID(imgID)
	return c.JSON(updated)
}

func (h *ImageHandler) DeleteImage(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	imgID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid image id"})
	}
	img, err := h.imageRepo.GetByID(imgID)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Image not found"})
	}
	isOwner := img.UserID == userID
	isPrivileged := false
	u, err := h.userRepo.GetByID(userID)
	if err == nil {
		isPrivileged = (u.IsAdmin || u.IsModerator) && !u.IsDisabled
	}
	if !isOwner && !isPrivileged {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Forbidden"})
	}
	// Remove file from disk first; if it's already gone, continue
	if img.Filename != "" {
		path := filepath.Join("uploads", img.Filename)
		if remErr := os.Remove(path); remErr != nil && !errors.Is(remErr, os.ErrNotExist) {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to remove image file"})
		}
	}
	if err := h.imageRepo.Delete(imgID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to delete image"})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *ImageHandler) isValidImageType(contentType string) bool {
	validTypes := []string{
		"image/jpeg",
		"image/jpg",
		"image/png",
		"image/webp",
	}

	for _, validType := range validTypes {
		if strings.EqualFold(contentType, validType) {
			return true
		}
	}
	return false
}
