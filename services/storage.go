package services

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Storage defines a minimal interface for saving and deleting public assets
// such as uploaded images, avatars, and site assets.
type Storage interface {
	// Save stores the content under key (relative path, e.g. "avatars/file.jpg").
	// Returns a public URL for accessing the content.
	Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error)
	// Delete removes the object at key. Should not error if the object does not exist.
	Delete(ctx context.Context, key string) error
	// PublicURL builds a public URL for a given key.
	PublicURL(key string) string
	// IsLocal indicates whether this storage writes to local filesystem.
	IsLocal() bool
}

// ----- Local storage implementation -----

type LocalStorage struct {
	baseDir    string // e.g. "uploads"
	publicBase string // e.g. "/uploads"
}

func NewLocalStorage(baseDir string) *LocalStorage {
	if baseDir == "" {
		baseDir = "uploads"
	}
	return &LocalStorage{baseDir: baseDir, publicBase: "/uploads"}
}

func (s *LocalStorage) Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error) {
	// Normalize separators
	key = filepath.ToSlash(key)
	dstPath := filepath.Join(s.baseDir, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return "", err
	}
	f, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
	// Return public URL using the local static mount
	return s.PublicURL(key), nil
}

func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	key = filepath.ToSlash(key)
	path := filepath.Join(s.baseDir, filepath.FromSlash(key))
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return nil
}

func (s *LocalStorage) PublicURL(key string) string {
	key = strings.TrimPrefix(filepath.ToSlash(key), "/")
	return s.publicBase + "/" + key
}

func (s *LocalStorage) IsLocal() bool { return true }

// ----- S3 (R2-compatible) configuration placeholders -----

type S3Config struct {
	Endpoint       string
	AccessKey      string
	SecretKey      string
	UseSSL         bool
	Bucket         string
	ForcePathStyle bool
	PublicBaseURL  string
}

// buildS3Storage is optionally provided by an s3-enabled build (see storage_s3.go).
var buildS3Storage func(S3Config) (Storage, error)

// NewStorageFromSettings builds a Storage from site settings and/or environment variables.
// Precedence: SiteSettings if storage_provider is set; otherwise environment variables.
type StorageSettings interface {
	GetStorageProvider() string
	GetS3Endpoint() string
	GetS3Bucket() string
	GetS3AccessKey() string
	GetS3SecretKey() string
	GetS3ForcePathStyle() bool
	GetPublicBaseURL() string
}

func NewStorageFromSettings(s StorageSettings) (Storage, error) {
	provider := s.GetStorageProvider()
	if provider == "" {
		provider = os.Getenv("STORAGE_PROVIDER")
	}
	if strings.EqualFold(provider, "s3") || strings.EqualFold(provider, "r2") {
		cfg := S3Config{
			Endpoint:       firstNonEmpty(s.GetS3Endpoint(), os.Getenv("S3_ENDPOINT"), os.Getenv("R2_ENDPOINT")),
			AccessKey:      firstNonEmpty(s.GetS3AccessKey(), os.Getenv("S3_ACCESS_KEY_ID"), os.Getenv("R2_ACCESS_KEY_ID")),
			SecretKey:      firstNonEmpty(s.GetS3SecretKey(), os.Getenv("S3_SECRET_ACCESS_KEY"), os.Getenv("R2_SECRET_ACCESS_KEY")),
			UseSSL:         true,
			Bucket:         firstNonEmpty(s.GetS3Bucket(), os.Getenv("S3_BUCKET"), os.Getenv("R2_BUCKET")),
			ForcePathStyle: s.GetS3ForcePathStyle(),
			PublicBaseURL:  firstNonEmpty(s.GetPublicBaseURL(), os.Getenv("STORAGE_PUBLIC_BASE_URL")),
		}
		if cfg.ForcePathStyle == false {
			cfg.ForcePathStyle = true
		}
		if buildS3Storage != nil {
			if st, err := buildS3Storage(cfg); err == nil {
				return st, nil
			}
		}
	}
	// default local
	baseDir := os.Getenv("UPLOADS_DIR")
	if baseDir == "" {
		baseDir = "uploads"
	}
	return NewLocalStorage(baseDir), nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Global storage registry for dynamic reconfiguration
var (
	storageMu      sync.RWMutex
	currentStorage Storage
)

func SetCurrentStorage(s Storage) {
	storageMu.Lock()
	defer storageMu.Unlock()
	currentStorage = s
}

func GetCurrentStorage() Storage {
	storageMu.RLock()
	defer storageMu.RUnlock()
	return currentStorage
}
