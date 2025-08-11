-- db/schema.sql
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE users (
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

CREATE TABLE images (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    filename VARCHAR(255) NOT NULL,
    original_name VARCHAR(255),
    file_size INTEGER,
    width INTEGER,
    height INTEGER,
    blurhash VARCHAR(100), -- For beautiful loading states
    dominant_color VARCHAR(7), -- For placeholder backgrounds
    is_nsfw BOOLEAN DEFAULT FALSE,
    ai_signature VARCHAR(500),
    ai_provider VARCHAR(100),
    exif_data JSONB,
    likes_count INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE likes (
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    image_id UUID REFERENCES images(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT NOW(),
    PRIMARY KEY (user_id, image_id)
);

-- Indexes for performance
CREATE INDEX idx_images_created ON images(created_at DESC);
CREATE INDEX idx_images_user ON images(user_id, created_at DESC);
CREATE INDEX idx_likes_image ON likes(image_id);