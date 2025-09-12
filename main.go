package main

import (
	"context"
	"html"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	gjson "github.com/goccy/go-json"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/etag"
	"github.com/gofiber/fiber/v2/middleware/logger"

	// limiter intentionally omitted to avoid adding new dependencies in this change
	"github.com/google/uuid"
	"github.com/yourusername/trough/db"
	"github.com/yourusername/trough/handlers"
	"github.com/yourusername/trough/middleware"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

func customErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
	}
	// Avoid leaking internal errors to clients. Log the detailed error server-side.
	log.Printf("error: %v", err)
	return c.Status(code).JSON(fiber.Map{
		"error": "internal server error",
	})
}

func maybeSeedAdmin(userRepo models.UserRepositoryInterface) {
	adminEmail := os.Getenv("ADMIN_EMAIL")
	adminUser := os.Getenv("ADMIN_USERNAME")
	adminPass := os.Getenv("ADMIN_PASSWORD")
	if adminEmail == "" || adminUser == "" || adminPass == "" {
		return
	}
	// Normalize admin username to match username policy (lowercase, trimmed)
	adminUser = strings.ToLower(strings.TrimSpace(adminUser))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := userRepo.GetByEmail(ctx, adminEmail); err == nil {
		log.Printf("Admin seed: user %s already exists", adminEmail)
		return
	}
	u := &models.User{Username: adminUser, Email: adminEmail}
	if err := u.HashPassword(adminPass); err != nil {
		log.Printf("Admin seed: failed to hash password: %v", err)
		return
	}
	if err := userRepo.Create(u); err != nil {
		log.Printf("Admin seed: create failed: %v", err)
		return
	}
	if err := userRepo.SetAdmin(u.ID, true); err != nil {
		log.Printf("Admin seed: set admin failed: %v", err)
		return
	}
	log.Printf("Admin seed: created admin %s (@%s)", adminEmail, adminUser)
}

// indexWithMetaHandler serves index.html with server-side SEO/OG meta tags injected from site settings
// and, for /i/:id routes, from the specific image. For /@:username, it uses the user's bio and latest image.
// For single-segment CMS pages, it keeps index SEO but adjusts the <title> to the page title (or meta title).
func indexWithMetaHandler(
	siteRepo models.SiteSettingsRepositoryInterface,
	imageRepo models.ImageRepositoryInterface,
	userRepo models.UserRepositoryInterface,
	pageRepo models.PageRepositoryInterface,
) fiber.Handler {
	// Precompile regexes once
	titleRe := regexp.MustCompile(`(?is)<title>.*?</title>`)
	descRe := regexp.MustCompile(`(?is)<meta\s+name=["']description["'][^>]*>`)
	// Validation helpers
	gaIDRe := regexp.MustCompile(`^G-[A-Z0-9]{6,}`)
	uuidRe := regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	httpsJSRe := regexp.MustCompile(`^https://[A-Za-z0-9.-]+(?::\d{2,5})?/.+\.js(\?.*)?$`)
	domainRe := regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)

	// Cache base HTML to avoid disk reads on every request
	var baseHTMLOnce sync.Once
	var baseHTML string
	loadBase := func() {
		if b, err := os.ReadFile("./static/index.html"); err == nil {
			baseHTML = string(b)
		}
	}
	return func(c *fiber.Ctx) error {
		baseHTMLOnce.Do(loadBase)
		htmlStr := baseHTML
		if htmlStr == "" {
			// Fallback to static file if cache missing or read failed
			return c.SendFile("./static/index.html")
		}

		set, _ := siteRepo.Get()

		// Defaults from site settings
		title := strings.TrimSpace(set.SEOTitle)
		if title == "" {
			if strings.TrimSpace(set.SiteName) != "" {
				title = set.SiteName + " · AI IMAGERY"
			} else {
				title = "TROUGH · AI IMAGERY"
			}
		}
		description := strings.TrimSpace(set.SEODescription)
		baseURL := strings.TrimSpace(set.SiteURL)
		if baseURL != "" {
			baseURL = strings.TrimRight(baseURL, "/")
		}
		path := c.OriginalURL()
		// Compute origin (scheme + host) to make absolute URLs when needed
		origin := baseURL
		if origin == "" {
			proto := c.Protocol()
			if proto == "" {
				proto = "https"
			}
			origin = proto + "://" + c.Hostname()
		}
		fullURL := origin + path
		imageURL := strings.TrimSpace(set.SocialImageURL)
		ogType := "website"

		// If this is an image page, override meta using the image
		if strings.HasPrefix(c.Path(), "/i/") {
			if idStr := c.Params("id"); idStr != "" {
				if imgID, err := uuid.Parse(idStr); err == nil {
					ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
					defer cancel()
					if img, err := imageRepo.GetByID(ctx, imgID); err == nil && img != nil {
						ogType = "article"
						// Compute site title for format "IMAGE TITLE - SITE TITLE"
						siteTitle := strings.TrimSpace(set.SiteName)
						if siteTitle == "" {
							siteTitle = "TROUGH"
						} else if strings.HasPrefix(c.Path(), "/@") {
							// Profile page meta: @user - SiteTitle, description from bio, image from latest user image
							username := strings.TrimSpace(c.Params("username"))
							if username == "" {
								username = strings.TrimPrefix(c.Path(), "/@")
								username = strings.TrimSpace(username)
							}
							if username != "" && userRepo != nil {
								ctx, cancel := context.WithTimeout(c.Context(), 5*time.Second)
								defer cancel()
								if u, err := userRepo.GetByUsername(ctx, username); err == nil && u != nil {
									siteTitle := strings.TrimSpace(set.SiteName)
									if siteTitle == "" {
										siteTitle = "TROUGH"
									}
									// Title: "@username - SiteTitle"
									title = "@" + u.Username + " - " + siteTitle
									// Description from bio when available; fallback to site description
									if u.Bio != nil {
										bio := strings.TrimSpace(*u.Bio)
										if bio != "" {
											if len(bio) > 280 {
												bio = bio[:280]
											}
											description = bio
										}
									}
									// Latest user image for social card
									if imageRepo != nil {
										if imgs, _, err := imageRepo.GetUserImages(u.ID, 1, 1); err == nil && len(imgs) > 0 {
											fn := strings.TrimSpace(imgs[0].Filename)
											if fn != "" {
												lowerFn := strings.ToLower(fn)
												if strings.HasPrefix(lowerFn, "http://") || strings.HasPrefix(lowerFn, "https://") {
													imageURL = fn
												} else {
													imageURL = origin + "/uploads/" + fn
												}
											}
										}
									}
									ogType = "profile"
								}
							}
						} else {
							// Single-segment CMS page: inherit index SEO but change only <title>
							slug := strings.Trim(strings.TrimSpace(c.Path()), "/")
							if slug != "" && !strings.Contains(slug, "/") {
								// Reserved prefixes that are not CMS slugs
								reserved := map[string]bool{"api": true, "uploads": true, "assets": true, "@": true, "i": true, "register": true, "reset": true, "verify": true, "settings": true, "admin": true}
								if !reserved[slug] && pageRepo != nil {
									if p, err := pageRepo.GetPublishedBySlug(strings.ToLower(slug)); err == nil && p != nil {
										siteTitle := strings.TrimSpace(set.SiteName)
										if siteTitle == "" {
											siteTitle = "TROUGH"
										}
										// Prefer page meta title when provided; otherwise use "Page - SiteTitle"
										if p.MetaTitle != nil && strings.TrimSpace(*p.MetaTitle) != "" {
											title = strings.TrimSpace(*p.MetaTitle)
										} else {
											pt := strings.TrimSpace(p.Title)
											if pt == "" {
												pt = "Page"
											}
											title = pt + " - " + siteTitle
										}
										// Keep description/image/ogType from site defaults to inherit index SEO
									}
								}
							}
						}
						// Title from image (original_name acts as title)
						imgTitle := "Untitled"
						if img.OriginalName != nil && strings.TrimSpace(*img.OriginalName) != "" {
							imgTitle = strings.TrimSpace(*img.OriginalName)
						}
						title = imgTitle + " - " + siteTitle
						// Description from user and caption
						author := strings.TrimSpace(img.Username)
						cap := ""
						if img.Caption != nil {
							cap = strings.TrimSpace(*img.Caption)
						}
						// Provide a subtle ASCII fallback when caption is missing
						asciiFallback := "~ artificial reverie ~"
						if author != "" && cap != "" {
							description = "by @" + author + " — " + cap
						} else if author != "" && cap == "" {
							description = "by @" + author + " — " + asciiFallback
						} else if author == "" && cap != "" {
							description = cap
						} else { // neither author nor caption
							description = asciiFallback
						}
						if len(description) > 280 {
							description = description[:280]
						}
						if img.Filename != "" {
							// If Filename is already an absolute URL (remote storage), use as-is
							lowerFn := strings.ToLower(img.Filename)
							if strings.HasPrefix(lowerFn, "http://") || strings.HasPrefix(lowerFn, "https://") {
								imageURL = img.Filename
							} else {
								// Local filename
								imageURL = origin + "/uploads/" + img.Filename
							}
						}
					}
				}
			}
		}

		// Replace title/meta description
		htmlStr = titleRe.ReplaceAllString(htmlStr, "<title>"+html.EscapeString(title)+"</title>")
		if description != "" {
			htmlStr = descRe.ReplaceAllString(htmlStr, `<meta name="description" content="`+html.EscapeString(description)+`">`)
		}

		// Inject OG/Twitter tags just before </head>
		var ogTags strings.Builder
		ogTags.WriteString("\n    <!-- Server-side social/OG tags -->\n")
		ogTags.WriteString(`    <meta property="og:site_name" content="` + html.EscapeString(set.SiteName) + `">\n`)
		ogTags.WriteString(`    <meta property="og:title" content="` + html.EscapeString(title) + `">\n`)
		if description != "" {
			ogTags.WriteString(`    <meta property="og:description" content="` + html.EscapeString(description) + `">\n`)
		}
		ogTags.WriteString(`    <meta property="og:type" content="` + ogType + `">\n`)
		ogTags.WriteString(`    <meta property="og:url" content="` + html.EscapeString(fullURL) + `">\n`)
		if imageURL != "" {
			ogTags.WriteString(`    <meta property="og:image" content="` + html.EscapeString(imageURL) + `">\n`)
			ogTags.WriteString(`    <meta property="og:image:alt" content="` + html.EscapeString(title) + `">\n`)
		}
		// Twitter
		card := "summary"
		if imageURL != "" {
			card = "summary_large_image"
		}
		ogTags.WriteString(`    <meta name="twitter:card" content="` + card + `">\n`)
		ogTags.WriteString(`    <meta name="twitter:title" content="` + html.EscapeString(title) + `">\n`)
		if description != "" {
			ogTags.WriteString(`    <meta name="twitter:description" content="` + html.EscapeString(description) + `">\n`)
		}
		if imageURL != "" {
			ogTags.WriteString(`    <meta name="twitter:image" content="` + html.EscapeString(imageURL) + `">\n`)
			// Add alt text for accessibility using the title
			ogTags.WriteString(`    <meta name="twitter:image:alt" content="` + html.EscapeString(title) + `">\n`)
		}

		// Build analytics snippet if configured and valid, and avoid tracking admins via cookie flag
		var analytics strings.Builder
		if set.AnalyticsEnabled && c.Cookies("trough_admin") != "1" {
			switch strings.ToLower(strings.TrimSpace(set.AnalyticsProvider)) {
			case "ga4":
				mid := strings.ToUpper(strings.TrimSpace(set.GA4MeasurementID))
				if gaIDRe.MatchString(mid) {
					// External GA4 loader only (no inline JS to comply with CSP)
					analytics.WriteString("\n    <!-- Analytics: Google Analytics 4 (loader only) -->\n")
					analytics.WriteString("    <script async src=\"https://www.googletagmanager.com/gtag/js?id=" + html.EscapeString(mid) + "\"></script>\n")
				}
			case "umami":
				src := strings.TrimSpace(set.UmamiSrc)
				wid := strings.TrimSpace(set.UmamiWebsiteID)
				if httpsJSRe.MatchString(src) && uuidRe.MatchString(wid) {
					analytics.WriteString("\n    <!-- Analytics: Umami -->\n")
					analytics.WriteString("    <script async src=\"" + html.EscapeString(src) + "\" data-website-id=\"" + html.EscapeString(strings.ToLower(wid)) + "\"></script>\n")
				}
			case "plausible":
				src := strings.TrimSpace(set.PlausibleSrc)
				dom := strings.TrimSpace(set.PlausibleDomain)
				if httpsJSRe.MatchString(src) && domainRe.MatchString(dom) {
					analytics.WriteString("\n    <!-- Analytics: Plausible -->\n")
					analytics.WriteString("    <script defer data-domain=\"" + html.EscapeString(strings.ToLower(dom)) + "\" src=\"" + html.EscapeString(src) + "\"></script>\n")
				}
			}
		}

		insertion := ogTags.String() + analytics.String()
		lower := strings.ToLower(htmlStr)
		if idx := strings.Index(lower, "</head>"); idx != -1 {
			htmlStr = htmlStr[:idx] + insertion + htmlStr[idx:]
		} else {
			htmlStr += insertion
		}

		c.Set("Content-Type", "text/html; charset=utf-8")
		return c.SendString(htmlStr)
	}
}

func main() {
	// Enforce strong JWT secret at startup
	if len(os.Getenv("JWT_SECRET")) < 32 {
		log.Fatalf("JWT_SECRET must be set and at least 32 characters")
	}
	config, err := services.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := db.Connect(); err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		log.Fatalf("Failed to migrate database: %v", err)
	}

	userRepo := models.NewUserRepository(db.DB)
	imageRepo := models.NewImageRepository(db.DB)
	likeRepo := models.NewLikeRepository(db.DB)
	collectRepo := models.NewCollectRepository(db.DB)
	siteRepo := models.NewSiteSettingsRepository(db.DB)

	maybeSeedAdmin(userRepo)

	// Build storage from settings or env
	// Note: inviteRepo will be created after storage since it depends only on DB
	// Build storage from settings or env
	stSettings := services.GetCachedSettings(siteRepo)
	storage, err := services.NewStorageFromSettings(stSettings)
	if err != nil {
		storage = services.NewLocalStorage("uploads")
	}
	services.SetCurrentStorage(storage)
	imageHandler := handlers.NewImageHandler(imageRepo, likeRepo, userRepo, *config, storage).WithCollect(collectRepo).WithSettings(siteRepo)
	pageRepo := models.NewPageRepository(db.DB)
	// Seed default CMS pages once per boot if missing (respect tombstones)
	seedDefaultPages(pageRepo, siteRepo)

	// Create rate limiters for enhanced security
	rateLimiter := services.NewRateLimiter(config.RateLimiting)
	progressiveRateLimiter := services.NewProgressiveRateLimiter(config.ProgressiveRateLimiting, config.RateLimiting)

	userHandler := handlers.NewUserHandler(userRepo, imageRepo, storage).WithSettings(siteRepo).WithCollect(collectRepo).WithPages(pageRepo)
	inviteRepo := models.NewInviteRepository(db.DB)
	adminHandler := handlers.NewAdminHandler(siteRepo, userRepo, imageRepo).WithStorage(storage).WithInvites(inviteRepo).WithPages(pageRepo).WithRateLimiter(rateLimiter).WithProgressiveRateLimiter(progressiveRateLimiter)
	pageHandler := handlers.NewPageHandler(pageRepo)
	authHandler := handlers.NewAuthHandlerWithRepos(userRepo, siteRepo).WithInvites(inviteRepo).WithProgressiveRateLimiter(progressiveRateLimiter)
	// Initialize async mail queue if SMTP is configured
	if set, err := siteRepo.Get(); err == nil && set != nil {
		if set.SMTPHost != "" && set.SMTPPort > 0 && set.SMTPUsername != "" && set.SMTPPassword != "" {
			services.InitMailQueue(services.NewMailSender, siteRepo)
		}
	}

	app := fiber.New(fiber.Config{
		BodyLimit:    10 * 1024 * 1024,
		ErrorHandler: customErrorHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		Prefork:      false, // enable in prod Linux if desired
		JSONEncoder:  gjson.Marshal,
		JSONDecoder:  gjson.Unmarshal,
	})

	// Initialize security components
	csrfProtection := middleware.NewCSRFProtection(os.Getenv("CSRF_SECRET"))
	securityHeaders := services.NewSecurityHeaders(nil)

	// Apply security headers globally
	app.Use(securityHeaders.Middleware())

	// Start backup scheduler goroutine (best-effort, non-blocking)
	go func() {
		// Simple ticker-based scheduler using settings cache
		for {
			set := services.GetCachedSettings(siteRepo)
			if set.BackupEnabled {
				// Parse interval; fallback to 24h
				d, err := time.ParseDuration(strings.TrimSpace(set.BackupInterval))
				if err != nil || d <= 0 {
					d = 24 * time.Hour
				}
				// Perform backup and cleanup
				if _, err := services.SaveBackupFile(context.Background(), db.DB, "backups"); err == nil {
					_ = services.CleanupBackups("backups", set.BackupKeepDays)
				}
				time.Sleep(d)
				continue
			}
			time.Sleep(30 * time.Minute)
		}
	}()

	// Cleanup rate limiters on shutdown
	defer rateLimiter.Stop()
	defer progressiveRateLimiter.Stop()

	// Application logger; skip noise for static and health endpoints
	app.Use(logger.New(logger.Config{
		Next: func(c *fiber.Ctx) bool {
			p := c.Path()
			if strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/uploads/") || p == "/healthz" || p == "/" {
				return true
			}
			return false
		},
	}))
	app.Use(etag.New(etag.Config{Weak: true}))
	app.Use(compress.New(compress.Config{
		Level: compress.LevelBestSpeed,
		Next: func(c *fiber.Ctx) bool {
			p := c.Path()
			// Skip already-compressed/static heavy assets
			if strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/uploads/") {
				return true
			}
			ct := c.Get("Content-Type")
			if strings.Contains(ct, "image/") || strings.Contains(ct, "/zip") || strings.Contains(ct, "/gzip") || strings.Contains(ct, "/br") {
				return true
			}
			return false
		},
	}))
	// Configure CORS for API. Do not affect images/scripts loading.
	app.Use(cors.New(cors.Config{
		AllowOriginsFunc: func(origin string) bool {
			set := services.GetCachedSettings(siteRepo)
			allowed := strings.TrimSpace(set.SiteURL)
			if allowed == "" || origin == "" {
				return false
			}
			allowed = strings.TrimRight(allowed, "/")
			// Allow exact match. Also allow same host with http/https scheme variance in dev.
			if strings.EqualFold(origin, allowed) {
				return true
			}
			// Best-effort: compare hosts ignoring scheme
			// origin like https://example.com
			oi := strings.Index(origin, "://")
			ai := strings.Index(allowed, "://")
			if oi > 0 && ai > 0 {
				return strings.EqualFold(origin[oi+3:], allowed[ai+3:])
			}
			return false
		},
		AllowHeaders:     "Content-Type, Authorization",
		AllowMethods:     "GET,POST,PATCH,DELETE,PUT,OPTIONS",
		AllowCredentials: true,
	}))

	// Security headers - using the security headers service for consistency
	app.Use(func(c *fiber.Ctx) error {
		// Let the security headers service handle most CSP/security headers
		// This ensures consistency with the rest of the security implementation
		return c.Next()
	})

	// Serve SPA entry with server-side meta tags for key routes
	index := indexWithMetaHandler(siteRepo, imageRepo, userRepo, pageRepo)
	app.Get("/", index)
	app.Get("/@:username", index)
	app.Get("/settings", index)
	app.Get("/admin", index)
	app.Get("/register", index)
	app.Get("/reset", index)
	app.Get("/verify", index)
	app.Get("/i/:id", index)
	// Single-segment CMS pages SSR entry
	app.Get("/:slug", func(c *fiber.Ctx) error {
		slug := strings.ToLower(strings.Trim(c.Params("slug"), "/"))
		if slug == "" {
			return index(c)
		}
		// Skip reserved prefixes and known routes
		reserved := map[string]bool{"api": true, "uploads": true, "assets": true, "@": true, "i": true, "register": true, "reset": true, "verify": true, "settings": true, "admin": true}
		if reserved[slug] {
			return index(c)
		}
		// If slug corresponds to a published redirect page, perform server-side redirect immediately
		if p, err := pageRepo.GetPublishedBySlug(slug); err == nil && p != nil {
			if p.RedirectURL != nil && strings.TrimSpace(*p.RedirectURL) != "" {
				return c.Redirect(strings.TrimSpace(*p.RedirectURL), fiber.StatusFound)
			}
		}
		// Otherwise serve SPA; client will fetch and render page content
		return index(c)
	})

	// Static assets
	app.Static("/", "./static", fiber.Static{Compress: true, CacheDuration: 3600, MaxAge: 31536000})
	// Local uploads are served statically when storage is local. For remote storage (S3/R2),
	// we keep this mount (for legacy/local files), and add a redirector for /uploads/* to the
	// configured public base if set.
	app.Static("/uploads", "./uploads", fiber.Static{Compress: true, CacheDuration: 86400, MaxAge: 31536000})
	// Dynamic redirector for remote storage; uses current storage and latest settings cache
	app.Get("/uploads/*", func(c *fiber.Ctx) error {
		st := services.GetCurrentStorage()
		if st == nil || st.IsLocal() {
			return c.Next()
		}
		set := services.GetCachedSettings(siteRepo)
		if strings.TrimSpace(set.PublicBaseURL) == "" {
			return c.Next()
		}
		key := c.Params("*")
		return c.Redirect(st.PublicURL(key), fiber.StatusFound)
	})
	// Simple health endpoint for uptime checks (not logged)
	app.Get("/healthz", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) })

	api := app.Group("/api")
	// Build auth middleware once to reuse its small cache
	authMW := middleware.Protected()

	// Add database health check middleware to all API routes
	api.Use(middleware.DBPing())

	// Apply CSRF protection to API routes that change state
	api.Use(csrfProtection.Middleware())

	api.Post("/register", progressiveRateLimiter.Middleware(), authHandler.Register)
	// NOTE: Consider adding rate limiting middleware in deployment env; omitted here to avoid new deps.
	api.Post("/login", progressiveRateLimiter.Middleware(), authHandler.Login)
	// Allow logout without auth guard so clients can always clear cookies
	api.Post("/logout", authHandler.Logout)
	api.Post("/forgot-password", progressiveRateLimiter.Middleware(), authHandler.ForgotPassword)
	api.Post("/reset-password", progressiveRateLimiter.Middleware(), authHandler.ResetPassword)
	api.Post("/verify-email", progressiveRateLimiter.Middleware(), authHandler.VerifyEmail)

	api.Get("/password-requirements", authHandler.GetPasswordRequirements)
	api.Get("/invites/validate", adminHandler.ValidateInviteCode)

	// Public CSRF token endpoint for initial page load
	api.Get("/csrf", func(c *fiber.Ctx) error {
		// Only set a new CSRF token if one doesn't already exist
		existingToken := csrfProtection.GetCSRFToken(c)
		if existingToken == "" {
			if err := csrfProtection.SetCSRFToken(c); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"error": "Failed to generate CSRF token",
				})
			}
			existingToken = csrfProtection.GetCSRFToken(c)
		}
		return c.JSON(fiber.Map{
			"csrf_token": existingToken,
		})
	})
	api.Post("/me/resend-verification", authMW, authHandler.ResendVerification)
	api.Get("/me", authMW, authHandler.Me)

	api.Get("/feed", imageHandler.GetFeed)
	api.Get("/images/:id", imageHandler.GetImage)
	api.Post("/upload", authMW, imageHandler.Upload)
	// Likes are deprecated; route retained for compatibility but returns 410
	api.Post("/images/:id/like", authMW, imageHandler.LikeImage)
	api.Post("/images/:id/collect", authMW, imageHandler.CollectImage)
	api.Patch("/images/:id", authMW, imageHandler.UpdateImage)
	api.Delete("/images/:id", authMW, imageHandler.DeleteImage)

	api.Get("/users/:username", userHandler.GetProfile)
	api.Get("/users/:username/images", userHandler.GetUserImages)
	api.Get("/users/:username/collections", userHandler.GetUserCollections)
	// Public pages list for footer
	api.Get("/pages", userHandler.ListPublicPages)
	// Public page data for SPA render (and server redirect)
	api.Get("/pages/:slug", pageHandler.GetPublicPage)
	api.Get("/me/profile", authMW, userHandler.GetMyProfile)
	api.Patch("/me/profile", authMW, userHandler.UpdateMyProfile)
	api.Get("/me/account", authMW, userHandler.GetMyAccount)
	api.Patch("/me/email", authMW, userHandler.UpdateEmail)
	api.Patch("/me/password", authMW, userHandler.UpdatePassword)
	api.Delete("/me", authMW, userHandler.DeleteMyAccount)
	api.Post("/me/avatar", authMW, userHandler.UploadAvatar)

	api.Get("/site", adminHandler.GetPublicSite)

	api.Get("/admin/users", authMW, userHandler.AdminListUsers)
	api.Post("/admin/users", authMW, userHandler.AdminCreateUser)
	api.Patch("/admin/users/:id", authMW, userHandler.AdminSetUserFlags)
	api.Patch("/admin/users/:id/password", authMW, userHandler.AdminSetUserPassword)
	api.Post("/admin/users/:id/send-verification", authMW, userHandler.AdminSendVerification)
	api.Delete("/admin/users/:id", authMW, userHandler.AdminDeleteUser)
	api.Delete("/admin/images/:id", authMW, userHandler.AdminDeleteImage)
	api.Patch("/admin/images/:id/nsfw", authMW, userHandler.AdminSetImageNSFW)

	// Admin invite management
	api.Post("/admin/invites", authMW, adminHandler.CreateInvite)
	api.Get("/admin/invites", authMW, adminHandler.ListInvites)
	api.Delete("/admin/invites/:id", authMW, adminHandler.DeleteInvite)
	api.Post("/admin/invites/prune", authMW, adminHandler.PruneInvites)

	api.Get("/admin/site", authMW, adminHandler.GetSiteSettings)
	api.Put("/admin/site", authMW, adminHandler.UpdateSiteSettings)
	api.Post("/admin/site/favicon", authMW, adminHandler.UploadFavicon)
	api.Post("/admin/site/social-image", authMW, adminHandler.UploadSocialImage)
	api.Post("/admin/site/test-smtp", authMW, adminHandler.TestSMTP)
	api.Post("/admin/site/export-uploads", authMW, adminHandler.ExportLocalUploadsToStorage)
	api.Post("/admin/site/test-storage", authMW, adminHandler.TestStorage)
	// Admin CMS pages
	// Admin backups
	api.Post("/admin/backups/download", authMW, adminHandler.AdminCreateBackup)
	api.Get("/admin/backups", authMW, adminHandler.AdminListBackups)
	api.Post("/admin/backups/save", authMW, adminHandler.AdminSaveBackup)
	api.Delete("/admin/backups/:name", authMW, adminHandler.AdminDeleteBackup)
	api.Post("/admin/backups/restore", authMW, adminHandler.AdminRestoreBackup)
	api.Get("/admin/backups/:name", authMW, adminHandler.AdminDownloadSavedBackup)
	api.Get("/admin/diag", authMW, adminHandler.AdminDiag)
	api.Get("/admin/rate-limiter-stats", authMW, adminHandler.AdminRateLimiterStats)
	api.Get("/admin/progressive-rate-limiter-stats", authMW, adminHandler.AdminProgressiveRateLimiterStats)
	api.Get("/admin/pages", authMW, adminHandler.AdminListPages)
	api.Post("/admin/pages", authMW, adminHandler.AdminCreatePage)
	api.Put("/admin/pages/:id", authMW, adminHandler.AdminUpdatePage)
	api.Delete("/admin/pages/:id", authMW, adminHandler.AdminDeletePage)

	app.Use(func(c *fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), "/api") {
			return fiber.ErrNotFound
		}
		if c.Method() == fiber.MethodGet {
			return c.Status(fiber.StatusNotFound).SendFile("./static/404.html")
		}
		return c.SendStatus(fiber.StatusNotFound)
	})

	log.Printf("Server starting on port 8080")
	log.Fatal(app.Listen(":8080"))
}

// Create a few default pages if they do not yet exist. If deleted by admin, they will not be recreated
func seedDefaultPages(pageRepo models.PageRepositoryInterface, siteRepo models.SiteSettingsRepositoryInterface) {
	type def struct{ slug, title, md string }
	// Use SMTP from settings for contact page when available
	from := ""
	if set, err := siteRepo.Get(); err == nil && set != nil {
		from = strings.TrimSpace(set.SMTPFromEmail)
	}
	var contactBody string
	if from != "" {
		contactBody = `# Contact

We would love to hear from you. Email us at <` + from + `>.

::: tip
If your message concerns account access, include your username and the email you registered with.
:::
`
	} else {
		contactBody = `# Contact

Email is not configured yet. Check back soon.

::: info
In the meantime, you can reach us through our public channels.
:::
`
	}

	defs := []def{
		{"about", "About", `# About

Trough is a focused home for AI-generated imagery. Fast to load, clear to navigate, respectful of attention.

::: note
We check for AI provenance in metadata. What you see is created by machines and curated by humans.
:::

## What you can expect

- Straightforward uploading with sensible limits
- Clean, responsive viewing on any device
- An emphasis on signal over noise

We keep the surface minimal so the work can speak for itself.
`},
		{"contact", "Contact", contactBody},
		{"terms", "Terms of Service", `# Terms of Service

Welcome to Trough. By accessing or using the Service, you agree to these Terms.

## 1. Accounts

You are responsible for your account and for any content you upload. You must be at least 13 years old.

## 2. Content

You retain rights to your uploads. By uploading, you grant us a non-exclusive license to host and display your images on the Service. Do not upload anything illegal, infringing, or malicious. We reserve the right to remove content that violates policy or law.

::: warning
We accept only AI-generated images with verifiable signals. Non-conforming uploads may be rejected or removed.
:::

### Moderation & Admin Actions

We may moderate content and take administrative action at our discretion to preserve the integrity and safety of the Service, including removal of content, rate limiting, or account suspension/termination.

### Prohibited Content

Illegal content is strictly prohibited. This includes (but is not limited to) CSAM, content that incites violence, doxxing, or infringement of third-party rights. We will cooperate with lawful requests from authorities where required.

## 3. Acceptable Use

Do not attempt to disrupt the Service, probe, or scrape at scale. Respect other users.

## 4. Disclaimers

The Service is provided “as is.” We make no guarantees about uptime or fitness for a particular purpose.

## 5. Limitation of Liability

To the fullest extent permitted by law, Trough, its contributors, and operators shall not be liable for indirect, incidental, or consequential damages.

## 6. Changes

We may update these Terms. Continued use constitutes acceptance of the new Terms.

## 7. Contact

See the Contact page for how to reach us.
`},
		{"privacy", "Privacy Policy", `# Privacy Policy

We respect your privacy. This document explains what we collect and why.

## Data We Process

- Account data you give us (email, username).\
- Uploaded images and their metadata (EXIF/XMP/C2PA when present).
- Minimal operational logs to keep the site running.

## What We Do Not Do

- We do not sell your data.\
- We do not run invasive trackers. Analytics are optional and disabled by default.

## Cookies

We use essential cookies for authentication. Optional analytics may set their own cookies when enabled by the admin.

## Storage & Retention

Uploads reside on our storage backend (local or S3/R2). You can delete your account and uploads at any time; backups may persist for a limited window.

## Your Rights

You can access, update, or delete your data by visiting Settings or contacting us.

## Changes

We may update this policy. Material changes will be reflected here.
`},
	}

	// FAQ (appended for clarity)
	defs = append(defs, def{"faq", "FAQ", `# Frequently Asked Questions

## Getting started

### How do I create an account?
Use the Register option. Admins may enable invites or require email verification.

### How do I upload an image?
Click Upload, choose a file (JPG/PNG/WebP up to 10MB), and add an optional caption. We require verifiable AI metadata.

### Why was my upload rejected?
Uploads without acceptable AI provenance are rejected. Ensure your generator includes EXIF/XMP/C2PA signals.

## Profiles and collections

### Can I change my username or avatar?
Yes — under Settings. Usernames are lowercase alphanumerics (3–30 chars).

### How do collections work?
Click ✧ on an image to collect; it becomes ✦ when collected. Find your collections on your profile.

## Safety and privacy

### Do you track me?
No invasive tracking. Optional analytics may be enabled by the admin.

### How are NSFW images handled?
We offer hide/show/blur preferences in Settings.

## Troubleshooting

### I forgot my password
Use “Forgot password” on the sign-in modal. You’ll receive a reset link if email is configured.

### My image won’t load
Check your connection and file type. If using remote storage, it may take a moment for CDN propagation.

::: details Contact support
If problems persist, visit the Contact page and email us. Include your username and a brief description.
:::
`})

	for _, d := range defs {
		// Respect tombstones: do not re-create if admin previously deleted
		var count int
		if db.DB != nil {
			_ = db.DB.Get(&count, `SELECT COUNT(*) FROM cms_tombstones WHERE slug=$1`, d.slug)
			if count > 0 {
				continue
			}
		}
		// Only create if missing
		if _, err := pageRepo.GetBySlug(d.slug); err == nil {
			continue
		}
		// Create as published default
		_ = pageRepo.Create(&models.Page{Slug: d.slug, Title: d.title, Markdown: d.md, HTML: "", IsPublished: true})
	}
}
