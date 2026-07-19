package store

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"jianmen/internal/config"
)

func normalizeConfigTarget(t config.Target) config.Target {
	t.ID = strings.TrimSpace(t.ID)
	t.Name = strings.TrimSpace(t.Name)
	t.HostID = strings.TrimSpace(t.HostID)
	t.Protocol = normalizedHostProtocol(t.Protocol)
	t.Group = strings.TrimSpace(t.Group)
	t.Remark = strings.TrimSpace(t.Remark)
	t.Host = strings.TrimSpace(t.Host)
	t.Username = strings.TrimSpace(t.Username)
	t.Domain = strings.TrimSpace(t.Domain)
	t.Password = strings.TrimSpace(t.Password)
	t.PrivateKeyPEM = strings.TrimSpace(t.PrivateKeyPEM)
	t.PrivateKeyPath = strings.TrimSpace(t.PrivateKeyPath)
	t.HostKeyFingerprint = strings.TrimSpace(t.HostKeyFingerprint)
	t.KnownHostsPath = strings.TrimSpace(t.KnownHostsPath)
	t.RDPSecurity = normalizedRDPSecurity(t.RDPSecurity)
	t.RDPCertFingerprints = strings.TrimSpace(t.RDPCertFingerprints)
	if t.Port == 0 {
		t.Port = defaultHostPort(t.Protocol)
	}
	if t.Name == "" {
		t.Name = t.Username
	}
	return t
}

func normalizeConfigTargetUpdate(t config.Target) config.Target {
	t.ID = strings.TrimSpace(t.ID)
	t.Name = strings.TrimSpace(t.Name)
	t.HostID = strings.TrimSpace(t.HostID)
	t.Protocol = strings.ToLower(strings.TrimSpace(t.Protocol))
	t.Group = strings.TrimSpace(t.Group)
	t.Remark = strings.TrimSpace(t.Remark)
	t.Host = strings.TrimSpace(t.Host)
	t.Username = strings.TrimSpace(t.Username)
	t.Domain = strings.TrimSpace(t.Domain)
	t.Password = strings.TrimSpace(t.Password)
	t.PrivateKeyPEM = strings.TrimSpace(t.PrivateKeyPEM)
	t.PrivateKeyPath = strings.TrimSpace(t.PrivateKeyPath)
	t.HostKeyFingerprint = strings.TrimSpace(t.HostKeyFingerprint)
	t.KnownHostsPath = strings.TrimSpace(t.KnownHostsPath)
	t.RDPSecurity = normalizedRDPSecurity(t.RDPSecurity)
	t.RDPCertFingerprints = strings.TrimSpace(t.RDPCertFingerprints)
	return t
}

func normalizedRDPSecurity(security string) string {
	security = strings.ToLower(strings.TrimSpace(security))
	if security == "" {
		return "any"
	}
	return security
}

func validateRDPSecurity(security string) error {
	switch normalizedRDPSecurity(security) {
	case "any", "nla", "nla-ext", "tls", "vmconnect", "rdp":
		return nil
	default:
		return errors.New("rdp_security must be one of any, nla, nla-ext, tls, vmconnect, rdp")
	}
}

func validateNewRDPAccount(target config.Target) error {
	if target.Password == "" {
		return errors.New("RDP account password is required")
	}
	if target.PrivateKeyPEM != "" || target.PrivateKeyPath != "" || target.Passphrase != "" {
		return errors.New("RDP accounts only support password authentication")
	}
	return validateRDPFilePolicy(target)
}

func validateRDPFilePolicy(target config.Target) error {
	if (target.RDPFileUpload || target.RDPFileDownload) && !target.RDPDriveMapping {
		return errors.New("RDP file transfer requires drive mapping")
	}
	return nil
}

func parseTargetExpiry(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("invalid expires_at: %w", err)
	}
	expiresAt = expiresAt.UTC()
	return &expiresAt, nil
}
