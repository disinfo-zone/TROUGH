package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/suite"
	"github.com/yourusername/trough/db"
	"github.com/yourusername/trough/handlers"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

type RateLimitingIntegrationTestSuite struct {
	suite.Suite
	app       *fiber.App
	userRepo  *models.UserRepository
	rateLimiter *services.RateLimiter
}

func (suite *RateLimitingIntegrationTestSuite) SetupSuite() {
	os.Setenv("DATABASE_URL", "postgres://trough:trough@localhost:5432/trough_test?sslmode=disable")

	err := db.Connect()
	if err != nil {
		suite.T().Skipf("Skipping rate limiting integration test suite: failed to connect to database: %v", err)
	}

	err = db.Migrate()
	suite.Require().NoError(err)

	suite.userRepo = models.NewUserRepository(db.DB)

	// Create rate limiter with strict limits for testing
	rateLimitConfig := services.RateLimitConfig{
		MaxEntries:      100,
		CleanupInterval: 100 * time.Millisecond,
		EntryTTL:        1 * time.Second,
		TrustedProxies:  []string{"127.0.0.1", "::1"},
		EnableDebug:     true,
	}
	suite.rateLimiter = services.NewRateLimiter(rateLimitConfig)

	authHandler := handlers.NewAuthHandler(suite.userRepo)

	suite.app = fiber.New()

	api := suite.app.Group("/api")
	// Apply strict rate limiting for testing
	api.Post("/register", suite.rateLimiter.Middleware(2, time.Minute), authHandler.Register)
	api.Post("/login", suite.rateLimiter.Middleware(2, time.Minute), authHandler.Login)
	api.Post("/forgot-password", suite.rateLimiter.Middleware(1, time.Minute), authHandler.ForgotPassword)
}

func (suite *RateLimitingIntegrationTestSuite) TearDownSuite() {
	suite.rateLimiter.Stop()
	db.Close()
}

func (suite *RateLimitingIntegrationTestSuite) SetupTest() {
	db.DB.Exec("TRUNCATE users, images, likes, collections CASCADE")
}

func (suite *RateLimitingIntegrationTestSuite) TestRegistrationRateLimiting() {
	registerData := map[string]string{
		"username": "testuser1",
		"email":    "test1@example.com",
		"password": "Password123!",
	}

	body, _ := json.Marshal(registerData)
	
	// First request should succeed
	req := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp.StatusCode)

	// Second request should succeed
	req = httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp.StatusCode)

	// Third request should be rate limited
	req = httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusTooManyRequests, resp.StatusCode)

	// Verify the rate limiting response
	var rateLimitResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&rateLimitResp)
	suite.Contains(rateLimitResp, "error")
	suite.Equal("Too many requests", rateLimitResp["error"])
}

func (suite *RateLimitingIntegrationTestSuite) TestLoginRateLimiting() {
	// First, create a user
	registerData := map[string]string{
		"username": "testuser2",
		"email":    "test2@example.com",
		"password": "Password123!",
	}

	body, _ := json.Marshal(registerData)
	req := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp.StatusCode)

	// Test login rate limiting
	loginData := map[string]string{
		"email":    "test2@example.com",
		"password": "Password123!",
	}

	body, _ = json.Marshal(loginData)
	
	// First login request should succeed
	req = httptest.NewRequest("POST", "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusOK, resp.StatusCode)

	// Second login request should succeed
	req = httptest.NewRequest("POST", "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusOK, resp.StatusCode)

	// Third login request should be rate limited
	req = httptest.NewRequest("POST", "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusTooManyRequests, resp.StatusCode)
}

func (suite *RateLimitingIntegrationTestSuite) TestForgotPasswordRateLimiting() {
	forgotData := map[string]string{
		"email": "test3@example.com",
	}

	body, _ := json.Marshal(forgotData)
	
	// First request should succeed
	req := httptest.NewRequest("POST", "/api/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusOK, resp.StatusCode) // Always returns 200 even if email doesn't exist

	// Second request should be rate limited (limit is 1 per minute)
	req = httptest.NewRequest("POST", "/api/forgot-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusTooManyRequests, resp.StatusCode)
}

func (suite *RateLimitingIntegrationTestSuite) TestDifferentIPsAreIndependent() {
	registerData1 := map[string]string{
		"username": "testuser3",
		"email":    "test3@example.com",
		"password": "Password123!",
	}

	registerData2 := map[string]string{
		"username": "testuser4",
		"email":    "test4@example.com",
		"password": "Password123!",
	}

	body1, _ := json.Marshal(registerData1)
	body2, _ := json.Marshal(registerData2)

	// Simulate requests from different IPs by creating new requests
	// First IP - first request
	req1 := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.RemoteAddr = "192.168.1.100:12345"
	resp1, err := suite.app.Test(req1)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp1.StatusCode)

	// First IP - second request
	req1 = httptest.NewRequest("POST", "/api/register", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.RemoteAddr = "192.168.1.100:12345"
	resp1, err = suite.app.Test(req1)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp1.StatusCode)

	// First IP - third request (should be rate limited)
	req1 = httptest.NewRequest("POST", "/api/register", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.RemoteAddr = "192.168.1.100:12345"
	resp1, err = suite.app.Test(req1)
	suite.NoError(err)
	suite.Equal(http.StatusTooManyRequests, resp1.StatusCode)

	// Second IP - first request (should be allowed, independent of first IP)
	req2 := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.RemoteAddr = "192.168.1.200:12345"
	resp2, err := suite.app.Test(req2)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp2.StatusCode)
}

func (suite *RateLimitingIntegrationTestSuite) TestRateLimitingStats() {
	registerData := map[string]string{
		"username": "testuser5",
		"email":    "test5@example.com",
		"password": "Password123!",
	}

	body, _ := json.Marshal(registerData)
	
	// Make some requests to generate stats
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.168.1.300:12345"
		resp, err := suite.app.Test(req)
		suite.NoError(err)
		if i < 2 {
			suite.Equal(http.StatusCreated, resp.StatusCode)
		} else {
			suite.Equal(http.StatusTooManyRequests, resp.StatusCode)
		}
	}

	// Check stats
	stats := suite.rateLimiter.GetStats()
	suite.GreaterOrEqual(suite.T(), stats.TotalEntries, int64(1))
	suite.GreaterOrEqual(suite.T(), stats.Uptime, time.Duration(0))
	suite.GreaterOrEqual(suite.T(), stats.MemoryUsage, int64(0))
}

func (suite *RateLimitingIntegrationTestSuite) TestRateLimitingWithXForwardedFor() {
	registerData := map[string]string{
		"username": "testuser6",
		"email":    "test6@example.com",
		"password": "Password123!",
	}

	body, _ := json.Marshal(registerData)
	
	// Test with X-Forwarded-For header
	req := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "192.168.1.400")
	resp, err := suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp.StatusCode)

	// Second request with same X-Forwarded-For should succeed
	req = httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "192.168.1.400")
	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp.StatusCode)

	// Third request with same X-Forwarded-For should be rate limited
	req = httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "192.168.1.400")
	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusTooManyRequests, resp.StatusCode)
}

func TestRateLimitingIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping rate limiting integration tests")
	}
	suite.Run(t, new(RateLimitingIntegrationTestSuite))
}