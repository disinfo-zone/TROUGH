package handlers

import (
	"database/sql"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/yourusername/trough/middleware"
	"github.com/yourusername/trough/models"
	"github.com/yourusername/trough/services"
)

type AuthHandler struct {
	userRepo      models.UserRepositoryInterface
	settingsRepo  models.SiteSettingsRepositoryInterface
	validator     *validator.Validate
	newMailSender func(*models.SiteSettings) services.MailSender
}

// Backwards-compatible constructor used by existing tests
func NewAuthHandler(userRepo models.UserRepositoryInterface) *AuthHandler {
	return &AuthHandler{
		userRepo:      userRepo,
		settingsRepo:  &inMemorySettingsRepo{settings: models.SiteSettings{SiteName: "TROUGH"}},
		validator:     validator.New(),
		newMailSender: services.NewMailSender,
	}
}

// Preferred explicit DI constructor used in main
func NewAuthHandlerWithRepos(userRepo models.UserRepositoryInterface, settingsRepo models.SiteSettingsRepositoryInterface) *AuthHandler {
	return &AuthHandler{userRepo: userRepo, settingsRepo: settingsRepo, validator: validator.New(), newMailSender: services.NewMailSender}
}

// For tests
func (h *AuthHandler) WithMailFactory(f func(*models.SiteSettings) services.MailSender) *AuthHandler {
	h.newMailSender = f
	return h
}

// simple in-memory SiteSettings repo for tests
type inMemorySettingsRepo struct{ settings models.SiteSettings }

func (r *inMemorySettingsRepo) Get() (*models.SiteSettings, error)  { s := r.settings; return &s, nil }
func (r *inMemorySettingsRepo) Upsert(s *models.SiteSettings) error { r.settings = *s; return nil }
func (r *inMemorySettingsRepo) UpdateFavicon(path string) error {
	r.settings.FaviconPath = path
	return nil
}
func (r *inMemorySettingsRepo) UpdateSocialImageURL(path string) error {
	r.settings.SocialImageURL = path
	return nil
}

func (h *AuthHandler) Register(c *fiber.Ctx) error {
	var req models.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := h.validator.Struct(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Validation failed", "details": err.Error()})
	}
	existingUser, _ := h.userRepo.GetByEmail(req.Email)
	if existingUser != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Email already registered"})
	}
	existingUser, _ = h.userRepo.GetByUsername(req.Username)
	if existingUser != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Username already taken"})
	}
	user := &models.User{Username: req.Username, Email: req.Email}
	if err := user.HashPassword(req.Password); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to process password"})
	}
	if err := h.userRepo.Create(user); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to create user"})
	}
	set, _ := h.settingsRepo.Get()
	if set.RequireEmailVerification && set.SMTPHost != "" && set.SMTPPort > 0 && set.SMTPUsername != "" && set.SMTPPassword != "" {
		u, _ := h.userRepo.GetByEmail(req.Email)
		if u != nil {
			_ = models.SetEmailVerified(u.ID, false)
			token := uuid.New().String()
			exp := time.Now().Add(24 * time.Hour)
			_ = models.CreateEmailVerification(u.ID, token, exp)
			sender := h.newMailSender(set)
			link := set.SiteURL + "/verify?token=" + token
			_ = sender.Send(u.Email, "Verify your email", "Click to verify: "+link)
		}
	}
	token, err := middleware.GenerateToken(user.ID, user.Username)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"user": user.ToResponse(), "token": token})
}

func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req models.LoginRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if err := h.validator.Struct(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Validation failed", "details": err.Error()})
	}
	user, err := h.userRepo.GetByEmail(req.Email)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Database error"})
	}
	if user.IsDisabled {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Account disabled"})
	}
	if !user.CheckPassword(req.Password) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
	}
	set, _ := h.settingsRepo.Get()
	if set.RequireEmailVerification && (set.SMTPHost != "") {
		if !user.EmailVerified {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Email not verified"})
		}
	}
	token, err := middleware.GenerateToken(user.ID, user.Username)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}
	return c.JSON(fiber.Map{"user": user.ToResponse(), "token": token})
}

func (h *AuthHandler) Me(c *fiber.Ctx) error {
	userID := middleware.GetUserID(c)
	if userID == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	user, err := h.userRepo.GetByID(userID)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}

	return c.JSON(fiber.Map{"user": user.ToResponse()})
}

func (h *AuthHandler) ForgotPassword(c *fiber.Ctx) error {
	type req struct {
		Email string `json:"email"`
	}
	var r req
	if err := c.BodyParser(&r); err != nil || r.Email == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Email required"})
	}
	u, err := h.userRepo.GetByEmail(r.Email)
	if err != nil {
		return c.SendStatus(fiber.StatusNoContent)
	}
	set, _ := h.settingsRepo.Get()
	if set.SMTPHost == "" || set.SMTPPort == 0 || set.SMTPUsername == "" || set.SMTPPassword == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP not configured"})
	}
	last, _ := models.LastPasswordResetSentAt(u.ID)
	if time.Since(last) < 5*time.Minute {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "Please wait before requesting again"})
	}
	token := uuid.New().String()
	expires := time.Now().Add(1 * time.Hour)
	if err := models.CreatePasswordReset(u.ID, token, expires); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed"})
	}
	sender := h.newMailSender(set)
	link := set.SiteURL + "/reset?token=" + token
	if err := sender.Send(u.Email, "Reset your password", "Use this link to reset your password: "+link); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP send failed", "details": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *AuthHandler) ResetPassword(c *fiber.Ctx) error {
	type req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	var r req
	if err := c.BodyParser(&r); err != nil || len(r.NewPassword) < 6 || r.Token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}
	uid, exp, err := models.GetPasswordReset(r.Token)
	if err != nil || time.Now().After(exp) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid or expired token"})
	}
	u, err := h.userRepo.GetByID(uid)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "User"})
	}
	if err := u.HashPassword(r.NewPassword); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed"})
	}
	if err := h.userRepo.UpdatePassword(uid, u.PasswordHash); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed"})
	}
	_ = models.DeletePasswordReset(r.Token)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *AuthHandler) VerifyEmail(c *fiber.Ctx) error {
	type req struct {
		Token string `json:"token"`
	}
	var r req
	if err := c.BodyParser(&r); err != nil || r.Token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Token required"})
	}
	uid, exp, err := models.GetEmailVerification(r.Token)
	if err != nil || time.Now().After(exp) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid or expired token"})
	}
	_ = models.SetEmailVerified(uid, true)
	_ = models.DeleteEmailVerification(r.Token)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *AuthHandler) ResendVerification(c *fiber.Ctx) error {
	uid := middleware.GetUserID(c)
	if uid == uuid.Nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	u, err := h.userRepo.GetByID(uid)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Unauthorized"})
	}
	if u.EmailVerified {
		return c.SendStatus(fiber.StatusNoContent)
	}
	set, _ := h.settingsRepo.Get()
	if !(set.SMTPHost != "" && set.SMTPPort > 0 && set.SMTPUsername != "" && set.SMTPPassword != "") {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP not configured"})
	}
	last, _ := models.LastVerificationSentAt(uid)
	if time.Since(last) < 5*time.Minute {
		return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{"error": "Please wait before requesting again"})
	}
	token := uuid.New().String()
	exp := time.Now().Add(24 * time.Hour)
	if err := models.CreateEmailVerification(uid, token, exp); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed"})
	}
	sender := h.newMailSender(set)
	link := set.SiteURL + "/verify?token=" + token
	if err := sender.Send(u.Email, "Verify your email", "Click to verify: "+link); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP send failed", "details": err.Error()})
	}
	return c.SendStatus(fiber.StatusNoContent)
}
