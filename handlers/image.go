package handlers

import (
	"encoding/json"
	"fmt"
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
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "Authentication required",
		})
	}

	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "No image file provided",
		})
	}

	if !h.isValidImageType(file.Header.Get("Content-Type")) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid image format. Supported: JPEG, PNG, WebP",
		})
	}

	if file.Size > 10*1024*1024 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "File too large. Maximum size: 10MB",
		})
	}

	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to open uploaded file",
		})
	}
	defer src.Close()

	imageMeta, err := services.ProcessImage(src)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Failed to process image",
		})
	}

	filename := fmt.Sprintf("%s%s", uuid.New().String(), filepath.Ext(file.Filename))
	uploadPath := filepath.Join("uploads", filename)

	if err := os.MkdirAll("uploads", 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to create upload directory",
		})
	}

	src.Seek(0, 0)
	dst, err := os.Create(uploadPath)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to save file",
		})
	}
	defer dst.Close()

	if _, err = io.Copy(dst, src); err != nil {
		os.Remove(uploadPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to save file",
		})
	}

	isAI, aiSignature := services.VerifyAIImage(uploadPath, h.config)

	var exifData json.RawMessage
	if len(aiSignature) > 0 {
		data := map[string]interface{}{
			"ai_detected": true,
			"signature":   aiSignature,
		}
		exifData, _ = json.Marshal(data)
	}

	originalName := file.Filename
	fileSize := int(file.Size)

	image := &models.Image{
		UserID:        userID,
		Filename:      filename,
		OriginalName:  &originalName,
		FileSize:      &fileSize,
		Width:         &imageMeta.Width,
		Height:        &imageMeta.Height,
		Blurhash:      &imageMeta.Blurhash,
		DominantColor: &imageMeta.DominantColor,
		IsNSFW:        false,
		AISignature:   nil,
		ExifData:      exifData,
	}

	if isAI {
		image.AISignature = &aiSignature
	}

	if err := h.imageRepo.Create(image); err != nil {
		os.Remove(uploadPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to save image metadata",
		})
	}

	return c.Status(fiber.StatusCreated).JSON(image.ToUploadResponse())
}

func (h *ImageHandler) GetFeed(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	if page < 1 {
		page = 1
	}
	
	limit := 20
	showNSFW := false

	userID := middleware.GetUserID(c)
	if userID != uuid.Nil {
		user, err := h.userRepo.GetByID(userID)
		if err == nil {
			showNSFW = user.ShowNSFW
		}
	}

	images, total, err := h.imageRepo.GetFeed(page, limit, showNSFW)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to fetch images",
		})
	}

	return c.JSON(models.FeedResponse{
		Images: images,
		Page:   page,
		Total:  total,
	})
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