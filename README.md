# Trough

An image stream for machine-born pictures: enforced, performant, and minimal.

### What it is
Trough is a self-hosted gallery for AI-generated images that enforces provenance. Uploads are accepted only when EXIF/XMP or C2PA metadata signals an AI source. The app provides a masonry feed, profiles, collections, and admin controls with strong defaults for security, performance, and usability.

## Purpose
Operate a precise, provenance-first image surface for synthetic media. Accept only images with reliable machine signatures; preserve or reconstruct metadata; serve clean, cache-friendly responses; do this securely and privately.

## Quick start (Docker)

```bash
cp config.example.yaml config.yaml
cp .env.example .env
# Set a strong JWT secret (>=32 random bytes)
# bash:   openssl rand -base64 48 >> .env && sed -i '' 's/^JWT_SECRET=.*/JWT_SECRET=<your-secret>/' .env
# pwsh:   $s=[Convert]::ToBase64String((New-Object Security.Cryptography.RNGCryptoServiceProvider).GetBytes(48))
#         Add-Content .env "JWT_SECRET=$s"

# Optional: seed an admin on first boot
echo ADMIN_EMAIL=admin@example.com >> .env
echo ADMIN_USERNAME=admin >> .env
echo ADMIN_PASSWORD=change-me >> .env

make docker-build
docker-compose logs -f app
```

App listens on http://localhost:8080.

## Local development

```bash
# Start PostgreSQL via compose (or provide your own DATABASE_URL)
make docker-up

go mod download
cp config.example.yaml config.yaml

# Export required env
# Windows PowerShell: $env:JWT_SECRET="<secret>"; $env:DATABASE_URL="postgres://trough:trough@localhost:5432/trough?sslmode=disable"
# bash/zsh: export JWT_SECRET=<secret>; export DATABASE_URL=postgres://trough:trough@localhost:5432/trough?sslmode=disable

# Run DB migrations (when using compose Postgres)
make migrate

# Start the server
make run
```

## How to use

- Register, then log in. Admins can disable public registration and issue invites.
- Upload an image via UI or `POST /api/upload` with form field `image`. Uploads without acceptable AI metadata are rejected.
- Toggle NSFW visibility in account settings; feed respects preferences.
- Configure site title/URL, analytics, SMTP, and storage (local or S3) in the admin panel.

### Custom Pages (CMS)

Admins can create simple content pages under the “Add/Edit Pages” tab in `/admin`.

- Pages are addressable by single-segment slugs, e.g. `/about`, `/faq`.
- A page may be a redirect by setting a Redirect URL (e.g., `/blog` -> external blog).
- Pages support rich markdown with enhancements and are sanitized client-side.
- Admins can edit in place via the Edit button on the page when logged in.

#### Markdown features

- Standard GitHub-flavored markdown (headings, lists, tables, code, images, links)
- Footnotes (markdown-it footnote):

  ```md
  Here is a statement with a footnote.[^1]

  [^1]: Footnote text appears at the bottom.
  ```

- Admonitions / callouts (containers):

  ```md
  ::: info
  Useful information block
  :::

  ::: warning
  Be careful here
  :::
  ```

  Supported types: `note`, `info`, `tip`, `warning`, `danger`, `success`, `quote`.

- Collapsible sections (great for FAQ):

  ```md
  ::: details How do I reset my password?
  Click “Forgot password” on the login screen and follow the instructions.
  :::
  ```

- Optional Table of Contents: include `[[TOC]]` where you want the TOC injected.

  - The TOC is generated from headings on the page and links to their anchors.
  - Example:

    ```md
    [[TOC]]

    # Heading 1
    ## Heading 2
    ```

Notes:

- External links open in a new tab; internal anchors (like footnotes, TOC links) navigate within the page.
- Images in markdown are responsive. You can link to uploaded assets (e.g., `/uploads/your-image.jpg`).

## Configuration

- `config.yaml` controls AI signature detection, aesthetic defaults, and rate limiting. Start from the example:

```bash
cp config.example.yaml config.yaml
```

- Docker compose mounts `./config.yaml` into the container read-only.
- If `config.yaml` is absent, sane defaults are used.

### Rate Limiting Configuration

The `rate_limiting` section in `config.yaml` allows fine-tuning of the rate limiting behavior:

```yaml
rate_limiting:
  max_entries: 1000        # Maximum IP entries to store in memory (~88KB max)
  cleanup_interval: 1m     # How often to clean up expired entries
  entry_ttl: 30m          # How long to keep IP entries before cleanup
  trusted_proxies: ["127.0.0.1", "::1"]  # Trusted proxy IPs for header extraction
  enable_debug: false     # Enable debug logging for rate limiting
```

These settings help balance memory usage, security, and performance for your specific deployment needs.

## Environment

Set via `.env` or environment variables:

```bash
# Required
JWT_SECRET=<32+ random bytes>
DATABASE_URL=postgres://trough:trough@localhost:5432/trough?sslmode=disable

# Optional admin seed (created once if not present)
ADMIN_EMAIL=
ADMIN_USERNAME=
ADMIN_PASSWORD=

# Optional cookie flags
FORCE_SECURE_COOKIES=false
ALLOW_INSECURE_COOKIES=false

# Storage (local by default)
STORAGE_PROVIDER=local            # local | s3 | r2
S3_ENDPOINT=
S3_BUCKET=
S3_ACCESS_KEY_ID=
S3_SECRET_ACCESS_KEY=
R2_ENDPOINT=
R2_BUCKET=
R2_ACCESS_KEY_ID=
R2_SECRET_ACCESS_KEY=
STORAGE_PUBLIC_BASE_URL=          # e.g. cdn.example.com or https://cdn.example.com
UPLOADS_DIR=uploads
```

Notes:
- S3/R2 require endpoint, bucket, and keys. Path-style is forced for compatibility.
- `STORAGE_PUBLIC_BASE_URL` enables CDN-style public URLs and runtime redirects from `/uploads/*`.
- CORS is limited to the `site_url` configured in admin settings.

## Running and build targets

```bash
make build           # Build binary
make run             # Run with go run
make docker-up       # Start compose services
make docker-down     # Stop compose services
make docker-build    # Build and start via compose
make migrate         # Apply schema via compose Postgres
make test            # Unit tests
make test-coverage   # Coverage report
make lint            # gofmt + go vet
```

## API surface

- Auth: `POST /api/register`, `POST /api/login`, `POST /api/logout`, `GET /api/me`
- Users: `GET /api/users/:username`, `GET /api/users/:username/images`, `GET /api/users/:username/collections`
- Images: `GET /api/feed`, `GET /api/images/:id`, `POST /api/upload`, `PATCH /api/images/:id`, `DELETE /api/images/:id`, `POST /api/images/:id/collect`
- Invites (admin): `POST /api/admin/invites`, `GET /api/admin/invites`, `DELETE /api/admin/invites/:id`, `POST /api/admin/invites/prune`
- Site settings (admin): `GET /api/admin/site`, `PUT /api/admin/site`, asset uploads and diagnostics
- **Rate limiting stats (admin)**: `GET /api/admin/rate-limiter-stats` - Monitor rate limiting performance and statistics

Notes:
- Admin endpoints require an authenticated admin user.
- All auth endpoints are rate limited to prevent brute force attacks.

## Storage

- Local: files persisted under `uploads/` and served at `/uploads/*`.
- S3/R2: objects written to bucket; public URL from `STORAGE_PUBLIC_BASE_URL` when provided.
- Admin can migrate local uploads to remote storage from the admin panel.

## Email

- Configure SMTP in admin to enable verification and password reset flows.
- Mail delivery uses bounded timeouts and a lightweight async queue.

## Security notes

- `JWT_SECRET` is mandatory; startup fails if it is missing or weak.
- Security headers include CSP, HSTS, X-Frame-Options, and others.
- Cookies are `HttpOnly` and honor TLS. Use `FORCE_SECURE_COOKIES=true` in production.
- **Enhanced Rate Limiting**: All sensitive endpoints are protected with configurable rate limiting to prevent brute force attacks:
  - Register: 5 requests per minute per IP
  - Login: 10 requests per minute per IP
  - Forgot Password: 3 requests per 5 minutes per IP
  - Reset Password: 5 requests per minute per IP
  - Verify Email: 10 requests per minute per IP
- Rate limiting includes LRU eviction, automatic cleanup, and IP validation to prevent spoofing.
- Admin users can monitor rate limiting statistics via `/api/admin/rate-limiter-stats`.

## Screenshots

To be added.

## License

MIT. See `LICENSE`.