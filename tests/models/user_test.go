package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/yourusername/trough/models"
)

func TestUserHashPassword(t *testing.T) {
	user := &models.User{}
	password := "testpassword123"

	err := user.HashPassword(password)
	assert.NoError(t, err)
	assert.NotEmpty(t, user.PasswordHash)
	assert.NotEqual(t, password, user.PasswordHash)
}

func TestUserCheckPassword(t *testing.T) {
	user := &models.User{}
	password := "testpassword123"
	
	err := user.HashPassword(password)
	assert.NoError(t, err)

	assert.True(t, user.CheckPassword(password))
	assert.False(t, user.CheckPassword("wrongpassword"))
}

func TestUserToResponse(t *testing.T) {
	user := &models.User{
		Username: "testuser",
		Bio:      nil,
	}

	response := user.ToResponse()
	assert.Equal(t, "testuser", response.Username)
	assert.Nil(t, response.Bio)
}