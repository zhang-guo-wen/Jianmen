package config

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
)

const (
	defaultManagedGuacdBinaryPath     = "tools/guacd/1.6.0/sbin/guacd"
	defaultManagedGuacdStartupTimeout = 15
)

// ManagedGuacdConfig configures a guacd process started and supervised by
// Jianmen. Process arguments are constructed by Jianmen to keep guacd in the
// foreground and bound to the configured loopback address.
type ManagedGuacdConfig struct {
	Enabled            bool   `json:"enabled"`
	BinaryPath         string `json:"binary_path"`
	WorkDir            string `json:"work_dir"`
	StartupTimeoutSecs int    `json:"startup_timeout_seconds"`
}

func (c *ManagedGuacdConfig) applyDefaults() {
	c.BinaryPath = valueOrDefault(c.BinaryPath, defaultManagedGuacdBinaryPath)
	c.WorkDir = strings.TrimSpace(c.WorkDir)
	if c.StartupTimeoutSecs == 0 {
		c.StartupTimeoutSecs = defaultManagedGuacdStartupTimeout
	}
}

func validateManagedGuacd(webRDP WebRDPConfig) error {
	host, _, _ := net.SplitHostPort(webRDP.GuacdAddress)
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return errors.New("web_rdp.guacd_address must be loopback when managed_guacd is enabled")
	}

	managed := webRDP.ManagedGuacd
	if err := validateManagedGuacdPath("binary_path", managed.BinaryPath, true); err != nil {
		return err
	}
	if err := validateManagedGuacdPath("work_dir", managed.WorkDir, false); err != nil {
		return err
	}
	if managed.StartupTimeoutSecs < 1 || managed.StartupTimeoutSecs > 300 {
		return errors.New("web_rdp.managed_guacd.startup_timeout_seconds must be between 1 and 300")
	}
	return nil
}

func validateManagedGuacdPath(name, value string, required bool) error {
	if strings.TrimSpace(value) == "" {
		if required {
			return fmt.Errorf("web_rdp.managed_guacd.%s is required", name)
		}
		return nil
	}
	if strings.ContainsRune(value, '\x00') {
		return fmt.Errorf("web_rdp.managed_guacd.%s contains an invalid null character", name)
	}
	if required && filepath.Clean(value) == "." {
		return fmt.Errorf("web_rdp.managed_guacd.%s must identify a file", name)
	}
	return nil
}
