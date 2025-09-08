package handlers

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/png"
	"io"
	"mime/multipart"
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
	imageRepo    models.ImageRepositoryInterface
	likeRepo     models.LikeRepositoryInterface
	userRepo     models.UserRepositoryInterface
	config       services.Config
	storage      services.Storage
	collectRepo  models.CollectRepositoryInterface
	settingsRepo models.SiteSettingsRepositoryInterface
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

func (h *ImageHandler) WithCollect(r models.CollectRepositoryInterface) *ImageHandler {
	h.collectRepo = r
	return h
}

func (h *ImageHandler) WithSettings(r models.SiteSettingsRepositoryInterface) *ImageHandler {
	h.settingsRepo = r
	return h
}

func (h *ImageHandler) Upload(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Authentication required"})
	}
	// Gate uploads for unverified users when email verification is enabled
	if h.userRepo != nil {
		if u, err := h.userRepo.GetByID(userID); err == nil && u != nil {
			// Read settings via cache for performance; treat missing repo as disabled
			var requireVerify bool
			if h.settingsRepo != nil {
				set := services.GetCachedSettings(h.settingsRepo)
				requireVerify = set.RequireEmailVerification && set.SMTPHost != "" && set.SMTPPort > 0 && set.SMTPUsername != "" && set.SMTPPassword != ""
			}
			if requireVerify && !u.EmailVerified {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Email not verified. Verify your email to upload images."})
			}
		}
	}

	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "No image file provided"})
	}


	title := strings.TrimSpace(c.FormValue("title"))
	isNSFW := strings.ToLower(strings.TrimSpace(c.FormValue("is_nsfw"))) == "true"
	caption := strings.TrimSpace(c.FormValue("caption"))

	src, err := file.Open()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to open uploaded file"})
	}
	defer src.Close()

	// Use comprehensive file validation with streaming support
	fileValidator := services.NewFileValidator()
	
	// Validate file and get stream back for AI detection
	result, remainingStream, err := fileValidator.ValidateImageStream(file.Filename, src)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to validate file"})
	}
	
	if !result.IsValid {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": result.ErrorMessage})
	}
	
	// Use the remaining stream for AI detection (avoids re-reading)
	var streamReader io.Reader = remainingStream
	
	// Add security information to response context
	if result.SecurityLevel == "low" {
		// Log low security files for monitoring
		// TODO: Add security event logging here
	}

	// OPTIMIZED: Early format-based rejection for better performance
	// Some formats are very unlikely to contain AI metadata
	formatContentType := file.Header.Get("Content-Type")
	if strings.Contains(formatContentType, "bmp") || strings.Contains(formatContentType, "gif") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "BMP and GIF formats rarely contain AI metadata. Please use JPEG, PNG, or WebP."})
	}

	var aiSignature string
	var aiOK bool
	var aiRes services.AIDetectionResult
	var xmpOriginal []byte

	// OPTIMIZED: Stream-based AI detection to avoid full file buffering
	// For large files (>2MB), use streaming detection first
	var originalBytes []byte
	if file.Size > 2*1024*1024 { // 2MB threshold
		// For large files, use streaming AI detection first
		if ok, res := detectAIStreaming(streamReader.(multipart.File), file.Size); ok {
			aiSignature = res.Details
			goto ai_validated
		}
		// If streaming detection fails, buffer the file for full detection
		streamReader.(multipart.File).Seek(0, 0)
		if buf, err := io.ReadAll(streamReader); err == nil {
			originalBytes = buf
		} else {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to buffer upload"})
		}
	} else {
		// For small files, buffer immediately
		if buf, err := io.ReadAll(streamReader); err == nil {
			originalBytes = buf
		} else {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to buffer upload"})
		}
	}

	// FAST PATH: Quick AI detection first (rejects obvious non-AI immediately)
	if aiOK, aiRes = services.DetectAIFast(originalBytes); aiOK {
		aiSignature = aiRes.Details
		goto ai_validated
	}

	// FALLBACK: Full concurrent AI detection for edge cases
	xmpOriginal = services.ExtractXMPXMLFromBytes(originalBytes)
	aiOK, aiRes = services.DetectAIProvenanceConcurrent(originalBytes, xmpOriginal)
	if !aiOK {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Upload rejected. Only AI-generated images with verifiable metadata (EXIF or XMP; C2PA optional) are accepted."})
	}
	aiSignature = aiRes.Details

ai_validated:

	// Now decode image for processing (only if AI validation passed)
	if _, err := src.Seek(0, 0); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to read uploaded file"})
	}
	img, format, err := image.Decode(src)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Failed to decode image"})
	}
	// Compute meta from decoded image to avoid double decode
	imageMeta := services.ProcessDecodedImage(img, format)

	// Build final bytes. Preserve C2PA by keeping original bytes untouched when detected via C2PA.
	var finalBytes []byte
	var finalContentType string = "image/jpeg"
	var filename string
	originalExt := strings.ToLower(filepath.Ext(file.Filename))
	if aiRes.Method == "c2pa" {
		finalBytes = originalBytes
		// Preserve original extension and content type if supported
		switch originalExt {
		case ".jpg", ".jpeg":
			finalContentType = "image/jpeg"
		case ".png":
			finalContentType = "image/png"
		case ".webp":
			finalContentType = "image/webp"
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
				finalContentType = "image/png"
			case ".webp":
				finalContentType = "image/webp"
			case ".jpg", ".jpeg":
				finalContentType = "image/jpeg"
			default:
				finalContentType = "image/png"
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
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to encode image"})
			}
			finalBytes = out
			filename = uuid.New().String() + ".jpg"
			finalContentType = "image/jpeg"
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
	publicURL, err := st.Save(c.Context(), filename, bytes.NewReader(finalBytes), finalContentType)
	if err != nil {
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
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch images", "details": err.Error()})
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
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to fetch images", "details": err.Error()})
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

// LikeImage has been deprecated and is intentionally disabled
func (h *ImageHandler) LikeImage(c *fiber.Ctx) error {
	return c.Status(fiber.StatusGone).JSON(fiber.Map{"error": "Likes are no longer supported"})
}

// CollectImage allows a user to collect another user's image. Collecting own image is disallowed.
func (h *ImageHandler) CollectImage(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Authentication required"})
	}
	imageID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid image ID"})
	}
	img, err := h.imageRepo.GetByID(imageID)
	if err != nil || img == nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": "Image not found"})
	}
	// Disallow collecting own image
	if img.UserID == userID {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Cannot collect your own image"})
	}
	if h.collectRepo == nil {
		// Initialize lazily if not set (for legacy construction)
		h.collectRepo = models.NewCollectRepository(models.DB())
	}
	if existing, _ := h.collectRepo.GetByUser(userID, imageID); existing != nil {
		if err := h.collectRepo.Delete(userID, imageID); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to uncollect image"})
		}
		return c.JSON(fiber.Map{"collected": false})
	}
	if err := h.collectRepo.Create(userID, imageID); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to collect image"})
	}
	return c.JSON(fiber.Map{"collected": true})
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

// detectAIStreaming performs AI detection on large files without full buffering
// It reads strategic sections of the file to find AI markers
func detectAIStreaming(src multipart.File, fileSize int64) (bool, services.AIDetectionResult) {
	// Create a buffer for reading sections
	buf := make([]byte, 32*1024) // 32KB buffer

	// Strategy: Check multiple strategic sections of the file

	// 1. First check the beginning for PNG signature and early metadata
	src.Seek(0, 0)
	n, _ := src.Read(buf)
	if n > 0 {
		// Check if this is a PNG file first
		isPNG := false
		if n >= 8 {
			isPNG = buf[0] == 0x89 && buf[1] == 0x50 && buf[2] == 0x4E && buf[3] == 0x47 &&
				buf[4] == 0x0D && buf[5] == 0x0A && buf[6] == 0x1A && buf[7] == 0x0A
		}

		var scanStart int
		if isPNG {
			// For PNG files, skip only the 8-byte signature
			scanStart = 8
		} else {
			// For other files, skip headers to avoid false positives
			scanStart = min(1000, n)
		}

		if ok, res := services.DetectAIFast(buf[scanStart:n]); ok {
			return ok, res
		}
	}

	// 2. Check middle section for embedded metadata
	middlePos := fileSize / 2
	src.Seek(middlePos, 0)
	n, _ = src.Read(buf)
	if n > 0 {
		// Skip binary headers in this section too
		scanStart := min(1000, n)
		if ok, res := services.DetectAIFast(buf[scanStart:n]); ok {
			return ok, res
		}
	}

	// 3. Check end section (often contains metadata in some formats)
	endPos := fileSize - int64(len(buf))
	if endPos > 0 {
		src.Seek(endPos, 0)
		n, _ = src.Read(buf)
		if n > 0 {
			if ok, res := services.DetectAIFast(buf[:n]); ok {
				return ok, res
			}
		}
	}

	return false, services.AIDetectionResult{}
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
