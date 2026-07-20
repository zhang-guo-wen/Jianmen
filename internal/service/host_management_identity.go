package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var ErrHostIdentityRefreshFailed = errors.New("ssh host identity refresh failed")

type HostIdentity struct {
	Fingerprint string
	KnownHosts  string
}

type HostIdentityCollector interface {
	Collect(context.Context, string, int) (HostIdentity, error)
}

type HostIdentityRefreshError struct {
	HostID         string
	HostStatus     string
	IdentityStatus string
	Cause          error
}

type HostIdentityUnavailableError struct {
	HostID string
}

func (e *HostIdentityUnavailableError) Error() string {
	if e == nil || strings.TrimSpace(e.HostID) == "" {
		return "ssh host identity is unavailable"
	}
	return fmt.Sprintf("ssh host identity is unavailable for host %q", e.HostID)
}

func (e *HostIdentityRefreshError) Error() string {
	if e == nil || strings.TrimSpace(e.HostID) == "" {
		return ErrHostIdentityRefreshFailed.Error()
	}
	return fmt.Sprintf("%s for host %q", ErrHostIdentityRefreshFailed, e.HostID)
}

func (e *HostIdentityRefreshError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *HostIdentityRefreshError) Is(target error) bool {
	return target == ErrHostIdentityRefreshFailed
}

func normalizedManagementHostRecord(record HostManagementHostRecord) HostManagementHostRecord {
	record.Protocol = strings.ToLower(strings.TrimSpace(record.Protocol))
	if record.Protocol == "" {
		record.Protocol = "ssh"
	}
	if record.Port == 0 {
		record.Port = 22
		if record.Protocol == "rdp" {
			record.Port = 3389
		}
	}
	record.Status = normalizedManagementHostStatus(record.Status)
	return record
}

func normalizedManagementHostStatus(status string) string {
	if strings.EqualFold(strings.TrimSpace(status), "disabled") {
		return "disabled"
	}
	return "active"
}

func hostEndpointChanged(current HostManagementHostView, next HostManagementHostRecord) bool {
	return !strings.EqualFold(strings.TrimSpace(current.Protocol), strings.TrimSpace(next.Protocol)) ||
		strings.TrimSpace(current.Address) != strings.TrimSpace(next.Address) ||
		current.Port != next.Port
}

func applyHostIdentity(record *HostManagementHostRecord, identity HostIdentity) {
	record.HostKeyFingerprint = strings.TrimSpace(identity.Fingerprint)
	record.KnownHosts = strings.TrimSpace(identity.KnownHosts)
}

func validateHostIdentity(identity HostIdentity) error {
	if strings.TrimSpace(identity.Fingerprint) == "" || strings.TrimSpace(identity.KnownHosts) == "" {
		return errors.New("ssh host identity is incomplete")
	}
	return nil
}

func clearHostIdentity(record *HostManagementHostRecord) {
	record.HostKeyFingerprint = ""
	record.KnownHosts = ""
}
