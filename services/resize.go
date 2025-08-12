package services

import (
	"image"
	"image/color"
	"image/draw"

	xdraw "golang.org/x/image/draw"
)

// ResizeIfNeeded scales the image down to max dimension while preserving aspect ratio.
// If max <= 0 or the image already fits, returns the original image.
func ResizeIfNeeded(src image.Image, max int) image.Image {
	if max <= 0 {
		return src
	}
	b := src.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= max && h <= max {
		return src
	}
	// Compute target size
	scale := float64(max) / float64(w)
	if h > w {
		scale = float64(max) / float64(h)
	}
	tw := int(float64(w) * scale)
	th := int(float64(h) * scale)
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, xdraw.Over, nil)
	return dst
}

// FlattenIfAlpha composites images with an alpha channel against the provided background color.
// If the source is already opaque, it returns the source unchanged.
func FlattenIfAlpha(src image.Image, bg color.Color) image.Image {
	if IsOpaque(src) {
		return src
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

// IsOpaque returns true if the image has no transparency.
func IsOpaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	// Sample a grid to avoid scanning all pixels (fast heuristic)
	b := img.Bounds()
	stepX := (b.Dx() / 20) + 1
	stepY := (b.Dy() / 20) + 1
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		for x := b.Min.X; x < b.Max.X; x += stepX {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xFFFF {
				return false
			}
		}
	}
	return true
}

// EstimateComplexity downscales the image to at most 256px and computes a simple
// gradient magnitude measure to estimate detail level. Higher values indicate
// more detail/edges.
func EstimateComplexity(src image.Image) float64 {
	// Downscale to speed up
	b := src.Bounds()
	maxDim := b.Dx()
	if b.Dy() > maxDim {
		maxDim = b.Dy()
	}
	small := src
	if maxDim > 256 {
		small = ResizeIfNeeded(src, 256)
	}
	sb := small.Bounds()
	// Compute sum of absolute differences horizontally and vertically
	var sum float64
	var count int
	// Sample every other pixel to reduce cost
	for y := sb.Min.Y + 1; y < sb.Max.Y-1; y += 2 {
		var prevR, prevG, prevB uint32
		for x := sb.Min.X + 1; x < sb.Max.X-1; x += 2 {
			r, g, b, _ := small.At(x, y).RGBA()
			// Horizontal gradient
			if x > sb.Min.X+1 {
				dr := int64(r) - int64(prevR)
				dg := int64(g) - int64(prevG)
				db := int64(b) - int64(prevB)
				if dr < 0 {
					dr = -dr
				}
				if dg < 0 {
					dg = -dg
				}
				if db < 0 {
					db = -db
				}
				sum += float64(dr+dg+db) / 65535.0
				count++
			}
			prevR, prevG, prevB = r, g, b
			// Vertical gradient
			r2, g2, b2, _ := small.At(x, y-1).RGBA()
			dr := int64(r) - int64(r2)
			dg := int64(g) - int64(g2)
			db2 := int64(b) - int64(b2)
			if dr < 0 {
				dr = -dr
			}
			if dg < 0 {
				dg = -dg
			}
			if db2 < 0 {
				db2 = -db2
			}
			sum += float64(dr+dg+db2) / 65535.0
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}
