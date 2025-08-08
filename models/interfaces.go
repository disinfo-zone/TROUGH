package models

import "github.com/google/uuid"

type UserRepositoryInterface interface {
	Create(user *User) error
	GetByEmail(email string) (*User, error)
	GetByUsername(username string) (*User, error)
	GetByID(id uuid.UUID) (*User, error)
}

type ImageRepositoryInterface interface {
	Create(image *Image) error
	GetFeed(page, limit int, showNSFW bool) ([]ImageWithUser, int, error)
	GetByID(id uuid.UUID) (*ImageWithUser, error)
	GetUserImages(userID uuid.UUID, page, limit int) ([]ImageWithUser, int, error)
}

type LikeRepositoryInterface interface {
	Create(userID, imageID uuid.UUID) error
	Delete(userID, imageID uuid.UUID) error
	GetByUser(userID uuid.UUID, imageID uuid.UUID) (*Like, error)
}