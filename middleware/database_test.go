package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/yourusername/trough/db"
	"github.com/yourusername/trough/middleware"
)

// setupTestDB connects to the database for testing.
// It's a simplified version of the main setup.
func setupTestDB(t *testing.T) {
	// Set a default DATABASE_URL if not present, for local testing.
	if os.Getenv("DATABASE_URL") == "" {
		os.Setenv("DATABASE_URL", "postgres://trough:trough@localhost:5432/trough?sslmode=disable")
	}

	err := db.Connect()
	if err != nil {
		// If the DB is not available, we skip the test.
		// This is common in CI environments that don't run a DB.
		t.Skipf("Skipping database integration test: failed to connect to database: %v", err)
	}
}

func TestDBPing_Middleware_ReconnectsOnFailure(t *testing.T) {
	// Setup: Connect to the database
	setupTestDB(t)
	defer db.Close()

	// Create a new Fiber app
	app := fiber.New()

	// Apply the DBPing middleware
	app.Use(middleware.DBPing())

	// Add a simple test route that performs a query
	app.Get("/test-db", func(c *fiber.Ctx) error {
		// This query will fail if the DB connection is not available.
		var result int
		err := db.DB.Get(&result, "SELECT 1")
		if err != nil {
			return c.Status(http.StatusInternalServerError).SendString(err.Error())
		}
		return c.Status(http.StatusOK).SendString("OK")
	})

	// --- Test Case 1: Initial request should succeed ---
	req1 := httptest.NewRequest("GET", "/test-db", nil)
	resp1, err1 := app.Test(req1, -1) // -1 timeout for long-running tests
	assert.NoError(t, err1)
	assert.Equal(t, http.StatusOK, resp1.StatusCode, "Initial request should succeed")

	// --- Test Case 2: Manually close the database connection to simulate a failure ---
	t.Log("Manually closing database connection...")
	err := db.Close()
	assert.NoError(t, err)
	// Add a small delay to ensure the connection is fully closed
	time.Sleep(100 * time.Millisecond)

	// --- Test Case 3: The next request should trigger the middleware to reconnect ---
	t.Log("Sending request after DB connection was closed...")
	req2 := httptest.NewRequest("GET", "/test-db", nil)
	resp2, err2 := app.Test(req2, -1)
	assert.NoError(t, err2)
	assert.Equal(t, http.StatusOK, resp2.StatusCode, "Request after disconnect should succeed due to middleware reconnect")

	// --- Test Case 4: Verify the connection is indeed active again ---
	t.Log("Sending a final request to ensure connection is stable...")
	req3 := httptest.NewRequest("GET", "/test-db", nil)
	resp3, err3 := app.Test(req3, -1)
	assert.NoError(t, err3)
	assert.Equal(t, http.StatusOK, resp3.StatusCode, "Subsequent request should also succeed")
}
