package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ensureDirectoryPath(root, relative string) (string, error) {
	current := root
	if relative == "" {
		return current, nil
	}
	for _, segment := range strings.Split(relative, "/") {
		next := filepath.Join(current, segment)
		info, err := os.Lstat(next)
		switch {
		case errors.Is(err, os.ErrNotExist):
			if err := os.Mkdir(next, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return "", err
			}
			info, err = os.Lstat(next)
		case err != nil:
			return "", err
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("%w: directory %q is not a real directory", ErrInvalidKey, next)
		}
		current = next
	}
	return current, nil
}

func (s *filesystemStore) prepareTarget(key string) (string, error) {
	parts := strings.Split(key, "/")
	if len(parts) > 1 {
		if _, err := ensureDirectoryPath(s.root, strings.Join(parts[:len(parts)-1], "/")); err != nil {
			return "", err
		}
	}
	target, err := s.targetPath(key)
	if err != nil {
		return "", err
	}
	if err := rejectSymlink(target); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return target, nil
}

func (s *filesystemStore) existingTarget(key string) (string, error) {
	target, err := s.targetPath(key)
	if err != nil {
		return "", err
	}
	if err := checkParentDirectories(s.root, key); err != nil {
		return "", err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", mapFilesystemError(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: object %q is not a regular file", ErrInvalidKey, key)
	}
	return target, nil
}

func checkParentDirectories(root, key string) error {
	parts := strings.Split(key, "/")
	current := root
	for _, segment := range parts[:len(parts)-1] {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if err != nil {
			return mapFilesystemError(err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("%w: parent %q is not a real directory", ErrInvalidKey, segment)
		}
	}
	return nil
}

func (s *filesystemStore) targetPath(key string) (string, error) {
	target := filepath.Join(s.root, filepath.FromSlash(key))
	relative, err := filepath.Rel(s.root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", fmt.Errorf("%w: %q escapes the storage root", ErrInvalidKey, key)
	}
	return target, nil
}

func rejectSymlink(target string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%w: symbolic links are not allowed", ErrInvalidKey)
	}
	return nil
}

func stageObject(ctx context.Context, dir string, src io.Reader, expectedSize int64) (string, int64, string, error) {
	temp, err := os.CreateTemp(dir, ".object-*")
	if err != nil {
		return "", 0, "", err
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), contextReader{ctx: ctx, src: src})
	if err != nil {
		return "", 0, "", err
	}
	if expectedSize >= 0 && size != expectedSize {
		return "", 0, "", fmt.Errorf("source size is %d bytes, expected %d", size, expectedSize)
	}
	if err := temp.Sync(); err != nil {
		return "", 0, "", err
	}
	if err := temp.Close(); err != nil {
		return "", 0, "", err
	}
	keep = true
	return tempPath, size, hex.EncodeToString(hash.Sum(nil)), nil
}

func mapFilesystemError(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return errors.Join(ErrNotFound, err)
	}
	return err
}
