package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/dsoprea/go-exif/v3"
)

// AIDetectionResult describes detected AI provenance for an image.
type AIDetectionResult struct {
	Provider string // e.g., "Midjourney", "OpenAI", "Adobe Firefly", "Google Imagen", "Grok", "Stable Diffusion (SDXL)", "ComfyUI", "Unknown C2PA"
	Method   string // e.g., "xmp", "exif", "c2pa"
	Details  string // matched field/value or brief explanation
}

var (
	guidRegex        = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
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
	// 1) Structural C2PA / Content Credentials manifest in file body.
	if b, err := os.ReadFile(imagePath); err == nil {
		if c2paOK, details := detectC2PA(b); c2paOK {
			provider := classifyC2PAProvider(xmpXML)
			if provider == "" {
				provider = classifyC2PAProviderFromBytes(b)
			}
			if provider == "" {
				provider = "Unknown C2PA"
			}
			return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: details}
		}
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
	// 1) Structural C2PA / Content Credentials manifest in the file body.
	if c2paOK, details := detectC2PA(imageBytes); c2paOK {
		provider := classifyC2PAProvider(xmpXML)
		if provider == "" {
			provider = classifyC2PAProviderFromBytes(imageBytes)
		}
		if provider == "" {
			provider = "Unknown C2PA"
		}
		return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: details}
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
	if low == "" {
		return "", false
	}
	switch {
	case strings.Contains(low, "midjourney"):
		return "Midjourney", true
	case strings.Contains(low, "dall-e") || strings.Contains(low, "dalle") || strings.Contains(low, "gpt-image") || strings.Contains(low, "openai") || strings.Contains(low, "chatgpt"):
		return "OpenAI", true
	case strings.Contains(low, "stable diffusion") || strings.Contains(low, "sdxl") ||
		strings.Contains(low, "automatic1111") || strings.Contains(low, "a1111") ||
		strings.Contains(low, "stable-diffusion-webui") || strings.Contains(low, "comfyui") ||
		strings.Contains(low, "invokeai") || strings.Contains(low, "fooocus") ||
		strings.Contains(low, "novelai") || strings.Contains(low, "draw things"):
		return "Stable Diffusion (SDXL)", true
	case strings.Contains(low, "flux") || strings.Contains(low, "black forest labs") || strings.Contains(low, "bfl"):
		return "FLUX", true
	case strings.Contains(low, "firefly") || strings.Contains(low, "adobe firefly"):
		return "Adobe Firefly", true
	case strings.Contains(low, "imagen") || strings.Contains(low, "made with google ai") || strings.Contains(low, "gemini") || strings.Contains(low, "nano banana"):
		return "Google Imagen", true
	case strings.Contains(low, "grok"):
		return "Grok", true
	case strings.Contains(low, "ideogram"):
		return "Ideogram", true
	case strings.Contains(low, "leonardo"):
		return "Leonardo.Ai", true
	case strings.Contains(low, "recraft"):
		return "Recraft", true
	case strings.Contains(low, "playground"):
		return "Playground AI", true
	case strings.Contains(low, "nightcafe"):
		return "NightCafe", true
	case strings.Contains(low, "seaart") || strings.Contains(low, "tensor.art") || strings.Contains(low, "tensorart"):
		return "Stable Diffusion (SDXL)", true
	case strings.Contains(low, "krea"):
		return "Krea", true
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

	// s is already lowercased above, so use the no-relower fast path with lowercase needles.
	if containsAnyLower(s, []string{"grok image prompt", "grok image upsampled prompt"}) {
		return true, AIDetectionResult{Provider: "Grok", Method: "binary", Details: "Grok prompt fields in metadata"}
	}

	if strings.Contains(s, "midjourney") && containsAnyLower(s, []string{"--ar", "--chaos", "--stylize", "--seed", "job id:"}) {
		return true, AIDetectionResult{Provider: "Midjourney", Method: "binary", Details: "Midjourney generation params present"}
	}

	if containsAnyLower(s, []string{"sui_image_params", "negative_prompt", "positive_prompt", "textual_inversion"}) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "SD generation params present"}
	}

	// Automatic1111 / SD WebUI / Forge / NovelAI parameter block. These tools write a
	// very distinctive colon-delimited settings string ("Steps: 20, Sampler: Euler a,
	// CFG scale: 7, Seed: 12345"). The combination is effectively impossible to hit by
	// chance in camera metadata, so it is a strong, low-false-positive signal — and it
	// does not contain the literal words "stable diffusion", which is why the older
	// rules missed these images.
	if strings.Contains(s, "steps:") && strings.Contains(s, "sampler:") &&
		(strings.Contains(s, "cfg scale:") || strings.Contains(s, "seed:") || strings.Contains(s, "denoising strength")) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "SD WebUI parameter block present"}
	}
	// A1111 also emits "Negative prompt:" (with a space) which the underscore rule above misses.
	if strings.Contains(s, "negative prompt:") && (strings.Contains(s, "steps:") || strings.Contains(s, "sampler:") || strings.Contains(s, "cfg scale:")) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "SD WebUI negative-prompt block present"}
	}

	if (strings.Contains(s, "\"prompt\"") || strings.Contains(s, " prompt")) &&
		(strings.Contains(s, "\"workflow\"") || containsAnyLower(s, []string{"comfyui", "checkpoint_loader", "k_sampler", "vae_decode"})) {
		return true, AIDetectionResult{Provider: "ComfyUI", Method: "binary", Details: "Prompt/workflow metadata present"}
	}

	if strings.Contains(s, "stable diffusion") && containsAnyLower(s, []string{"sampler", "steps", "cfg", "seed", "checkpoint", "lora"}) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "SD metadata terms present"}
	}

	if (strings.Contains(s, "dall-e") || strings.Contains(s, "dalle") || strings.Contains(s, "openai")) &&
		containsAnyLower(s, []string{"prompt", "image generation"}) {
		return true, AIDetectionResult{Provider: "OpenAI", Method: "binary", Details: "OpenAI generation metadata present"}
	}

	if strings.Contains(s, "flux") && containsAnyLower(s, []string{"black forest labs", "bfl", "prompt", "sampler", "steps", "cfg", "seed"}) {
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
	if containsAnyLower(rawLower, []string{"sui_image_params", "negative_prompt", "positive_prompt", "textual_inversion"}) {
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

	// PNG tEXt/zTXt/iTXt metadata, decompressing zTXt/iTXt which a raw byte scan
	// cannot see. Common for ComfyUI/A1111/NovelAI PNG exports.
	if pngText := extractPNGTextMetadata(b); pngText != "" {
		if ok, res := detectAIBinaryMarkersFromText(pngText); ok {
			return true, res
		}
	}

	// Scan a bounded text window for performance and lower false positives.
	return detectAIBinaryMarkersFromText(limitedLowerText(b, 768*1024))
}

// detectC2PA looks for a genuine C2PA / Content Credentials manifest embedded as a
// JUMBF container rather than a bare "c2pa"/"jumbf" substring. The previous
// substring sniff accepted any file that merely contained one of those words
// anywhere (e.g. a photo with "jumbf" typed into an EXIF comment), which let
// non-AI images through. This requires the JUMBF box marker to co-occur with a
// C2PA-specific label or URN, or an explicit c2pa.* manifest assertion.
//
// Note: this is still a structural heuristic, not cryptographic verification of the
// signing certificate chain. Full trust would require validating the C2PA claim
// signature against known generators (out of scope without a C2PA library), but
// requiring the manifest structure eliminates the single-token false positives.
func detectC2PA(b []byte) (bool, string) {
	if len(b) == 0 {
		return false, ""
	}
	// Explicit C2PA URN is unambiguous.
	if bytes.Contains(b, []byte("urn:c2pa:")) {
		return true, "C2PA manifest URN present"
	}
	// C2PA manifest labels/assertions defined by the C2PA spec.
	for _, label := range [][]byte{
		[]byte("c2pa.assertions"), []byte("c2pa.claim"), []byte("c2pa.signature"),
		[]byte("c2pa.manifest"), []byte("com.adobe.c2pa"), []byte("cai/c2pa"),
	} {
		if bytes.Contains(b, label) {
			return true, "C2PA manifest assertions present"
		}
	}
	// JUMBF container (box type "jumb" + description box "jumd") carrying a c2pa label.
	if bytes.Contains(b, []byte("jumb")) && bytes.Contains(b, []byte("jumd")) &&
		(bytes.Contains(b, []byte("c2pa")) || bytes.Contains(b, []byte("contentauth"))) {
		return true, "C2PA JUMBF manifest present"
	}
	return false, ""
}

func sniffC2PA(imagePath string) bool {
	b, err := os.ReadFile(imagePath)
	if err != nil {
		return false
	}
	ok, _ := detectC2PA(b)
	return ok
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

// classifyC2PAProviderFromBytes inspects the C2PA manifest bytes (claim_generator,
// software agent) to name the generating tool when XMP does not identify it.
func classifyC2PAProviderFromBytes(b []byte) string {
	// The claim_generator string lives near the manifest; scan a bounded window.
	s := limitedLowerText(b, 512*1024)
	switch {
	case strings.Contains(s, "firefly") || strings.Contains(s, "adobe"):
		return "Adobe Firefly"
	case strings.Contains(s, "openai") || strings.Contains(s, "dall-e") || strings.Contains(s, "gpt-image") || strings.Contains(s, "chatgpt"):
		return "OpenAI"
	case strings.Contains(s, "made with google ai") || strings.Contains(s, "imagen") || strings.Contains(s, "gemini") || strings.Contains(s, "google"):
		return "Google Imagen"
	case strings.Contains(s, "microsoft") || strings.Contains(s, "bing image") || strings.Contains(s, "designer"):
		return "Microsoft Designer"
	case strings.Contains(s, "leonardo"):
		return "Leonardo.Ai"
	case strings.Contains(s, "midjourney"):
		return "Midjourney"
	case strings.Contains(s, "stability") || strings.Contains(s, "stable diffusion"):
		return "Stable Diffusion (SDXL)"
	case strings.Contains(s, "black forest") || strings.Contains(s, "flux"):
		return "FLUX"
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

	// SD WebUI (A1111/Forge/NovelAI) parameter block embedded in XMP.
	if strings.Contains(s, "steps:") && strings.Contains(s, "sampler:") &&
		(strings.Contains(s, "cfg scale:") || strings.Contains(s, "seed:")) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "xmp", Details: "SD WebUI parameter block in XMP"}
	}

	// Flux in XMP: mentions of Flux or Black Forest Labs
	if aiSoftwareRegex.MatchString(s) && strings.Contains(strings.ToLower(s), "flux") {
		return true, AIDetectionResult{Provider: "FLUX", Method: "xmp", Details: "Flux terms in XMP"}
	}

	// Named generators that reliably identify themselves in XMP creator/software.
	// XMP is a structured text field (unlike raw compressed pixel bytes), so a token
	// match here is a deliberate provenance signal rather than a coincidence.
	for token, provider := range map[string]string{
		"ideogram": "Ideogram", "leonardo.ai": "Leonardo.Ai", "leonardo ai": "Leonardo.Ai",
		"recraft": "Recraft", "playground ai": "Playground AI", "nightcafe": "NightCafe",
		"invokeai": "Stable Diffusion (SDXL)", "fooocus": "Stable Diffusion (SDXL)",
		"novelai": "Stable Diffusion (SDXL)", "krea.ai": "Krea", "getimg.ai": "Stable Diffusion (SDXL)",
		"nano banana": "Google Imagen",
	} {
		if strings.Contains(s, token) {
			return true, AIDetectionResult{Provider: provider, Method: "xmp", Details: "XMP mentions " + provider}
		}
	}

	// Generic IPTC digital source type markers for synthetic/AI media.
	if strings.Contains(s, strings.ToLower(iptcTrainedMedia)) {
		return true, AIDetectionResult{Provider: "AI (IPTC Trained Media)", Method: "xmp", Details: iptcTrainedMedia}
	}
	if strings.Contains(s, "digitalsourcetype/compositewithtrainedalgorithmicmedia") ||
		strings.Contains(s, "digitalsourcetype/algorithmicmedia") {
		return true, AIDetectionResult{Provider: "AI (IPTC Synthetic Media)", Method: "xmp", Details: "IPTC synthetic/composite digital source type"}
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
	return containsAnyLower(hs, needles)
}

// containsAnyLower is containsAnyFold's fast path for callers whose haystack is
// already lowercased (and whose needles are lowercase literals). It avoids
// re-allocating a lowercase copy of the — potentially very large — haystack on
// every call, which the binary-text scanner does repeatedly per upload.
func containsAnyLower(lowerHaystack string, lowerNeedles []string) bool {
	for _, n := range lowerNeedles {
		if strings.Contains(lowerHaystack, n) {
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

// DetectAIProvenanceConcurrent runs full provenance detection with a hard timeout
// so a pathologically crafted image (e.g. one that makes the EXIF parser pathologically
// slow) cannot stall an upload worker. It delegates to the ordered, short-circuiting
// DetectAIProvenanceFromBytes (C2PA → EXIF → binary → XMP) which returns on the first
// hit — far cheaper for the common case than fanning out four goroutines that each
// re-scan the whole buffer and then waiting for all of them. The name is retained for
// call-site compatibility.
func DetectAIProvenanceConcurrent(imageBytes []byte, xmpXML []byte) (bool, AIDetectionResult) {
	type detection struct {
		ok  bool
		res AIDetectionResult
	}
	ch := make(chan detection, 1)
	go func() {
		ok, res := DetectAIProvenanceFromBytes(imageBytes, xmpXML)
		ch <- detection{ok, res}
	}()

	select {
	case d := <-ch:
		return d.ok, d.res
	case <-time.After(5 * time.Second):
		log.Printf("AI Detection: detection timed out after 5 seconds")
		return false, AIDetectionResult{}
	}
}
