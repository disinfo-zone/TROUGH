package services

import (
	"bytes"
	"image"
	"image/jpeg"
	"io/ioutil"
	"os"
	"regexp"
)

var xmpRegex = regexp.MustCompile(`(?is)<x:xmpmeta[\s\S]*?</x:xmpmeta>`) // greedy across lines

// ExtractXMPXML scans the file for an XMP packet and returns its XML bytes if found.
func ExtractXMPXML(filePath string) []byte {
	b, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil
	}
	m := xmpRegex.Find(b)
	if len(m) > 0 {
		return m
	}
	return nil
}

// WriteJPEGWithXMP encodes img as JPEG and embeds XMP (if provided) as an APP1 segment.
func WriteJPEGWithXMP(img image.Image, quality int, destPath string, xmp []byte) error {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return err
	}
	data := buf.Bytes()
	// JPEG must start with SOI 0xFFD8
	if len(data) < 2 || !(data[0] == 0xFF && data[1] == 0xD8) {
		return os.WriteFile(destPath, data, 0644)
	}
	if len(xmp) == 0 {
		return os.WriteFile(destPath, data, 0644)
	}
	// Build APP1 XMP segment
	header := []byte("http://ns.adobe.com/xap/1.0/\x00")
	segmentContent := append(header, xmp...)
	segLen := len(segmentContent) + 2 // length includes the two length bytes
	app1 := []byte{0xFF, 0xE1, byte(segLen >> 8), byte(segLen & 0xFF)}
	app1 = append(app1, segmentContent...)
	// Insert after SOI
	out := make([]byte, 0, len(data)+len(app1))
	out = append(out, data[:2]...) // SOI
	out = append(out, app1...)
	out = append(out, data[2:]...)
	return os.WriteFile(destPath, out, 0644)
}
