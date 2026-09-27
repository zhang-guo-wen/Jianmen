package main

import (
	"io"
	"log/slog"
	"testing"

	"jianmen/internal/config"
)

func TestApplyRuntimeOptionsDisablesWebRDP(t *testing.T) {
	cfg := &config.Config{
		WebRDP: config.WebRDPConfig{
			Enabled: true,
			ManagedGuacd: config.ManagedGuacdConfig{
				Enabled: true,
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	applyRuntimeOptions(cfg, runtimeOptions{disableWebRDP: true}, logger)

	if cfg.WebRDP.Enabled || cfg.WebRDP.ManagedGuacd.Enabled {
		t.Fatalf("Web RDP runtime override was not applied: %#v", cfg.WebRDP)
	}
}

func TestApplyRuntimeOptionsPreservesWebRDPByDefault(t *testing.T) {
	cfg := &config.Config{
		WebRDP: config.WebRDPConfig{
			Enabled: true,
			ManagedGuacd: config.ManagedGuacdConfig{
				Enabled: true,
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	applyRuntimeOptions(cfg, runtimeOptions{}, logger)

	if !cfg.WebRDP.Enabled || !cfg.WebRDP.ManagedGuacd.Enabled {
		t.Fatalf("default runtime options changed Web RDP: %#v", cfg.WebRDP)
	}
}
