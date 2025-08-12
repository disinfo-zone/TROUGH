package main

import (
	"html"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
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
	return c.Status(code).JSON(fiber.Map{
		"error": err.Error(),
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
	if _, err := userRepo.GetByEmail(adminEmail); err == nil {
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
// and, for /i/:id routes, from the specific image.
func indexWithMetaHandler(siteRepo models.SiteSettingsRepositoryInterface, imageRepo models.ImageRepositoryInterface) fiber.Handler {
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
					if img, err := imageRepo.GetByID(imgID); err == nil && img != nil {
						ogType = "article"
						// Compute site title for format "IMAGE TITLE - SITE TITLE"
						siteTitle := strings.TrimSpace(set.SiteName)
						if siteTitle == "" {
							siteTitle = "TROUGH"
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
					// Use standard GA4 snippet with IP anonymization
					analytics.WriteString("\n    <!-- Analytics: Google Analytics 4 -->\n")
					analytics.WriteString("    <script async src=\"https://www.googletagmanager.com/gtag/js?id=" + html.EscapeString(mid) + "\"></script>\n")
					analytics.WriteString("    <script>\n")
					analytics.WriteString("      window.dataLayer = window.dataLayer || [];\n")
					analytics.WriteString("      function gtag(){dataLayer.push(arguments);}\n")
					analytics.WriteString("      gtag('js', new Date());\n")
					analytics.WriteString("      gtag('config', '" + html.EscapeString(mid) + "', { 'anonymize_ip': true });\n")
					analytics.WriteString("    </script>\n")
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
	siteRepo := models.NewSiteSettingsRepository(db.DB)

	maybeSeedAdmin(userRepo)

	// Build storage from settings or env
	// Note: inviteRepo will be created after storage since it depends only on DB
	// Build storage from settings or env
	stSettings, _ := siteRepo.Get()
	storage, err := services.NewStorageFromSettings(*stSettings)
	if err != nil {
		storage = services.NewLocalStorage("uploads")
	}
	services.SetCurrentStorage(storage)
	imageHandler := handlers.NewImageHandler(imageRepo, likeRepo, userRepo, *config, storage)
	userHandler := handlers.NewUserHandler(userRepo, imageRepo, storage)
	inviteRepo := models.NewInviteRepository(db.DB)
	adminHandler := handlers.NewAdminHandler(siteRepo, userRepo, imageRepo).WithStorage(storage).WithInvites(inviteRepo)
	authHandler := handlers.NewAuthHandlerWithRepos(userRepo, siteRepo).WithInvites(inviteRepo)

	app := fiber.New(fiber.Config{BodyLimit: 10 * 1024 * 1024, ErrorHandler: customErrorHandler})

	app.Use(logger.New())
	app.Use(compress.New())
	app.Use(cors.New())

	// Serve SPA entry with server-side meta tags for key routes
	index := indexWithMetaHandler(siteRepo, imageRepo)
	app.Get("/", index)
	app.Get("/@:username", index)
	app.Get("/settings", index)
	app.Get("/admin", index)
	app.Get("/register", index)
	app.Get("/reset", index)
	app.Get("/verify", index)
	app.Get("/i/:id", index)

	// Static assets
	app.Static("/", "./static", fiber.Static{Compress: true, CacheDuration: 3600})
	// Local uploads are served statically when storage is local. For remote storage (S3/R2),
	// we keep this mount (for legacy/local files), and add a redirector for /uploads/* to the
	// configured public base if set.
	app.Static("/uploads", "./uploads", fiber.Static{Compress: true, CacheDuration: 86400})
	if !storage.IsLocal() && strings.TrimSpace(stSettings.PublicBaseURL) != "" {
		app.Get("/uploads/*", func(c *fiber.Ctx) error {
			key := c.Params("*")
			return c.Redirect(storage.PublicURL(key), fiber.StatusFound)
		})
	}

	api := app.Group("/api")

	api.Post("/register", authHandler.Register)
	api.Post("/login", authHandler.Login)
	api.Post("/forgot-password", authHandler.ForgotPassword)
	api.Post("/reset-password", authHandler.ResetPassword)
	api.Post("/verify-email", authHandler.VerifyEmail)
	api.Get("/validate-invite", authHandler.ValidateInvite)
	api.Post("/me/resend-verification", middleware.Protected(), authHandler.ResendVerification)
	api.Get("/me", middleware.Protected(), authHandler.Me)

	api.Get("/feed", imageHandler.GetFeed)
	api.Get("/images/:id", imageHandler.GetImage)
	api.Post("/upload", middleware.Protected(), imageHandler.Upload)
	api.Post("/images/:id/like", middleware.Protected(), imageHandler.LikeImage)
	api.Patch("/images/:id", middleware.Protected(), imageHandler.UpdateImage)
	api.Delete("/images/:id", middleware.Protected(), imageHandler.DeleteImage)

	api.Get("/users/:username", userHandler.GetProfile)
	api.Get("/users/:username/images", userHandler.GetUserImages)
	api.Get("/me/profile", middleware.Protected(), userHandler.GetMyProfile)
	api.Patch("/me/profile", middleware.Protected(), userHandler.UpdateMyProfile)
	api.Get("/me/account", middleware.Protected(), userHandler.GetMyAccount)
	api.Patch("/me/email", middleware.Protected(), userHandler.UpdateEmail)
	api.Patch("/me/password", middleware.Protected(), userHandler.UpdatePassword)
	api.Delete("/me", middleware.Protected(), userHandler.DeleteMyAccount)
	api.Post("/me/avatar", middleware.Protected(), userHandler.UploadAvatar)

	api.Get("/site", adminHandler.GetPublicSite)

	api.Get("/admin/users", middleware.Protected(), userHandler.AdminListUsers)
	api.Post("/admin/users", middleware.Protected(), userHandler.AdminCreateUser)
	api.Patch("/admin/users/:id", middleware.Protected(), userHandler.AdminSetUserFlags)
	api.Patch("/admin/users/:id/password", middleware.Protected(), userHandler.AdminSetUserPassword)
	api.Post("/admin/users/:id/send-verification", middleware.Protected(), userHandler.AdminSendVerification)
	api.Delete("/admin/users/:id", middleware.Protected(), userHandler.AdminDeleteUser)
	api.Delete("/admin/images/:id", middleware.Protected(), userHandler.AdminDeleteImage)
	api.Patch("/admin/images/:id/nsfw", middleware.Protected(), userHandler.AdminSetImageNSFW)

	// Admin invite management
	api.Post("/admin/invites", middleware.Protected(), adminHandler.CreateInvite)
	api.Get("/admin/invites", middleware.Protected(), adminHandler.ListInvites)
	api.Delete("/admin/invites/:id", middleware.Protected(), adminHandler.DeleteInvite)
	api.Post("/admin/invites/prune", middleware.Protected(), adminHandler.PruneInvites)

	api.Get("/admin/site", middleware.Protected(), adminHandler.GetSiteSettings)
	api.Put("/admin/site", middleware.Protected(), adminHandler.UpdateSiteSettings)
	api.Post("/admin/site/favicon", middleware.Protected(), adminHandler.UploadFavicon)
	api.Post("/admin/site/social-image", middleware.Protected(), adminHandler.UploadSocialImage)
	api.Post("/admin/site/test-smtp", middleware.Protected(), adminHandler.TestSMTP)
	api.Post("/admin/site/export-uploads", middleware.Protected(), adminHandler.ExportLocalUploadsToStorage)
	api.Post("/admin/site/test-storage", middleware.Protected(), adminHandler.TestStorage)

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
