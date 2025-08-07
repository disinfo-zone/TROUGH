package main

import (
	"log"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/compress"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
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
	app := fiber.New(fiber.Config{
		BodyLimit:    10 * 1024 * 1024, // 10MB
		ErrorHandler: customErrorHandler,
	})

	// Middleware for that premium feel
	app.Use(logger.New())
	app.Use(compress.New())
	app.Use(cors.New())

	// Serve static files with caching
	app.Static("/", "./static", fiber.Static{
		Compress:      true,
		CacheDuration: 3600,
	})

	app.Static("/uploads", "./uploads", fiber.Static{
		Compress:      true,
		CacheDuration: 86400,
	})

	// API routes
	api := app.Group("/api")

	// Auth
	api.Post("/register", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Register endpoint - TODO"})
	})
	api.Post("/login", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Login endpoint - TODO"})
	})

	// Images - the heart of the app
	api.Get("/feed", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"images": []interface{}{}})
	}) // Main feed with infinite scroll
	api.Get("/images/:id", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Get image endpoint - TODO"})
	})
	api.Post("/upload", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Upload endpoint - TODO"})
	})
	api.Post("/images/:id/like", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Like endpoint - TODO"})
	})

	// Users
	api.Get("/users/:username", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Get profile endpoint - TODO"})
	})
	api.Get("/users/:username/images", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{"message": "Get user images endpoint - TODO"})
	})

	log.Fatal(app.Listen(":8080"))
}