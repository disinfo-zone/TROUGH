package services

import "testing"

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
