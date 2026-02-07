package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
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

func (m *MockUserRepository) CreateWithTx(tx *sqlx.Tx, user *models.User) error {
	args := m.Called(tx, user)
	user.ID = uuid.New()
	return args.Error(0)
}

func (m *MockUserRepository) BeginTx() (*sqlx.Tx, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*sqlx.Tx), args.Error(1)
}

func (m *MockUserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	args := m.Called(ctx, email)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
	args := m.Called(ctx, username)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) GetByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) UpdateProfile(id uuid.UUID, updates models.UpdateUserRequest) (*models.User, error) {
	args := m.Called(id, updates)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.User), args.Error(1)
}

func (m *MockUserRepository) UpdateEmail(id uuid.UUID, email string) error {
	args := m.Called(id, email)
	return args.Error(0)
}

func (m *MockUserRepository) UpdatePassword(id uuid.UUID, passwordHash string) error {
	args := m.Called(id, passwordHash)
	return args.Error(0)
}

func (m *MockUserRepository) DeleteUser(id uuid.UUID) error {
	args := m.Called(id)
	return args.Error(0)
}

func (m *MockUserRepository) SetAdmin(id uuid.UUID, isAdmin bool) error {
	args := m.Called(id, isAdmin)
	return args.Error(0)
}

func (m *MockUserRepository) SetDisabled(id uuid.UUID, disabled bool) error {
	args := m.Called(id, disabled)
	return args.Error(0)
}

func (m *MockUserRepository) SetModerator(id uuid.UUID, isModerator bool) error {
	args := m.Called(id, isModerator)
	return args.Error(0)
}

func (m *MockUserRepository) ListUsers(page, limit int) ([]models.User, int, error) {
	args := m.Called(page, limit)
	var users []models.User
	if u := args.Get(0); u != nil {
		users = u.([]models.User)
	}
	total := 0
	if t := args.Get(1); t != nil {
		total = t.(int)
	}
	return users, total, args.Error(2)
}

func (m *MockUserRepository) SearchUsers(q string, page, limit int) ([]models.User, int, error) {
	args := m.Called(q, page, limit)
	var users []models.User
	if u := args.Get(0); u != nil {
		users = u.([]models.User)
	}
	total := 0
	if t := args.Get(1); t != nil {
		total = t.(int)
	}
	return users, total, args.Error(2)
}

func TestRegisterSuccess(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	mockRepo := new(MockUserRepository)
	handler := handlers.NewAuthHandler(mockRepo)

	app := fiber.New()
	app.Post("/register", handler.Register)

	mockRepo.On("GetByEmail", mock.Anything, "test@example.com").Return(nil, sql.ErrNoRows)
	mockRepo.On("GetByUsername", mock.Anything, "testuser").Return(nil, sql.ErrNoRows)
	mockRepo.On("BeginTx").Return(nil, nil)
	mockRepo.On("Create", mock.AnythingOfType("*models.User")).Return(nil)

	reqBody := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "Password123!",
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
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	mockRepo := new(MockUserRepository)
	handler := handlers.NewAuthHandler(mockRepo)

	app := fiber.New()
	app.Post("/register", handler.Register)

	existingUser := &models.User{Email: "test@example.com"}
	mockRepo.On("GetByEmail", mock.Anything, "test@example.com").Return(existingUser, nil)

	reqBody := map[string]string{
		"username": "testuser",
		"email":    "test@example.com",
		"password": "Password123!",
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
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	mockRepo := new(MockUserRepository)
	handler := handlers.NewAuthHandler(mockRepo)

	app := fiber.New()
	app.Post("/login", handler.Login)

	user := &models.User{
		ID:       uuid.New(),
		Username: "testuser",
		Email:    "test@example.com",
	}
	user.HashPassword("Password123!")

	mockRepo.On("GetByEmail", mock.Anything, "test@example.com").Return(user, nil)

	reqBody := map[string]string{
		"login_identifier": "test@example.com",
		"login_password":   "Password123!",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	mockRepo.AssertExpectations(t)
}

func TestLoginSuccessWithUsername(t *testing.T) {
	t.Setenv("JWT_SECRET", strings.Repeat("a", 32))
	mockRepo := new(MockUserRepository)
	handler := handlers.NewAuthHandler(mockRepo)

	app := fiber.New()
	app.Post("/login", handler.Login)

	user := &models.User{
		ID:       uuid.New(),
		Username: "testuser",
		Email:    "test@example.com",
	}
	user.HashPassword("Password123!")

	mockRepo.On("GetByUsername", mock.Anything, "testuser").Return(user, nil)

	reqBody := map[string]string{
		"login_identifier": "testuser",
		"login_password":   "Password123!",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("POST", "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, fiber.StatusOK, resp.StatusCode)

	mockRepo.AssertExpectations(t)
}
