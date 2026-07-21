//go:build !embedded_guacd

package main

import (
	"errors"
	"path/filepath"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/guacdruntime"
)

func TestLiteBuildRejectsEmbeddedGuacdConfiguration(t *testing.T) {
	cfg := &config.Config{
		WebRDP: config.WebRDPConfig{
			Enabled:      true,
			GuacdAddress: "127.0.0.1:4822",
			SpoolDir:     filepath.Join(t.TempDir(), "rdp-spool"),
			ManagedGuacd: config.ManagedGuacdConfig{
				Enabled:            true,
				BinaryPath:         guacdruntime.EmbeddedBinaryPath,
				StartupTimeoutSecs: 15,
			},
		},
	}

	_, err := managedGuacdProcessConfig(cfg)
	if !errors.Is(err, guacdruntime.ErrNotIncluded) {
		t.Fatalf("managedGuacdProcessConfig() error = %v, want ErrNotIncluded", err)
	}
}
