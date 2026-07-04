package services

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"strings"
)

var pngSignature = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

const (
	// Caps to keep parsing cheap and to refuse decompression-bomb text chunks.
	maxPNGTextTotal = 4 << 20 // 4 MiB of accumulated decoded text
	maxPNGChunks    = 1024
)

// extractPNGTextMetadata parses PNG tEXt/zTXt/iTXt chunks and returns their
// concatenated keyword+text content. Many AI tools (ComfyUI, Automatic1111, NovelAI,
// Fooocus) write the prompt/workflow into these chunks — and zTXt/iTXt are
// zlib-compressed, so a raw byte scan of the file misses them entirely. Feeding the
// decompressed text into the existing marker detection closes that recall gap.
//
// Decompression is bounded (per-chunk via the shared budget) so a maliciously tiny
// but explosively compressible chunk cannot exhaust memory.
func extractPNGTextMetadata(b []byte) string {
	if len(b) < len(pngSignature)+12 || !bytes.HasPrefix(b, pngSignature) {
		return ""
	}
	var sb strings.Builder
	pos := len(pngSignature)
	chunks := 0
	for pos+8 <= len(b) && sb.Len() < maxPNGTextTotal && chunks < maxPNGChunks {
		length := int(binary.BigEndian.Uint32(b[pos : pos+4]))
		if length < 0 {
			break
		}
		ctype := string(b[pos+4 : pos+8])
		dataStart := pos + 8
		// Bounds-check data + 4-byte CRC.
		if dataStart+length+4 > len(b) {
			break
		}
		data := b[dataStart : dataStart+length]
		chunks++

		switch ctype {
		case "tEXt":
			if i := bytes.IndexByte(data, 0); i >= 0 {
				writeKV(&sb, data[:i], data[i+1:])
			}
		case "zTXt":
			// keyword \0 method(1) compressed-text(zlib)
			if i := bytes.IndexByte(data, 0); i >= 0 && i+2 <= len(data) {
				writeKV(&sb, data[:i], []byte(inflateBounded(data[i+2:], maxPNGTextTotal-sb.Len())))
			}
		case "iTXt":
			// keyword \0 compFlag(1) compMethod(1) lang \0 translated \0 text
			if i := bytes.IndexByte(data, 0); i >= 0 && i+3 <= len(data) {
				keyword := data[:i]
				rest := data[i+1:]
				compFlag := rest[0]
				rest = rest[2:]                            // skip compFlag + compMethod
				if j := bytes.IndexByte(rest, 0); j >= 0 { // skip language tag
					rest = rest[j+1:]
				} else {
					break
				}
				if j := bytes.IndexByte(rest, 0); j >= 0 { // skip translated keyword
					rest = rest[j+1:]
				} else {
					break
				}
				if compFlag == 1 {
					writeKV(&sb, keyword, []byte(inflateBounded(rest, maxPNGTextTotal-sb.Len())))
				} else {
					writeKV(&sb, keyword, rest)
				}
			}
		case "IEND":
			pos = len(b)
			continue
		}
		pos = dataStart + length + 4 // advance past data + CRC
	}
	return sb.String()
}

func writeKV(sb *strings.Builder, key, val []byte) {
	sb.Write(key)
	sb.WriteByte(' ')
	sb.Write(val)
	sb.WriteByte('\n')
}

// inflateBounded zlib-decompresses up to limit bytes, refusing to allocate more.
func inflateBounded(compressed []byte, limit int) string {
	if limit <= 0 || len(compressed) == 0 {
		return ""
	}
	zr, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return ""
	}
	defer zr.Close()
	out, _ := io.ReadAll(io.LimitReader(zr, int64(limit)))
	return string(out)
}
