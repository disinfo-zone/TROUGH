package models

import (
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           uuid.UUID `json:"id" db:"id"`
	Username     string    `json:"username" db:"username"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Bio          *string   `json:"bio" db:"bio"`
	AvatarURL    *string   `json:"avatar_url" db:"avatar_url"`
	IsAdmin      bool      `json:"is_admin" db:"is_admin"`
	ShowNSFW     bool      `json:"show_nsfw" db:"show_nsfw"`
	IsDisabled   bool      `json:"is_disabled" db:"is_disabled"`
	CreatedAt    time.Time `json:"created_at" db:"created_at"`
}

type CreateUserRequest struct {
	Username string `json:"username" validate:"required,min=3,max=30,alphanum"`
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=6"`
}

type LoginRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type UpdateUserRequest struct {
	Username  *string `json:"username" validate:"omitempty,min=3,max=30,alphanum"`
	Bio       *string `json:"bio" validate:"omitempty,max=500"`
	AvatarURL *string `json:"avatar_url" validate:"omitempty,url"`
	ShowNSFW  *bool   `json:"show_nsfw"`
	Password  *string `json:"password" validate:"omitempty,min=6"`
}

type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Bio       *string   `json:"bio"`
	AvatarURL *string   `json:"avatar_url"`
	IsAdmin   bool      `json:"is_admin"`
	ShowNSFW  bool      `json:"show_nsfw"`
	CreatedAt time.Time `json:"created_at"`
}

func (u *User) HashPassword(password string) error {
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(hashedPassword)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	return err == nil
}

func (u *User) ToResponse() UserResponse {
	return UserResponse{
		ID:        u.ID,
		Username:  u.Username,
		Bio:       u.Bio,
		AvatarURL: u.AvatarURL,
		IsAdmin:   u.IsAdmin,
		ShowNSFW:  u.ShowNSFW,
		CreatedAt: u.CreatedAt,
	}
}
