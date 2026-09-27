package recording

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// ReplayStorage provides bounded access to replay files below a configured root.
type ReplayStorage struct {
	root string
}

// NewReplayStorage creates storage rooted at root. The root is not created.
func NewReplayStorage(root string) (*ReplayStorage, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("create replay storage: empty root")
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return nil, fmt.Errorf("create replay storage: resolve root: %w", err)
	}
	return &ReplayStorage{root: absRoot}, nil
}

// UsageBytes returns the size of all regular files below the replay root.
func (s *ReplayStorage) UsageBytes(ctx context.Context) (int64, error) {
	if err := contextErr(ctx); err != nil {
		return 0, fmt.Errorf("scan replay storage: %w", err)
	}
	var total int64
	err := filepath.WalkDir(s.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := contextErr(ctx); err != nil {
			return err
		}
		if walkErr != nil {
			return fmt.Errorf("inspect %q: %w", path, walkErr)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("stat %q: %w", path, err)
		}
		total += info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("scan replay storage: %w", err)
	}
	return total, nil
}

// DeleteSession deletes one protocol/session directory and returns its size before deletion.
func (s *ReplayStorage) DeleteSession(ctx context.Context, sessionDir string) (int64, error) {
	path, err := s.validateSessionPath(sessionDir)
	if err != nil {
		return 0, fmt.Errorf("delete replay session: %w", err)
	}
	if err := contextErr(ctx); err != nil {
		return 0, fmt.Errorf("delete replay session: %w", err)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("stat %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return 0, fmt.Errorf("session path is not a directory: %q", sessionDir)
	}
	bytes, err := directoryBytes(ctx, path)
	if err != nil {
		return 0, fmt.Errorf("measure %q: %w", path, err)
	}
	if err := removeDirectory(ctx, path); err != nil {
		return 0, fmt.Errorf("remove %q: %w", path, err)
	}
	return bytes, nil
}

func (s *ReplayStorage) validateSessionPath(sessionDir string) (string, error) {
	if strings.TrimSpace(sessionDir) == "" {
		return "", fmt.Errorf("empty session path")
	}
	absPath, err := filepath.Abs(filepath.Clean(sessionDir))
	if err != nil {
		return "", fmt.Errorf("resolve session path: %w", err)
	}
	rel, err := filepath.Rel(s.root, absPath)
	if err != nil || rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("session path outside replay root: %q", sessionDir)
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	if len(parts) != 2 || (parts[0] != "ssh" && parts[0] != "db") || parts[1] == "" || parts[1] == "." || parts[1] == ".." {
		return "", fmt.Errorf("session path must be root/<ssh|db>/<session>: %q", sessionDir)
	}
	for _, component := range []string{s.root, filepath.Join(s.root, parts[0])} {
		info, statErr := os.Lstat(component)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("session path traverses symbolic link: %q", component)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat %q: %w", component, statErr)
		}
	}
	return absPath, nil
}

func directoryBytes(ctx context.Context, root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := contextErr(ctx); err != nil {
			return err
		}
		if walkErr != nil {
			return fmt.Errorf("inspect %q: %w", path, walkErr)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symbolic link: %q", path)
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return fmt.Errorf("stat %q: %w", path, err)
			}
			total += info.Size()
		}
		return nil
	})
	return total, err
}

func removeDirectory(ctx context.Context, root string) error {
	var directories []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := contextErr(ctx); err != nil {
			return err
		}
		if walkErr != nil {
			return fmt.Errorf("inspect %q: %w", path, walkErr)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refuse symbolic link: %q", path)
		}
		if path == root {
			directories = append(directories, path)
			return nil
		}
		if entry.IsDir() {
			directories = append(directories, path)
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove file %q: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(directories) - 1; i >= 0; i-- {
		if err := os.Remove(directories[i]); err != nil {
			return fmt.Errorf("remove directory %q: %w", directories[i], err)
		}
	}
	return nil
}

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("nil context")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
