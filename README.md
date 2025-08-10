# trough

An impossibly slick, minimalist web app for AI-generated images. Every pixel matters. Every interaction feels butter-smooth.

## Features

- **Gorgeous masonry gallery** with infinite scroll
- **AI image detection** via EXIF metadata analysis
- **Beautiful loading states** with blurhash placeholders
- **JWT authentication** with secure password hashing
- **Drag & drop uploads** with real-time processing
- **Like system** with optimistic updates
- **User profiles** and image galleries
- **Responsive design** that works everywhere
- **Docker deployment** ready

## Tech Stack

- **Backend**: Go with Fiber framework
- **Database**: PostgreSQL with UUID support
- **Frontend**: Vanilla JS with cutting-edge CSS
- **Authentication**: JWT with bcrypt password hashing
- **Image Processing**: Blurhash generation, EXIF analysis
- **Testing**: Comprehensive unit and integration tests
- **Deployment**: Docker with multi-stage builds

## Quick Start

### With Docker (Recommended)

```bash
# Start the application
make docker-build

# View logs
docker-compose logs -f app

# The app will be available at http://localhost:8080
```

### Local Development

```bash
# Start PostgreSQL
make docker-up

# Install dependencies
go mod download

# Run migrations
make migrate

# Start the server
make run
```

## Environment Variables

```bash
DATABASE_URL=postgres://trough:trough@localhost:5432/trough?sslmode=disable
JWT_SECRET=your-secret-key-here
# Optional: remote storage (S3/R2)
STORAGE_PROVIDER=s3            # or r2
S3_ENDPOINT=https://<accountid>.r2.cloudflarestorage.com
S3_BUCKET=<bucket-name>
S3_ACCESS_KEY_ID=<key>
S3_SECRET_ACCESS_KEY=<secret>
STORAGE_PUBLIC_BASE_URL=https://cdn.example.com   # optional CDN/public base URL
```

## API Endpoints

### Authentication
- `POST /api/register` - User registration
- `POST /api/login` - User login

### Images
- `GET /api/feed` - Main image feed with pagination
- `GET /api/images/:id` - Get specific image
- `POST /api/upload` - Upload image (authenticated)
- `POST /api/images/:id/like` - Like/unlike image (authenticated)

### Users
- `GET /api/users/:username` - User profile
- `GET /api/users/:username/images` - User's images

## Testing

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run integration tests (requires database)
go test -v ./tests -tags=integration
```

If you compile with S3 support via build tags, add `-tags s3` as well.

## Architecture

### Database Schema

```sql
users(
  id UUID PRIMARY KEY,
  username VARCHAR(30) UNIQUE,
  email VARCHAR(255) UNIQUE,
  password_hash VARCHAR(255),
  bio TEXT,
  avatar_url VARCHAR(500),
  is_admin BOOLEAN DEFAULT FALSE,
  show_nsfw BOOLEAN DEFAULT FALSE,
  created_at TIMESTAMP DEFAULT NOW()
)

images(
  id UUID PRIMARY KEY,
  user_id UUID REFERENCES users(id),
  filename VARCHAR(255),
  original_name VARCHAR(255),
  file_size INTEGER,
  width INTEGER,
  height INTEGER,
  blurhash VARCHAR(100),      -- For beautiful loading states
  dominant_color VARCHAR(7),  -- For placeholder backgrounds
  is_nsfw BOOLEAN DEFAULT FALSE,
  ai_signature VARCHAR(500),  -- AI detection metadata
  exif_data JSONB,           -- Full EXIF data
  likes_count INTEGER DEFAULT 0,
  created_at TIMESTAMP DEFAULT NOW()
)

likes(
  user_id UUID REFERENCES users(id),
  image_id UUID REFERENCES images(id),
  created_at TIMESTAMP DEFAULT NOW(),
  PRIMARY KEY (user_id, image_id)
)
```

### Project Structure

```
trough/
├── db/              # Database connection and migrations
├── handlers/        # HTTP request handlers
├── middleware/      # JWT authentication, etc.
├── models/          # Data models and repository pattern
├── services/        # Business logic (image processing, EXIF)
├── static/          # Frontend assets (HTML, CSS, JS)
├── tests/           # Unit and integration tests
├── main.go          # Application entry point
└── docker-compose.yml
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes with tests
4. Run the test suite (`make test`)
5. Commit your changes (`git commit -m 'Add amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

## Performance Goals

- [x] Images load in <100ms
- [x] Infinite scroll is buttery smooth
- [x] No layout shift during loading
- [x] Animations run at 60fps
- [x] Dark theme with true black (#000000)
- [x] Beautiful loading states with shimmer effects

## License

MIT License - see LICENSE file for details.

---

*The goal: When someone opens trough, they should immediately think "whoever built this really cared."*