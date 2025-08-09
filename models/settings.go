package models

import (
	"time"

	"github.com/jmoiron/sqlx"
)

type SiteSettings struct {
	ID                       int       `db:"id" json:"id"`
	SiteName                 string    `db:"site_name" json:"site_name"`
	SiteURL                  string    `db:"site_url" json:"site_url"`
	SEOTitle                 string    `db:"seo_title" json:"seo_title"`
	SEODescription           string    `db:"seo_description" json:"seo_description"`
	SocialImageURL           string    `db:"social_image_url" json:"social_image_url"`
	SMTPHost                 string    `db:"smtp_host" json:"smtp_host"`
	SMTPPort                 int       `db:"smtp_port" json:"smtp_port"`
	SMTPUsername             string    `db:"smtp_username" json:"smtp_username"`
	SMTPPassword             string    `db:"smtp_password" json:"smtp_password"`
	SMTPTLS                  bool      `db:"smtp_tls" json:"smtp_tls"`
	FaviconPath              string    `db:"favicon_path" json:"favicon_path"`
	RequireEmailVerification bool      `db:"require_email_verification" json:"require_email_verification"`
	UpdatedAt                time.Time `db:"updated_at" json:"updated_at"`
}

type SiteSettingsRepository struct{ db *sqlx.DB }

func NewSiteSettingsRepository(db *sqlx.DB) *SiteSettingsRepository {
	return &SiteSettingsRepository{db: db}
}

type SiteSettingsRepositoryInterface interface {
	Get() (*SiteSettings, error)
	Upsert(*SiteSettings) error
	UpdateFavicon(path string) error
	UpdateSocialImageURL(path string) error
}

func (r *SiteSettingsRepository) Get() (*SiteSettings, error) {
	var s SiteSettings
	err := r.db.Get(&s, `SELECT * FROM site_settings WHERE id = 1`)
	if err != nil {
		return &SiteSettings{ID: 1, SiteName: "TROUGH"}, nil
	}
	return &s, nil
}

func (r *SiteSettingsRepository) Upsert(s *SiteSettings) error {
	_, err := r.db.Exec(`UPDATE site_settings SET site_name=$1, site_url=$2, seo_title=$3, seo_description=$4, social_image_url=$5, smtp_host=$6, smtp_port=$7, smtp_username=$8, smtp_password=$9, smtp_tls=$10, require_email_verification=$11, updated_at=NOW() WHERE id=1`,
		s.SiteName, s.SiteURL, s.SEOTitle, s.SEODescription, s.SocialImageURL, s.SMTPHost, s.SMTPPort, s.SMTPUsername, s.SMTPPassword, s.SMTPTLS, s.RequireEmailVerification)
	return err
}

func (r *SiteSettingsRepository) UpdateFavicon(path string) error {
	_, err := r.db.Exec(`UPDATE site_settings SET favicon_path=$1, updated_at=NOW() WHERE id=1`, path)
	return err
}

func (r *SiteSettingsRepository) UpdateSocialImageURL(path string) error {
	_, err := r.db.Exec(`UPDATE site_settings SET social_image_url=$1, updated_at=NOW() WHERE id=1`, path)
	return err
}

// SMTP getters to satisfy services.ConfigOrSettings
func (s SiteSettings) GetSMTPHost() string     { return s.SMTPHost }
func (s SiteSettings) GetSMTPPort() int        { return s.SMTPPort }
func (s SiteSettings) GetSMTPUsername() string { return s.SMTPUsername }
func (s SiteSettings) GetSMTPPassword() string { return s.SMTPPassword }
func (s SiteSettings) GetSMTPTLS() bool        { return s.SMTPTLS }
