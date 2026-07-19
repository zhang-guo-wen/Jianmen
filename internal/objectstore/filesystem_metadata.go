package objectstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
)

func (s *filesystemStore) stageMetadata(key string, metadata filesystemMetadata) (string, string, error) {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "", "", err
	}
	target := s.metadataPath(key)
	temp, err := os.CreateTemp(s.metaRoot, ".metadata-*")
	if err != nil {
		return "", "", err
	}
	tempPath := temp.Name()
	keep := false
	defer func() {
		_ = temp.Close()
		if !keep {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.Write(raw); err != nil {
		return "", "", err
	}
	if err := temp.Sync(); err != nil {
		return "", "", err
	}
	if err := temp.Close(); err != nil {
		return "", "", err
	}
	keep = true
	return tempPath, target, nil
}

func (s *filesystemStore) metadataPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(s.metaRoot, hex.EncodeToString(sum[:])+".json")
}

func (s *filesystemStore) statLocked(key string) (Info, error) {
	target, err := s.existingTarget(key)
	if err != nil {
		return Info{}, fmt.Errorf("stat filesystem object %q: %w", key, err)
	}
	fileInfo, err := os.Stat(target)
	if err != nil {
		return Info{}, fmt.Errorf("stat filesystem object %q: %w", key, mapFilesystemError(err))
	}
	contentType := mime.TypeByExtension(filepath.Ext(target))
	etag := ""
	var metadata filesystemMetadata
	if raw, readErr := os.ReadFile(s.metadataPath(key)); readErr == nil &&
		json.Unmarshal(raw, &metadata) == nil &&
		metadata.Key == key {
		contentType = metadata.ContentType
		etag = metadata.ETag
	}
	return Info{
		Key:          key,
		Size:         fileInfo.Size(),
		ContentType:  normalizedContentType(contentType),
		ETag:         etag,
		LastModified: fileInfo.ModTime().UTC(),
	}, nil
}
