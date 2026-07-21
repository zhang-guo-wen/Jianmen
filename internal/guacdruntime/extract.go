package guacdruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func ensureExtracted(target string, archive []byte, digest string) error {
	readyPath := filepath.Join(target, readyFileName)
	if ready, err := os.ReadFile(readyPath); err == nil && strings.TrimSpace(string(ready)) == digest {
		return nil
	}

	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("create embedded guacd runtime parent: %w", err)
	}
	if err := removeIncompleteTarget(parent, target); err != nil {
		return err
	}
	temporary, err := os.MkdirTemp(parent, ".guacd-extract-")
	if err != nil {
		return fmt.Errorf("create embedded guacd extraction directory: %w", err)
	}
	defer os.RemoveAll(temporary)

	rootfs := filepath.Join(temporary, "rootfs")
	if err := os.MkdirAll(rootfs, 0o750); err != nil {
		return fmt.Errorf("create embedded guacd rootfs: %w", err)
	}
	if err := extractTarGzip(rootfs, archive); err != nil {
		return fmt.Errorf("extract embedded guacd runtime: %w", err)
	}
	if err := os.WriteFile(filepath.Join(temporary, readyFileName), []byte(digest+"\n"), 0o600); err != nil {
		return fmt.Errorf("write embedded guacd ready marker: %w", err)
	}
	if err := os.Rename(temporary, target); err != nil {
		if ready, readErr := os.ReadFile(readyPath); readErr == nil && strings.TrimSpace(string(ready)) == digest {
			return nil
		}
		return fmt.Errorf("activate embedded guacd runtime: %w", err)
	}
	return nil
}

func removeIncompleteTarget(parent, target string) error {
	relative, err := filepath.Rel(parent, target)
	if err != nil || relative == "." || relative == ".." || strings.Contains(relative, string(filepath.Separator)) {
		return fmt.Errorf("refusing unsafe embedded guacd target %q", target)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("remove incomplete embedded guacd runtime: %w", err)
	}
	return nil
}

func extractTarGzip(root string, archive []byte) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, nextErr := tarReader.Next()
		if nextErr == io.EOF {
			return nil
		}
		if nextErr != nil {
			return nextErr
		}
		destination, pathErr := archivePath(root, header.Name)
		if pathErr != nil {
			return pathErr
		}
		if destination == root {
			continue
		}
		if err := extractEntry(root, destination, header, tarReader); err != nil {
			return fmt.Errorf("extract %q: %w", header.Name, err)
		}
	}
}

func archivePath(root, name string) (string, error) {
	cleaned := path.Clean(strings.TrimPrefix(name, "./"))
	if cleaned == "." {
		return root, nil
	}
	if path.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("archive path escapes runtime root: %q", name)
	}
	return filepath.Join(root, filepath.FromSlash(cleaned)), nil
}

func extractEntry(root, destination string, header *tar.Header, reader io.Reader) error {
	mode := header.FileInfo().Mode().Perm()
	switch header.Typeflag {
	case tar.TypeDir:
		return os.MkdirAll(destination, mode|0o700)
	case tar.TypeReg, tar.TypeRegA:
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return err
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode|0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	case tar.TypeSymlink:
		return extractSymlink(root, destination, header.Linkname)
	case tar.TypeLink:
		target, err := archivePath(root, header.Linkname)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
			return err
		}
		return os.Link(target, destination)
	default:
		return nil
	}
}

func extractSymlink(root, destination, linkName string) error {
	if path.IsAbs(linkName) {
		target := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path.Clean(linkName), "/")))
		relative, err := filepath.Rel(filepath.Dir(destination), target)
		if err != nil {
			return fmt.Errorf("rewrite absolute symbolic link %q: %w", linkName, err)
		}
		linkName = filepath.ToSlash(relative)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(destination), filepath.FromSlash(linkName)))
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("symbolic link escapes runtime root: %q", linkName)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	return os.Symlink(filepath.FromSlash(linkName), destination)
}
