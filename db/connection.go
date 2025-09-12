package db

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

var (
	DB            *sqlx.DB
	reconnectLock sync.Mutex
)

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
	// Recycle connections to prevent memory bloat and stale server-side state
	DB.SetConnMaxLifetime(30 * time.Minute)
	DB.SetConnMaxIdleTime(5 * time.Minute)

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
        -- Track password change time for token invalidation (NULL means never changed)
        ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at TIMESTAMP NULL;

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
			ai_provider VARCHAR(100),
			exif_data JSONB,
			caption TEXT,
			likes_count INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT NOW()
		);

		-- Ensure new columns exist on already-created tables
		ALTER TABLE images ADD COLUMN IF NOT EXISTS caption TEXT;
		ALTER TABLE images ADD COLUMN IF NOT EXISTS ai_provider VARCHAR(100);

		CREATE TABLE IF NOT EXISTS likes (
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			image_id UUID REFERENCES images(id) ON DELETE CASCADE,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, image_id)
		);

		-- Collections: users can collect images uploaded by others
		CREATE TABLE IF NOT EXISTS collections (
			user_id UUID REFERENCES users(id) ON DELETE CASCADE,
			image_id UUID REFERENCES images(id) ON DELETE CASCADE,
			created_at TIMESTAMP DEFAULT NOW(),
			PRIMARY KEY (user_id, image_id)
		);

		CREATE INDEX IF NOT EXISTS idx_images_created ON images(created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_images_created_id ON images(created_at DESC, id DESC);
		CREATE INDEX IF NOT EXISTS idx_images_user ON images(user_id, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_images_user_created_id ON images(user_id, created_at DESC, id DESC);
		CREATE INDEX IF NOT EXISTS idx_likes_image ON likes(image_id);
		CREATE INDEX IF NOT EXISTS idx_collections_user ON collections(user_id);
		CREATE INDEX IF NOT EXISTS idx_collections_image ON collections(image_id);

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
			public_registration_enabled BOOLEAN DEFAULT TRUE,
			-- storage config
			storage_provider TEXT DEFAULT 'local',
			s3_endpoint TEXT DEFAULT '',
			s3_bucket TEXT DEFAULT '',
			s3_access_key TEXT DEFAULT '',
			s3_secret_key TEXT DEFAULT '',
			s3_force_path_style BOOLEAN DEFAULT TRUE,
			public_base_url TEXT DEFAULT '',
			-- analytics/tracking config
			analytics_enabled BOOLEAN DEFAULT FALSE,
			analytics_provider TEXT DEFAULT '', -- '', 'ga4', 'umami', 'plausible'
			ga4_measurement_id TEXT DEFAULT '',
			umami_src TEXT DEFAULT '',
			umami_website_id TEXT DEFAULT '',
			plausible_src TEXT DEFAULT '',
			plausible_domain TEXT DEFAULT '',
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
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS public_registration_enabled BOOLEAN DEFAULT TRUE;
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS storage_provider TEXT DEFAULT 'local';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_endpoint TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_bucket TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_access_key TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_secret_key TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS s3_force_path_style BOOLEAN DEFAULT TRUE;
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS public_base_url TEXT DEFAULT '';

		-- Analytics columns (safe defaults)
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS analytics_enabled BOOLEAN DEFAULT FALSE;
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS analytics_provider TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS ga4_measurement_id TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS umami_src TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS umami_website_id TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS plausible_src TEXT DEFAULT '';
		ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS plausible_domain TEXT DEFAULT '';

			-- Backup scheduler settings
			ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS backup_enabled BOOLEAN DEFAULT FALSE;
			ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS backup_interval TEXT DEFAULT '24h';
			ALTER TABLE site_settings ADD COLUMN IF NOT EXISTS backup_keep_days INTEGER DEFAULT 7;

			-- Invitation codes for gated registration
		CREATE TABLE IF NOT EXISTS invites (
			id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
			code VARCHAR(64) UNIQUE NOT NULL,
			max_uses INTEGER,
			uses INTEGER NOT NULL DEFAULT 0,
			expires_at TIMESTAMP NULL,
			created_by UUID REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMP DEFAULT NOW(),
			last_used_at TIMESTAMP NULL
		);
		CREATE UNIQUE INDEX IF NOT EXISTS idx_invites_code ON invites(code);
		-- Ensure uses column exists (for upgrades) and constraints reasonable
		ALTER TABLE invites ADD COLUMN IF NOT EXISTS uses INTEGER DEFAULT 0;
		ALTER TABLE invites ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMP NULL;

			-- CMS tombstones: remember admin-deleted default slugs to avoid re-seeding
			CREATE TABLE IF NOT EXISTS cms_tombstones (
				slug VARCHAR(60) PRIMARY KEY,
				deleted_at TIMESTAMP NOT NULL DEFAULT NOW()
			);

			-- CMS pages
			CREATE TABLE IF NOT EXISTS pages (
				id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
				slug VARCHAR(60) UNIQUE NOT NULL,
				title VARCHAR(200) NOT NULL,
            markdown TEXT NOT NULL DEFAULT '',
            html TEXT NOT NULL DEFAULT '',
				is_published BOOLEAN NOT NULL DEFAULT FALSE,
				redirect_url TEXT NULL,
				meta_title VARCHAR(200),
				meta_description VARCHAR(300),
				created_at TIMESTAMP NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMP NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_pages_published ON pages(is_published);
			-- Constrain slug to single path segment [a-z0-9-], no leading/trailing hyphens
			DO $$ BEGIN
			  IF NOT EXISTS (
			    SELECT 1 FROM pg_constraint WHERE conname = 'pages_slug_check'
			  ) THEN
			    ALTER TABLE pages
			      ADD CONSTRAINT pages_slug_check CHECK (slug ~ '^[a-z0-9](?:[a-z0-9-]{0,58}[a-z0-9])?$');
			  END IF;
			END $$;
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

// Ping checks the database connection and returns an error if it's not available.
func Ping(ctx context.Context) error {
	if DB == nil {
		return fmt.Errorf("database not connected")
	}
	return DB.PingContext(ctx)
}

// Reconnect closes the existing database connection and establishes a new one.
// It uses a mutex to prevent race conditions from multiple concurrent requests.
func Reconnect() error {
	reconnectLock.Lock()
	defer reconnectLock.Unlock()

	// The lock ensures this block is only executed by one goroutine at a time.
	// We proceed directly to closing the old connection and creating a new one.

	if err := Close(); err != nil {
		// Log the error but don't fail if closing fails, as we're trying to reconnect anyway
		fmt.Printf("Error closing database connection during reconnect: %v\n", err)
	}
	fmt.Println("Attempting to reconnect to the database...")
	return Connect()
}
