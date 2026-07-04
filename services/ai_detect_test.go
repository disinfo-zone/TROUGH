package services

import (
	"strings"
	"testing"
)

// End-to-end through the entry point the upload handler uses. A plain non-AI blob
// must be rejected; a bare "c2pa" mention must NOT pass (the old false positive);
// an A1111 parameter block and a genuine C2PA structure must pass.
func TestDetectAIProvenanceConcurrentEndToEnd(t *testing.T) {
	t.Parallel()

	// Minimal PNG-ish header + payload so length checks pass; content drives detection.
	pngHeader := "\x89PNG\r\n\x1a\n"

	nonAI := []byte(pngHeader + strings.Repeat("plain pixels ", 64) + " shot on a camera, iso 100")
	if ok, _ := DetectAIProvenanceConcurrent(nonAI, nil); ok {
		t.Fatalf("expected non-AI image to be rejected")
	}

	bareC2PA := []byte(pngHeader + "a caption that merely says c2pa somewhere")
	if ok, _ := DetectAIProvenanceConcurrent(bareC2PA, nil); ok {
		t.Fatalf("expected bare 'c2pa' mention to be rejected (structural C2PA required)")
	}

	a1111 := []byte(pngHeader + "parameters\x00a cat\nNegative prompt: blurry\nSteps: 30, Sampler: Euler a, CFG scale: 7, Seed: 42")
	if ok, res := DetectAIProvenanceConcurrent(a1111, nil); !ok || res.Provider == "" {
		t.Fatalf("expected A1111 parameter block to be accepted, got ok=%v provider=%q", ok, res.Provider)
	}

	realC2PA := []byte(pngHeader + "\x00\x00\x01\x00jumb\x00\x00jumd\x00c2pa.assertions urn:c2pa:1234")
	if ok, res := DetectAIProvenanceConcurrent(realC2PA, nil); !ok || res.Method != "c2pa" {
		t.Fatalf("expected genuine C2PA manifest to be accepted as c2pa, got ok=%v method=%q", ok, res.Method)
	}
}

func TestIsLikelyAIPromptPayloadRejectsCameraModel(t *testing.T) {
	t.Parallel()

	if isLikelyAIPromptPayload("Model: Canon EOS R5") {
		t.Fatalf("expected camera model metadata to be rejected")
	}
}

func TestIsLikelyAIPromptPayloadAcceptsStrongSDMarkers(t *testing.T) {
	t.Parallel()

	payload := `{"parameters":{"positive_prompt":"city skyline","negative_prompt":"blurry"}}`
	if !isLikelyAIPromptPayload(payload) {
		t.Fatalf("expected strong stable diffusion markers to be accepted")
	}
}

func TestDetectAIBinaryMarkersFromTextRequiresCombinations(t *testing.T) {
	t.Parallel()

	ok, _ := detectAIBinaryMarkersFromText("this photo has a prompt for camera setup only")
	if ok {
		t.Fatalf("expected weak binary text to be rejected")
	}

	ok, res := detectAIBinaryMarkersFromText(`{"sui_image_params":"sampler=euler,steps=30,cfg=7"}`)
	if !ok {
		t.Fatalf("expected strong binary markers to be accepted")
	}
	if res.Provider == "" {
		t.Fatalf("expected provider for strong binary marker match")
	}
}

func TestDetectAISoftwareProviderStrict(t *testing.T) {
	t.Parallel()

	if _, ok := detectAISoftwareProvider("Canon Camera Utility"); ok {
		t.Fatalf("expected non-AI software to be rejected")
	}

	provider, ok := detectAISoftwareProvider("Stable Diffusion WebUI")
	if !ok {
		t.Fatalf("expected stable diffusion software to be accepted")
	}
	if provider != "Stable Diffusion (SDXL)" {
		t.Fatalf("unexpected provider: %s", provider)
	}
}

func TestDetectFromXMPRejectsGenericWorkflowOnly(t *testing.T) {
	t.Parallel()

	xmp := []byte(`<x:xmpmeta><rdf:Description><dc:description>workflow notes for shoot</dc:description></rdf:Description></x:xmpmeta>`)
	if ok, _ := detectFromXMP(xmp); ok {
		t.Fatalf("expected generic workflow-only xmp to be rejected")
	}
}

func TestDetectFromXMPAcceptsStrongMarkers(t *testing.T) {
	t.Parallel()

	xmp := []byte(`<x:xmpmeta><rdf:Description><ai:sui_image_params>prompt text</ai:sui_image_params></rdf:Description></x:xmpmeta>`)
	ok, res := detectFromXMP(xmp)
	if !ok {
		t.Fatalf("expected strong xmp markers to be accepted")
	}
	if res.Provider != "Stable Diffusion (SDXL)" {
		t.Fatalf("unexpected provider: %s", res.Provider)
	}
}

// detectC2PA must not accept a bare "c2pa"/"jumbf" substring (the old false-positive
// hole): any camera photo could carry that word in a comment.
func TestDetectC2PARejectsBareSubstring(t *testing.T) {
	t.Parallel()

	for _, s := range []string{
		"a normal caption mentioning c2pa in passing",
		"jumbf",
		"contentcredentials",
		"Content Credentials were not applied here",
	} {
		if ok, _ := detectC2PA([]byte(s)); ok {
			t.Fatalf("expected bare substring %q to be rejected as C2PA", s)
		}
	}
}

// detectC2PA must accept genuine manifest structure.
func TestDetectC2PAAcceptsManifestStructure(t *testing.T) {
	t.Parallel()

	cases := []string{
		"\x00\x00\x01\x00jumb\x00\x00jumd c2pa manifest data",
		"prefix urn:c2pa:abcd-1234 suffix",
		`{"c2pa.assertions":[{"label":"c2pa.actions"}]}`,
	}
	for _, s := range cases {
		if ok, _ := detectC2PA([]byte(s)); !ok {
			t.Fatalf("expected genuine C2PA structure to be accepted: %q", s)
		}
	}
}

// Automatic1111 / SD WebUI images carry a distinctive parameter block and often no
// literal "stable diffusion" string; ensure they are recognized.
func TestDetectA1111ParameterBlock(t *testing.T) {
	t.Parallel()

	a1111 := "masterpiece, a cat\nNegative prompt: blurry\nSteps: 28, Sampler: DPM++ 2M Karras, CFG scale: 7, Seed: 123456789, Size: 1024x1024, Model hash: abc123"
	ok, res := detectAIBinaryMarkersFromText(a1111)
	if !ok {
		t.Fatalf("expected A1111 parameter block to be accepted")
	}
	if res.Provider == "" {
		t.Fatalf("expected a provider for A1111 block")
	}
}

// A camera JPEG's coincidental single term must not trigger the parameter-block rule.
func TestDetectA1111RequiresFullBlock(t *testing.T) {
	t.Parallel()

	if ok, _ := detectAIBinaryMarkersFromText("shutter speed 1/250, iso 100, steps to reproduce: none"); ok {
		t.Fatalf("expected partial/coincidental terms to be rejected")
	}
}

func TestDetectAISoftwareProviderExpandedCoverage(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"NovelAI":         "Stable Diffusion (SDXL)",
		"Ideogram":        "Ideogram",
		"Leonardo.Ai":     "Leonardo.Ai",
		"Adobe Firefly":   "Adobe Firefly",
		"Recraft":         "Recraft",
		"ComfyUI":         "Stable Diffusion (SDXL)",
		"Google Imagen 3": "Google Imagen",
	}
	for software, want := range cases {
		got, ok := detectAISoftwareProvider(software)
		if !ok || got != want {
			t.Fatalf("software %q: got (%q,%v), want %q", software, got, ok, want)
		}
	}

	if _, ok := detectAISoftwareProvider("Adobe Photoshop 25.0"); ok {
		// Photoshop alone is an editor, not a generator signal.
		t.Fatalf("expected plain photo editor software to be rejected")
	}
}
