package models

import (
	"time"

	"github.com/jmoiron/sqlx"
)

type SiteSettings struct {
	ID                        int    `db:"id" json:"id"`
	SiteName                  string `db:"site_name" json:"site_name"`
	SiteURL                   string `db:"site_url" json:"site_url"`
	SEOTitle                  string `db:"seo_title" json:"seo_title"`
	SEODescription            string `db:"seo_description" json:"seo_description"`
	SocialImageURL            string `db:"social_image_url" json:"social_image_url"`
	SMTPHost                  string `db:"smtp_host" json:"smtp_host"`
	SMTPPort                  int    `db:"smtp_port" json:"smtp_port"`
	SMTPUsername              string `db:"smtp_username" json:"smtp_username"`
	SMTPPassword              string `db:"smtp_password" json:"smtp_password"`
	SMTPFromEmail             string `db:"smtp_from_email" json:"smtp_from_email"`
	SMTPTLS                   bool   `db:"smtp_tls" json:"smtp_tls"`
	FaviconPath               string `db:"favicon_path" json:"favicon_path"`
	RequireEmailVerification  bool   `db:"require_email_verification" json:"require_email_verification"`
	PublicRegistrationEnabled bool   `db:"public_registration_enabled" json:"public_registration_enabled"`
	// Storage configuration (optional). When empty or provider=="local", use local filesystem under /uploads.
	StorageProvider  string    `db:"storage_provider" json:"storage_provider"`
	S3Endpoint       string    `db:"s3_endpoint" json:"s3_endpoint"`
	S3Bucket         string    `db:"s3_bucket" json:"s3_bucket"`
	S3AccessKey      string    `db:"s3_access_key" json:"s3_access_key"`
	S3SecretKey      string    `db:"s3_secret_key" json:"s3_secret_key"`
	S3ForcePathStyle bool      `db:"s3_force_path_style" json:"s3_force_path_style"`
	PublicBaseURL    string    `db:"public_base_url" json:"public_base_url"`
	UpdatedAt        time.Time `db:"updated_at" json:"updated_at"`
	// Analytics / tracking configuration
	AnalyticsEnabled  bool   `db:"analytics_enabled" json:"analytics_enabled"`
	AnalyticsProvider string `db:"analytics_provider" json:"analytics_provider"`
	GA4MeasurementID  string `db:"ga4_measurement_id" json:"ga4_measurement_id"`
	UmamiSrc          string `db:"umami_src" json:"umami_src"`
	UmamiWebsiteID    string `db:"umami_website_id" json:"umami_website_id"`
	PlausibleSrc      string `db:"plausible_src" json:"plausible_src"`
	PlausibleDomain   string `db:"plausible_domain" json:"plausible_domain"`
	// Backups
	BackupEnabled  bool   `db:"backup_enabled" json:"backup_enabled"`
	BackupInterval string `db:"backup_interval" json:"backup_interval"`
	BackupKeepDays int    `db:"backup_keep_days" json:"backup_keep_days"`
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
		// Safe defaults when no settings row exists yet
		return &SiteSettings{ID: 1, SiteName: "TROUGH", PublicRegistrationEnabled: true, BackupInterval: "24h", BackupKeepDays: 7}, nil
	}
	return &s, nil
}

func (r *SiteSettingsRepository) Upsert(s *SiteSettings) error {
	_, err := r.db.Exec(`
        INSERT INTO site_settings (
            id, site_name, site_url, seo_title, seo_description, social_image_url,
            smtp_host, smtp_port, smtp_username, smtp_password, smtp_from_email, smtp_tls,
            require_email_verification, public_registration_enabled, storage_provider, s3_endpoint, s3_bucket,
            s3_access_key, s3_secret_key, s3_force_path_style, public_base_url,
            analytics_enabled, analytics_provider, ga4_measurement_id, umami_src, umami_website_id,
            plausible_src, plausible_domain,
            backup_enabled, backup_interval, backup_keep_days,
            updated_at
        ) VALUES (
            1, $1, $2, $3, $4, $5,
            $6, $7, $8, $9, $10, $11,
            $12, $13, $14, $15, $16,
            $17, $18, $19, $20,
            $21, $22, $23, $24, $25,
            $26, $27,
            $28, $29, $30,
            NOW()
        )
        ON CONFLICT (id) DO UPDATE SET
            site_name = EXCLUDED.site_name,
            site_url = EXCLUDED.site_url,
            seo_title = EXCLUDED.seo_title,
            seo_description = EXCLUDED.seo_description,
            social_image_url = EXCLUDED.social_image_url,
            smtp_host = EXCLUDED.smtp_host,
            smtp_port = EXCLUDED.smtp_port,
            smtp_username = EXCLUDED.smtp_username,
            smtp_password = EXCLUDED.smtp_password,
            smtp_from_email = EXCLUDED.smtp_from_email,
            smtp_tls = EXCLUDED.smtp_tls,
            require_email_verification = EXCLUDED.require_email_verification,
            public_registration_enabled = EXCLUDED.public_registration_enabled,
            storage_provider = EXCLUDED.storage_provider,
            s3_endpoint = EXCLUDED.s3_endpoint,
            s3_bucket = EXCLUDED.s3_bucket,
            s3_access_key = EXCLUDED.s3_access_key,
            s3_secret_key = EXCLUDED.s3_secret_key,
            s3_force_path_style = EXCLUDED.s3_force_path_style,
            public_base_url = EXCLUDED.public_base_url,
            analytics_enabled = EXCLUDED.analytics_enabled,
            analytics_provider = EXCLUDED.analytics_provider,
            ga4_measurement_id = EXCLUDED.ga4_measurement_id,
            umami_src = EXCLUDED.umami_src,
            umami_website_id = EXCLUDED.umami_website_id,
            plausible_src = EXCLUDED.plausible_src,
            plausible_domain = EXCLUDED.plausible_domain,
            backup_enabled = EXCLUDED.backup_enabled,
            backup_interval = EXCLUDED.backup_interval,
            backup_keep_days = EXCLUDED.backup_keep_days,
            updated_at = NOW()
    `,
		s.SiteName, s.SiteURL, s.SEOTitle, s.SEODescription, s.SocialImageURL,
		s.SMTPHost, s.SMTPPort, s.SMTPUsername, s.SMTPPassword, s.SMTPFromEmail, s.SMTPTLS,
		s.RequireEmailVerification, s.PublicRegistrationEnabled, s.StorageProvider, s.S3Endpoint, s.S3Bucket,
		s.S3AccessKey, s.S3SecretKey, s.S3ForcePathStyle, s.PublicBaseURL,
		s.AnalyticsEnabled, s.AnalyticsProvider, s.GA4MeasurementID, s.UmamiSrc, s.UmamiWebsiteID,
		s.PlausibleSrc, s.PlausibleDomain,
		s.BackupEnabled, s.BackupInterval, s.BackupKeepDays,
	)
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
func (s SiteSettings) GetSMTPHost() string      { return s.SMTPHost }
func (s SiteSettings) GetSMTPPort() int         { return s.SMTPPort }
func (s SiteSettings) GetSMTPUsername() string  { return s.SMTPUsername }
func (s SiteSettings) GetSMTPPassword() string  { return s.SMTPPassword }
func (s SiteSettings) GetSMTPFromEmail() string { return s.SMTPFromEmail }
func (s SiteSettings) GetSMTPTLS() bool         { return s.SMTPTLS }

// Storage getters
func (s SiteSettings) GetStorageProvider() string { return s.StorageProvider }
func (s SiteSettings) GetS3Endpoint() string      { return s.S3Endpoint }
func (s SiteSettings) GetS3Bucket() string        { return s.S3Bucket }
func (s SiteSettings) GetS3AccessKey() string     { return s.S3AccessKey }
func (s SiteSettings) GetS3SecretKey() string     { return s.S3SecretKey }
func (s SiteSettings) GetS3ForcePathStyle() bool  { return s.S3ForcePathStyle }
func (s SiteSettings) GetPublicBaseURL() string   { return s.PublicBaseURL }
