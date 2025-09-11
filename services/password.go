package services

import (
	"errors"
	"unicode"
)

// PasswordPolicy defines the password requirements
type PasswordPolicy struct {
	MinLength      int
	MaxLength      int
	RequireUpper   bool
	RequireLower   bool
	RequireNumber  bool
	RequireSpecial bool
}

// PasswordRequirements provides detailed password requirements info
type PasswordRequirements struct {
	MinLength      int     `json:"min_length"`
	MaxLength      int     `json:"max_length"`
	RequireUpper   bool    `json:"require_upper"`
	RequireLower   bool    `json:"require_lower"`
	RequireNumber  bool    `json:"require_number"`
	RequireSpecial bool    `json:"require_special"`
	AllowedSpecial string `json:"allowed_special"`
	Examples       []string `json:"examples"`
}

// DefaultPasswordPolicy returns the default password policy
func DefaultPasswordPolicy() *PasswordPolicy {
	return &PasswordPolicy{
		MinLength:      8,   // Reduced from 12
		MaxLength:      128,
		RequireUpper:   true,
		RequireLower:   true,
		RequireNumber:  true,
		RequireSpecial: true,
	}
}

// GetPasswordRequirements returns detailed password requirements for UI display
func GetPasswordRequirements() *PasswordRequirements {
	policy := DefaultPasswordPolicy()
	return &PasswordRequirements{
		MinLength:      policy.MinLength,
		MaxLength:      policy.MaxLength,
		RequireUpper:   policy.RequireUpper,
		RequireLower:   policy.RequireLower,
		RequireNumber:  policy.RequireNumber,
		RequireSpecial: policy.RequireSpecial,
		AllowedSpecial: "!@#$%^&*()_+-=[]{}|;:,.<>/?",
		Examples: []string{
			"MySecureP@ss",
			"StrongPass123!",
			"GoLangRocks24",
		},
	}
}

// ValidatePassword enforces strong password rules
func ValidatePassword(password string) error {
	policy := DefaultPasswordPolicy()
	return policy.ValidatePassword(password)
}

// ValidatePassword validates a password against the policy
func (pp *PasswordPolicy) ValidatePassword(password string) error {
	// Length validation
	if len(password) < pp.MinLength {
		return errors.New("password must be at least 8 characters long")
	}
	if len(password) > pp.MaxLength {
		return errors.New("password must be less than 128 characters")
	}

	// Character type validation
	var hasUpper, hasLower, hasNumber, hasSpecial bool

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	// Check requirements
	if pp.RequireUpper && !hasUpper {
		return errors.New("password must contain at least one uppercase letter")
	}
	if pp.RequireLower && !hasLower {
		return errors.New("password must contain at least one lowercase letter")
	}
	if pp.RequireNumber && !hasNumber {
		return errors.New("password must contain at least one number")
	}
	if pp.RequireSpecial && !hasSpecial {
		return errors.New("password must contain at least one special character (!@#$%^&* etc.)")
	}

	return nil
}

// CheckPasswordStrength provides a strength assessment (0-4, for UI meter)
// 0: Very Weak, 1: Weak, 2: Fair, 3: Good, 4: Strong
func CheckPasswordStrength(password string) int {
	if len(password) < 1 {
		return 0
	}

	score := 0
	hasUpper, hasLower, hasNumber, hasSpecial := false, false, false, false

	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			hasUpper = true
		case unicode.IsLower(char):
			hasLower = true
		case unicode.IsNumber(char):
			hasNumber = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			hasSpecial = true
		}
	}

	categories := 0
	if hasUpper { categories++ }
	if hasLower { categories++ }
	if hasNumber { categories++ }
	if hasSpecial { categories++ }

	// Base score on length
	if len(password) >= 8 {
		score = 1 // At least 8 chars
	}
	if len(password) >= 12 {
		score = 2 // At least 12 chars
	}
	if len(password) >= 16 {
		score = 3 // At least 16 chars
	}

	// Add bonus for character categories
	if categories >= 3 {
		score++
	}
	if categories >= 4 {
		score++
	}

	// Cap score at 4
	if score > 4 {
		score = 4
	}

	return score
}