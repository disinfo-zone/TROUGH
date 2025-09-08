package services

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// FileValidator handles comprehensive file validation
type FileValidator struct {
	AllowedExtensions []string
	AllowedMIMETypes  []string
	MaxFileSize       int64
}

// NewFileValidator creates a new file validator
func NewFileValidator() *FileValidator {
	return &FileValidator{
		AllowedExtensions: []string{".jpg", ".jpeg", ".png", ".webp"},
		AllowedMIMETypes:  []string{"image/jpeg", "image/png", "image/webp"},
		MaxFileSize:       50 * 1024 * 1024, // 50MB
	}
}

// ValidationResult contains the results of file validation
type ValidationResult struct {
	IsValid      bool
	Extension    string
	MIMEType     string
	Size         int64
	ErrorMessage string
}

// ValidateFile performs comprehensive file validation
func (fv *FileValidator) ValidateFile(filename string, file io.Reader) (*ValidationResult, error) {
	result := &ValidationResult{
		Extension: strings.ToLower(filepath.Ext(filename)),
		Size:      0,
	}
	
	// Step 1: Validate extension
	if !fv.isValidExtension(result.Extension) {
		result.ErrorMessage = fmt.Sprintf("Invalid file extension: %s", result.Extension)
		return result, nil
	}
	
	// Step 2: Read first 512 bytes for MIME type detection and magic byte validation
	buffer := make([]byte, 512)
	n, err := file.Read(buffer)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	
	// Create a new reader that combines the buffer with the rest of the file
	multiReader := io.MultiReader(bytes.NewReader(buffer[:n]), file)
	
	// Step 3: Detect MIME type
	mimeType := http.DetectContentType(buffer[:n])
	result.MIMEType = mimeType
	
	if !fv.isValidMIMEType(mimeType) {
		result.ErrorMessage = fmt.Sprintf("Invalid MIME type: %s", mimeType)
		return result, nil
	}
	
	// Step 4: Validate magic bytes
	if !fv.isValidMagicBytes(buffer[:n], result.Extension, mimeType) {
		result.ErrorMessage = "File signature does not match declared type"
		return result, nil
	}
	
	// Step 5: Validate extension matches MIME type
	if !fv.extensionMatchesMIME(result.Extension, mimeType) {
		result.ErrorMessage = fmt.Sprintf("Extension %s does not match MIME type %s", result.Extension, mimeType)
		return result, nil
	}
	
	// Step 6: Check file size by reading the rest
	if n == 512 {
		// Read the rest to get total size
		restBuffer := make([]byte, fv.MaxFileSize)
		restSize, err := multiReader.Read(restBuffer)
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("failed to read complete file: %w", err)
		}
		result.Size = int64(n) + int64(restSize)
	} else {
		result.Size = int64(n)
	}
	
	if result.Size > fv.MaxFileSize {
		result.ErrorMessage = fmt.Sprintf("File size %d exceeds maximum allowed size %d", result.Size, fv.MaxFileSize)
		return result, nil
	}
	
	result.IsValid = true
	return result, nil
}

// isValidExtension checks if the extension is allowed
func (fv *FileValidator) isValidExtension(ext string) bool {
	for _, allowed := range fv.AllowedExtensions {
		if strings.EqualFold(ext, allowed) {
			return true
		}
	}
	return false
}

// isValidMIMEType checks if the MIME type is allowed
func (fv *FileValidator) isValidMIMEType(mimeType string) bool {
	for _, allowed := range fv.AllowedMIMETypes {
		if strings.EqualFold(mimeType, allowed) {
			return true
		}
	}
	return false
}

// isValidMagicBytes validates file magic bytes
func (fv *FileValidator) isValidMagicBytes(data []byte, ext, mimeType string) bool {
	if len(data) < 12 {
		return false
	}
	
	switch ext {
	case ".jpg", ".jpeg":
		return len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
	case ".png":
		return len(data) >= 8 && 
			data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
			data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A
	case ".webp":
		return len(data) >= 12 && 
			data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 && // "RIFF"
			data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50  // "WEBP"
	}
	
	// Fallback to MIME type validation
	switch mimeType {
	case "image/jpeg":
		return len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF
	case "image/png":
		return len(data) >= 8 && 
			data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 &&
			data[4] == 0x0D && data[5] == 0x0A && data[6] == 0x1A && data[7] == 0x0A
	case "image/webp":
		return len(data) >= 12 && 
			data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 &&
			data[8] == 0x57 && data[9] == 0x45 && data[10] == 0x42 && data[11] == 0x50
	}
	
	return false
}

// extensionMatchesMIME validates that extension matches MIME type
func (fv *FileValidator) extensionMatchesMIME(ext, mimeType string) bool {
	switch ext {
	case ".jpg", ".jpeg":
		return mimeType == "image/jpeg"
	case ".png":
		return mimeType == "image/png"
	case ".webp":
		return mimeType == "image/webp"
	}
	return false
}

// SafeFileName creates a safe filename from the original
func (fv *FileValidator) SafeFileName(original string) string {
	// Remove path components
	base := filepath.Base(original)
	
	// Remove extension
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	
	// Clean name - remove special characters
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, name)
	
	// Ensure name is not empty
	if name == "" {
		name = "image"
	}
	
	return name + ext
}

// ValidateImageStream validates an image stream efficiently
// This is optimized for the existing AI detection pipeline
func (fv *FileValidator) ValidateImageStream(filename string, stream io.Reader) (*ValidationResult, io.Reader, error) {
	// Create a buffer to capture the first 512 bytes
	buffer := make([]byte, 512)
	n, err := stream.Read(buffer)
	if err != nil && err != io.EOF {
		return nil, nil, fmt.Errorf("failed to read file header: %w", err)
	}
	
	// Validate the file
	result, err := fv.ValidateFile(filename, bytes.NewReader(buffer[:n]))
	if err != nil {
		return nil, nil, err
	}
	
	if !result.IsValid {
		return result, nil, nil
	}
	
	// Return a new reader that combines the buffer with the rest of the stream
	remainingStream := io.MultiReader(bytes.NewReader(buffer[:n]), stream)
	
	return result, remainingStream, nil
}

