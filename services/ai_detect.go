package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/dsoprea/go-exif/v3"
)

// Buffer pool for memory optimization
var bufferPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, 0, 1024*1024) // 1MB initial buffer
	},
}

// getBuffer gets a buffer from the pool
func getBuffer() []byte {
	return bufferPool.Get().([]byte)
}

// putBuffer returns a buffer to the pool
func putBuffer(buf []byte) {
	if cap(buf) <= 2*1024*1024 { // Don't pool buffers larger than 2MB
		bufferPool.Put(buf[:0])
	}
}

// AIDetectionResult describes detected AI provenance for an image.
type AIDetectionResult struct {
	Provider string // e.g., "Midjourney", "OpenAI", "Adobe Firefly", "Google Imagen", "Grok", "Stable Diffusion (SDXL)", "ComfyUI", "Unknown C2PA"
	Method   string // e.g., "xmp", "exif", "c2pa"
	Details  string // matched field/value or brief explanation
}

var (
	guidRegex        = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	c2paSniffRegex   = regexp.MustCompile(`(?is)(c2pa|jumbf|contentcredentials)`)
	iptcTrainedMedia = "http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia"

	// Pre-compiled regex patterns for performance
	aiSoftwareRegex = regexp.MustCompile(`(?i)(midjourney|dall-?e|openai|stable.*diffusion|sdxl|flux|black.*forest.*labs|bfl)`)
	promptRegex     = regexp.MustCompile(`(?i)("prompt"|prompt:|\nprompt|\sprompt\s|positive_prompt|negative_prompt|textual_inversion|checkpoint|lora)`)
	grokRegex       = regexp.MustCompile(`(?i)(grok.*image.*prompt|grok.*image.*upsampled.*prompt|\x00grok\x00|g*r*o*k|grok:|"grok")`)
	comfyuiRegex    = regexp.MustCompile(`(?i)("filename_prefix":"comfyui"|comfyui|workflow|node|k_sampler|checkpoint_loader|vae_decode|empty_latent_image)`)

	// Additional optimized patterns for common string matching
	genericAIRegex = regexp.MustCompile(`(?i)(ai|diffusion|artificial|generator|synthetic|stability)`)
	workflowRegex  = regexp.MustCompile(`(?i)(workflow|sampler|steps|cfg|seed|checkpoint|controlnet|embeddings|vae|clip_skip|hypernetwork)`)
	adobeRegex     = regexp.MustCompile(`(?i)(adobe.*firefly|firefly.*adobe)`)
	googleAIRegex  = regexp.MustCompile(`(?i)(made.*with.*google.*ai|google.*ai)`)
	suiParamsRegex = regexp.MustCompile(`(?i)(sui_image_params)`)

	// Fast detection patterns (ordered by probability)
	fastPatterns = []struct {
		pattern  *regexp.Regexp
		provider string
		method   string
	}{
		{aiSoftwareRegex, "AI (Software)", "exif"},
		{promptRegex, "AI (Prompt Embedded)", "binary"},
		{grokRegex, "Grok", "binary"},
		{comfyuiRegex, "ComfyUI", "binary"},
	}

	// Specific AI model patterns - replacing generic "model"
	aiModelPatterns = []string{"sdxl", "flux", "wan", "midjourney", "dall-e", "stability", "dreamshaper", "realistic vision", "epic realism", "deliberate", "anything v", "counterfeit", "protogen", "rev animated", "chilloutmix", "meinamix", "f222", "anime", "sd_xl", "stable-diffusion-xl", "txt2img", "img2img", "controlnet", "lora", "hypernetwork", "embeddings", "textual_inversion", "vae", "clip_skip"}

	// Expanded Stable Diffusion terms
	sdxlTerms = []string{"sdxl", "stable diffusion", "sd_xl", "stable-diffusion-xl", "txt2img", "img2img", "controlnet", "lora", "hypernetwork", "embeddings", "textual_inversion", "vae", "clip_skip", "ksampler", "sampler_name", "negativeprompt", "negative_prompt", "cfg", "steps"}

	// Expanded ComfyUI patterns
	comfyuiPatterns = []string{"comfyui", "comfy", "workflow", "node", "k_sampler", "checkpoint_loader", "clip_text_encode", "vae_decode", "empty_latent_image", "latent_upscale", "filename_prefix"}

	// More prompt variations
	promptVariations = []string{"prompt", "prompts", "positive_prompt", "negative_prompt", "text_prompt", "input_prompt", "ai_prompt", "generation_prompt"}

	// Generic AI terms - REMOVED to prevent false positives
	// These terms were too generic and caused non-AI images to be accepted
	// genericAITerms = []string{"ai_art", "ai_generated", "ai_artwork", "machine_learning", "neural_network", "gan", "generative", "synthetic", "computer_vision", "deep_learning", "text_to_image", "artificial", "generator", "synthetic"}
)

// DetectAIProvenance attempts to determine if an image has AI provenance markers.
// It returns ok=false when no acceptable provenance is found.
// The xmpXML should be the raw XMP packet if available; pass nil if unknown.
func DetectAIProvenance(imagePath string, xmpXML []byte) (ok bool, result AIDetectionResult) {
	// 1) Heuristic presence of C2PA JUMBF/labels in file body
	if sniffC2PA(imagePath) {
		// Try to differentiate by XMP credit/creator if present
		provider := classifyC2PAProvider(xmpXML)
		if provider == "" {
			provider = "Unknown C2PA"
		}
		return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA/JUMBF markers present"}
	}

	// 2) EXIF flat scan for common tells (Software, UserComment, custom fields)
	if ok, res := detectFromEXIF(imagePath); ok {
		return true, res
	}

	// 3) PNG/Web formats often store generation params as plain text chunks
	if ok, res := detectFromBinaryText(imagePath); ok {
		return true, res
	}

	// 4) XMP text scan for IPTC and vendor-specific fields
	if ok, res := detectFromXMP(xmpXML); ok {
		return true, res
	}

	return false, AIDetectionResult{}
}

// DetectAIProvenanceFromBytes is the bytes-based variant avoiding disk I/O.
func DetectAIProvenanceFromBytes(imageBytes []byte, xmpXML []byte) (ok bool, result AIDetectionResult) {
	// 1) Heuristic presence of C2PA JUMBF/labels in file body
	if c2paSniffRegex.Find(imageBytes) != nil {
		provider := classifyC2PAProvider(xmpXML)
		if provider == "" {
			provider = "Unknown C2PA"
		}
		return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA/JUMBF markers present"}
	}

	// Enhanced C2PA detection for binary JUMBF chunks
	// C2PA manifests are stored in PNG chunks as binary data
	if bytes.Contains(imageBytes, []byte("jumb")) && bytes.Contains(imageBytes, []byte("c2pa")) {
		provider := classifyC2PAProvider(xmpXML)
		if provider == "" {
			provider = "Unknown C2PA"
		}
		return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA JUMBF binary chunks detected"}
	}

	// Check for C2PA URN pattern (binary)
	if bytes.Contains(imageBytes, []byte("urn:c2pa:")) {
		provider := classifyC2PAProvider(xmpXML)
		if provider == "" {
			provider = "Unknown C2PA"
		}
		return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA URN detected"}
	}
	// 2) EXIF
	if ok, res := detectFromEXIFBytes(imageBytes); ok {
		return true, res
	}
	// 3) Binary text blobs
	if ok, res := detectFromBinaryTextBytes(imageBytes); ok {
		return true, res
	}
	// 4) XMP
	if ok, res := detectFromXMP(xmpXML); ok {
		return true, res
	}
	return false, AIDetectionResult{}
}

func detectAISoftwareProvider(software string) (string, bool) {
	low := strings.ToLower(strings.TrimSpace(software))
	switch {
	case strings.Contains(low, "midjourney"):
		return "Midjourney", true
	case strings.Contains(low, "dall-e") || strings.Contains(low, "dalle") || strings.Contains(low, "openai"):
		return "OpenAI", true
	case strings.Contains(low, "stable diffusion") || strings.Contains(low, "sdxl"):
		return "Stable Diffusion (SDXL)", true
	case strings.Contains(low, "flux") || strings.Contains(low, "black forest labs") || strings.Contains(low, "bfl"):
		return "FLUX", true
	}
	return "", false
}

func isPromptMetadataTag(tagName string) bool {
	tn := strings.ToLower(strings.TrimSpace(tagName))
	return strings.Contains(tn, "prompt") ||
		strings.Contains(tn, "workflow") ||
		strings.Contains(tn, "comment") ||
		strings.Contains(tn, "description") ||
		strings.Contains(tn, "parameter")
}

func isLikelyAIPromptPayload(value string) bool {
	low := strings.ToLower(strings.TrimSpace(value))
	if low == "" {
		return false
	}
	// Strong single-marker signals
	if containsAnyFold(low, []string{
		"sui_image_params", "positive_prompt", "negative_prompt", "negativeprompt",
		"textual_inversion", "controlnet", "checkpoint_loader",
	}) {
		return true
	}

	// Require multiple weak markers to avoid false positives from words like "model".
	score := 0
	if strings.Contains(low, "prompt") {
		score++
	}
	if containsAnyFold(low, []string{
		"sampler", "steps", "cfg", "seed", "checkpoint", "lora", "vae", "embeddings",
		"txt2img", "img2img", "workflow", "comfyui", "stable diffusion", "job id:",
		"--ar", "--stylize", "--chaos", "--seed",
	}) {
		score++
	}
	if looksLikePromptJSON(low) {
		score++
	}
	return score >= 2
}

func limitedLowerText(b []byte, maxBytes int) string {
	if len(b) == 0 {
		return ""
	}
	if maxBytes <= 0 || len(b) <= maxBytes {
		return strings.ToLower(string(b))
	}
	head := maxBytes / 2
	tail := maxBytes - head
	var buf strings.Builder
	buf.Grow(maxBytes + 1)
	buf.WriteString(strings.ToLower(string(b[:head])))
	buf.WriteByte(' ')
	buf.WriteString(strings.ToLower(string(b[len(b)-tail:])))
	return buf.String()
}

func detectAIBinaryMarkersFromText(text string) (bool, AIDetectionResult) {
	s := strings.ToLower(text)
	if s == "" {
		return false, AIDetectionResult{}
	}

	if containsAnyFold(s, []string{"grok image prompt", "grok image upsampled prompt"}) {
		return true, AIDetectionResult{Provider: "Grok", Method: "binary", Details: "Grok prompt fields in metadata"}
	}

	if strings.Contains(s, "midjourney") && containsAnyFold(s, []string{"--ar", "--chaos", "--stylize", "--seed", "job id:"}) {
		return true, AIDetectionResult{Provider: "Midjourney", Method: "binary", Details: "Midjourney generation params present"}
	}

	if containsAnyFold(s, []string{"sui_image_params", "negative_prompt", "positive_prompt", "textual_inversion"}) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "SD generation params present"}
	}

	if (strings.Contains(s, "\"prompt\"") || strings.Contains(s, " prompt")) &&
		(strings.Contains(s, "\"workflow\"") || containsAnyFold(s, []string{"comfyui", "checkpoint_loader", "k_sampler", "vae_decode"})) {
		return true, AIDetectionResult{Provider: "ComfyUI", Method: "binary", Details: "Prompt/workflow metadata present"}
	}

	if strings.Contains(s, "stable diffusion") && containsAnyFold(s, []string{"sampler", "steps", "cfg", "seed", "checkpoint", "lora"}) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "SD metadata terms present"}
	}

	if (strings.Contains(s, "dall-e") || strings.Contains(s, "dalle") || strings.Contains(s, "openai")) &&
		containsAnyFold(s, []string{"prompt", "image generation"}) {
		return true, AIDetectionResult{Provider: "OpenAI", Method: "binary", Details: "OpenAI generation metadata present"}
	}

	if strings.Contains(s, "flux") && containsAnyFold(s, []string{"black forest labs", "bfl", "prompt", "sampler", "steps", "cfg", "seed"}) {
		return true, AIDetectionResult{Provider: "FLUX", Method: "binary", Details: "FLUX generation metadata present"}
	}

	return false, AIDetectionResult{}
}

func detectFromEXIFBytes(b []byte) (bool, AIDetectionResult) {
	rawExif, err := exif.SearchAndExtractExif(b)
	if err != nil {
		return false, AIDetectionResult{}
	}

	// Quick raw scan for strong markers only.
	rawLower := strings.ToLower(string(rawExif))
	if containsAnyFold(rawLower, []string{"sui_image_params", "negative_prompt", "positive_prompt", "textual_inversion"}) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: "strong SD markers in raw EXIF"}
	}

	for _, pattern := range []string{"sui_image_params", "negative_prompt", "positive_prompt", "textual_inversion"} {
		if bytes.Contains(rawExif, buildUTF16LEPattern(pattern)) || bytes.Contains(rawExif, buildUTF16BEPattern(pattern)) {
			return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: "strong SD markers in UTF-16 EXIF"}
		}
	}

	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		return false, AIDetectionResult{}
	}

	for _, e := range entries {
		tn := strings.TrimSpace(e.TagName)
		val := strings.TrimSpace(e.Formatted)
		if strings.EqualFold(tn, "Software") {
			if provider, ok := detectAISoftwareProvider(val); ok {
				return true, AIDetectionResult{Provider: provider, Method: "exif", Details: "Software tag indicates " + provider}
			}
		}

		if strings.EqualFold(tn, "DigitalSourceType") && strings.TrimSpace(val) == iptcTrainedMedia {
			return true, AIDetectionResult{Provider: "AI (IPTC Trained Media)", Method: "exif", Details: "DigitalSourceType trainedAlgorithmicMedia"}
		}

		decoded := val
		if strings.EqualFold(tn, "UserComment") && e.Value != nil {
			if v, ok := e.Value.([]byte); ok && len(v) > 0 {
				if d, derr := decodeUTF16(v); derr == nil && strings.TrimSpace(d) != "" {
					decoded = d
				}
			}
		}

		if isPromptMetadataTag(tn) && isLikelyAIPromptPayload(decoded) {
			provider := "Stable Diffusion (SDXL)"
			if containsAnyFold(decoded, []string{"--chaos", "--ar", "--profile", "--stylize", "--weird", "--v ", "--no ", "--seed", "job id:"}) {
				provider = "Midjourney"
			}
			return true, AIDetectionResult{Provider: provider, Method: "exif", Details: tn + " contains strong generation metadata"}
		}
	}

	return false, AIDetectionResult{}
}

func detectFromBinaryTextBytes(b []byte) (bool, AIDetectionResult) {
	// Strong UTF-16 markers first.
	for _, n := range []string{"sui_image_params", "negative_prompt", "positive_prompt", "textual_inversion"} {
		if bytes.Contains(b, buildUTF16LEPattern(n)) || bytes.Contains(b, buildUTF16BEPattern(n)) {
			return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "UTF-16 generation markers present"}
		}
	}

	// Scan a bounded text window for performance and lower false positives.
	return detectAIBinaryMarkersFromText(limitedLowerText(b, 768*1024))
}

func sniffC2PA(imagePath string) bool {
	b, err := os.ReadFile(imagePath)
	if err != nil {
		return false
	}
	return c2paSniffRegex.Find(b) != nil
}

func classifyC2PAProvider(xmp []byte) string {
	if len(xmp) == 0 {
		return ""
	}
	s := strings.ToLower(string(xmp))
	// OpenAI often indicates DALL-E/OpenAI within Credit/Creator, or XMP namespaces may mention openai
	if aiSoftwareRegex.MatchString(s) && (strings.Contains(strings.ToLower(s), "openai") || strings.Contains(strings.ToLower(s), "dall")) {
		return "OpenAI"
	}
	// Adobe Firefly uses Content Credentials and often adobe/firefly appears in XMP
	if adobeRegex.MatchString(s) {
		return "Adobe Firefly"
	}
	// Google Imagen (Gemini) may include credit "Made with Google AI"
	if googleAIRegex.MatchString(s) {
		return "Google Imagen"
	}
	return ""
}

func detectFromEXIF(imagePath string) (bool, AIDetectionResult) {
	b, err := os.ReadFile(imagePath)
	if err != nil {
		return false, AIDetectionResult{}
	}
	return detectFromEXIFBytes(b)
}

func detectFromXMP(xmp []byte) (bool, AIDetectionResult) {
	if len(xmp) == 0 {
		return false, AIDetectionResult{}
	}
	s := strings.ToLower(string(xmp))

	// Midjourney: Digital Image GUID + Digital Source Type
	if strings.Contains(s, strings.ToLower(iptcTrainedMedia)) && guidRegex.Find(xmp) != nil {
		return true, AIDetectionResult{Provider: "Midjourney", Method: "xmp", Details: "IPTC trained media + GUID"}
	}

	// Google Imagen (Gemini): Digital Source/Type + Credit: Made with Google AI
	if strings.Contains(s, strings.ToLower(iptcTrainedMedia)) && strings.Contains(s, "made with google ai") {
		return true, AIDetectionResult{Provider: "Google Imagen", Method: "xmp", Details: "IPTC + Credit"}
	}

	// Grok custom fields (any mention)
	if strings.Contains(s, "grok image prompt") || strings.Contains(s, "grok image upsampled prompt") || strings.Contains(s, ">grok<") || strings.Contains(s, "\"grok\"") || strings.Contains(s, " g r o k ") || strings.Contains(s, "grok:") {
		return true, AIDetectionResult{Provider: "Grok", Method: "xmp", Details: "Grok prompt fields"}
	}

	// ComfyUI: Prompt and Workflow fields
	if (strings.Contains(s, ">prompt<") && strings.Contains(s, ">workflow<")) ||
		(strings.Contains(s, "\"prompt\"") && containsAnyFold(s, []string{"\"workflow\"", "comfyui", "k_sampler", "checkpoint_loader", "vae_decode", "clip_text_encode"})) {
		return true, AIDetectionResult{Provider: "ComfyUI", Method: "xmp", Details: "Prompt + Workflow"}
	}

	// Adobe Firefly via XMP mentions
	if adobeRegex.MatchString(s) {
		return true, AIDetectionResult{Provider: "Adobe Firefly", Method: "xmp", Details: "XMP mentions Adobe Firefly"}
	}

	// OpenAI via XMP mentions
	if aiSoftwareRegex.MatchString(s) && (strings.Contains(strings.ToLower(s), "openai") || strings.Contains(strings.ToLower(s), "dall")) {
		return true, AIDetectionResult{Provider: "OpenAI", Method: "xmp", Details: "XMP mentions OpenAI/DALL-E"}
	}

	// Stable Diffusion / SDXL in XMP: require strong markers or combined SD-specific signals.
	if containsAnyFold(s, []string{"sui_image_params", "negativeprompt", "negative_prompt", "positive_prompt", "textual_inversion"}) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "xmp", Details: "Prompt/SD terms in XMP"}
	}
	if strings.Contains(s, "stable diffusion") && containsAnyFold(s, []string{"sampler", "steps", "cfg", "seed", "checkpoint", "lora"}) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "xmp", Details: "Stable Diffusion params in XMP"}
	}

	// Flux in XMP: mentions of Flux or Black Forest Labs
	if aiSoftwareRegex.MatchString(s) && strings.Contains(strings.ToLower(s), "flux") {
		return true, AIDetectionResult{Provider: "FLUX", Method: "xmp", Details: "Flux terms in XMP"}
	}

	// Generic IPTC trained media marker
	if strings.Contains(s, strings.ToLower(iptcTrainedMedia)) {
		return true, AIDetectionResult{Provider: "AI (IPTC Trained Media)", Method: "xmp", Details: iptcTrainedMedia}
	}

	// Midjourney parameters in XMP (very specific)
	if strings.Contains(s, "--chaos") || strings.Contains(s, "--ar") || strings.Contains(s, "--profile") || strings.Contains(s, "--stylize") || strings.Contains(s, "--weird") || strings.Contains(s, "--v ") || strings.Contains(s, "--no ") || strings.Contains(s, "--seed") || strings.Contains(s, "Job ID:") {
		return true, AIDetectionResult{Provider: "Midjourney", Method: "xmp", Details: "Midjourney parameters in XMP"}
	}

	return false, AIDetectionResult{}
}

func looksLikePromptJSON(s string) bool {
	if s == "" {
		return false
	}
	// Try to parse as JSON first
	var tmp interface{}
	if json.Unmarshal([]byte(s), &tmp) == nil {
		// Valid JSON, check for AI generation markers
		low := strings.ToLower(s)
		if promptRegex.MatchString(low) || workflowRegex.MatchString(low) || suiParamsRegex.MatchString(low) ||
			aiSoftwareRegex.MatchString(low) || containsAnyFold(low, aiModelPatterns) {
			return true
		}
	}
	// If not valid JSON, check for prompt-like content anyway
	low := strings.ToLower(s)
	if promptRegex.MatchString(low) && workflowRegex.MatchString(low) {
		return true
	}
	return false
}

func containsAnyFold(haystack string, needles []string) bool {
	hs := strings.ToLower(haystack)
	for _, n := range needles {
		if strings.Contains(hs, strings.ToLower(n)) {
			return true
		}
	}
	return false
}

// detectFromBinaryText scans the raw file bytes for common prompt/workflow markers
func detectFromBinaryText(imagePath string) (bool, AIDetectionResult) {
	b, err := os.ReadFile(imagePath)
	if err != nil {
		return false, AIDetectionResult{}
	}
	return detectFromBinaryTextBytes(b)
}

// buildUTF16LEPattern returns the UTF-16LE bytes for the lowercase ASCII needle
func buildUTF16LEPattern(needle string) []byte {
	lower := strings.ToLower(needle)
	out := make([]byte, 0, len(lower)*2)
	for i := 0; i < len(lower); i++ {
		out = append(out, lower[i], 0x00)
	}
	return out
}

// buildUTF16BEPattern returns the UTF-16BE bytes for the lowercase ASCII needle
func buildUTF16BEPattern(needle string) []byte {
	lower := strings.ToLower(needle)
	out := make([]byte, 0, len(lower)*2)
	for i := 0; i < len(lower); i++ {
		out = append(out, 0x00, lower[i])
	}
	return out
}

// decodeUTF16 attempts to decode UTF-16LE or UTF-16BE encoded data
func decodeUTF16(data []byte) (string, error) {
	if len(data) < 2 {
		return "", fmt.Errorf("data too short for UTF-16")
	}

	// Check BOM (Byte Order Mark)
	var isLE bool
	if bytes.HasPrefix(data, []byte{0xFF, 0xFE}) {
		// UTF-16 Little Endian
		isLE = true
		data = data[2:]
	} else if bytes.HasPrefix(data, []byte{0xFE, 0xFF}) {
		// UTF-16 Big Endian
		isLE = false
		data = data[2:]
	} else {
		// No BOM, assume Little Endian (common in Windows EXIF)
		isLE = true
	}

	// UTF-16 decoding
	if len(data)%2 != 0 {
		return "", fmt.Errorf("invalid UTF-16 data length")
	}

	runes := make([]rune, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		var r uint16
		if isLE {
			r = uint16(data[i]) | uint16(data[i+1])<<8
		} else {
			r = uint16(data[i])<<8 | uint16(data[i+1])
		}

		// Handle surrogate pairs for Unicode characters beyond BMP
		if utf16.IsSurrogate(rune(r)) {
			if i+4 > len(data) {
				return "", fmt.Errorf("incomplete UTF-16 surrogate pair")
			}
			var r2 uint16
			if isLE {
				r2 = uint16(data[i+2]) | uint16(data[i+3])<<8
			} else {
				r2 = uint16(data[i+2])<<8 | uint16(data[i+3])
			}
			runes = append(runes, utf16.DecodeRune(rune(r), rune(r2)))
			i += 2
		} else {
			runes = append(runes, rune(r))
		}
	}

	return string(runes), nil
}

// DetectAIFast performs quick AI detection using pre-compiled regex patterns
func DetectAIFast(imageBytes []byte) (bool, AIDetectionResult) {
	if len(imageBytes) < 128 {
		return false, AIDetectionResult{}
	}
	return detectFromBinaryTextBytes(imageBytes)
}

// DetectAIProvenanceConcurrent performs AI detection concurrently for maximum performance
func DetectAIProvenanceConcurrent(imageBytes []byte, xmpXML []byte) (bool, AIDetectionResult) {
	// Create channels for concurrent detection
	c2paChan := make(chan AIDetectionResult, 1)
	exifChan := make(chan AIDetectionResult, 1)
	binaryChan := make(chan AIDetectionResult, 1)
	xmpChan := make(chan AIDetectionResult, 1)

	var wg sync.WaitGroup
	wg.Add(4)

	// Start C2PA detection
	go func() {
		defer wg.Done()

		if c2paSniffRegex.Find(imageBytes) != nil {
			provider := classifyC2PAProvider(xmpXML)
			if provider == "" {
				provider = "Unknown C2PA"
			}
			c2paChan <- AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA/JUMBF markers present"}
			return
		}

		// Enhanced C2PA detection for binary JUMBF chunks
		if bytes.Contains(imageBytes, []byte("jumb")) && bytes.Contains(imageBytes, []byte("c2pa")) {
			provider := classifyC2PAProvider(xmpXML)
			if provider == "" {
				provider = "Unknown C2PA"
			}
			c2paChan <- AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA JUMBF binary chunks detected"}
			return
		}

		// Check for C2PA URN pattern (binary)
		if bytes.Contains(imageBytes, []byte("urn:c2pa:")) {
			provider := classifyC2PAProvider(xmpXML)
			if provider == "" {
				provider = "Unknown C2PA"
			}
			c2paChan <- AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA URN detected"}
			return
		}

		c2paChan <- AIDetectionResult{}
	}()

	// Start EXIF detection
	go func() {
		defer wg.Done()
		if ok, result := detectFromEXIFBytes(imageBytes); ok {
			exifChan <- result
			return
		} else {
		}
		exifChan <- AIDetectionResult{}
	}()

	// Start Binary detection
	go func() {
		defer wg.Done()
		if ok, result := detectFromBinaryTextBytes(imageBytes); ok {
			binaryChan <- result
			return
		} else {
		}
		binaryChan <- AIDetectionResult{}
	}()

	// Start XMP detection
	go func() {
		defer wg.Done()
		if ok, result := detectFromXMP(xmpXML); ok {
			xmpChan <- result
			return
		} else {
		}
		xmpChan <- AIDetectionResult{}
	}()

	// Wait for the first positive result or all to complete (with timeout)
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	// Add timeout to prevent hanging on edge cases
	timeout := time.After(5 * time.Second) // 5 second timeout

	select {
	case <-done:
		// All detections completed, collect all results
		c2paResult := <-c2paChan
		exifResult := <-exifChan
		binaryResult := <-binaryChan
		xmpResult := <-xmpChan

		// Check if ANY detection method succeeded
		if c2paResult.Provider != "" {
			return true, c2paResult
		}
		if exifResult.Provider != "" {
			return true, exifResult
		}
		if binaryResult.Provider != "" {
			return true, binaryResult
		}
		if xmpResult.Provider != "" {
			return true, xmpResult
		}
		return false, AIDetectionResult{}
	case <-timeout:
		// Timeout reached, assume no AI to prevent hanging
		log.Printf("AI Detection: Concurrent detection timed out after 5 seconds")
		return false, AIDetectionResult{}
	}
}

// detectFromEXIFBytesOptimized is an optimized version that exits early
func detectFromEXIFBytesOptimized(b []byte) (bool, AIDetectionResult) {
	return detectFromEXIFBytes(b)
}
