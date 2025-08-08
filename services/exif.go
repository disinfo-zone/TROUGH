package services

import (
	"encoding/json"
	"strings"

	"github.com/dsoprea/go-exif/v3"
)

func VerifyAIImage(imagePath string, config Config) (bool, string) {
	rawExif, err := exif.SearchFileAndExtractExif(imagePath)
	if err != nil {
		return false, ""
	}

	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		return false, ""
	}

	for _, entry := range entries {
		for _, sig := range config.AISignatures {
			if entry.TagName == sig.Key {
				if len(sig.Contains) > 0 {
					for _, substr := range sig.Contains {
						if strings.Contains(entry.Formatted, substr) {
							return true, entry.Formatted
						}
					}
				} else if entry.Formatted == sig.Value {
					return true, entry.Formatted
				}
			}
		}
	}

	return false, ""
}

// ExtractExifJSON returns a JSON object with all EXIF tags and values for display/preservation.
func ExtractExifJSON(imagePath string) json.RawMessage {
	rawExif, err := exif.SearchFileAndExtractExif(imagePath)
	if err != nil {
		return json.RawMessage("null")
	}
	entries, _, err := exif.GetFlatExifData(rawExif, nil)
	if err != nil {
		return json.RawMessage("null")
	}
	m := map[string]interface{}{}
	for _, e := range entries {
		// TagName might repeat; accumulate with suffix index if needed
		key := e.TagName
		if _, exists := m[key]; exists {
			key = key + "_dup"
		}
		m[key] = e.Formatted
	}
	b, err := json.Marshal(m)
	if err != nil {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}
