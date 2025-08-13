package handlers

import (
	"html"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/yourusername/trough/models"
)

// Public page view handler: returns JSON for SPA render or performs redirect if configured
type PageHandler struct {
	pages models.PageRepositoryInterface
}

func NewPageHandler(repo models.PageRepositoryInterface) *PageHandler {
	return &PageHandler{pages: repo}
}

// GetPublicPage returns the public page content or redirect
func (h *PageHandler) GetPublicPage(c *fiber.Ctx) error {
	slug := strings.ToLower(strings.TrimSpace(c.Params("slug")))
	if slug == "" {
		return fiber.ErrNotFound
	}
	// Restrict to single-segment, alphanumeric/hyphen
	if strings.Contains(slug, "/") {
		return fiber.ErrNotFound
	}
	p, err := h.pages.GetPublishedBySlug(slug)
	if err != nil || p == nil {
		return fiber.ErrNotFound
	}
	// Return minimal JSON content for SPA to render; also include safe meta
	title := p.Title
	metaTitle := title
	if p.MetaTitle != nil && strings.TrimSpace(*p.MetaTitle) != "" {
		metaTitle = strings.TrimSpace(*p.MetaTitle)
	}
	desc := ""
	if p.MetaDescription != nil {
		desc = strings.TrimSpace(*p.MetaDescription)
	}
	return c.JSON(fiber.Map{
		"slug":             p.Slug,
		"title":            title,
		"html":             p.HTML,
		"markdown":         p.Markdown,
		"redirect_url":     strings.TrimSpace(coalesce(p.RedirectURL)),
		"meta_title":       html.EscapeString(metaTitle),
		"meta_description": html.EscapeString(desc),
	})
}

func coalesce(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
