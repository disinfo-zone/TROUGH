package services

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"mime/multipart"

	"github.com/bbrks/go-blurhash"
)

type ImageMeta struct {
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	Format        string `json:"format"`
	Blurhash      string `json:"blurhash"`
	DominantColor string `json:"dominant_color"`
}

func ProcessImage(file multipart.File) (ImageMeta, error) {
	// Decode image
	img, format, err := image.Decode(file)
	if err != nil {
		return ImageMeta{}, err
	}

	bounds := img.Bounds()
	meta := ImageMeta{
		Width:  bounds.Dx(),
		Height: bounds.Dy(),
		Format: format,
	}

	// Generate blurhash for beautiful loading
	hash, err := blurhash.Encode(4, 3, img)
	if err == nil {
		meta.Blurhash = hash
	}

	// Extract dominant color
	meta.DominantColor = extractDominantColor(img)

	return meta, nil
}

func extractDominantColor(img image.Image) string {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()

	// Sample the image at regular intervals to find dominant color
	var r, g, b uint32
	sampleCount := 0

	// Sample every 10th pixel to avoid processing the entire image
	for y := bounds.Min.Y; y < bounds.Max.Y; y += height / 10 {
		for x := bounds.Min.X; x < bounds.Max.X; x += width / 10 {
			pixel := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
			r += uint32(pixel.R)
			g += uint32(pixel.G)
			b += uint32(pixel.B)
			sampleCount++
		}
	}

	if sampleCount == 0 {
		return "#1a1a2e" // Default dark color
	}

	// Average the colors
	avgR := r / uint32(sampleCount)
	avgG := g / uint32(sampleCount)
	avgB := b / uint32(sampleCount)

	// Darken the color for aesthetic purposes (better as background)
	avgR = avgR * 70 / 100
	avgG = avgG * 70 / 100
	avgB = avgB * 70 / 100

	return fmt.Sprintf("#%02x%02x%02x", avgR, avgG, avgB)
}