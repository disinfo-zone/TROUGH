package middleware

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/trough/db"
)

// DBPing middleware checks the database connection before proceeding.
// If the connection is lost, it attempts to reconnect.
func DBPing() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Create a context with a short timeout for the ping.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			log.Printf("Database ping failed or timed out: %v. Attempting to reconnect...", err)
			if reconErr := db.Reconnect(); reconErr != nil {
				log.Printf("Failed to reconnect to database: %v", reconErr)
				return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
					"error": "Database connection is down",
				})
			}
			log.Println("Successfully reconnected to the database.")
		}
		return c.Next()
	}
}
