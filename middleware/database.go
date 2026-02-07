package middleware

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/trough/db"
)

// DBPing middleware checks the database connection before proceeding.
// If the connection is lost, it attempts to reconnect.
func DBPing() fiber.Handler {
	const checkInterval = 5 * time.Second
	var state struct {
		mu        sync.Mutex
		lastCheck time.Time
		healthy   bool
	}
	state.healthy = true

	return func(c *fiber.Ctx) error {
		state.mu.Lock()
		due := !state.healthy || time.Since(state.lastCheck) >= checkInterval
		if due {
			// Set this first so concurrent requests do not all probe at once.
			state.lastCheck = time.Now()
		}
		state.mu.Unlock()

		if !due {
			return c.Next()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pingErr := db.Ping(ctx)
		cancel()

		healthy := true
		if pingErr != nil {
			log.Printf("Database ping failed or timed out: %v. Attempting to reconnect...", pingErr)
			if reconErr := db.Reconnect(); reconErr != nil {
				log.Printf("Failed to reconnect to database: %v", reconErr)
				healthy = false
			} else {
				log.Println("Successfully reconnected to the database.")
			}
		}

		state.mu.Lock()
		state.healthy = healthy
		state.mu.Unlock()
		if !healthy {
			return c.Status(http.StatusServiceUnavailable).JSON(fiber.Map{
				"error": "Database connection is down",
			})
		}

		return c.Next()
	}
}
