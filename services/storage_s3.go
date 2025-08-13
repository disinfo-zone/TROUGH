package services

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// s3Storage is an implementation of Storage backed by S3-compatible services (incl. R2)
type s3Storage struct {
	client        *minio.Client
	bucket        string
	publicBaseURL string
	forcePath     bool
}

func buildS3StorageImpl(cfg S3Config) (Storage, error) {
	if cfg.Endpoint == "" || cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.Bucket == "" {
		return nil, fmt.Errorf("incomplete S3 config")
	}
	endpoint := cfg.Endpoint
	useSSL := cfg.UseSSL
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, err
		}
		endpoint = u.Host
		useSSL = (u.Scheme == "https")
	}
	cli, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: useSSL,
		Region: "auto",
		BucketLookup: func() minio.BucketLookupType {
			if cfg.ForcePathStyle {
				return minio.BucketLookupPath
			}
			return minio.BucketLookupAuto
		}(),
	})
	if err != nil {
		return nil, err
	}
	return &s3Storage{client: cli, bucket: cfg.Bucket, publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/"), forcePath: cfg.ForcePathStyle}, nil
}

func (s *s3Storage) Save(ctx context.Context, key string, r io.Reader, contentType string) (string, error) {
	key = strings.TrimPrefix(key, "/")
	// Bound network time for save operations
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		ctx = c
	}
	var size int64 = -1
	if br, ok := r.(*bytes.Reader); ok {
		size = int64(br.Len())
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: "public, max-age=31536000, immutable",
	})
	if err != nil {
		return "", err
	}
	return s.PublicURL(key), nil
}

func (s *s3Storage) Delete(ctx context.Context, key string) error {
	key = strings.TrimPrefix(key, "/")
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		c, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		ctx = c
	}
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

func (s *s3Storage) PublicURL(key string) string {
	key = strings.TrimPrefix(key, "/")
	if s.publicBaseURL != "" {
		baseURL := s.publicBaseURL
		// Ensure the base URL has a protocol
		if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
			baseURL = "https://" + baseURL
		}
		return baseURL + "/" + key
	}
	u := url.URL{}
	u.Scheme = "https"
	if s.forcePath {
		u.Host = s.client.EndpointURL().Host
		u.Path = "/" + s.bucket + "/" + key
	} else {
		u.Host = s.bucket + "." + s.client.EndpointURL().Host
		u.Path = "/" + key
	}
	return u.String()
}

func (s *s3Storage) IsLocal() bool { return false }

// Wire function pointer used by storage.go
func init() {
	buildS3Storage = func(cfg S3Config) (Storage, error) { return buildS3StorageImpl(cfg) }
}
