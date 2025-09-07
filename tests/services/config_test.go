package services

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourusername/trough/services"
)

func TestLoadConfigWithDefaults(t *testing.T) {
	config, err := services.LoadConfig("nonexistent.yaml")
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.NotEmpty(t, config.AISignatures)
	assert.Equal(t, 2048, config.Aesthetic.MaxWidth)
}

func TestLoadConfigFromFile(t *testing.T) {
	configData := `
ai_signatures:
  - key: "TestKey"
    value: "TestValue"
aesthetic:
  max_width: 1024
  formats: [".jpg"]
`

	tempFile, err := os.CreateTemp("", "test-config-*.yaml")
	assert.NoError(t, err)
	defer os.Remove(tempFile.Name())

	_, err = tempFile.WriteString(configData)
	assert.NoError(t, err)
	tempFile.Close()

	config, err := services.LoadConfig(tempFile.Name())
	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Len(t, config.AISignatures, 1)
	assert.Equal(t, "TestKey", config.AISignatures[0].Key)
	assert.Equal(t, 1024, config.Aesthetic.MaxWidth)
}
