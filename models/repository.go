package models

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

type UserRepository struct {
	db *sqlx.DB
}

func NewUserRepository(db *sqlx.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(user *User) error {
	query := `
		INSERT INTO users (username, email, password_hash, bio, avatar_url)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`
	
	return r.db.QueryRow(query, user.Username, user.Email, user.PasswordHash, user.Bio, user.AvatarURL).
		Scan(&user.ID, &user.CreatedAt)
}

func (r *UserRepository) GetByEmail(email string) (*User, error) {
	var user User
	query := `SELECT * FROM users WHERE email = $1`
	err := r.db.Get(&user, query, email)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) GetByUsername(username string) (*User, error) {
	var user User
	query := `SELECT * FROM users WHERE username = $1`
	err := r.db.Get(&user, query, username)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) GetByID(id uuid.UUID) (*User, error) {
	var user User
	query := `SELECT * FROM users WHERE id = $1`
	err := r.db.Get(&user, query, id)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

type ImageRepository struct {
	db *sqlx.DB
}

func NewImageRepository(db *sqlx.DB) *ImageRepository {
	return &ImageRepository{db: db}
}

func (r *ImageRepository) Create(image *Image) error {
	query := `
		INSERT INTO images (user_id, filename, original_name, file_size, width, height, blurhash, dominant_color, is_nsfw, ai_signature, exif_data)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, created_at`
	
	return r.db.QueryRow(query, 
		image.UserID, image.Filename, image.OriginalName, image.FileSize,
		image.Width, image.Height, image.Blurhash, image.DominantColor,
		image.IsNSFW, image.AISignature, image.ExifData).
		Scan(&image.ID, &image.CreatedAt)
}

func (r *ImageRepository) GetFeed(page, limit int, showNSFW bool) ([]ImageWithUser, int, error) {
	offset := (page - 1) * limit
	
	var images []ImageWithUser
	var total int
	
	countQuery := `SELECT COUNT(*) FROM images WHERE ($1 OR is_nsfw = false)`
	err := r.db.Get(&total, countQuery, showNSFW)
	if err != nil {
		return nil, 0, err
	}
	
	query := `
		SELECT i.*, u.username, u.avatar_url
		FROM images i
		JOIN users u ON i.user_id = u.id
		WHERE ($1 OR i.is_nsfw = false)
		ORDER BY i.created_at DESC
		LIMIT $2 OFFSET $3`
	
	err = r.db.Select(&images, query, showNSFW, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	
	return images, total, nil
}

func (r *ImageRepository) GetByID(id uuid.UUID) (*ImageWithUser, error) {
	var image ImageWithUser
	query := `
		SELECT i.*, u.username, u.avatar_url
		FROM images i
		JOIN users u ON i.user_id = u.id
		WHERE i.id = $1`
	
	err := r.db.Get(&image, query, id)
	if err != nil {
		return nil, err
	}
	return &image, nil
}

func (r *ImageRepository) GetUserImages(userID uuid.UUID, page, limit int) ([]ImageWithUser, int, error) {
	offset := (page - 1) * limit
	
	var images []ImageWithUser
	var total int
	
	countQuery := `SELECT COUNT(*) FROM images WHERE user_id = $1`
	err := r.db.Get(&total, countQuery, userID)
	if err != nil {
		return nil, 0, err
	}
	
	query := `
		SELECT i.*, u.username, u.avatar_url
		FROM images i
		JOIN users u ON i.user_id = u.id
		WHERE i.user_id = $1
		ORDER BY i.created_at DESC
		LIMIT $2 OFFSET $3`
	
	err = r.db.Select(&images, query, userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	
	return images, total, nil
}

type LikeRepository struct {
	db *sqlx.DB
}

func NewLikeRepository(db *sqlx.DB) *LikeRepository {
	return &LikeRepository{db: db}
}

func (r *LikeRepository) Create(userID, imageID uuid.UUID) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	
	query := `INSERT INTO likes (user_id, image_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	_, err = tx.Exec(query, userID, imageID)
	if err != nil {
		return err
	}
	
	updateQuery := `UPDATE images SET likes_count = likes_count + 1 WHERE id = $1`
	result, err := tx.Exec(updateQuery, imageID)
	if err != nil {
		return err
	}
	
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	
	if affected == 0 {
		return fmt.Errorf("image not found")
	}
	
	return tx.Commit()
}

func (r *LikeRepository) Delete(userID, imageID uuid.UUID) error {
	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	
	query := `DELETE FROM likes WHERE user_id = $1 AND image_id = $2`
	result, err := tx.Exec(query, userID, imageID)
	if err != nil {
		return err
	}
	
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	
	if affected > 0 {
		updateQuery := `UPDATE images SET likes_count = GREATEST(likes_count - 1, 0) WHERE id = $1`
		_, err = tx.Exec(updateQuery, imageID)
		if err != nil {
			return err
		}
	}
	
	return tx.Commit()
}

func (r *LikeRepository) GetByUser(userID uuid.UUID, imageID uuid.UUID) (*Like, error) {
	var like Like
	query := `SELECT * FROM likes WHERE user_id = $1 AND image_id = $2`
	err := r.db.Get(&like, query, userID, imageID)
	if err != nil {
		return nil, err
	}
	return &like, nil
}