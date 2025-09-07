package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
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
	c2paMatch := c2paSniffRegex.Find(imageBytes)
	if c2paMatch != nil {
		log.Printf("AI Detection Debug: C2PA pattern found: %s", string(c2paMatch))
		provider := classifyC2PAProvider(xmpXML)
		if provider == "" {
			provider = "Unknown C2PA"
		}
		return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA/JUMBF markers present"}
	}

	// Enhanced C2PA detection for binary JUMBF chunks
	// C2PA manifests are stored in PNG chunks as binary data
	if bytes.Contains(imageBytes, []byte("jumb")) && bytes.Contains(imageBytes, []byte("c2pa")) {
		log.Printf("AI Detection Debug: C2PA JUMBF binary chunks detected")
		provider := classifyC2PAProvider(xmpXML)
		if provider == "" {
			provider = "Unknown C2PA"
		}
		return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA JUMBF binary chunks detected"}
	}

	// Check for C2PA URN pattern (binary)
	if bytes.Contains(imageBytes, []byte("urn:c2pa:")) {
		log.Printf("AI Detection Debug: C2PA URN pattern detected")
		provider := classifyC2PAProvider(xmpXML)
		if provider == "" {
			provider = "Unknown C2PA"
		}
		return true, AIDetectionResult{Provider: provider, Method: "c2pa", Details: "C2PA URN detected"}
	}

	// DEBUG: Check if C2PA should be found but isn't
	if len(imageBytes) > 1000 {
		// Look for "c2pa" manually in the first few KB
		preview := string(imageBytes[:min(4096, len(imageBytes))])
		if strings.Contains(strings.ToLower(preview), "c2pa") {
			log.Printf("AI Detection Debug: C2PA found manually in preview but not by regex")
		}
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

func detectFromEXIFBytes(b []byte) (bool, AIDetectionResult) {
	rawExif, err := exif.SearchAndExtractExif(b)
	if err != nil {
		return false, AIDetectionResult{}
	}

	// quick raw scan
	if suiParamsRegex.MatchString(string(rawExif)) ||
		strings.Contains(string(rawExif), "prompt") ||
		bytes.Contains(rawExif, buildUTF16LEPattern("sui_image_params")) ||
		bytes.Contains(rawExif, buildUTF16BEPattern("sui_image_params")) ||
		bytes.Contains(rawExif, buildUTF16LEPattern("prompt")) ||
		bytes.Contains(rawExif, buildUTF16BEPattern("prompt")) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: "sui_image_params/prompt in raw EXIF"}
	}

	// Check for UTF-16 patterns (common SDXL parameters)
	patterns := []string{"steps", "cfg", "seed", "sampler", "dtirflash"}
	for _, pattern := range patterns {
		if bytes.Contains(rawExif, buildUTF16LEPattern(pattern)) || bytes.Contains(rawExif, buildUTF16BEPattern(pattern)) {
			return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: "AI parameters in UTF-16 EXIF"}
		}
	}

	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		return false, AIDetectionResult{}
	}
	var softwareVal string
	for _, e := range entries {
		tn := strings.TrimSpace(e.TagName)
		val := strings.TrimSpace(e.Formatted)
		if strings.EqualFold(tn, "Software") {
			softwareVal = val
			low := strings.ToLower(val)
			switch {
			case strings.Contains(low, "midjourney"):
				return true, AIDetectionResult{Provider: "Midjourney", Method: "exif", Details: val}
			case strings.Contains(low, "dall-e") || strings.Contains(low, "dalle") || strings.Contains(low, "openai"):
				return true, AIDetectionResult{Provider: "OpenAI", Method: "exif", Details: val}
			case strings.Contains(low, "stable diffusion") || strings.Contains(low, "sdxl"):
				return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: val}
			case strings.Contains(low, "flux"):
				return true, AIDetectionResult{Provider: "FLUX", Method: "exif", Details: val}
			}
		}
		if containsAnyFold(val, []string{"prompt", "negativeprompt", "negative_prompt", "sampler", "steps", "cfg", "seed", "model"}) {
			return true, AIDetectionResult{Provider: "AI (Prompt in EXIF)", Method: "exif", Details: tn}
		}
		if strings.EqualFold(tn, "DigitalSourceType") && strings.TrimSpace(val) == iptcTrainedMedia {
			return true, AIDetectionResult{Provider: "AI (IPTC Trained Media)", Method: "exif", Details: val}
		}
	}
	if softwareVal != "" {
		low := strings.ToLower(softwareVal)
		if genericAIRegex.MatchString(low) || aiSoftwareRegex.MatchString(low) || containsAnyFold(low, aiModelPatterns) {
			return true, AIDetectionResult{Provider: "AI (Software)", Method: "exif", Details: softwareVal}
		}
	}
	return false, AIDetectionResult{}
}

func detectFromBinaryTextBytes(b []byte) (bool, AIDetectionResult) {
	s := strings.ToLower(string(b))
	if strings.Contains(s, "grok image prompt") || strings.Contains(s, "grok image upsampled prompt") || strings.Contains(s, "\x00grok\x00") || strings.Contains(s, " g r o k ") || strings.Contains(s, "grok:") || strings.Contains(s, "\"grok\"") {
		return true, AIDetectionResult{Provider: "Grok", Method: "binary", Details: "Grok prompt fields in image"}
	}
	if containsAnyFold(s, comfyuiPatterns) || (strings.Contains(s, "prompt") && strings.Contains(s, "workflow")) {
		return true, AIDetectionResult{Provider: "ComfyUI", Method: "binary", Details: "ComfyUI markers present"}
	}
	if containsAnyFold(s, sdxlTerms) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "SDXL/SD params present"}
	}
	if aiSoftwareRegex.MatchString(s) && strings.Contains(strings.ToLower(s), "flux") {
		return true, AIDetectionResult{Provider: "FLUX", Method: "binary", Details: "Flux markers present"}
	}
	// Generic AI terms detection - DISABLED to prevent false positives
	// if containsAnyFold(s, genericAITerms) {
	// 	return true, AIDetectionResult{Provider: "AI (Generic Terms)", Method: "binary", Details: "Generic AI terminology present"}
	// }

	// Enhanced prompt detection - requires additional context to avoid false positives
	if containsAnyFold(s, promptVariations) {
		// Only accept as AI if there are ALSO technical AI terms present
		if containsAnyFold(s, sdxlTerms) || containsAnyFold(s, comfyuiPatterns) || containsAnyFold(s, []string{"sampler", "steps", "cfg", "seed", "checkpoint", "lora", "vae", "embeddings"}) {
			return true, AIDetectionResult{Provider: "AI (Prompt + Technical Terms)", Method: "binary", Details: "Prompt with technical AI parameters present"}
		}
	}
	return false, AIDetectionResult{}
}

func sniffC2PA(imagePath string) bool {
	b, err := ioutil.ReadFile(imagePath)
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
	rawExif, err := exif.SearchFileAndExtractExif(imagePath)
	if err != nil {
		log.Printf("AI Detection: EXIF extraction failed for %s: %v", imagePath, err)
		return false, AIDetectionResult{}
	}

	// Try to search for UserComment in raw EXIF data directly
	if suiParamsRegex.MatchString(string(rawExif)) ||
		strings.Contains(string(rawExif), "prompt") ||
		bytes.Contains(rawExif, buildUTF16LEPattern("sui_image_params")) ||
		bytes.Contains(rawExif, buildUTF16BEPattern("sui_image_params")) ||
		bytes.Contains(rawExif, buildUTF16LEPattern("prompt")) ||
		bytes.Contains(rawExif, buildUTF16BEPattern("prompt")) {
		log.Printf("AI Detection: Found SDXL markers in raw EXIF data")
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: "sui_image_params/prompt in raw EXIF"}
	}

	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		log.Printf("AI Detection: EXIF parsing failed for %s: %v", imagePath, err)
		return false, AIDetectionResult{}
	}

	// Avoid verbose logging of user-provided metadata to reduce leakage/noise
	log.Printf("AI Detection: EXIF parsed for %s", imagePath)
	var softwareVal string
	for _, e := range entries {
		tn := strings.TrimSpace(e.TagName)
		val := strings.TrimSpace(e.Formatted)

		// Log UserComment specifically since that's where SDXL params often are
		if strings.EqualFold(tn, "UserComment") {
			// Avoid logging raw user comment content
			log.Printf("AI Detection: UserComment present (formatted)")

			// Try to get raw value for UserComment since formatted might not work
			if e.Value != nil {
				rawStr := ""
				// Try different ways to extract the value
				switch v := e.Value.(type) {
				case []byte:
					rawStr = string(v)
				case string:
					rawStr = v
				default:
					// Fallback to string representation
					rawStr = fmt.Sprintf("%v", e.Value)
				}
				if len(rawStr) > 0 && rawStr != val {
					// Avoid logging raw user comment content
					log.Printf("AI Detection: UserComment raw present")
					val = rawStr // Use raw value instead of formatted
				}
			}
		}

		// Software hints (Midjourney, DALL-E, Stable Diffusion, Flux, etc.)
		if strings.EqualFold(tn, "Software") {
			softwareVal = val
			low := strings.ToLower(val)
			switch {
			case strings.Contains(low, "midjourney"):
				return true, AIDetectionResult{Provider: "Midjourney", Method: "exif", Details: val}
			case strings.Contains(low, "dall-e") || strings.Contains(low, "dalle") || strings.Contains(low, "openai"):
				return true, AIDetectionResult{Provider: "OpenAI", Method: "exif", Details: val}
			case strings.Contains(low, "stable diffusion") || strings.Contains(low, "sdxl"):
				return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: val}
			case strings.Contains(low, "flux"):
				return true, AIDetectionResult{Provider: "FLUX", Method: "exif", Details: val}
			}
		}
		// Any EXIF value containing common generation params or 'prompt' - removed 'model' to prevent false positives
		if containsAnyFold(val, []string{"prompt", "negativeprompt", "negative_prompt", "sampler", "steps", "cfg", "seed"}) {
			return true, AIDetectionResult{Provider: "AI (Prompt in EXIF)", Method: "exif", Details: tn}
		}
		// UserComment / ImageDescription / XPComment often store generation params
		if strings.EqualFold(tn, "UserComment") || strings.EqualFold(tn, "ImageDescription") || strings.EqualFold(tn, "XPComment") {
			// Try to decode UTF-16 UserComment data
			decodedVal := val
			if strings.EqualFold(tn, "UserComment") && e.Value != nil {
				switch v := e.Value.(type) {
				case []byte:
					// Handle UTF-16 encoded UserComment
					if len(v) > 8 {
						// UserComment format: first 8 bytes = encoding ID, then the actual text
						if bytes.HasPrefix(v[8:], []byte{0xFF, 0xFE}) || bytes.HasPrefix(v[8:], []byte{0xFE, 0xFF}) {
							// UTF-16 encoded after header
							if decoded, err := decodeUTF16(v[8:]); err == nil && len(decoded) > 0 {
								decodedVal = decoded
							}
						} else if decoded, err := decodeUTF16(v); err == nil && len(decoded) > 0 {
							// Try to decode entire byte array as UTF-16
							decodedVal = decoded
						}
					}
				}
			}

			// Use decoded value for further checks
			val = decodedVal
			isPromptJSON := looksLikePromptJSON(val)
			hasParams := containsAnyFold(val, []string{"prompt", "negativeprompt", "negative_prompt", "sampler", "steps", "cfg", "sui_image_params", "sui_extra_data"})
			// Check for Midjourney parameters (very specific)
			hasMidjourneyParams := containsAnyFold(val, []string{"--chaos", "--ar", "--profile", "--stylize", "--weird", "--v ", "--no ", "--seed", "Job ID:"})
			if isPromptJSON || hasParams || hasMidjourneyParams {
				provider := "Stable Diffusion (SDXL)"
				if hasMidjourneyParams {
					provider = "Midjourney"
				}
				return true, AIDetectionResult{Provider: provider, Method: "exif", Details: tn + " contains generation params"}
			}
		}
		// Grok: match in tag name OR value
		if strings.Contains(strings.ToLower(tn), "grok") || strings.Contains(strings.ToLower(val), "grok") {
			return true, AIDetectionResult{Provider: "Grok", Method: "exif", Details: tn + ": " + val}
		}
		// ComfyUI fields commonly named Prompt/Workflow in EXIF as well
		if strings.EqualFold(tn, "Prompt") || strings.EqualFold(tn, "Workflow") {
			return true, AIDetectionResult{Provider: "ComfyUI", Method: "exif", Details: tn}
		}
		// IPTC via EXIF flatten sometimes includes DigitalSourceType
		if strings.EqualFold(tn, "DigitalSourceType") && val == iptcTrainedMedia {
			// Try to refine with other hints later; default to generic
			// Try to pair with Google credit or MJ GUID later in XMP detection
			return true, AIDetectionResult{Provider: "AI (IPTC Trained Media)", Method: "exif", Details: val}
		}
		// Flux hints from Software value
		if strings.EqualFold(tn, "Software") {
			low := strings.ToLower(val)
			if strings.Contains(low, "flux") || strings.Contains(low, "black forest labs") || strings.Contains(low, "bfl") {
				return true, AIDetectionResult{Provider: "FLUX", Method: "exif", Details: val}
			}
		}
	}

	// Fallback: if Software value suggests, accept generically
	if softwareVal != "" {
		low := strings.ToLower(softwareVal)
		if genericAIRegex.MatchString(low) || aiSoftwareRegex.MatchString(low) || containsAnyFold(low, aiModelPatterns) {
			return true, AIDetectionResult{Provider: "AI (Software)", Method: "exif", Details: softwareVal}
		}
	}
	return false, AIDetectionResult{}
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
	if strings.Contains(s, ">prompt<") && strings.Contains(s, ">workflow<") || containsAnyFold(s, comfyuiPatterns) {
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

	// Stable Diffusion / SDXL in XMP: look for prompt-like keys or SD terms
	if strings.Contains(s, "\"prompt\"") || strings.Contains(s, "negativeprompt") || strings.Contains(s, "negative_prompt") || strings.Contains(s, ">prompt<") || containsAnyFold(s, sdxlTerms) {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "xmp", Details: "Prompt/SD terms in XMP"}
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
// FIXED: Now much more restrictive to avoid binary false positives
func detectFromBinaryText(imagePath string) (bool, AIDetectionResult) {
	b, err := ioutil.ReadFile(imagePath)
	if err != nil {
		return false, AIDetectionResult{}
	}
	s := strings.ToLower(string(b))

	// DEBUG: Log file type and size
	log.Printf("AI Detection Debug: Binary text detection - file size: %d bytes", len(b))

	// FIXED: Skip binary JPEG headers to avoid false positives (first ~1000 bytes)
	scanStart := 1000
	if len(b) > scanStart {
		// Check if this is a PNG file using proper PNG signature BEFORE string conversion
		isPNG := false
		if len(b) >= 8 {
			sig0 := b[0]
			sig1 := b[1]
			sig2 := b[2]
			sig3 := b[3]
			sig4 := b[4]
			sig5 := b[5]
			sig6 := b[6]
			sig7 := b[7]

			log.Printf("AI Detection Debug: Binary text PNG signature check - bytes: %x %x %x %x %x %x %x %x", sig0, sig1, sig2, sig3, sig4, sig5, sig6, sig7)
			log.Printf("AI Detection Debug: Expected PNG signature: %x %x %x %x %x %x %x %x", 0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A)

			isPNG = sig0 == 0x89 && sig1 == 0x50 && sig2 == 0x4E && sig3 == 0x47 &&
				sig4 == 0x0D && sig5 == 0x0A && sig6 == 0x1A && sig7 == 0x0A

			log.Printf("AI Detection Debug: Binary text PNG signature detected: %v", isPNG)
		}

		if isPNG {
			log.Printf("AI Detection Debug: Binary detection - Detected PNG file via binary signature, scanning for text chunks")
			// For PNG files, scan the entire file but skip just the signature
			scanStart = 8 // Skip PNG signature (8 bytes)
		} else {
			log.Printf("AI Detection Debug: Binary detection - Detected non-PNG file, skipping first %d bytes", scanStart)
		}
		s = s[scanStart:]
	}

	// DEBUG: Check for Midjourney parameters in the remaining content
	midjourneyParams := []string{"--chaos", "--ar", "--profile", "--stylize", "--weird", "--v ", "--no ", "--seed", "Job ID:"}
	for _, param := range midjourneyParams {
		if strings.Contains(s, param) {
			log.Printf("AI Detection Debug: Found Midjourney parameter in binary detection: %s", param)
		}
	}

	// FIXED: Only look for very specific AI markers that wouldn't appear in regular images
	// These are highly specific to AI generation tools

	// 1. Look for specific AI generation parameters
	if strings.Contains(s, "sui_image_params") {
		log.Printf("AI Detection Debug: Found sui_image_params in binary")
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "sui_image_params found"}
	}

	// 2. Look for specific AI workflow patterns (full phrases)
	aiPhrases := []string{
		"stable diffusion",
		"midjourney",
		"dall-e",
		"textual_inversion",
		"negative_prompt",
		"positive_prompt",
		// Midjourney parameters (highly specific)
		"--chaos", "--ar", "--profile", "--stylize", "--weird", "--v ", "--no ", "--seed",
	}

	for _, phrase := range aiPhrases {
		if strings.Contains(s, phrase) {
			log.Printf("AI Detection Debug: Found AI phrase in binary: %s", phrase)
			return true, AIDetectionResult{Provider: "AI (Binary Phrase)", Method: "binary", Details: "AI phrase: " + phrase}
		}
	}

	// 3. Check UTF-16 encoded AI parameters (these are legitimate AI markers)
	utf16Needles := []string{"sui_image_params", "textual_inversion", "checkpoint", "lora", "vae", "embeddings"}
	for _, n := range utf16Needles {
		if bytes.Contains(b, buildUTF16LEPattern(n)) || bytes.Contains(b, buildUTF16BEPattern(n)) {
			log.Printf("AI Detection Debug: Found UTF-16 AI parameter: %s", n)
			return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "UTF-16 AI param: " + n}
		}
	}

	// REMOVED: Generic patterns that were causing false positives
	// - Grok detection (was matching binary patterns)
	// - ComfyUI detection (was matching generic terms)
	// - Generic SDXL terms (was matching individual words)
	// - Flux detection (was matching binary patterns)
	// - Generic prompt detection (was too broad)

	return false, AIDetectionResult{}
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
// FIXED: Now only scans text-based metadata, not entire binary file
func DetectAIFast(imageBytes []byte) (bool, AIDetectionResult) {
	// Use buffer pool for string conversion to avoid allocations
	buf := getBuffer()
	defer putBuffer(buf)

	buf = append(buf, imageBytes...)

	// Early exit for very small files (unlikely to be AI)
	if len(imageBytes) < 1024 {
		return false, AIDetectionResult{}
	}

	// Check PNG signature BEFORE string conversion (string conversion corrupts binary signature)
	isPNG := false
	if len(imageBytes) >= 8 {
		sig0 := imageBytes[0]
		sig1 := imageBytes[1]
		sig2 := imageBytes[2]
		sig3 := imageBytes[3]
		sig4 := imageBytes[4]
		sig5 := imageBytes[5]
		sig6 := imageBytes[6]
		sig7 := imageBytes[7]

		log.Printf("AI Detection Debug: PNG signature check - bytes: %x %x %x %x %x %x %x %x", sig0, sig1, sig2, sig3, sig4, sig5, sig6, sig7)
		log.Printf("AI Detection Debug: Expected PNG signature: %x %x %x %x %x %x %x %x", 0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A)

		isPNG = sig0 == 0x89 && sig1 == 0x50 && sig2 == 0x4E && sig3 == 0x47 &&
			sig4 == 0x0D && sig5 == 0x0A && sig6 == 0x1A && sig7 == 0x0A

		log.Printf("AI Detection Debug: PNG signature detected: %v", isPNG)
	}

	content := strings.ToLower(string(buf))

	// DEBUG: Log the first 200 chars to see what we're matching
	if len(content) > 0 {
		log.Printf("AI Detection Debug: Fast detection scanning content (first 200 chars): %s", content[:min(200, len(content))])
	}

	// DEBUG: Check for Midjourney parameters specifically
	midjourneyParams := []string{"--chaos", "--ar", "--profile", "--stylize", "--weird", "--v ", "--no ", "--seed", "Job ID:"}
	for _, param := range midjourneyParams {
		if strings.Contains(content, param) {
			log.Printf("AI Detection Debug: Found Midjourney parameter in fast detection: %s", param)
		}
	}

	// FIXED: Skip binary JPEG headers and ICC profiles to avoid false positives
	// But for PNG files, we need to check text chunks which might be in the first part
	scanStart := 1000
	if len(imageBytes) > scanStart {
		// Use the pre-computed isPNG variable from above
		if isPNG {
			log.Printf("AI Detection Debug: Detected PNG file via binary signature, checking text chunks in full content")
			// For PNG files, scan the entire file but skip just the signature
			scanStart = 8 // Skip PNG signature (8 bytes)
		} else {
			log.Printf("AI Detection Debug: Detected non-PNG file, skipping first %d bytes (binary headers)", scanStart)
		}
		content = content[scanStart:]
	}

	// FIXED: Be much more restrictive - only scan for very specific AI markers
	// Don't scan for generic software patterns that can appear in binary data

	// Check for very specific AI markers that wouldn't appear in regular JPEGs
	specificMarkers := []string{
		"sui_image_params",  // Very specific to AI generation
		"textual_inversion", // AI-specific term
		"stable diffusion",  // Full phrase less likely to appear accidentally
		"midjourney",        // Full phrase
		"dall-e",            // Full phrase
		"negative_prompt",   // AI-specific term
		"positive_prompt",   // AI-specific term
		// Midjourney parameters (very specific to AI generation)
		"--chaos", "--ar", "--profile", "--stylize", "--weird", "--v ", "--no ", "--seed",
	}

	for _, marker := range specificMarkers {
		if strings.Contains(content, marker) {
			log.Printf("AI Detection Debug: Specific marker matched! Marker: %s", marker)
			return true, AIDetectionResult{
				Provider: "AI (Specific Marker)",
				Method:   "binary",
				Details:  "Specific AI marker: " + marker,
			}
		}
	}

	// REMOVED: Generic pattern matching that was causing false positives
	// The fastPatterns were too broad and matching binary data

	// REMOVED: Generic technical terms that can appear in regular images
	// These were causing false positives with binary JPEG data

	return false, AIDetectionResult{}
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

	// Should not reach here, but just in case
	return false, AIDetectionResult{}
}

// detectFromEXIFBytesOptimized is an optimized version that exits early
func detectFromEXIFBytesOptimized(b []byte) (bool, AIDetectionResult) {
	rawExif, err := exif.SearchAndExtractExif(b)
	if err != nil {
		return false, AIDetectionResult{}
	}

	// Quick raw scan for obvious AI markers first
	rawExifStr := string(rawExif)
	if strings.Contains(rawExifStr, "sui_image_params") {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: "sui_image_params in raw EXIF"}
	}

	// Check for UTF-16 encoded patterns
	for _, pattern := range []string{"sui_image_params", "prompt"} {
		if bytes.Contains(rawExif, buildUTF16LEPattern(pattern)) || bytes.Contains(rawExif, buildUTF16BEPattern(pattern)) {
			return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: pattern + " in UTF-16 EXIF"}
		}
	}

	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		return false, AIDetectionResult{}
	}

	var softwareVal string
	for _, e := range entries {
		tn := strings.TrimSpace(e.TagName)
		val := strings.TrimSpace(e.Formatted)

		// Software field check (high probability)
		if strings.EqualFold(tn, "Software") {
			softwareVal = val
			low := strings.ToLower(val)
			if strings.Contains(low, "midjourney") {
				return true, AIDetectionResult{Provider: "Midjourney", Method: "exif", Details: val}
			}
			if strings.Contains(low, "dall-e") || strings.Contains(low, "dalle") || strings.Contains(low, "openai") {
				return true, AIDetectionResult{Provider: "OpenAI", Method: "exif", Details: val}
			}
			if strings.Contains(low, "stable diffusion") || strings.Contains(low, "sdxl") {
				return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: val}
			}
			if strings.Contains(low, "flux") {
				return true, AIDetectionResult{Provider: "FLUX", Method: "exif", Details: val}
			}
		}

		// UserComment field (high probability for AI)
		if strings.EqualFold(tn, "UserComment") {
			// Try to decode UTF-16 UserComment data
			decodedVal := val
			if e.Value != nil {
				switch v := e.Value.(type) {
				case []byte:
					// Handle UTF-16 encoded UserComment
					if len(v) > 8 {
						// UserComment format: first 8 bytes = encoding ID, then the actual text
						if bytes.HasPrefix(v[8:], []byte{0xFF, 0xFE}) || bytes.HasPrefix(v[8:], []byte{0xFE, 0xFF}) {
							// UTF-16 encoded after header
							if decoded, err := decodeUTF16(v[8:]); err == nil && len(decoded) > 0 {
								decodedVal = decoded
							}
						} else if decoded, err := decodeUTF16(v); err == nil && len(decoded) > 0 {
							// Try to decode entire byte array as UTF-16
							decodedVal = decoded
						}
					}
				}
			}

			// Check for AI parameters in the decoded value
			if containsAnyFold(decodedVal, []string{"prompt", "negativeprompt", "negative_prompt", "sampler", "steps", "cfg", "seed", "sui_image_params"}) {
				return true, AIDetectionResult{Provider: "AI (Prompt in EXIF)", Method: "exif", Details: tn}
			}
		}

		// DigitalSourceType (definitive AI marker)
		if strings.EqualFold(tn, "DigitalSourceType") && strings.TrimSpace(val) == iptcTrainedMedia {
			return true, AIDetectionResult{Provider: "AI (IPTC Trained Media)", Method: "exif", Details: val}
		}
	}

	// Software fallback check
	if softwareVal != "" {
		low := strings.ToLower(softwareVal)
		if strings.Contains(low, "ai") || strings.Contains(low, "diffusion") ||
			strings.Contains(low, "artificial") || strings.Contains(low, "generator") ||
			strings.Contains(low, "synthetic") || strings.Contains(low, "stability") ||
			strings.Contains(low, "midjourney") || strings.Contains(low, "dall-e") ||
			strings.Contains(low, "flux") || containsAnyFold(low, aiModelPatterns) {
			return true, AIDetectionResult{Provider: "AI (Software)", Method: "exif", Details: softwareVal}
		}
	}

	return false, AIDetectionResult{}
}
