package services

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	AISignatures        []AISignature          `yaml:"ai_signatures"`
	Aesthetic           Aesthetic              `yaml:"aesthetic"`
	RateLimiting        RateLimitConfig        `yaml:"rate_limiting"`
	ProgressiveRateLimiting ProgressiveRateLimitConfig `yaml:"progressive_rate_limiting"`
}

type AISignature struct {
	Key      string   `yaml:"key"`
	Value    string   `yaml:"value,omitempty"`
	Contains []string `yaml:"contains,omitempty"`
}

type Aesthetic struct {
	BlurRadius       int      `yaml:"blur_radius"`
	ThumbnailQuality int      `yaml:"thumbnail_quality"`
	MaxWidth         int      `yaml:"max_width"`
	Formats          []string `yaml:"formats"`
}

func LoadConfig(path string) (*Config, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &Config{
			AISignatures: []AISignature{
				{
					Key:   "DigitalSourceType",
					Value: "http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia",
				},
				{
					Key:      "Software",
					Contains: []string{"Midjourney", "DALL-E", "Stable Diffusion", "Flux"},
				},
			},
			Aesthetic: Aesthetic{
				BlurRadius:       20,
				ThumbnailQuality: 85,
				MaxWidth:         2048,
				Formats:          []string{".jpg", ".jpeg", ".png", ".webp"},
			},
			RateLimiting: RateLimitConfig{
				MaxEntries:      1000,
				CleanupInterval: 1 * time.Minute,
				EntryTTL:        30 * time.Minute,
				TrustedProxies:  []string{"127.0.0.1", "::1"},
				EnableDebug:     false,
			},
			ProgressiveRateLimiting: ProgressiveRateLimitConfig{
				BaseWindow:      1 * time.Minute,
				MaxWindow:       1 * time.Hour,
				BaseCapacity:    60,
				MinCapacity:     5,
				BackoffFactor:   2.0,
				LockoutThreshold: 10,
				LockoutDuration: 15 * time.Minute,
				EnableLogging:   true,
			},
		}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}
