package services

import (
	"errors"
	"math"
	"strings"
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
	ForbiddenWords []string
	CommonPatterns []string
}

// PasswordRequirements provides detailed password requirements info
type PasswordRequirements struct {
	MinLength      int     `json:"min_length"`
	MaxLength      int     `json:"max_length"`
	RequireUpper   bool    `json:"require_upper"`
	RequireLower   bool    `json:"require_lower"`
	RequireNumber  bool    `json:"require_number"`
	RequireSpecial bool    `json:"require_special"`
	ForbiddenWords []string `json:"forbidden_words"`
	AllowedSpecial string `json:"allowed_special"`
	Examples       []string `json:"examples"`
}

// DefaultPasswordPolicy returns the default password policy
func DefaultPasswordPolicy() *PasswordPolicy {
	return &PasswordPolicy{
		MinLength:      12,
		MaxLength:      128,
		RequireUpper:   true,
		RequireLower:   true,
		RequireNumber:  true,
		RequireSpecial: true,
		ForbiddenWords: []string{"password", "trough", "admin", "user", "login", "welcome", "123456", "qwerty"},
		CommonPatterns: []string{"123", "abc", "qwerty", "password", "admin", "user", "welcome"},
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
		ForbiddenWords: policy.ForbiddenWords,
		AllowedSpecial: "!@#$%^&*()_+-=[]{}|;:,.<>?",
		Examples: []string{
			"SecureP@ssw0rd!",
			"MyTr0ugh!mageGallery",
			"A1b2C3d4E5f6G7",
			"Creative@Art2024",
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
		return errors.New("password must be at least 12 characters long")
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
	
	// Check for forbidden words (case-insensitive)
	lowerPassword := strings.ToLower(password)
	for _, word := range pp.ForbiddenWords {
		if strings.Contains(lowerPassword, word) {
			return errors.New("password contains common words that are not allowed")
		}
	}
	
	// Check for common patterns
	for _, pattern := range pp.CommonPatterns {
		if strings.Contains(lowerPassword, pattern) {
			return errors.New("password contains common patterns that are not allowed")
		}
	}
	
	// Check for sequential characters
	if pp.hasSequentialChars(password) {
		return errors.New("password contains sequential characters")
	}
	
	// Check for repeating characters
	if pp.hasRepeatingChars(password) {
		return errors.New("password contains too many repeating characters")
	}
	
	// Check for keyboard patterns
	if pp.hasKeyboardPattern(password) {
		return errors.New("password contains keyboard patterns")
	}
	
	// Check for entropy (basic measure)
	if pp.calculateEntropy(password) < 3.0 {
		return errors.New("password is too predictable")
	}
	
	return nil
}

// hasSequentialChars checks for sequential characters (123, abc, etc.)
func (pp *PasswordPolicy) hasSequentialChars(password string) bool {
	for i := 0; i < len(password)-2; i++ {
		if password[i]+1 == password[i+1] && password[i+1]+1 == password[i+2] {
			return true
		}
		if password[i]-1 == password[i+1] && password[i+1]-1 == password[i+2] {
			return true
		}
	}
	return false
}

// hasRepeatingChars checks for repeating characters
func (pp *PasswordPolicy) hasRepeatingChars(password string) bool {
	consecutiveCount := 1
	for i := 1; i < len(password); i++ {
		if password[i] == password[i-1] {
			consecutiveCount++
			if consecutiveCount >= 3 {
				return true
			}
		} else {
			consecutiveCount = 1
		}
	}
	return false
}

// hasKeyboardPattern checks for common keyboard patterns
func (pp *PasswordPolicy) hasKeyboardPattern(password string) bool {
	keyboardPatterns := []string{
		"qwerty", "asdfgh", "zxcvbn", "123456", "qazwsx",
		"1qaz", "2wsx", "3edc", "4rfv", "5tgb", "6yhn", "7ujm", "8ik,",
		"!qaz", "@wsx", "#edc", "$rfv", "%tgb", "^yhn", "&ujm", "*ik,",
	}
	
	lowerPassword := strings.ToLower(password)
	for _, pattern := range keyboardPatterns {
		if strings.Contains(lowerPassword, pattern) {
			return true
		}
	}
	return false
}

// calculateEntropy calculates password entropy (basic approximation)
func (pp *PasswordPolicy) calculateEntropy(password string) float64 {
	var charsetSize float64
	hasUpper := false
	hasLower := false
	hasNumber := false
	hasSpecial := false
	
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
	
	if hasUpper {
		charsetSize += 26
	}
	if hasLower {
		charsetSize += 26
	}
	if hasNumber {
		charsetSize += 10
	}
	if hasSpecial {
		charsetSize += 32
	}
	
	if charsetSize == 0 {
		return 0
	}
	
	return float64(len(password)) * math.Log2(charsetSize)
}

// CheckPasswordStrength provides a strength assessment (0-100)
func CheckPasswordStrength(password string) int {
	if err := ValidatePassword(password); err != nil {
		return 0
	}
	
	policy := DefaultPasswordPolicy()
	entropy := policy.calculateEntropy(password)
	
	// Base score from entropy
	score := int(entropy * 10)
	if score > 100 {
		score = 100
	}
	
	// Bonus for length
	if len(password) >= 16 {
		score += 10
	}
	if len(password) >= 20 {
		score += 10
	}
	
	// Bonus for complexity
	complexity := 0
	for _, char := range password {
		switch {
		case unicode.IsUpper(char):
			complexity++
		case unicode.IsLower(char):
			complexity++
		case unicode.IsNumber(char):
			complexity++
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			complexity += 2
		}
	}
	
	if float64(complexity)/float64(len(password)) > 1.5 {
		score += 10
	}
	
	if score > 100 {
		score = 100
	}
	
	return score
}
