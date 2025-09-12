package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type UserRepositoryInterface interface {
	Create(user *User) error
	CreateWithTx(tx *sqlx.Tx, user *User) error
	    GetByEmail(ctx context.Context, email string) (*User, error)
    GetByUsername(ctx context.Context, username string) (*User, error)
	    GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	UpdateProfile(id uuid.UUID, updates UpdateUserRequest) (*User, error)
	UpdateEmail(id uuid.UUID, email string) error
	UpdatePassword(id uuid.UUID, passwordHash string) error
	DeleteUser(id uuid.UUID) error
	SetAdmin(id uuid.UUID, isAdmin bool) error
	SetDisabled(id uuid.UUID, disabled bool) error
	SetModerator(id uuid.UUID, isModerator bool) error
	ListUsers(page, limit int) ([]User, int, error)
	SearchUsers(q string, page, limit int) ([]User, int, error)
	BeginTx() (*sqlx.Tx, error)
}

type ImageRepositoryInterface interface {
	Create(image *Image) error
	GetFeed(page, limit int, showNSFW bool) ([]ImageWithUser, int, error)
	GetFeedSeek(limit int, showNSFW bool, cursorEncoded string) ([]ImageWithUser, string, error)
	CountFeed(showNSFW bool) (int, error)
	GetByID(id uuid.UUID) (*ImageWithUser, error)
	GetUserImages(userID uuid.UUID, page, limit int) ([]ImageWithUser, int, error)
	GetUserImagesSeek(userID uuid.UUID, limit int, cursorEncoded string) ([]ImageWithUser, string, error)
	CountUserImages(userID uuid.UUID) (int, error)
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

type CollectRepositoryInterface interface {
	Create(userID, imageID uuid.UUID) error
	Delete(userID, imageID uuid.UUID) error
	GetByUser(userID uuid.UUID, imageID uuid.UUID) (*Collect, error)
	GetUserCollections(userID uuid.UUID, page, limit int) ([]ImageWithUser, int, error)
	GetUserCollectionsSeek(userID uuid.UUID, limit int, cursorEncoded string) ([]ImageWithUser, string, error)
}

type InviteRepositoryInterface interface {
	Create(maxUses *int, expiresAt *time.Time, createdBy *uuid.UUID) (*Invite, error)
	List(page, limit int) ([]Invite, int, error)
	GetByCode(code string) (*Invite, error)
	GetByCodeWithTx(tx *sqlx.Tx, code string) (*Invite, error)
	Consume(code string) (*Invite, error)
	ConsumeWithTx(tx *sqlx.Tx, code string) (*Invite, error)
	RevertConsume(id uuid.UUID) error
	RevertConsumeWithTx(tx *sqlx.Tx, id uuid.UUID) error
	Delete(id uuid.UUID) error
	DeleteUsedAndExpired() (int, error)
}

// Pages CMS
type PageRepositoryInterface interface {
	Create(p *Page) error
	Update(p *Page) error
	Delete(id uuid.UUID) error
	GetBySlug(slug string) (*Page, error)
	GetPublishedBySlug(slug string) (*Page, error)
	ListAll(page, limit int) ([]Page, int, error)
	ListPublished() ([]Page, error)
}
