package services

import (
	"strings"

	"github.com/dsoprea/go-exif/v3"
)

type Config struct {
	AISignatures []AISignature `yaml:"ai_signatures"`
}

type AISignature struct {
	Key      string   `yaml:"key"`
	Value    string   `yaml:"value,omitempty"`
	Contains []string `yaml:"contains,omitempty"`
}

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