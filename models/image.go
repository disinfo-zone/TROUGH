package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type Image struct {
	ID            uuid.UUID       `json:"id" db:"id"`
	UserID        uuid.UUID       `json:"user_id" db:"user_id"`
	Filename      string          `json:"filename" db:"filename"`
	OriginalName  *string         `json:"original_name" db:"original_name"`
	FileSize      *int            `json:"file_size" db:"file_size"`
	Width         *int            `json:"width" db:"width"`
	Height        *int            `json:"height" db:"height"`
	Blurhash      *string         `json:"blurhash" db:"blurhash"`
	DominantColor *string         `json:"dominant_color" db:"dominant_color"`
	IsNSFW        bool            `json:"is_nsfw" db:"is_nsfw"`
	AISignature   *string         `json:"ai_signature" db:"ai_signature"`
	AIProvider    *string         `json:"ai_provider" db:"ai_provider"`
	ExifData      json.RawMessage `json:"exif_data,omitempty" db:"exif_data"`
	Caption       *string         `json:"caption" db:"caption"`
	LikesCount    int             `json:"likes_count" db:"likes_count"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
}

type ImageWithUser struct {
	Image
	Username  string  `json:"username" db:"username"`
	AvatarURL *string `json:"user_avatar_url" db:"avatar_url"`
}

type Like struct {
	UserID    uuid.UUID `json:"user_id" db:"user_id"`
	ImageID   uuid.UUID `json:"image_id" db:"image_id"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

type UploadResponse struct {
	ID            uuid.UUID `json:"id"`
	Filename      string    `json:"filename"`
	OriginalName  *string   `json:"original_name"`
	Width         *int      `json:"width"`
	Height        *int      `json:"height"`
	Blurhash      *string   `json:"blurhash"`
	DominantColor *string   `json:"dominant_color"`
	FileSize      *int      `json:"file_size"`
	Caption       *string   `json:"caption"`
	CreatedAt     time.Time `json:"created_at"`
}

func (i *Image) ToUploadResponse() UploadResponse {
	return UploadResponse{
		ID:            i.ID,
		Filename:      i.Filename,
		OriginalName:  i.OriginalName,
		Width:         i.Width,
		Height:        i.Height,
		Blurhash:      i.Blurhash,
		DominantColor: i.DominantColor,
		FileSize:      i.FileSize,
		Caption:       i.Caption,
		CreatedAt:     i.CreatedAt,
	}
}

type FeedResponse struct {
	Images []ImageWithUser `json:"images"`
	Page   int             `json:"page"`
	Total  int             `json:"total"`
}
