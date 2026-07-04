package services

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"strings"
	"testing"
)

func pngChunk(ctype string, data []byte) []byte {
	out := make([]byte, 0, 12+len(data))
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], uint32(len(data)))
	out = append(out, l[:]...)
	out = append(out, []byte(ctype)...)
	out = append(out, data...)
	out = append(out, 0, 0, 0, 0) // CRC (not validated by the parser)
	return out
}

func buildPNG(chunks ...[]byte) []byte {
	out := append([]byte{}, pngSignature...)
	for _, ch := range chunks {
		out = append(out, ch...)
	}
	out = append(out, pngChunk("IEND", nil)...)
	return out
}

func zlibCompress(s string) []byte {
	var b bytes.Buffer
	zw := zlib.NewWriter(&b)
	_, _ = zw.Write([]byte(s))
	_ = zw.Close()
	return b.Bytes()
}

// A compressed zTXt chunk carrying A1111 params must be decompressed and detected —
// a raw byte scan cannot see it because the bytes are deflated.
func TestDetectPNGCompressedZTXtMetadata(t *testing.T) {
	t.Parallel()

	params := "a portrait\nNegative prompt: blurry\nSteps: 30, Sampler: Euler a, CFG scale: 7, Seed: 12345"
	ztxt := append([]byte("parameters\x00\x00"), zlibCompress(params)...) // keyword \0 method(0=zlib) + data
	png := buildPNG(pngChunk("zTXt", ztxt))

	// Sanity: the params must NOT be findable as plain bytes (proving compression).
	if bytes.Contains(png, []byte("Sampler:")) {
		t.Fatalf("test setup wrong: params should be compressed, not plaintext")
	}

	extracted := extractPNGTextMetadata(png)
	if !strings.Contains(extracted, "Sampler:") {
		t.Fatalf("expected decompressed params to contain the parameter block, got %q", extracted)
	}

	ok, res := detectFromBinaryTextBytes(png)
	if !ok || res.Provider == "" {
		t.Fatalf("expected compressed PNG metadata to be detected, ok=%v provider=%q", ok, res.Provider)
	}
}

// iTXt with the uncompressed flag must also be read.
func TestDetectPNGUncompressedITXtMetadata(t *testing.T) {
	t.Parallel()

	text := "workflow comfyui k_sampler checkpoint_loader \"prompt\" \"workflow\""
	// keyword \0 compFlag(0) compMethod(0) lang \0 translated \0 text
	itxt := append([]byte("Comment\x00"), 0, 0)
	itxt = append(itxt, 0)               // empty language tag + \0
	itxt = append(itxt, 0)               // empty translated keyword + \0
	itxt = append(itxt, []byte(text)...) // uncompressed text
	png := buildPNG(pngChunk("iTXt", itxt))

	if ok, _ := detectFromBinaryTextBytes(png); !ok {
		t.Fatalf("expected uncompressed iTXt ComfyUI metadata to be detected")
	}
}

// A PNG with no AI metadata must still be rejected (no false positive from chunk parsing).
func TestPNGWithoutAIMetadataRejected(t *testing.T) {
	t.Parallel()

	png := buildPNG(pngChunk("tEXt", []byte("Comment\x00just a holiday photo")))
	if ok, _ := detectFromBinaryTextBytes(png); ok {
		t.Fatalf("expected non-AI PNG text to be rejected")
	}
}
