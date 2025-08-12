package services

import (
	"bytes"
	"image"
	"image/jpeg"
)

// EncodeJPEGWithMetadata encodes the provided image as a JPEG at the given quality
// and injects EXIF and/or XMP metadata as APP1 segments when provided.
// Order of APP1 segments: EXIF first, then XMP. If a segment would exceed
// the JPEG APP1 maximum size, it is skipped to preserve a valid file.
func EncodeJPEGWithMetadata(img image.Image, quality int, xmpXML []byte, exifRaw []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	data := buf.Bytes()
	// JPEG must start with SOI 0xFFD8
	if len(data) < 2 || !(data[0] == 0xFF && data[1] == 0xD8) {
		// Not a normal JPEG stream; just return as is
		return data, nil
	}

	// Build APP1 EXIF segment if provided.
	var app1Segments [][]byte
	if len(exifRaw) > 0 {
		// APP1 EXIF payload: "Exif\x00\x00" + TIFF data
		exifHeader := []byte("Exif\x00\x00")
		exifContent := append(exifHeader, exifRaw...)
		if seg := buildAPP1Segment(exifContent); len(seg) > 0 {
			app1Segments = append(app1Segments, seg)
		}
	}

	// Build APP1 XMP segment if provided.
	if len(xmpXML) > 0 {
		xmpHeader := []byte("http://ns.adobe.com/xap/1.0/\x00")
		xmpContent := append(xmpHeader, xmpXML...)
		if seg := buildAPP1Segment(xmpContent); len(seg) > 0 {
			app1Segments = append(app1Segments, seg)
		}
	}

	if len(app1Segments) == 0 {
		return data, nil
	}

	// Insert APP1 segments immediately after SOI
	out := make([]byte, 0, len(data)+len(app1Segments)*len(app1Segments[0]))
	out = append(out, data[:2]...) // SOI
	for _, seg := range app1Segments {
		out = append(out, seg...)
	}
	out = append(out, data[2:]...)
	return out, nil
}

// buildAPP1Segment constructs a JPEG APP1 segment from the provided content body.
// The length field includes its own two bytes per the JPEG specification.
// If the segment would be too large (> 65535 bytes including length), returns nil.
func buildAPP1Segment(content []byte) []byte {
	// APP1 marker 0xFFE1, then 2-byte big-endian length (including the two length bytes)
	segLen := len(content) + 2
	if segLen > 0xFFFF { // exceeds JPEG APP1 length capacity
		return nil
	}
	seg := []byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}
	seg = append(seg, content...)
	return seg
}
