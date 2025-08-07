package models

import (
	"database/sql/driver"
	"fmt"
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

type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Bio       *string   `json:"bio"`
	AvatarURL *string   `json:"avatar_url"`
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
		CreatedAt: u.CreatedAt,
	}
}

func (u uuid.UUID) Value() (driver.Value, error) {
	return u.String(), nil
}

func (u *uuid.UUID) Scan(value interface{}) error {
	if value == nil {
		*u = uuid.Nil
		return nil
	}
	
	switch v := value.(type) {
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			return err
		}
		*u = parsed
		return nil
	case []byte:
		parsed, err := uuid.Parse(string(v))
		if err != nil {
			return err
		}
		*u = parsed
		return nil
	default:
		return fmt.Errorf("cannot scan %T into UUID", value)
	}
}