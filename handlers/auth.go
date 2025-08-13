package handlers

import (
	"database/sql"
	"log"
	"strings"
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
	inviteRepo    models.InviteRepositoryInterface
}

// Backwards-compatible constructor used by existing tests
func NewAuthHandler(userRepo models.UserRepositoryInterface) *AuthHandler {
	return &AuthHandler{
		userRepo:      userRepo,
		settingsRepo:  &inMemorySettingsRepo{settings: models.SiteSettings{SiteName: "TROUGH", PublicRegistrationEnabled: true}},
		validator:     validator.New(),
		newMailSender: services.NewMailSender,
	}
}

// Preferred explicit DI constructor used in main
func NewAuthHandlerWithRepos(userRepo models.UserRepositoryInterface, settingsRepo models.SiteSettingsRepositoryInterface) *AuthHandler {
	return &AuthHandler{userRepo: userRepo, settingsRepo: settingsRepo, validator: validator.New(), newMailSender: services.NewMailSender}
}

// WithInvites provides the invite repository dependency.
func (h *AuthHandler) WithInvites(r models.InviteRepositoryInterface) *AuthHandler {
	h.inviteRepo = r
	return h
}

// ValidateInvite checks whether an invite code exists and is currently usable (not expired/exhausted).
func (h *AuthHandler) ValidateInvite(c *fiber.Ctx) error {
	code := strings.TrimSpace(c.Query("code", ""))
	if code == "" || h.inviteRepo == nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid invite"})
	}
	inv, err := h.inviteRepo.GetByCode(code)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid invite"})
	}
	if inv.ExpiresAt != nil && time.Now().After(*inv.ExpiresAt) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invite expired"})
	}
	if inv.MaxUses != nil && inv.Uses >= *inv.MaxUses {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invite exhausted"})
	}
	return c.SendStatus(fiber.StatusNoContent)
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
	// Support invite codes which can bypass public registration toggle.
	inviteCode := strings.TrimSpace(c.Query("invite", ""))
	mustHaveInvite := false
	if set, err := h.settingsRepo.Get(); err == nil {
		mustHaveInvite = !set.PublicRegistrationEnabled
	}
	var req models.CreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request body"})
	}
	if mustHaveInvite {
		if inviteCode == "" {
			// Also allow JSON body field to carry invite
			if v := strings.TrimSpace(req.Password); v == "" { /* no-op to keep req used */
			}
			type rawReq struct {
				Invite string `json:"invite"`
			}
			var rr rawReq
			_ = c.BodyParser(&rr)
			if strings.TrimSpace(rr.Invite) != "" {
				inviteCode = strings.TrimSpace(rr.Invite)
			}
		}
		if inviteCode == "" || h.inviteRepo == nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Registration is currently disabled"})
		}
	}
	// Normalize input early and validate path params consistently
	req.Username = strings.ToLower(strings.TrimSpace(req.Username))
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if isReservedUsername(req.Username) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "That username is reserved"})
	}
	if err := h.validator.Struct(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Validation failed", "details": err.Error()})
	}
	// Server-side password policy
	if err := services.ValidatePassword(req.Password); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	existingUser, _ := h.userRepo.GetByEmail(req.Email)
	if existingUser != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Email already registered"})
	}
	existingUser, _ = h.userRepo.GetByUsername(req.Username)
	if existingUser != nil {
		return c.Status(fiber.StatusConflict).JSON(fiber.Map{"error": "Username already taken"})
	}
	// If an invite is required, consume only after validation and conflict checks
	var consumedInviteID *uuid.UUID
	if mustHaveInvite {
		inv, err := h.inviteRepo.Consume(inviteCode)
		if err != nil {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "Invalid or expired invite"})
		}
		consumedInviteID = &inv.ID
	}
	user := &models.User{Username: req.Username, Email: req.Email}
	if err := user.HashPassword(req.Password); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to process password"})
	}
	if err := h.userRepo.Create(user); err != nil {
		if consumedInviteID != nil && h.inviteRepo != nil {
			_ = h.inviteRepo.RevertConsume(*consumedInviteID)
		}
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
			link := set.SiteURL + "/verify?token=" + token
			services.EnqueueMail(u.Email, "Verify your email", "Click to verify: "+link)
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
	// Normalize email for lookup to match registration normalization
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if err := h.validator.Struct(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Validation failed", "details": err.Error()})
	}
	user, err := h.userRepo.GetByEmail(req.Email)
	if err != nil {
		if err == sql.ErrNoRows {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
		}
		// Log server-side; avoid leaking DB state to clients
		log.Printf("login error: GetByEmail failed: %v", err)
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "Invalid credentials"})
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
	link := set.SiteURL + "/reset?token=" + token
	// Plain-text, ASCII-styled message with clear instructions and expiry notice
	body := "" +
		"============================\n" +
		"  PASSWORD RESET REQUEST\n" +
		"============================\n\n" +
		"We received a request to reset your password.\n\n" +
		"If you made this request, use the link below to set a new password.\n" +
		"If you did NOT request this, you can safely ignore this email.\n\n" +
		">>> RESET LINK (valid for 1 hour, single-use) <<<\n" +
		link + "\n\n" +
		"Tips for a strong password:\n" +
		"- 8+ characters\n" +
		"- mix of UPPER/lower case, numbers, symbols\n\n" +
		"This link expires in 1 hour or after it is used once.\n" +
		"For security, never share this link.\n\n" +
		"— TROUGH\n"
		// Try async path; also attempt sync for immediate error surface
	if err := h.newMailSender(set).Send(u.Email, "Reset your password", body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP send failed", "details": err.Error()})
	}
	services.EnqueueMail(u.Email, "Reset your password", body)
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *AuthHandler) ResetPassword(c *fiber.Ctx) error {
	type req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	var r req
	if err := c.BodyParser(&r); err != nil || r.Token == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid request"})
	}
	if err := services.ValidatePassword(r.NewPassword); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
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
	// Issue a fresh token so client can auto-login
	tokenStr, err := middleware.GenerateToken(u.ID, u.Username)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "Failed to generate token"})
	}
	return c.JSON(fiber.Map{"user": u.ToResponse(), "token": tokenStr})
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
	link := set.SiteURL + "/verify?token=" + token
	if err := h.newMailSender(set).Send(u.Email, "Verify your email", "Click to verify: "+link); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "SMTP send failed", "details": err.Error()})
	}
	services.EnqueueMail(u.Email, "Verify your email", "Click to verify: "+link)
	return c.SendStatus(fiber.StatusNoContent)
}
