package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/model"
	"jianmen/internal/sshhost"
)

type sshHostIdentitySnapshot struct {
	ID                 string
	Address            string
	Port               int
	Protocol           string
	Status             string
	HostKeyFingerprint string
	KnownHosts         string
	UpdatedAt          time.Time
}

func newSSHHostIdentitySnapshot(host model.Host) sshHostIdentitySnapshot {
	return sshHostIdentitySnapshot{
		ID:                 host.ID,
		Address:            host.Address,
		Port:               host.Port,
		Protocol:           host.Protocol,
		Status:             host.Status,
		HostKeyFingerprint: host.HostKeyFingerprint,
		KnownHosts:         host.KnownHosts,
		UpdatedAt:          host.UpdatedAt,
	}
}

func effectiveHostIdentity(account model.HostAccount) (fingerprint, knownHosts string) {
	fingerprint = strings.TrimSpace(account.Host.HostKeyFingerprint)
	knownHosts = strings.TrimSpace(account.Host.KnownHosts)
	if fingerprint == "" && knownHosts == "" {
		fingerprint = strings.TrimSpace(account.HostKeyFingerprint)
	}
	return fingerprint, knownHosts
}

func hostIdentityStatus(host model.Host) string {
	if normalizedHostProtocol(host.Protocol) != "ssh" {
		return "not_applicable"
	}
	if strings.TrimSpace(host.HostKeyFingerprint) == "" || strings.TrimSpace(host.KnownHosts) == "" {
		return "unavailable"
	}
	return "available"
}

func (s *DBStore) hostKeyChangeHandler(snapshot sshHostIdentitySnapshot) sshhost.ChangeHandler {
	hostID := strings.TrimSpace(snapshot.ID)
	return func(change sshhost.Change) (bool, error) {
		if strings.TrimSpace(change.HostID) != hostID {
			return false, errors.New("ssh host identity change references an unexpected host")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.disableHostForKeyChange(ctx, snapshot)
	}
}

// disableHostForKeyChange changes only the complete host snapshot that built
// the SSH verifier. Endpoint, identity, lifecycle, or timestamp changes make a
// stale callback harmless.
func (s *DBStore) disableHostForKeyChange(ctx context.Context, snapshot sshHostIdentitySnapshot) (bool, error) {
	result := s.db.WithContext(ctx).Model(&model.Host{}).Scopes(ActiveScope).
		Where(
			"id = ? AND address = ? AND port = ? AND protocol = ? AND status = ?",
			snapshot.ID, snapshot.Address, snapshot.Port, snapshot.Protocol, snapshot.Status,
		).
		Where(
			"host_key_fingerprint = ? AND known_hosts = ? AND updated_at = ?",
			snapshot.HostKeyFingerprint, snapshot.KnownHosts, snapshot.UpdatedAt,
		).
		Where("status <> ?", "disabled").
		Update("status", "disabled")
	if result.Error != nil {
		return false, fmt.Errorf("disable host after ssh key change: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	var host model.Host
	if err := s.db.WithContext(ctx).First(&host, "id = ?", strings.TrimSpace(snapshot.ID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, fmt.Errorf("%w: %q", ErrHostNotFound, snapshot.ID)
		}
		return false, fmt.Errorf("load host after ssh key change: %w", err)
	}
	return strings.EqualFold(strings.TrimSpace(host.Status), "disabled"), nil
}
