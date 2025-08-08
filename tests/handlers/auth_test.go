package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/yourusername/trough/handlers"
	"github.com/yourusername/trough/models"
)

type MockUserRepository struct {
	mock.Mock
}

var _ models.UserRepositoryInterface = (*MockUserRepository)(nil)

func (m *MockUserRepository) Create(user *models.User) error {
	args := m.Called(user)
	user.ID = uuid.New()
	return args.Error(0)
}

func (m *MockUserRepository) GetByEmail(email string) (*models.User, error) {
	args := m.Called(email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) GetByUsername(username string) (*models.User, error) {
	args := m.Called(username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) GetByID(id uuid.UUID) (*models.User, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func TestRegisterSuccess(t *testing.T) {
	mockRepo := new(MockUserRepository)
	handler := handlers.NewAuthHandler(mockRepo)
	
	app := fiber.New()
	app.Post("/register", handler.Register)

	mockRepo.On("GetByEmail", "test@example.com").Return(nil, sql.ErrNoRows)
	mockRepo.On("GetByUsername", "testuser").Return(nil, sql.ErrNoRows)
	mockRepo.On("Create", mock.AnythingOfType("*models.User")).Return(nil)

	reqBody := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "password123",
	}
	
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusCreated, resp.StatusCode)

	mockRepo.AssertExpectations(t)
}

func TestRegisterEmailExists(t *testing.T) {
	mockRepo := new(MockUserRepository)
	handler := handlers.NewAuthHandler(mockRepo)
	
	app := fiber.New()
	app.Post("/register", handler.Register)

	existingUser := &models.User{Email: "test@example.com"}
	mockRepo.On("GetByEmail", "test@example.com").Return(existingUser, nil)

	reqBody := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "password123",
	}
	
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusConflict, resp.StatusCode)

	mockRepo.AssertExpectations(t)
}

func TestLoginSuccess(t *testing.T) {
	mockRepo := new(MockUserRepository)
	handler := handlers.NewAuthHandler(mockRepo)
	
	app := fiber.New()
	app.Post("/login", handler.Login)

	user := &models.User{
		ID:       uuid.New(),
		Username: "testuser",
		Email:    "test@example.com",
	}
	user.HashPassword("password123")
	
	mockRepo.On("GetByEmail", "test@example.com").Return(user, nil)

	reqBody := map[string]string{
		"email":    "test@example.com",
		"password": "password123",
	}
	
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	mockRepo.AssertExpectations(t)
}