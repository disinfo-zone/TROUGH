package handlers

import (
	"bytes"
	"encoding/json"
	"image"
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

// extractStorageKey extracts the storage key from either a filename or full URL
func extractStorageKey(filenameOrURL string) string {
	if filenameOrURL == "" {
		return ""
	}
	// If it's a full URL, extract the filename part
	if strings.HasPrefix(filenameOrURL, "http://") || strings.HasPrefix(filenameOrURL, "https://") {
		parts := strings.Split(filenameOrURL, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1] // Return the last part (filename)
		}
	}
	// If it's a domain-based URL without protocol (like z.disinfo.zone/file.jpg)
	if strings.Contains(filenameOrURL, "/") && strings.Contains(filenameOrURL, ".") && !strings.HasPrefix(filenameOrURL, "/") {
		parts := strings.Split(filenameOrURL, "/")
		if len(parts) > 0 {
			return parts[len(parts)-1] // Return the last part (filename)
		}
	}
	// If it's just a filename, return as-is
	return filenameOrURL
}

type ImageHandler struct {
	imageRepo models.ImageRepositoryInterface
	likeRepo  models.LikeRepositoryInterface
	userRepo  models.UserRepositoryInterface
	config    services.Config
	storage   services.Storage
}

func NewImageHandler(imageRepo models.ImageRepositoryInterface, likeRepo models.LikeRepositoryInterface, userRepo models.UserRepositoryInterface, config services.Config, storage services.Storage) *ImageHandler {
	return &ImageHandler{
		imageRepo: imageRepo,
		likeRepo:  likeRepo,
		userRepo:  userRepo,
		config:    config,
		storage:   storage,
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

	// Quick dimension guard using DecodeConfig to avoid decompressing huge images
	if _, err := src.Seek(0, 0); err == nil {
		if cfg, _, err := image.DecodeConfig(src); err == nil {
			maxDim := 20000 // hard safety guard against decompression bombs
			maxPixels := 100 * 1024 * 1024
			if cfg.Width > maxDim || cfg.Height > maxDim || (cfg.Width > 0 && cfg.Height > 0 && cfg.Width*cfg.Height > maxPixels) {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Image dimensions too large"})
			}
		}
	}
	// Decode image once (register webp via blank import)
	if _, err := src.Seek(0, 0); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to read uploaded file"})
	}
	img, format, err := image.Decode(src)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to decode image"})
	}
	// Compute meta from decoded image to avoid double decode
	imageMeta := services.ProcessDecodedImage(img, format)

	// Write out as JPEG with XMP preserved (if present)
	tmpPath := filepath.Join("uploads", uuid.New().String()+filepath.Ext(file.Filename))
	if err := os.MkdirAll("uploads", 0755); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create upload directory"})
	}
	// Save original bytes to temp to scan for XMP/EXIF and for possible re-encode source
	src.Seek(0, 0)
	var originalBytes []byte
	if buf, err := io.ReadAll(src); err == nil {
		originalBytes = buf
	} else {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to buffer upload"})
	}
	if err := os.WriteFile(tmpPath, originalBytes, 0o644); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to save temp file"})
	}

	// AI provenance detection on the original file. Reject early if none.
	xmpOriginal := services.ExtractXMPXMLFromBytes(originalBytes)
	aiOK, aiRes := services.DetectAIProvenanceFromBytes(originalBytes, xmpOriginal)
	if !aiOK {
		os.Remove(tmpPath)
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Upload rejected. Only AI-generated images with verifiable metadata (EXIF or XMP; C2PA optional) are accepted."})
	}
	aiSignature := aiRes.Details

	// Build final bytes. Preserve C2PA by keeping original bytes untouched when detected via C2PA.
	var finalBytes []byte
	var contentType string = "image/jpeg"
	var filename string
	originalExt := strings.ToLower(filepath.Ext(file.Filename))
	if aiRes.Method == "c2pa" {
		finalBytes = originalBytes
		// Preserve original extension and content type if supported
		switch originalExt {
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".png":
			contentType = "image/png"
		case ".webp":
			contentType = "image/webp"
		}
		if originalExt == "" {
			originalExt = ".jpg"
		}
		filename = uuid.New().String() + originalExt
	} else {
		// If the image has transparency, preserve the original bytes to keep alpha and any metadata intact.
		// This avoids flattening artifacts and respects original authoring.
		if !services.IsOpaque(img) {
			finalBytes = originalBytes
			switch originalExt {
			case ".png":
				contentType = "image/png"
			case ".webp":
				contentType = "image/webp"
			case ".jpg", ".jpeg":
				contentType = "image/jpeg"
			default:
				contentType = "image/png"
			}
			if originalExt == "" {
				originalExt = ".png"
			}
			filename = uuid.New().String() + originalExt
		} else {
			// Opaque images: optionally resize (disabled by default via config), adaptive quality, and inject EXIF/XMP.
			resized := img
			if h.config.Aesthetic.MaxWidth > 0 {
				resized = services.ResizeIfNeeded(img, h.config.Aesthetic.MaxWidth)
			}
			// Ensure DB width/height reflect the stored master
			rb := resized.Bounds()
			imageMeta.Width = rb.Dx()
			imageMeta.Height = rb.Dy()
			// Complexity score to choose quality bucket
			complexity := services.EstimateComplexity(resized)
			quality := 82
			if complexity < 0.5 {
				quality = 78
			} else if complexity > 1.5 {
				quality = 86
			}
			// Extract raw EXIF to reattach if available
			exifRaw := services.ExtractExifRawFromBytes(originalBytes)
			out, err := services.EncodeJPEGWithMetadata(resized, quality, xmpOriginal, exifRaw)
			if err != nil {
				os.Remove(tmpPath)
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to encode image"})
			}
			finalBytes = out
			filename = uuid.New().String() + ".jpg"
			contentType = "image/jpeg"
		}
	}
	// Save to storage (local or remote) under top-level key = filename
	st := services.GetCurrentStorage()
	if st == nil {
		st = h.storage
	}
	if st == nil {
		st = services.NewLocalStorage("uploads")
	}
	publicURL, err := st.Save(c.Context(), filename, bytes.NewReader(finalBytes), contentType)
	if err != nil {
		os.Remove(tmpPath)
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to store image"})
	}

	// For local storage, ensure the public URL is just the filename for backward compatibility
	// For remote storage, use the full public URL
	var filenameOrURL string
	if st.IsLocal() {
		filenameOrURL = filename
	} else {
		filenameOrURL = publicURL
	}
	// Remove tmp original
	os.Remove(tmpPath)

	// Extract EXIF JSON from the final file (after any re-encode)
	var exifFull json.RawMessage
	if len(finalBytes) > 0 {
		exifFull = services.ExtractExifJSONFromBytes(finalBytes)
	} else {
		exifFull = services.ExtractExifJSONFromBytes(originalBytes)
	}

	var exifData json.RawMessage
	// Prepare EXIF data payload
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
	fileSize := len(finalBytes)

	imageModel := &models.Image{
		UserID:        userID,
		Filename:      filenameOrURL, // Store either filename (local) or full URL (remote)
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
	// Mark AI provenance
	imageModel.AISignature = &aiSignature
	if aiRes.Provider != "" {
		imageModel.AIProvider = &aiRes.Provider
	}
	if title != "" {
		imageModel.OriginalName = &title
	}
	if caption != "" {
		imageModel.Caption = &caption
	}

	if err := h.imageRepo.Create(imageModel); err != nil {
		_ = st.Delete(c.Context(), filename) // Use original filename for cleanup
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
	if lq := strings.TrimSpace(c.Query("limit", "")); lq != "" {
		if v, err := strconv.Atoi(lq); err == nil && v > 0 && v <= 100 {
			limit = v
		}
	}

	// Determine NSFW visibility based on user pref
	showNSFW := false
	uid := middleware.OptionalUserID(c)
	if uid != uuid.Nil {
		if user, err := h.userRepo.GetByID(uid); err == nil {
			showNSFW = user.ShowNSFW || strings.ToLower(strings.TrimSpace(user.NsfwPref)) != "hide"
		}
	}

	// Prefer seek-based when cursor is provided; optional totals only when asked and on first page/no cursor
	cursor := strings.TrimSpace(c.Query("cursor", ""))
	if cursor != "" {
		images, next, err := h.imageRepo.GetFeedSeek(limit, showNSFW, cursor)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch images"})
		}
		return c.JSON(models.FeedResponse{Images: images, NextCursor: next})
	}
	// Optional totals flag
	includeTotal := strings.EqualFold(strings.TrimSpace(c.Query("include_total", "")), "true")
	if includeTotal && page == 1 {
		images, _, err := h.imageRepo.GetFeedSeek(limit, showNSFW, "")
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch images"})
		}
		total, _ := h.imageRepo.CountFeed(showNSFW)
		return c.JSON(models.FeedResponse{Images: images, Page: 1, Total: total, NextCursor: func() string {
			if len(images) > 0 {
				last := images[len(images)-1]
				return models.EncodeCursor(last.CreatedAt, last.ID)
			}
			return ""
		}()})
	}
	// Backward-compatible page/offset fallback
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
	// Remove file from storage first; if it's already gone, continue
	if img.Filename != "" {
		st := services.GetCurrentStorage()
		if st == nil {
			st = h.storage
		}
		if st == nil {
			st = services.NewLocalStorage("uploads")
		}
		// Extract the actual storage key from filename (which might be a full URL)
		storageKey := extractStorageKey(img.Filename)
		if remErr := st.Delete(c.Context(), storageKey); remErr != nil {
			// best-effort; ignore not found
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
