package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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
	app       *fiber.App
	userRepo  *models.UserRepository
	imageRepo *models.ImageRepository
	likeRepo  *models.LikeRepository
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

	authHandler := handlers.NewAuthHandler(suite.userRepo)
	storage := services.NewLocalStorage("uploads")
	imageHandler := handlers.NewImageHandler(suite.imageRepo, suite.likeRepo, suite.userRepo, *config, storage)
	userHandler := handlers.NewUserHandler(suite.userRepo, suite.imageRepo, storage)

	suite.app = fiber.New()

	api := suite.app.Group("/api")
	api.Post("/register", authHandler.Register)
	api.Post("/login", authHandler.Login)
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
	db.DB.Exec("TRUNCATE users, images, likes CASCADE")
}

func (suite *IntegrationTestSuite) TestUserRegistrationAndLogin() {
	registerData := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "password123",
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
		"password": "password123",
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
