package middleware

import (
	"log"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/trough/db"
)

// DBPing middleware checks the database connection before proceeding.
// If the connection is lost, it attempts to reconnect.
func DBPing() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if err := db.Ping(); err != nil {
			log.Printf("Database ping failed: %v. Attempting to reconnect...", err)
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
