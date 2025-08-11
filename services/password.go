package services

import (
	"errors"
	"unicode"
)

// ValidatePassword enforces sensible password rules:
// - Minimum length 8
// - At least 3 of 4 categories present: lowercase, uppercase, digit, symbol
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return errors.New("password must be at least 8 characters long")
	}
	hasLower := false
	hasUpper := false
	hasDigit := false
	hasSymbol := false
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			// Count any non-letter, non-digit as symbol
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				hasSymbol = true
			}
		}
	}
	categories := 0
	if hasLower {
		categories++
	}
	if hasUpper {
		categories++
	}
	if hasDigit {
		categories++
	}
	if hasSymbol {
		categories++
	}
	if categories < 3 {
		return errors.New("password must include at least three of: lowercase, uppercase, number, symbol")
	}
	return nil
}
