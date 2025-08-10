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
		-- Moderator role
		ALTER TABLE users ADD COLUMN IF NOT EXISTS is_moderator BOOLEAN DEFAULT FALSE;
		-- Email verified (default true for legacy users)
		ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified BOOLEAN DEFAULT TRUE;

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

		-- Site settings (single row, id=1)
		CREATE TABLE IF NOT EXISTS site_settings (
			id SMALLINT PRIMARY KEY DEFAULT 1,
			site_name TEXT DEFAULT 'TROUGH',
			site_url TEXT DEFAULT '',
			seo_title TEXT DEFAULT '',
			seo_description TEXT DEFAULT '',
			social_image_url TEXT DEFAULT '',
			smtp_host TEXT DEFAULT '',
			smtp_port INTEGER DEFAULT 0,
			smtp_username TEXT DEFAULT '',
			smtp_password TEXT DEFAULT '',
			smtp_from_email TEXT DEFAULT '',
			smtp_tls BOOLEAN DEFAULT FALSE,
			favicon_path TEXT DEFAULT '',
			require_email_verification BOOLEAN DEFAULT FALSE,
			-- storage config
			storage_provider TEXT DEFAULT 'local',
			s3_endpoint TEXT DEFAULT '',
			s3_bucket TEXT DEFAULT '',
			s3_access_key TEXT DEFAULT '',
			s3_secret_key TEXT DEFAULT '',
			s3_force_path_style BOOLEAN DEFAULT TRUE,
			public_base_url TEXT DEFAULT '',
			updated_at TIMESTAMP DEFAULT NOW()
		);

		INSERT INTO site_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

		-- Password reset tokens
		CREATE TABLE IF NOT EXISTS password_resets (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			token VARCHAR(255) UNIQUE NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		);
		ALTER TABLE password_resets ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT NOW();

		-- Email verification tokens
		CREATE TABLE IF NOT EXISTS email_verifications (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			token VARCHAR(255) UNIQUE NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT NOW()
		);
		ALTER TABLE email_verifications ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT NOW();

		-- Ensure new storage columns exist for upgrades
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS require_email_verification BOOLEAN DEFAULT FALSE;
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS smtp_tls BOOLEAN DEFAULT FALSE;
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS smtp_from_email TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS favicon_path TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS storage_provider TEXT DEFAULT 'local';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_endpoint TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_bucket TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_access_key TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_secret_key TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_force_path_style BOOLEAN DEFAULT TRUE;
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS public_base_url TEXT DEFAULT '';
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
