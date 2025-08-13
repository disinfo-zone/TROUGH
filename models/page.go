package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Page represents a simple CMS page or redirect.
// If RedirectURL is non-empty, the page acts as a redirect and HTML/Markdown are ignored in serving.
type Page struct {
	ID              uuid.UUID `db:"id" json:"id"`
	Slug            string    `db:"slug" json:"slug"`
	Title           string    `db:"title" json:"title"`
	Markdown        string    `db:"markdown" json:"markdown"`
	HTML            string    `db:"html" json:"html"`
	IsPublished     bool      `db:"is_published" json:"is_published"`
	RedirectURL     *string   `db:"redirect_url" json:"redirect_url,omitempty"`
	MetaTitle       *string   `db:"meta_title" json:"meta_title,omitempty"`
	MetaDescription *string   `db:"meta_description" json:"meta_description,omitempty"`
	CreatedAt       time.Time `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time `db:"updated_at" json:"updated_at"`
}

type PageRepository struct {
	db *sqlx.DB
}

func NewPageRepository(db *sqlx.DB) *PageRepository { return &PageRepository{db: db} }

func (r *PageRepository) Create(p *Page) error {
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	now := time.Now()
	q := `
        INSERT INTO pages (slug, title, markdown, html, is_published, redirect_url, meta_title, meta_description, created_at, updated_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$9)
        RETURNING id, created_at, updated_at`
	return r.db.QueryRow(q, p.Slug, p.Title, p.Markdown, p.HTML, p.IsPublished, p.RedirectURL, p.MetaTitle, p.MetaDescription, now).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt)
}

func (r *PageRepository) Update(p *Page) error {
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	now := time.Now()
	q := `
        UPDATE pages
        SET slug=$1, title=$2, markdown=$3, html=$4, is_published=$5, redirect_url=$6, meta_title=$7, meta_description=$8, updated_at=$9
        WHERE id=$10`
	_, err := r.db.Exec(q, p.Slug, p.Title, p.Markdown, p.HTML, p.IsPublished, p.RedirectURL, p.MetaTitle, p.MetaDescription, now, p.ID)
	if err == nil {
		p.UpdatedAt = now
	}
	return err
}

func (r *PageRepository) Delete(id uuid.UUID) error {
	// Before delete, capture slug for tombstone if this is a seeded default
	var slug string
	_ = r.db.Get(&slug, `SELECT slug FROM pages WHERE id=$1`, id)
	if slug != "" && (slug == "about" || slug == "contact" || slug == "terms" || slug == "privacy" || slug == "faq") {
		_, _ = r.db.Exec(`INSERT INTO cms_tombstones(slug, deleted_at) VALUES($1, NOW()) ON CONFLICT (slug) DO NOTHING`, slug)
	}
	_, err := r.db.Exec(`DELETE FROM pages WHERE id=$1`, id)
	return err
}

func (r *PageRepository) GetBySlug(slug string) (*Page, error) {
	var p Page
	err := r.db.Get(&p, `SELECT * FROM pages WHERE slug=$1`, strings.ToLower(strings.TrimSpace(slug)))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PageRepository) GetPublishedBySlug(slug string) (*Page, error) {
	var p Page
	err := r.db.Get(&p, `SELECT * FROM pages WHERE slug=$1 AND is_published=true`, strings.ToLower(strings.TrimSpace(slug)))
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *PageRepository) ListAll(page, limit int) ([]Page, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	offset := (page - 1) * limit
	var total int
	if err := r.db.Get(&total, `SELECT COUNT(*) FROM pages`); err != nil {
		return nil, 0, err
	}
	var list []Page
	if err := r.db.Select(&list, `SELECT * FROM pages ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (r *PageRepository) ListPublished() ([]Page, error) {
	var list []Page
	if err := r.db.Select(&list, `SELECT * FROM pages WHERE is_published=true ORDER BY title ASC`); err != nil {
		return nil, err
	}
	return list, nil
}
