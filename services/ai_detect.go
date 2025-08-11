package services

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"regexp"
	"strings"

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
	c2paSniffRegex   = regexp.MustCompile(`(?is)(c2pa|jumbf|contentcredentials)`)
	iptcTrainedMedia = "http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia"
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
	if strings.Contains(s, "openai") || strings.Contains(s, "dall-e") || strings.Contains(s, "dalle") {
		return "OpenAI"
	}
	// Adobe Firefly uses Content Credentials and often adobe/firefly appears in XMP
	if strings.Contains(s, "adobe") && strings.Contains(s, "firefly") {
		return "Adobe Firefly"
	}
	// Google Imagen (Gemini) may include credit "Made with Google AI"
	if strings.Contains(s, "made with google ai") || strings.Contains(s, "google ai") {
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
	if strings.Contains(string(rawExif), "sui_image_params") ||
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

	log.Printf("AI Detection: Found %d EXIF entries for %s", len(entries), imagePath)
	var softwareVal string
	for _, e := range entries {
		tn := strings.TrimSpace(e.TagName)
		val := strings.TrimSpace(e.Formatted)

		// Log UserComment specifically since that's where SDXL params often are
		if strings.EqualFold(tn, "UserComment") {
			logLen := 200
			if len(val) < logLen {
				logLen = len(val)
			}
			log.Printf("AI Detection: UserComment found (formatted): %s", val[:logLen])

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
					logLen := 200
					if len(rawStr) < logLen {
						logLen = len(rawStr)
					}
					log.Printf("AI Detection: UserComment raw: %s", rawStr[:logLen])
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
		// Any EXIF value containing common generation params or 'prompt'
		if containsAnyFold(val, []string{"prompt", "negativeprompt", "negative_prompt", "sampler", "steps", "cfg", "seed", "model"}) {
			return true, AIDetectionResult{Provider: "AI (Prompt in EXIF)", Method: "exif", Details: tn}
		}
		// UserComment / ImageDescription / XPComment often store generation params
		if strings.EqualFold(tn, "UserComment") || strings.EqualFold(tn, "ImageDescription") || strings.EqualFold(tn, "XPComment") {
			isPromptJSON := looksLikePromptJSON(val)
			hasParams := containsAnyFold(val, []string{"prompt", "negativeprompt", "negative_prompt", "sampler", "steps", "cfg", "sui_image_params", "sui_extra_data"})
			log.Printf("AI Detection: %s - isPromptJSON=%v, hasParams=%v", tn, isPromptJSON, hasParams)
			if isPromptJSON || hasParams {
				log.Printf("AI Detection: DETECTED via %s", tn)
				return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "exif", Details: tn + " contains generation params"}
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
		if strings.Contains(low, "ai") || strings.Contains(low, "diffusion") {
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
	if strings.Contains(s, ">prompt<") && strings.Contains(s, ">workflow<") {
		return true, AIDetectionResult{Provider: "ComfyUI", Method: "xmp", Details: "Prompt + Workflow"}
	}

	// Adobe Firefly via XMP mentions
	if strings.Contains(s, "adobe") && strings.Contains(s, "firefly") {
		return true, AIDetectionResult{Provider: "Adobe Firefly", Method: "xmp", Details: "XMP mentions Adobe Firefly"}
	}

	// OpenAI via XMP mentions
	if strings.Contains(s, "openai") || strings.Contains(s, "dall-e") || strings.Contains(s, "dalle") {
		return true, AIDetectionResult{Provider: "OpenAI", Method: "xmp", Details: "XMP mentions OpenAI/DALL-E"}
	}

	// Stable Diffusion / SDXL in XMP: look for prompt-like keys or SD terms
	if strings.Contains(s, "\"prompt\"") || strings.Contains(s, "negativeprompt") || strings.Contains(s, "negative_prompt") || strings.Contains(s, ">prompt<") || strings.Contains(s, "sdxl") || strings.Contains(s, "stable diffusion") || strings.Contains(s, "ksampler") || strings.Contains(s, "sampler_name") {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "xmp", Details: "Prompt/SD terms in XMP"}
	}

	// Flux in XMP: mentions of Flux or Black Forest Labs
	if strings.Contains(s, "flux") || strings.Contains(s, "black forest labs") || strings.Contains(s, "bfl") {
		return true, AIDetectionResult{Provider: "FLUX", Method: "xmp", Details: "Flux terms in XMP"}
	}

	// Generic IPTC trained media marker
	if strings.Contains(s, strings.ToLower(iptcTrainedMedia)) {
		return true, AIDetectionResult{Provider: "AI (IPTC Trained Media)", Method: "xmp", Details: iptcTrainedMedia}
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
		if strings.Contains(low, "prompt") || strings.Contains(low, "negativeprompt") || strings.Contains(low, "negative_prompt") ||
			strings.Contains(low, "sampler") || strings.Contains(low, "steps") || strings.Contains(low, "cfg") ||
			strings.Contains(low, "seed") || strings.Contains(low, "model") || strings.Contains(low, "sui_image_params") {
			return true
		}
	}
	// If not valid JSON, check for prompt-like content anyway
	low := strings.ToLower(s)
	if strings.Contains(low, "prompt") && (strings.Contains(low, "sampler") || strings.Contains(low, "steps") || strings.Contains(low, "cfg") || strings.Contains(low, "seed")) {
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
// present in PNG/WebP text chunks or embedded sidecar-like blobs.
func detectFromBinaryText(imagePath string) (bool, AIDetectionResult) {
	b, err := ioutil.ReadFile(imagePath)
	if err != nil {
		return false, AIDetectionResult{}
	}
	s := strings.ToLower(string(b))
	// Grok explicit fields in PNG text/iTXt or generic Grok mentions
	if strings.Contains(s, "grok image prompt") || strings.Contains(s, "grok image upsampled prompt") || strings.Contains(s, "\x00grok\x00") || strings.Contains(s, " g r o k ") || strings.Contains(s, "grok:") || strings.Contains(s, "\"grok\"") {
		return true, AIDetectionResult{Provider: "Grok", Method: "binary", Details: "Grok prompt fields in image"}
	}

	// ComfyUI markers: filename prefix, literal keys, or general mentions
	if strings.Contains(s, "\"filename_prefix\":\"comfyui\"") || strings.Contains(s, "comfyui") || strings.Contains(s, "prompt\t{") || (strings.Contains(s, "prompt") && strings.Contains(s, "workflow")) {
		return true, AIDetectionResult{Provider: "ComfyUI", Method: "binary", Details: "ComfyUI markers present"}
	}

	// SDXL / Stable Diffusion hints in embedded params (ASCII)
	if strings.Contains(s, "sdxl") || strings.Contains(s, "sdxlpromptstyler") || strings.Contains(s, "stable diffusion") || strings.Contains(s, "ksampler") || strings.Contains(s, "sampler_name") || strings.Contains(s, "negativeprompt") || strings.Contains(s, "negative_prompt") || strings.Contains(s, "cfg") || strings.Contains(s, "steps") {
		return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "SDXL/SD params present"}
	}

	// SDXL / Stable Diffusion hints in embedded params (UTF-16 in EXIF, e.g., UserComment)
	utf16Needles := []string{"sui_image_params", "prompt", "negativeprompt", "negative_prompt", "sampler", "steps", "cfg", "seed", "model"}
	for _, n := range utf16Needles {
		if bytes.Contains(b, buildUTF16LEPattern(n)) || bytes.Contains(b, buildUTF16BEPattern(n)) {
			return true, AIDetectionResult{Provider: "Stable Diffusion (SDXL)", Method: "binary", Details: "UTF-16 prompt/params present"}
		}
	}

	// Flux hints in binary text
	if strings.Contains(s, "flux") || strings.Contains(s, "black forest labs") || strings.Contains(s, "bfl") {
		return true, AIDetectionResult{Provider: "FLUX", Method: "binary", Details: "Flux markers present"}
	}

	// Generic prompt presence as a last resort
	if strings.Contains(s, "\"prompt\"") || strings.Contains(s, "prompt:") || strings.Contains(s, "\nprompt") || strings.Contains(s, " prompt ") || bytes.Contains(b, buildUTF16LEPattern("prompt")) || bytes.Contains(b, buildUTF16BEPattern("prompt")) {
		return true, AIDetectionResult{Provider: "AI (Prompt Embedded)", Method: "binary", Details: "Prompt-like text present"}
	}
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
