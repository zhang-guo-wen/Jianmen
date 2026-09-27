package main

import (
	"flag"
	"log/slog"

	"jianmen/internal/config"
)

type runtimeOptions struct {
	configPath    string
	disableWebRDP bool
}

func parseRuntimeOptions() runtimeOptions {
	configPath := flag.String("config", "config.local.json", "path to config file")
	disableWebRDP := flag.Bool(
		"disable-web-rdp",
		false,
		"disable Web RDP and managed guacd for this process",
	)
	flag.Parse()
	return runtimeOptions{
		configPath:    *configPath,
		disableWebRDP: *disableWebRDP,
	}
}

func applyRuntimeOptions(cfg *config.Config, options runtimeOptions, logger *slog.Logger) {
	if !options.disableWebRDP {
		return
	}
	cfg.WebRDP.Enabled = false
	cfg.WebRDP.ManagedGuacd.Enabled = false
	logger.Info("Web RDP disabled by runtime option")
}
