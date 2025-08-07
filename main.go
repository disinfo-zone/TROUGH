package main

import (
	"log"

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

	api := app.Group("/api")

	api.Post("/register", authHandler.Register)
	api.Post("/login", authHandler.Login)

	api.Get("/feed", imageHandler.GetFeed)
	api.Get("/images/:id", imageHandler.GetImage)
	api.Post("/upload", middleware.Protected(), imageHandler.Upload)
	api.Post("/images/:id/like", middleware.Protected(), imageHandler.LikeImage)

	api.Get("/users/:username", userHandler.GetProfile)
	api.Get("/users/:username/images", userHandler.GetUserImages)

	log.Printf("Server starting on port 8080")
	log.Fatal(app.Listen(":8080"))
}