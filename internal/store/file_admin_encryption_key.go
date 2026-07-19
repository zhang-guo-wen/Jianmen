package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type FileAdminEncryptionKeyReader struct {
	path string
}

func NewFileAdminEncryptionKeyReader(dataDir string) (*FileAdminEncryptionKeyReader, error) {
	dataDir = strings.TrimSpace(dataDir)
	if dataDir == "" {
		return nil, errors.New("admin data directory is required")
	}
	return &FileAdminEncryptionKeyReader{path: filepath.Join(dataDir, "encryption.key")}, nil
}

func (r *FileAdminEncryptionKeyReader) ReadAdminEncryptionKey(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	key, err := os.ReadFile(r.path)
	if err != nil {
		return nil, fmt.Errorf("read admin encryption key: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return key, nil
}
