package db

import (
	"fmt"
	"os"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

var DB *sqlx.DB

func Connect() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://trough:trough@localhost:5432/trough?sslmode=disable"
	}

	var err error

	// Retry connection logic for Docker container startup
	for i := 0; i < 30; i++ {
		DB, err = sqlx.Connect("postgres", databaseURL)
		if err == nil {
			break
		}

		fmt.Printf("Database connection attempt %d failed: %v\n", i+1, err)
		time.Sleep(1 * time.Second)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to database after retries: %w", err)
	}

	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(25)

	return nil
}

func Migrate() error {
	schema := `
		CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

		CREATE TABLE IF NOT EXISTS users (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			username VARCHAR(30) UNIQUE NOT NULL,
			email VARCHAR(255) UNIQUE NOT NULL,
			password_hash VARCHAR(255) NOT NULL,
			bio TEXT,
			avatar_url VARCHAR(500),
			is_admin BOOLEAN DEFAULT FALSE,
			show_nsfw BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT NOW()
		);

		-- New admin moderation field
		ALTER TABLE users ADD COLUMN IF NOT EXISTS is_disabled BOOLEAN DEFAULT FALSE;
		-- NSFW preference tri-state: hide|show|blur (default hide)
		ALTER TABLE users ADD COLUMN IF NOT EXISTS nsfw_pref VARCHAR(10) DEFAULT 'hide';

		CREATE TABLE IF NOT EXISTS images (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			filename VARCHAR(255) NOT NULL,
			original_name VARCHAR(255),
			file_size INTEGER,
			width INTEGER,
			height INTEGER,
			blurhash VARCHAR(100),
			dominant_color VARCHAR(7),
			is_nsfw BOOLEAN DEFAULT FALSE,
			ai_signature VARCHAR(500),
			exif_data JSONB,
			caption TEXT,
			likes_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT NOW()
		);

		-- Ensure new columns exist on already-created tables
		ALTER TABLE images ADD COLUMN IF NOT EXISTS caption TEXT;

		CREATE TABLE IF NOT EXISTS likes (
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			image_id UUID REFERENCES images(id) ON DELETE CASCADE,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, image_id)
		);

		CREATE INDEX IF NOT EXISTS idx_images_created ON images(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_images_user ON images(user_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_likes_image ON likes(image_id);
	`

	_, err := DB.Exec(schema)
	return err
}

func Close() error {
	if DB != nil {
		return DB.Close()
	}
	return nil
}
