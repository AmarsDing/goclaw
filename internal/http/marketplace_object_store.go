package http

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
)

type marketplaceArtifactStore interface {
	UploadFile(ctx context.Context, key, localPath string, size int64) (string, error)
}

type s3MarketplaceArtifactStore struct {
	client   *backup.S3Client
	endpoint string
	bucket   string
	region   string
	prefix   string
}

func newMarketplaceArtifactStoreFromEnv() (marketplaceArtifactStore, error) {
	accessKey := strings.TrimSpace(os.Getenv("GOCLAW_MARKETPLACE_S3_ACCESS_KEY_ID"))
	secretKey := strings.TrimSpace(os.Getenv("GOCLAW_MARKETPLACE_S3_SECRET_ACCESS_KEY"))
	bucket := strings.TrimSpace(os.Getenv("GOCLAW_MARKETPLACE_S3_BUCKET"))
	if accessKey == "" || secretKey == "" || bucket == "" {
		return nil, nil
	}
	cfg := &backup.S3Config{
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		Bucket:          bucket,
		Region:          strings.TrimSpace(os.Getenv("GOCLAW_MARKETPLACE_S3_REGION")),
		Endpoint:        strings.TrimSpace(os.Getenv("GOCLAW_MARKETPLACE_S3_ENDPOINT")),
		Prefix:          strings.TrimSpace(os.Getenv("GOCLAW_MARKETPLACE_S3_PREFIX")),
	}
	client, err := backup.NewS3Client(cfg)
	if err != nil {
		return nil, err
	}
	return &s3MarketplaceArtifactStore{
		client:   client,
		endpoint: cfg.Endpoint,
		bucket:   cfg.Bucket,
		region:   cfg.Region,
		prefix:   strings.Trim(cfg.Prefix, "/"),
	}, nil
}

func (s *s3MarketplaceArtifactStore) UploadFile(ctx context.Context, key, localPath string, size int64) (string, error) {
	f, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := s.client.Upload(ctx, key, f, size); err != nil {
		return "", err
	}
	return s.objectURI(key), nil
}

func (s *s3MarketplaceArtifactStore) objectURI(key string) string {
	fullKey := strings.TrimPrefix(filepath.ToSlash(filepath.Join(s.prefix, key)), "/")
	if ep := strings.TrimSpace(s.endpoint); ep != "" {
		parsed, err := url.Parse(ep)
		if err == nil {
			return strings.TrimRight(parsed.String(), "/") + "/" + s.bucket + "/" + fullKey
		}
		return fmt.Sprintf("%s/%s/%s", strings.TrimRight(ep, "/"), s.bucket, fullKey)
	}
	if s.region == "" {
		return fmt.Sprintf("s3://%s/%s", s.bucket, fullKey)
	}
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", s.bucket, s.region, fullKey)
}
