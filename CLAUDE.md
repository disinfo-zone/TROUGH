# Agent Instructions: Building trough

## Vision
Build "trough" - an impossibly slick, minimalist web app for AI-generated images. Every pixel matters. Every interaction should feel butter-smooth. This isn't just an image gallery - it's a piece of digital art itself.

## Core Stack
- **Backend**: Go with Fiber (fast, minimal)
- **Database**: PostgreSQL
- **Frontend**: Vanilla JS with cutting-edge CSS
- **Container**: Single Docker container with embedded static files

## Aesthetic Requirements
**CRITICAL**: This app must look absolutely stunning. Think:
- Apple-level attention to detail
- Subtle animations that delight
- Typography that makes designers weep
- Interactions that feel like magic
- Dark mode that's actually beautiful
- Loading states that are art

## Project Setup

### Initialize Git Repository
```bash
mkdir trough && cd trough
git init
git branch -M main

# Create .gitignore
cat > .gitignore << 'EOF'
.env
.DS_Store
*.log
/tmp
/uploads
/data
*.exe
trough
EOF

git add .gitignore
git commit -m "Initial commit with .gitignore"
```

## Phase 1: Minimal Structure

### Create Project Structure
```bash
# Create directories
mkdir -p {static/{css,js,assets},handlers,models,services,db}

# Create initial files
touch main.go go.mod config.yaml docker-compose.yml Dockerfile
touch static/index.html static/css/style.css static/js/app.js
touch db/schema.sql README.md

git add .
git commit -m "Add project structure"
```

### Docker Setup (Minimal)
```yaml
# docker-compose.yml
version: '3.8'

services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      - DATABASE_URL=postgres://trough:trough@db:5432/trough?sslmode=disable
    volumes:
      - ./uploads:/app/uploads
      - ./config.yaml:/app/config.yaml:ro
    depends_on:
      - db

  db:
    image: postgres:15-alpine
    environment:
      - POSTGRES_USER=trough
      - POSTGRES_PASSWORD=trough
      - POSTGRES_DB=trough
    volumes:
      - postgres_data:/var/lib/postgresql/data

volumes:
  postgres_data:
```

```dockerfile
# Dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o trough .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /build/trough .
COPY --from=builder /build/static ./static
COPY --from=builder /build/db ./db
EXPOSE 8080
CMD ["./trough"]
```

```bash
git add docker-compose.yml Dockerfile
git commit -m "Add Docker configuration"
```

## Phase 2: Database & Models

### Schema
```sql
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
```

```bash
git add db/schema.sql
git commit -m "Add database schema with aesthetic fields"
```

## Phase 3: Go Backend with Fiber

### Initialize Go Module
```bash
go mod init github.com/yourusername/trough
go get github.com/gofiber/fiber/v2
go get github.com/gofiber/jwt/v3
go get github.com/lib/pq
go get github.com/jmoiron/sqlx
go get github.com/dsoprea/go-exif/v3
go get github.com/bbrks/go-blurhash
go get golang.org/x/crypto/bcrypt
go get github.com/google/uuid
go get gopkg.in/yaml.v3

git add go.mod go.sum
git commit -m "Initialize Go dependencies"
```

### Main Application
```go
// main.go
package main

import (
    "log"
    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/cors"
    "github.com/gofiber/fiber/v2/middleware/logger"
    "github.com/gofiber/fiber/v2/middleware/compress"
)

func main() {
    app := fiber.New(fiber.Config{
        BodyLimit: 10 * 1024 * 1024, // 10MB
        ErrorHandler: customErrorHandler,
    })

    // Middleware for that premium feel
    app.Use(logger.New())
    app.Use(compress.New())
    app.Use(cors.New())

    // Serve static files with caching
    app.Static("/", "./static", fiber.Static{
        Compress: true,
        CacheDuration: 3600,
    })
    
    app.Static("/uploads", "./uploads", fiber.Static{
        Compress: true,
        CacheDuration: 86400,
    })

    // API routes
    api := app.Group("/api")
    
    // Auth
    api.Post("/register", handlers.Register)
    api.Post("/login", handlers.Login)
    
    // Images - the heart of the app
    api.Get("/feed", handlers.GetFeed) // Main feed with infinite scroll
    api.Get("/images/:id", handlers.GetImage)
    api.Post("/upload", middleware.Protected(), handlers.Upload)
    api.Post("/images/:id/like", middleware.Protected(), handlers.LikeImage)
    
    // Users
    api.Get("/users/:username", handlers.GetProfile)
    api.Get("/users/:username/images", handlers.GetUserImages)
    
    log.Fatal(app.Listen(":8080"))
}
```

```bash
git add main.go
git commit -m "Add Fiber server setup"
```

### Configuration
```yaml
# config.yaml
ai_signatures:
  - key: "DigitalSourceType"
    value: "http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia"
  - key: "Software"
    contains: ["Midjourney", "DALL-E", "Stable Diffusion", "Flux"]

aesthetic:
  blur_radius: 20
  thumbnail_quality: 85
  max_width: 2048
  formats: [".jpg", ".jpeg", ".png", ".webp"]
```

## Phase 4: The Frontend - Make It Gorgeous

### HTML - Minimal, Semantic, Beautiful
```html
<!-- static/index.html -->
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>trough · ai imagery</title>
    <link rel="stylesheet" href="/css/style.css">
    <link rel="preconnect" href="https://fonts.googleapis.com">
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@200;400;500&display=swap" rel="stylesheet">
</head>
<body>
    <!-- Minimal header - just the essentials -->
    <header class="header">
        <h1 class="logo">trough</h1>
        <nav class="nav">
            <button class="nav-btn" id="auth-btn">enter</button>
        </nav>
    </header>

    <!-- The gallery - where the magic happens -->
    <main class="gallery" id="gallery">
        <!-- Images dynamically loaded here -->
    </main>

    <!-- Full-screen image viewer -->
    <div class="lightbox" id="lightbox">
        <div class="lightbox-content">
            <img class="lightbox-image" id="lightbox-img">
            <div class="lightbox-info">
                <a class="lightbox-user" id="lightbox-user"></a>
                <button class="lightbox-like" id="lightbox-like">
                    <svg><!-- Heart icon --></svg>
                </button>
            </div>
        </div>
    </div>

    <!-- Upload zone (hidden by default) -->
    <div class="upload-zone" id="upload-zone">
        <div class="upload-inner">
            <p>Drop your AI imagery</p>
        </div>
    </div>

    <script src="/js/app.js" type="module"></script>
</body>
</html>
```

### CSS - The Soul of the Design
```css
/* static/css/style.css */

/* Design System */
:root {
    --black: #000000;
    --white: #ffffff;
    --gray-900: #0a0a0a;
    --gray-800: #1a1a1a;
    --gray-700: #2a2a2a;
    --gray-300: #a0a0a0;
    --gray-100: #f0f0f0;
    
    --accent: #00ff88;
    --accent-dim: #00ff8820;
    
    --font-sans: 'Inter', -apple-system, system-ui, sans-serif;
    --transition: cubic-bezier(0.4, 0, 0.2, 1);
}

* {
    margin: 0;
    padding: 0;
    box-sizing: border-box;
}

html {
    background: var(--black);
    color: var(--white);
    font-family: var(--font-sans);
    -webkit-font-smoothing: antialiased;
    -moz-osx-font-smoothing: grayscale;
}

body {
    min-height: 100vh;
    overscroll-behavior: none;
}

/* Header - Floating, Minimal */
.header {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    z-index: 100;
    padding: 2rem 3rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
    background: linear-gradient(180deg, 
        rgba(0,0,0,0.8) 0%, 
        rgba(0,0,0,0) 100%);
    backdrop-filter: blur(10px);
    -webkit-backdrop-filter: blur(10px);
}

.logo {
    font-size: 1.25rem;
    font-weight: 200;
    letter-spacing: 0.05em;
    cursor: pointer;
    transition: opacity 0.3s var(--transition);
}

.logo:hover {
    opacity: 0.7;
}

/* Navigation Button */
.nav-btn {
    background: transparent;
    border: 1px solid rgba(255,255,255,0.2);
    color: var(--white);
    padding: 0.5rem 1.5rem;
    border-radius: 100px;
    font-size: 0.875rem;
    font-weight: 400;
    cursor: pointer;
    transition: all 0.3s var(--transition);
    backdrop-filter: blur(10px);
}

.nav-btn:hover {
    background: rgba(255,255,255,0.1);
    border-color: rgba(255,255,255,0.3);
    transform: translateY(-1px);
}

/* Gallery - Masonry Magic */
.gallery {
    padding: 8rem 2rem 4rem;
    columns: 5;
    column-gap: 1rem;
}

@media (max-width: 1400px) { .gallery { columns: 4; } }
@media (max-width: 1000px) { .gallery { columns: 3; } }
@media (max-width: 700px) { .gallery { columns: 2; } }
@media (max-width: 400px) { .gallery { columns: 1; } }

/* Image Cards - The Stars of the Show */
.image-card {
    position: relative;
    margin-bottom: 1rem;
    break-inside: avoid;
    cursor: zoom-in;
    overflow: hidden;
    border-radius: 0.5rem;
    animation: fadeIn 0.6s var(--transition) both;
}

@keyframes fadeIn {
    from {
        opacity: 0;
        transform: translateY(20px) scale(0.95);
    }
    to {
        opacity: 1;
        transform: translateY(0) scale(1);
    }
}

.image-card img {
    width: 100%;
    height: auto;
    display: block;
    transition: transform 0.6s var(--transition);
}

.image-card::before {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(180deg, 
        transparent 0%, 
        transparent 70%, 
        rgba(0,0,0,0.4) 100%);
    opacity: 0;
    transition: opacity 0.3s var(--transition);
    pointer-events: none;
}

.image-card:hover img {
    transform: scale(1.05);
}

.image-card:hover::before {
    opacity: 1;
}

/* Image Loading State - Beautiful Skeletons */
.image-skeleton {
    aspect-ratio: 1;
    background: linear-gradient(
        90deg,
        var(--gray-900) 0%,
        var(--gray-800) 50%,
        var(--gray-900) 100%
    );
    background-size: 200% 100%;
    animation: shimmer 1.5s infinite;
    border-radius: 0.5rem;
    margin-bottom: 1rem;
}

@keyframes shimmer {
    0% { background-position: -200% 0; }
    100% { background-position: 200% 0; }
}

/* Lightbox - Full Screen Beauty */
.lightbox {
    position: fixed;
    inset: 0;
    z-index: 1000;
    background: rgba(0,0,0,0.95);
    backdrop-filter: blur(20px);
    -webkit-backdrop-filter: blur(20px);
    display: flex;
    align-items: center;
    justify-content: center;
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.3s var(--transition);
}

.lightbox.active {
    opacity: 1;
    pointer-events: all;
}

.lightbox-content {
    position: relative;
    max-width: 90vw;
    max-height: 90vh;
}

.lightbox-image {
    max-width: 100%;
    max-height: 90vh;
    object-fit: contain;
    border-radius: 0.5rem;
    animation: zoomIn 0.3s var(--transition);
}

@keyframes zoomIn {
    from {
        transform: scale(0.9);
        opacity: 0;
    }
    to {
        transform: scale(1);
        opacity: 1;
    }
}

/* Upload Zone - Drag & Drop Delight */
.upload-zone {
    position: fixed;
    inset: 0;
    z-index: 2000;
    background: rgba(0,0,0,0.98);
    display: none;
    align-items: center;
    justify-content: center;
}

.upload-zone.active {
    display: flex;
}

.upload-inner {
    width: 60%;
    height: 60%;
    border: 2px dashed var(--accent);
    border-radius: 1rem;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-dim);
    animation: pulse 2s infinite;
}

@keyframes pulse {
    0%, 100% { opacity: 0.5; }
    50% { opacity: 1; }
}

/* Loading Indicator - Smooth as Butter */
.loader {
    position: fixed;
    bottom: 2rem;
    left: 50%;
    transform: translateX(-50%);
    width: 40px;
    height: 40px;
    border: 2px solid var(--gray-700);
    border-top-color: var(--accent);
    border-radius: 50%;
    animation: spin 0.6s linear infinite;
}

@keyframes spin {
    to { transform: translateX(-50%) rotate(360deg); }
}
```

### JavaScript - Smooth Interactions
```javascript
// static/js/app.js
class Trough {
    constructor() {
        this.images = [];
        this.page = 1;
        this.loading = false;
        this.hasMore = true;
        this.gallery = document.getElementById('gallery');
        this.lightbox = document.getElementById('lightbox');
        this.uploadZone = document.getElementById('upload-zone');
    }

    async init() {
        await this.loadImages();
        this.setupInfiniteScroll();
        this.setupLightbox();
        this.setupUpload();
        this.animateOnScroll();
    }

    async loadImages() {
        if (this.loading || !this.hasMore) return;
        
        this.loading = true;
        this.showLoader();
        
        try {
            const res = await fetch(`/api/feed?page=${this.page}`);
            const data = await res.json();
            
            if (data.images.length === 0) {
                this.hasMore = false;
                return;
            }
            
            this.renderImages(data.images);
            this.page++;
        } finally {
            this.loading = false;
            this.hideLoader();
        }
    }

    renderImages(images) {
        images.forEach((image, index) => {
            const card = document.createElement('div');
            card.className = 'image-card';
            card.style.animationDelay = `${index * 0.05}s`;
            
            // Use blurhash for beautiful loading
            if (image.blurhash) {
                card.style.backgroundColor = image.dominant_color;
            }
            
            const img = new Image();
            img.onload = () => {
                card.appendChild(img);
                // Trigger reflow for smooth animation
                card.offsetHeight;
            };
            img.src = `/uploads/${image.filename}`;
            img.alt = image.original_name;
            
            card.addEventListener('click', () => this.openLightbox(image));
            
            this.gallery.appendChild(card);
        });
    }

    openLightbox(image) {
        const img = document.getElementById('lightbox-img');
        img.src = `/uploads/${image.filename}`;
        
        const user = document.getElementById('lightbox-user');
        user.textContent = `@${image.username}`;
        user.href = `/@${image.username}`;
        
        this.lightbox.classList.add('active');
        document.body.style.overflow = 'hidden';
        
        // ESC to close
        const closeHandler = (e) => {
            if (e.key === 'Escape') {
                this.closeLightbox();
                document.removeEventListener('keydown', closeHandler);
            }
        };
        document.addEventListener('keydown', closeHandler);
        
        // Click outside to close
        this.lightbox.onclick = (e) => {
            if (e.target === this.lightbox) {
                this.closeLightbox();
            }
        };
    }

    closeLightbox() {
        this.lightbox.classList.remove('active');
        document.body.style.overflow = '';
    }

    setupInfiniteScroll() {
        let ticking = false;
        
        const handleScroll = () => {
            if (!ticking) {
                requestAnimationFrame(() => {
                    const { scrollTop, scrollHeight, clientHeight } = document.documentElement;
                    
                    if (scrollTop + clientHeight >= scrollHeight - 1000) {
                        this.loadImages();
                    }
                    
                    ticking = false;
                });
                
                ticking = true;
            }
        };
        
        window.addEventListener('scroll', handleScroll, { passive: true });
    }

    setupUpload() {
        // Only for logged-in users on their profile
        if (!window.location.pathname.startsWith('/@')) return;
        
        let dragCounter = 0;
        
        document.addEventListener('dragenter', (e) => {
            e.preventDefault();
            dragCounter++;
            if (dragCounter === 1) {
                this.uploadZone.classList.add('active');
            }
        });
        
        document.addEventListener('dragleave', (e) => {
            e.preventDefault();
            dragCounter--;
            if (dragCounter === 0) {
                this.uploadZone.classList.remove('active');
            }
        });
        
        document.addEventListener('dragover', (e) => {
            e.preventDefault();
        });
        
        document.addEventListener('drop', async (e) => {
            e.preventDefault();
            dragCounter = 0;
            this.uploadZone.classList.remove('active');
            
            const files = Array.from(e.dataTransfer.files);
            for (const file of files) {
                if (file.type.startsWith('image/')) {
                    await this.uploadImage(file);
                }
            }
        });
    }

    async uploadImage(file) {
        const formData = new FormData();
        formData.append('image', file);
        
        // Show upload progress with style
        const progressBar = this.createProgressBar();
        document.body.appendChild(progressBar);
        
        try {
            const res = await fetch('/api/upload', {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${localStorage.getItem('token')}`
                },
                body: formData
            });
            
            if (res.ok) {
                const image = await res.json();
                // Prepend to gallery with animation
                this.renderImages([image]);
                progressBar.classList.add('complete');
            } else {
                progressBar.classList.add('error');
            }
        } finally {
            setTimeout(() => progressBar.remove(), 1000);
        }
    }

    createProgressBar() {
        const bar = document.createElement('div');
        bar.className = 'upload-progress';
        bar.innerHTML = '<div class="upload-progress-bar"></div>';
        return bar;
    }

    showLoader() {
        if (!document.querySelector('.loader')) {
            const loader = document.createElement('div');
            loader.className = 'loader';
            document.body.appendChild(loader);
        }
    }

    hideLoader() {
        const loader = document.querySelector('.loader');
        if (loader) loader.remove();
    }

    animateOnScroll() {
        const observer = new IntersectionObserver((entries) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    entry.target.classList.add('visible');
                }
            });
        }, { threshold: 0.1 });
        
        document.querySelectorAll('.image-card').forEach(card => {
            observer.observe(card);
        });
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    const app = new Trough();
    app.init();
});
```

```bash
git add static/
git commit -m "Add gorgeous frontend with smooth interactions"
```

## Phase 5: EXIF Verification Service

```go
// services/exif.go
package services

import (
    "github.com/dsoprea/go-exif/v3"
    "strings"
)

func VerifyAIImage(imagePath string, config Config) (bool, string) {
    rawExif, err := exif.SearchFileAndExtractExif(imagePath)
    if err != nil {
        return false, ""
    }
    
    entries, _, err := exif.GetFlatExifData(rawExif, nil)
    if err != nil {
        return false, ""
    }
    
    for _, entry := range entries {
        for _, sig := range config.AISignatures {
            if entry.TagName == sig.Key {
                if sig.Contains != "" {
                    for _, substr := range sig.Contains {
                        if strings.Contains(entry.Formatted, substr) {
                            return true, entry.Formatted
                        }
                    }
                } else if entry.Formatted == sig.Value {
                    return true, entry.Formatted
                }
            }
        }
    }
    
    return false, ""
}
```

```bash
git add services/
git commit -m "Add EXIF verification for AI images"
```

## Phase 6: Image Processing for Beauty

```go
// services/image.go
package services

import (
    "github.com/bbrks/go-blurhash"
    "image"
    _ "image/jpeg"
    _ "image/png"
)

func ProcessImage(file multipart.File) (ImageMeta, error) {
    // Decode image
    img, format, err := image.Decode(file)
    if err != nil {
        return ImageMeta{}, err
    }
    
    bounds := img.Bounds()
    meta := ImageMeta{
        Width:  bounds.Dx(),
        Height: bounds.Dy(),
        Format: format,
    }
    
    // Generate blurhash for beautiful loading
    hash, err := blurhash.Encode(4, 3, img)
    if err == nil {
        meta.Blurhash = hash
    }
    
    // Extract dominant color
    meta.DominantColor = extractDominantColor(img)
    
    return meta, nil
}
```

```bash
git add services/image.go
git commit -m "Add image processing with blurhash"
```

## Phase 7: Run & Deploy

```bash
# Build and run
docker-compose up --build -d

# Check logs
docker-compose logs -f app

# Initialize database
docker-compose exec app psql $DATABASE_URL -f /app/db/schema.sql

git add -A
git commit -m "Complete trough v1.0 - impossibly slick AI image gallery"
git tag v1.0.0
git push origin main --tags
```

## Critical Success Metrics

### Performance
- [ ] Images load in <100ms
- [ ] Infinite scroll is buttery smooth
- [ ] No layout shift during loading
- [ ] Animations run at 60fps

### Aesthetics
- [ ] Typography is crisp and beautiful
- [ ] Spacing is perfect - not too tight, not too loose
- [ ] Animations feel natural and delightful
- [ ] Dark theme is actually dark (true black)
- [ ] Loading states are works of art
- [ ] Every interaction has feedback

### User Experience
- [ ] Upload is drag-and-drop simple
- [ ] Navigation is intuitive
- [ ] Errors are helpful, not scary
- [ ] Mobile experience is flawless

## Design Philosophy

**Less is More**: Every element must earn its place. If it doesn't make the experience better, remove it.

**Performance is Aesthetic**: A slow site can never be beautiful. Optimize everything.

**Delight in Details**: The difference between good and incredible is in the micro-interactions, the perfect easings, the thoughtful shadows.

**Dark by Default**: This is 2025. Dark mode isn't an option, it's the default. Make it gorgeous.

## Remember

This isn't just a gallery. It's a showcase for AI art that should feel as premium as the images it displays. Every pixel, every animation, every interaction should whisper "quality."

The goal: When someone opens trough, they should immediately think "whoever built this really cared."