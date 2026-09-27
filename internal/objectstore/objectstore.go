package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	ProviderFilesystem = "filesystem"
	ProviderS3         = "s3"
)

var (
	ErrInvalidKey = errors.New("invalid object key")
	ErrNotFound   = errors.New("object not found")
)

type Config struct {
	Provider         string
	LocalDir         string
	Endpoint         string
	AccessKeyID      string
	SecretAccessKey  string
	SessionToken     string
	Bucket           string
	Region           string
	Prefix           string
	Secure           bool
	PathStyle        bool
	AutoCreateBucket bool
}

type Info struct {
	Key          string
	Size         int64
	ContentType  string
	ETag         string
	LastModified time.Time
}

type Reader interface {
	io.ReadSeeker
	io.Closer
}

type Store interface {
	Put(ctx context.Context, key string, src io.Reader, size int64, contentType string) (Info, error)
	Open(ctx context.Context, key string) (Reader, error)
	Stat(ctx context.Context, key string) (Info, error)
	Delete(ctx context.Context, key string) error
}

func New(ctx context.Context, cfg Config) (Store, error) {
	if err := contextError(ctx); err != nil {
		return nil, fmt.Errorf("initialize object store: %w", err)
	}
	prefix, err := normalizePrefix(cfg.Prefix)
	if err != nil {
		return nil, err
	}
	cfg.Prefix = prefix

	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case ProviderFilesystem:
		return newFilesystemStore(ctx, cfg)
	case ProviderS3:
		return newS3Store(ctx, cfg)
	case "":
		return nil, errors.New("object store provider is required")
	default:
		return nil, fmt.Errorf("object store provider %q is not supported", cfg.Provider)
	}
}

func normalizeKey(key string) (string, error) {
	if key == "" || strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("%w: key is required", ErrInvalidKey)
	}
	if strings.ContainsRune(key, '\x00') || strings.Contains(key, `\`) || strings.HasPrefix(key, "/") {
		return "", fmt.Errorf("%w: %q is not a safe relative path", ErrInvalidKey, key)
	}
	if filepath.IsAbs(key) || filepath.VolumeName(key) != "" {
		return "", fmt.Errorf("%w: %q is not a safe relative path", ErrInvalidKey, key)
	}
	cleaned := path.Clean(key)
	if cleaned == "." || cleaned != key || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%w: %q is not canonical", ErrInvalidKey, key)
	}
	for _, part := range strings.Split(cleaned, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("%w: %q contains an unsafe segment", ErrInvalidKey, key)
		}
	}
	return cleaned, nil
}

func normalizePrefix(prefix string) (string, error) {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return "", nil
	}
	normalized, err := normalizeKey(prefix)
	if err != nil {
		return "", fmt.Errorf("invalid object store prefix: %w", err)
	}
	return normalized, nil
}

func prefixedKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "/" + key
}

func normalizedContentType(contentType string) string {
	contentType = strings.TrimSpace(contentType)
	if contentType == "" {
		return "application/octet-stream"
	}
	return contentType
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return errors.New("context is required")
	}
	return ctx.Err()
}

type contextReader struct {
	ctx context.Context
	src io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := contextError(r.ctx); err != nil {
		return 0, err
	}
	return r.src.Read(p)
}
