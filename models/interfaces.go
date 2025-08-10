package models

import (
	"github.com/google/uuid"
)

type UserRepositoryInterface interface {
	Create(user *User) error
	GetByEmail(email string) (*User, error)
	GetByUsername(username string) (*User, error)
	GetByID(id uuid.UUID) (*User, error)
	UpdateProfile(id uuid.UUID, updates UpdateUserRequest) (*User, error)
	UpdateEmail(id uuid.UUID, email string) error
	UpdatePassword(id uuid.UUID, passwordHash string) error
	DeleteUser(id uuid.UUID) error
	SetAdmin(id uuid.UUID, isAdmin bool) error
	SetDisabled(id uuid.UUID, disabled bool) error
	SetModerator(id uuid.UUID, isModerator bool) error
	ListUsers(page, limit int) ([]User, int, error)
	SearchUsers(q string, page, limit int) ([]User, int, error)
}

type ImageRepositoryInterface interface {
	Create(image *Image) error
	GetFeed(page, limit int, showNSFW bool) ([]ImageWithUser, int, error)
	GetByID(id uuid.UUID) (*ImageWithUser, error)
	GetUserImages(userID uuid.UUID, page, limit int) ([]ImageWithUser, int, error)
	Delete(id uuid.UUID) error
	SetNSFW(id uuid.UUID, isNSFW bool) error
	CountByUser(userID uuid.UUID) (int, error)
	UpdateMeta(id uuid.UUID, title *string, caption *string, isNSFW *bool) error
	UpdateFilename(id uuid.UUID, newFilename string) error
	GetImagesByFilename(filename string) ([]ImageWithUser, error)
}

type LikeRepositoryInterface interface {
	Create(userID, imageID uuid.UUID) error
	Delete(userID, imageID uuid.UUID) error
	GetByUser(userID uuid.UUID, imageID uuid.UUID) (*Like, error)
}
