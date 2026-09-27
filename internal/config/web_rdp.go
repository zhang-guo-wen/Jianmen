package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
)

const (
	defaultGuacdAddress     = "127.0.0.1:4822"
	defaultRDPSpoolDir      = "data/rdp-spool"
	defaultRDPDriveDir      = "data/rdp-drive"
	defaultObjectStorageDir = "data/objects"
)

// WebRDPConfig configures the Go control-plane adapter around guacd. guacd may
// either be managed by Jianmen or supplied as an external service.
type WebRDPConfig struct {
	Enabled            bool               `json:"enabled"`
	GuacdAddress       string             `json:"guacd_address"`
	ManagedGuacd       ManagedGuacdConfig `json:"managed_guacd"`
	ConnectTimeoutSecs int                `json:"connect_timeout_seconds"`
	SpoolDir           string             `json:"spool_dir"`
	GuacdRecordingRoot string             `json:"guacd_recording_root"`
	LocalDriveRoot     string             `json:"local_drive_root"`
	GuacdDriveRoot     string             `json:"guacd_drive_root"`
	AllowUnrecorded    bool               `json:"allow_unrecorded"`
}

// ObjectStorageConfig configures the authoritative recording object store.
// The filesystem provider exists for local development; production should use
// the S3-compatible provider.
type ObjectStorageConfig struct {
	Provider         string `json:"provider"`
	LocalDir         string `json:"local_dir"`
	Endpoint         string `json:"endpoint"`
	AccessKeyID      string `json:"access_key_id"`
	SecretAccessKey  string `json:"secret_access_key"`
	SessionToken     string `json:"session_token"`
	Bucket           string `json:"bucket"`
	Region           string `json:"region"`
	Prefix           string `json:"prefix"`
	Secure           bool   `json:"secure"`
	PathStyle        bool   `json:"path_style"`
	AutoCreateBucket bool   `json:"auto_create_bucket"`
	secureSet        bool
}

func (c *ObjectStorageConfig) UnmarshalJSON(data []byte) error {
	type alias ObjectStorageConfig
	*c = ObjectStorageConfig{}
	value := struct {
		Secure *bool `json:"secure"`
		*alias
	}{
		alias: (*alias)(c),
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	if value.Secure != nil {
		c.Secure = *value.Secure
		c.secureSet = true
	}
	return nil
}

func (c *Config) applyWebRDPDefaults() {
	c.WebRDP.GuacdAddress = valueOrDefault(c.WebRDP.GuacdAddress, defaultGuacdAddress)
	c.WebRDP.ManagedGuacd.applyDefaults()
	if c.WebRDP.ConnectTimeoutSecs == 0 {
		c.WebRDP.ConnectTimeoutSecs = 15
	}
	c.WebRDP.SpoolDir = valueOrDefault(c.WebRDP.SpoolDir, defaultRDPSpoolDir)
	c.WebRDP.LocalDriveRoot = valueOrDefault(c.WebRDP.LocalDriveRoot, defaultRDPDriveDir)
	c.WebRDP.GuacdRecordingRoot = valueOrDefault(c.WebRDP.GuacdRecordingRoot, c.WebRDP.SpoolDir)
	c.WebRDP.GuacdDriveRoot = valueOrDefault(c.WebRDP.GuacdDriveRoot, c.WebRDP.LocalDriveRoot)

	c.ObjectStorage.Provider = strings.ToLower(valueOrDefault(c.ObjectStorage.Provider, "filesystem"))
	c.ObjectStorage.LocalDir = valueOrDefault(c.ObjectStorage.LocalDir, defaultObjectStorageDir)
	c.ObjectStorage.Prefix = strings.Trim(strings.TrimSpace(c.ObjectStorage.Prefix), "/")
	if c.ObjectStorage.Provider == "s3" && !c.ObjectStorage.secureSet {
		c.ObjectStorage.Secure = true
	}
}

func (c *Config) validateWebRDP() error {
	objectStorage := c.ObjectStorage
	if objectStorage == (ObjectStorageConfig{}) {
		objectStorage.Provider = "filesystem"
		objectStorage.LocalDir = defaultObjectStorageDir
	}
	switch objectStorage.Provider {
	case "filesystem":
		if strings.TrimSpace(objectStorage.LocalDir) == "" || filepath.Clean(objectStorage.LocalDir) == "." {
			return errors.New("object_storage.local_dir is required for filesystem provider")
		}
	case "s3":
		endpoint := strings.TrimSpace(objectStorage.Endpoint)
		if endpoint == "" {
			return errors.New("object_storage.endpoint is required for s3 provider")
		}
		if strings.Contains(endpoint, "://") || strings.ContainsAny(endpoint, "/?#") {
			return errors.New("object_storage.endpoint must be host[:port] without scheme or path")
		}
		if strings.TrimSpace(objectStorage.Bucket) == "" {
			return errors.New("object_storage.bucket is required for s3 provider")
		}
		if strings.TrimSpace(objectStorage.AccessKeyID) == "" || strings.TrimSpace(objectStorage.SecretAccessKey) == "" {
			return errors.New("object_storage access_key_id and secret_access_key are required for s3 provider")
		}
	default:
		return fmt.Errorf("object_storage.provider %q is not supported", objectStorage.Provider)
	}
	if !c.WebRDP.Enabled {
		return nil
	}
	if _, _, err := net.SplitHostPort(c.WebRDP.GuacdAddress); err != nil {
		return fmt.Errorf("invalid web_rdp.guacd_address %q: %w", c.WebRDP.GuacdAddress, err)
	}
	if c.WebRDP.ManagedGuacd.Enabled {
		if err := validateManagedGuacd(c.WebRDP); err != nil {
			return err
		}
	}
	if c.WebRDP.ConnectTimeoutSecs < 1 || c.WebRDP.ConnectTimeoutSecs > 300 {
		return errors.New("web_rdp.connect_timeout_seconds must be between 1 and 300")
	}
	for name, value := range map[string]string{
		"web_rdp.spool_dir":            c.WebRDP.SpoolDir,
		"web_rdp.guacd_recording_root": c.WebRDP.GuacdRecordingRoot,
		"web_rdp.local_drive_root":     c.WebRDP.LocalDriveRoot,
		"web_rdp.guacd_drive_root":     c.WebRDP.GuacdDriveRoot,
	} {
		if strings.TrimSpace(value) == "" || filepath.Clean(value) == "." {
			return fmt.Errorf("%s is required", name)
		}
	}
	return nil
}

func valueOrDefault(value, fallback string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return fallback
}
