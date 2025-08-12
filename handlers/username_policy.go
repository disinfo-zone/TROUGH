package handlers

import "strings"

// Centralized username normalization and policy helpers

var reservedUsernames = map[string]struct{}{
	"admin":     {},
	"root":      {},
	"system":    {},
	"support":   {},
	"moderator": {},
	"owner":     {},
	"undefined": {},
	"null":      {},
}

func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func isReservedUsername(u string) bool {
	_, ok := reservedUsernames[u]
	return ok
}
