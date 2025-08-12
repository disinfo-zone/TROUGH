package handlers

import "strings"

// Centralized username normalization and policy helpers

var reservedUsernames = map[string]struct{}{
	"admin":         {},
	"administrator": {},
	"adminteam":     {},
	"admins":        {},
	"root":          {},
	"system":        {},
	"sysadmin":      {},
	"superadmin":    {},
	"superuser":     {},
	"support":       {},
	"help":          {},
	"helpdesk":      {},
	"moderator":     {},
	"mod":           {},
	"mods":          {},
	"staff":         {},
	"team":          {},
	"security":      {},
	"official":      {},
	"noreply":       {},
	"no-reply":      {},
	"postmaster":    {},
	"abuse":         {},
	"report":        {},
	"reports":       {},
	"owner":         {},
	"undefined":     {},
	"null":          {},
	"trough":        {},
}

func normalizeUsername(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func isReservedUsername(u string) bool {
	_, ok := reservedUsernames[u]
	return ok
}
