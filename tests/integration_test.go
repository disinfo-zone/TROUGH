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
	"github.com/yourusername/trough/middleware"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

type IntegrationTestSuite struct {
	suite.Suite
	app         *fiber.App
	userRepo    *models.UserRepository
	imageRepo   *models.ImageRepository
	likeRepo    *models.LikeRepository
	inviteRepo  *models.InviteRepository
	settingsRepo *models.SiteSettingsRepository
}

func (suite *IntegrationTestSuite) SetupSuite() {
	os.Setenv("DATABASE_URL", "postgres://trough:trough@localhost:5432/trough_test?sslmode=disable")

	err := db.Connect()
	suite.Require().NoError(err)

	err = db.Migrate()
	suite.Require().NoError(err)

	config, err := services.LoadConfig("../config.yaml")
	suite.Require().NoError(err)

	suite.userRepo = models.NewUserRepository(db.DB)
	suite.imageRepo = models.NewImageRepository(db.DB)
	suite.likeRepo = models.NewLikeRepository(db.DB)
	suite.inviteRepo = models.NewInviteRepository(db.DB)
	suite.settingsRepo = models.NewSiteSettingsRepository(db.DB)

	authHandler := handlers.NewAuthHandlerWithRepos(suite.userRepo, suite.settingsRepo).WithInvites(suite.inviteRepo)
	storage := services.NewLocalStorage("uploads")
	imageHandler := handlers.NewImageHandler(suite.imageRepo, suite.likeRepo, suite.userRepo, *config, storage)
	userHandler := handlers.NewUserHandler(suite.userRepo, suite.imageRepo, storage)

	suite.app = fiber.New()

	// Create rate limiter for testing
	rateLimiter := services.NewRateLimiter(config.RateLimiting)
	defer rateLimiter.Stop()

	api := suite.app.Group("/api")
	api.Post("/register", rateLimiter.Middleware(10, time.Minute), authHandler.Register)
	api.Post("/login", rateLimiter.Middleware(15, time.Minute), authHandler.Login)
	api.Get("/feed", imageHandler.GetFeed)
	api.Get("/images/:id", imageHandler.GetImage)
	api.Post("/upload", middleware.Protected(), imageHandler.Upload)
	api.Post("/images/:id/like", middleware.Protected(), imageHandler.LikeImage)
	api.Get("/users/:username", userHandler.GetProfile)
	api.Get("/users/:username/images", userHandler.GetUserImages)
}

func (suite *IntegrationTestSuite) TearDownSuite() {
	db.Close()
}

func (suite *IntegrationTestSuite) SetupTest() {
	db.DB.Exec("TRUNCATE users, images, likes, collections, invites CASCADE")
}

func (suite *IntegrationTestSuite) TestUserRegistrationAndLogin() {
	registerData := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "Password123!",
	}

	body, _ := json.Marshal(registerData)
	req := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp.StatusCode)

	var registerResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&registerResp)
	suite.Contains(registerResp, "token")
	suite.Contains(registerResp, "user")

	loginData := map[string]string{
		"email":    "test@example.com",
		"password": "Password123!",
	}

	body, _ = json.Marshal(loginData)
	req = httptest.NewRequest("POST", "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err = suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusOK, resp.StatusCode)

	var loginResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&loginResp)
	suite.Contains(loginResp, "token")
	suite.Contains(loginResp, "user")
}

func (suite *IntegrationTestSuite) TestUserRegistrationWithInviteCode() {
	// Disable public registration
	settings, err := suite.settingsRepo.Get()
	suite.NoError(err)
	settings.PublicRegistrationEnabled = false
	suite.settingsRepo.Upsert(settings)

	// Create an invite code with 1 use
	maxUses := 1
	invite, err := suite.inviteRepo.Create(&maxUses, nil, nil)
	suite.NoError(err)
	suite.NotNil(invite)

	// Attempt to register with the invite code (first use)
	registerData := map[string]string{
		"username": "inviteuser1",
		"email":    "invite1@example.com",
		"password": "Password123!",
		"invite":   invite.Code,
	}
	body, _ := json.Marshal(registerData)
	req := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusCreated, resp.StatusCode)

	// Attempt to register with the same invite code again (should fail)
	registerData2 := map[string]string{
		"username": "inviteuser2",
		"email":    "invite2@example.com",
		"password": "Password123!",
		"invite":   invite.Code,
	}
	body2, _ := json.Marshal(registerData2)
	req2 := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")

	resp2, err2 := suite.app.Test(req2)
	suite.NoError(err2)
	suite.Equal(http.StatusForbidden, resp2.StatusCode)

	// Test with an invite code with multiple uses
	maxUsesMulti := 2
	inviteMulti, err := suite.inviteRepo.Create(&maxUsesMulti, nil, nil)
	suite.NoError(err)
	suite.NotNil(inviteMulti)

	// First use of multi-use invite
	registerData3 := map[string]string{
		"username": "inviteuser3",
		"email":    "invite3@example.com",
		"password": "Password123!",
		"invite":   inviteMulti.Code,
	}
	body3, _ := json.Marshal(registerData3)
	req3 := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body3))
	req3.Header.Set("Content-Type", "application/json")

	resp3, err3 := suite.app.Test(req3)
	suite.NoError(err3)
	suite.Equal(http.StatusCreated, resp3.StatusCode)

	// Second use of multi-use invite
	registerData4 := map[string]string{
		"username": "inviteuser4",
		"email":    "invite4@example.com",
		"password": "Password123!",
		"invite":   inviteMulti.Code,
	}
	body4, _ := json.Marshal(registerData4)
	req4 := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body4))
	req4.Header.Set("Content-Type", "application/json")

	resp4, err4 := suite.app.Test(req4)
	suite.NoError(err4)
	suite.Equal(http.StatusCreated, resp4.StatusCode)

	// Third use of multi-use invite (should fail)
	registerData5 := map[string]string{
		"username": "inviteuser5",
		"email":    "invite5@example.com",
		"password": "Password123!",
		"invite":   inviteMulti.Code,
	}
	body5, _ := json.Marshal(registerData5)
	req5 := httptest.NewRequest("POST", "/api/register", bytes.NewReader(body5))
	req5.Header.Set("Content-Type", "application/json")

	resp5, err5 := suite.app.Test(req5)
	suite.NoError(err5)
	suite.Equal(http.StatusForbidden, resp5.StatusCode)
}

func (suite *IntegrationTestSuite) TestFeedEndpoint() {
	req := httptest.NewRequest("GET", "/api/feed", nil)

	resp, err := suite.app.Test(req)
	suite.NoError(err)
	suite.Equal(http.StatusOK, resp.StatusCode)

	var feedResp models.FeedResponse
	json.NewDecoder(resp.Body).Decode(&feedResp)
	suite.Equal(1, feedResp.Page)
	suite.Equal(0, feedResp.Total)
	suite.Empty(feedResp.Images)
}

func TestIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests")
	}
	suite.Run(t, new(IntegrationTestSuite))
}
