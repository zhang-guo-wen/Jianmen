package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const filesystemMetadataDir = ".jianmen-objectstore"

type filesystemStore struct {
	root     string
	metaRoot string
	mu       sync.RWMutex
}

type filesystemMetadata struct {
	Key         string `json:"key"`
	ContentType string `json:"content_type"`
	ETag        string `json:"etag"`
}

func newFilesystemStore(ctx context.Context, cfg Config) (Store, error) {
	if strings.TrimSpace(cfg.LocalDir) == "" {
		return nil, errors.New("filesystem object store local directory is required")
	}
	if hasReservedFilesystemSegment(cfg.Prefix) {
		return nil, fmt.Errorf(
			"%w: filesystem object store prefix %q conflicts with reserved directory %q",
			ErrInvalidKey,
			cfg.Prefix,
			filesystemMetadataDir,
		)
	}
	if err := contextError(ctx); err != nil {
		return nil, fmt.Errorf("initialize filesystem object store: %w", err)
	}
	base, err := filepath.Abs(filepath.Clean(cfg.LocalDir))
	if err != nil {
		return nil, fmt.Errorf("resolve filesystem object store directory: %w", err)
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, fmt.Errorf("create filesystem object store directory: %w", err)
	}
	base, err = filepath.EvalSymlinks(base)
	if err != nil {
		return nil, fmt.Errorf("resolve filesystem object store symlinks: %w", err)
	}
	root, err := ensureDirectoryPath(base, cfg.Prefix)
	if err != nil {
		return nil, fmt.Errorf("create filesystem object store prefix: %w", err)
	}
	metaRoot, err := ensureDirectoryPath(base, filesystemMetadataDir)
	if err != nil {
		return nil, fmt.Errorf("create filesystem object metadata directory: %w", err)
	}
	if cfg.Prefix != "" {
		metaRoot, err = ensureDirectoryPath(metaRoot, cfg.Prefix)
		if err != nil {
			return nil, fmt.Errorf("create filesystem object metadata prefix: %w", err)
		}
	}
	return &filesystemStore{root: root, metaRoot: metaRoot}, nil
}

func (s *filesystemStore) Put(ctx context.Context, key string, src io.Reader, expectedSize int64, contentType string) (Info, error) {
	key, err := normalizeKey(key)
	if err != nil {
		return Info{}, err
	}
	if err := validateFilesystemKey(key); err != nil {
		return Info{}, err
	}
	if src == nil {
		return Info{}, errors.New("put filesystem object: source is required")
	}
	if expectedSize < -1 {
		return Info{}, errors.New("put filesystem object: size must be -1 or greater")
	}
	if err := contextError(ctx); err != nil {
		return Info{}, fmt.Errorf("put filesystem object %q: %w", key, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	target, err := s.prepareTarget(key)
	if err != nil {
		return Info{}, fmt.Errorf("put filesystem object %q: %w", key, err)
	}
	temp, size, etag, err := stageObject(ctx, filepath.Dir(target), src, expectedSize)
	if err != nil {
		return Info{}, fmt.Errorf("put filesystem object %q: %w", key, err)
	}
	defer os.Remove(temp)

	metadata := filesystemMetadata{Key: key, ContentType: normalizedContentType(contentType), ETag: etag}
	metaTemp, metaTarget, err := s.stageMetadata(key, metadata)
	if err != nil {
		return Info{}, fmt.Errorf("put filesystem object metadata %q: %w", key, err)
	}
	defer os.Remove(metaTemp)

	if err := replaceFile(temp, target); err != nil {
		return Info{}, fmt.Errorf("commit filesystem object %q: %w", key, err)
	}
	if err := replaceFile(metaTemp, metaTarget); err != nil {
		return Info{}, fmt.Errorf("commit filesystem object metadata %q: %w", key, err)
	}
	info, err := s.statLocked(key)
	if err != nil {
		return Info{}, err
	}
	info.Size = size
	return info, nil
}

func (s *filesystemStore) Open(ctx context.Context, key string) (Reader, error) {
	key, err := normalizeKey(key)
	if err != nil {
		return nil, err
	}
	if err := validateFilesystemKey(key); err != nil {
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		return nil, fmt.Errorf("open filesystem object %q: %w", key, err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, err := s.existingTarget(key)
	if err != nil {
		return nil, fmt.Errorf("open filesystem object %q: %w", key, err)
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, mapFilesystemError(err)
	}
	return file, nil
}

func (s *filesystemStore) Stat(ctx context.Context, key string) (Info, error) {
	key, err := normalizeKey(key)
	if err != nil {
		return Info{}, err
	}
	if err := validateFilesystemKey(key); err != nil {
		return Info{}, err
	}
	if err := contextError(ctx); err != nil {
		return Info{}, fmt.Errorf("stat filesystem object %q: %w", key, err)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statLocked(key)
}

func (s *filesystemStore) Delete(ctx context.Context, key string) error {
	key, err := normalizeKey(key)
	if err != nil {
		return err
	}
	if err := validateFilesystemKey(key); err != nil {
		return err
	}
	if err := contextError(ctx); err != nil {
		return fmt.Errorf("delete filesystem object %q: %w", key, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	target, err := s.targetPath(key)
	if err != nil {
		return fmt.Errorf("delete filesystem object %q: %w", key, err)
	}
	if err := checkParentDirectories(s.root, key); err != nil {
		if errors.Is(err, ErrNotFound) {
			return s.removeMetadata(key)
		}
		return fmt.Errorf("delete filesystem object %q: %w", key, err)
	}
	if err := rejectSymlink(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete filesystem object %q: %w", key, err)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete filesystem object %q: %w", key, err)
	}
	return s.removeMetadata(key)
}

func validateFilesystemKey(key string) error {
	if hasReservedFilesystemSegment(key) {
		return fmt.Errorf(
			"%w: key %q conflicts with reserved directory %q",
			ErrInvalidKey,
			key,
			filesystemMetadataDir,
		)
	}
	return nil
}

func hasReservedFilesystemSegment(value string) bool {
	first, _, _ := strings.Cut(value, "/")
	return strings.EqualFold(first, filesystemMetadataDir)
}

func (s *filesystemStore) removeMetadata(key string) error {
	if err := os.Remove(s.metadataPath(key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete filesystem object metadata %q: %w", key, err)
	}
	return nil
}
