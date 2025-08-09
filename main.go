package main

import (
	"log"
	"os"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
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

	// First-time admin seed (optional)
	maybeSeedAdmin(userRepo)

	authHandler := handlers.NewAuthHandler(userRepo)
	imageHandler := handlers.NewImageHandler(imageRepo, likeRepo, userRepo, *config)
	userHandler := handlers.NewUserHandler(userRepo, imageRepo)

	app := fiber.New(fiber.Config{
		BodyLimit:    10 * 1024 * 1024,
		ErrorHandler: customErrorHandler,
	})

	app.Use(logger.New())
	app.Use(compress.New())
	app.Use(cors.New())

	app.Static("/", "./static", fiber.Static{
		Compress:      true,
		CacheDuration: 3600,
	})

	app.Static("/uploads", "./uploads", fiber.Static{
		Compress:      true,
		CacheDuration: 86400,
	})

	// Serve profile and settings URLs client-side
	app.Get("/@:username", func(c *fiber.Ctx) error {
		return c.SendFile("./static/index.html")
	})
	app.Get("/settings", func(c *fiber.Ctx) error {
		return c.SendFile("./static/index.html")
	})

	api := app.Group("/api")

	api.Post("/register", authHandler.Register)
	api.Post("/login", authHandler.Login)
	api.Get("/me", middleware.Protected(), authHandler.Me)

	api.Get("/feed", imageHandler.GetFeed)
	api.Get("/images/:id", imageHandler.GetImage)
	api.Post("/upload", middleware.Protected(), imageHandler.Upload)
	api.Post("/images/:id/like", middleware.Protected(), imageHandler.LikeImage)
	api.Patch("/images/:id", middleware.Protected(), imageHandler.UpdateImage)
	api.Delete("/images/:id", middleware.Protected(), imageHandler.DeleteImage)

	// Users
	api.Get("/users/:username", userHandler.GetProfile)
	api.Get("/users/:username/images", userHandler.GetUserImages)
	api.Get("/me/profile", middleware.Protected(), userHandler.GetMyProfile)
	api.Patch("/me/profile", middleware.Protected(), userHandler.UpdateMyProfile)
	api.Get("/me/account", middleware.Protected(), userHandler.GetMyAccount)
	api.Patch("/me/email", middleware.Protected(), userHandler.UpdateEmail)
	api.Patch("/me/password", middleware.Protected(), userHandler.UpdatePassword)
	api.Delete("/me", middleware.Protected(), userHandler.DeleteMyAccount)
	api.Post("/me/avatar", middleware.Protected(), userHandler.UploadAvatar)

	// Admin (guarded in handler)
	api.Get("/admin/users", middleware.Protected(), userHandler.AdminListUsers)
	api.Patch("/admin/users/:id", middleware.Protected(), userHandler.AdminSetUserFlags)
	api.Delete("/admin/images/:id", middleware.Protected(), userHandler.AdminDeleteImage)
	api.Patch("/admin/images/:id/nsfw", middleware.Protected(), userHandler.AdminSetImageNSFW)

	// Fallback: pretty 404 for non-API GETs, JSON for API
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
