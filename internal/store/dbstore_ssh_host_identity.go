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
	ActorID            string
}

func newSSHHostIdentitySnapshot(ctx context.Context, host model.Host) sshHostIdentitySnapshot {
	return sshHostIdentitySnapshot{
		ID:                 host.ID,
		Address:            host.Address,
		Port:               host.Port,
		Protocol:           host.Protocol,
		Status:             host.Status,
		HostKeyFingerprint: host.HostKeyFingerprint,
		KnownHosts:         host.KnownHosts,
		UpdatedAt:          host.UpdatedAt,
		ActorID:            strings.TrimSpace(model.AuditUserIDFromContext(ctx)),
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

// RefreshHostIdentity accepts a freshly collected SSH identity only while the
// persisted endpoint still matches the endpoint that was scanned. Identity
// fields and lifecycle status are changed by one UPDATE so callers never
// observe an enabled host with stale or incomplete verification material.
func (s *DBStore) RefreshHostIdentity(ctx context.Context, id string, refresh HostIdentityRefresh) (HostView, error) {
	id = strings.TrimSpace(id)
	refresh.Address = strings.TrimSpace(refresh.Address)
	refresh.Protocol = normalizedHostProtocol(refresh.Protocol)
	refresh.Fingerprint = strings.TrimSpace(refresh.Fingerprint)
	refresh.KnownHosts = strings.TrimSpace(refresh.KnownHosts)
	if id == "" || refresh.Address == "" || refresh.Port <= 0 ||
		refresh.Protocol != "ssh" || strings.TrimSpace(refresh.Status) == "" || refresh.UpdatedAt.IsZero() ||
		refresh.Fingerprint == "" || refresh.KnownHosts == "" {
		return HostView{}, errors.New("invalid ssh host identity refresh")
	}

	var host model.Host
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"host_key_fingerprint": refresh.Fingerprint,
			"known_hosts":          refresh.KnownHosts,
			"status":               "active",
			"updated_at":           time.Now().UTC(),
		}
		if actorID := strings.TrimSpace(model.AuditUserIDFromContext(ctx)); actorID != "" {
			updates["updated_by"] = actorID
		}
		result := tx.Model(&model.Host{}).Scopes(ActiveScope).
			Where("id = ? AND address = ? AND port = ?", id, refresh.Address, refresh.Port).
			Where("LOWER(TRIM(protocol)) IN ?", []string{"", "ssh"}).
			Where("status = ? AND host_key_fingerprint = ? AND known_hosts = ? AND updated_at = ?",
				refresh.Status, refresh.PreviousFingerprint, refresh.PreviousKnownHosts, refresh.UpdatedAt).
			Updates(updates)
		if result.Error != nil {
			return fmt.Errorf("refresh ssh host identity: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			var current model.Host
			if err := tx.Scopes(ActiveScope).First(&current, "id = ?", id).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("%w: %q", ErrHostNotFound, id)
				}
				return fmt.Errorf("load ssh host identity state: %w", err)
			}
			if strings.EqualFold(strings.TrimSpace(current.Status), "active") &&
				strings.TrimSpace(current.Address) == refresh.Address &&
				current.Port == refresh.Port &&
				normalizedHostProtocol(current.Protocol) == refresh.Protocol &&
				strings.TrimSpace(current.HostKeyFingerprint) == refresh.Fingerprint &&
				strings.TrimSpace(current.KnownHosts) == refresh.KnownHosts {
				host = current
				return nil
			}
			return ErrHostIdentityStateChanged
		}
		if err := tx.Scopes(ActiveScope).First(&host, "id = ?", id).Error; err != nil {
			return fmt.Errorf("load refreshed ssh host identity: %w", err)
		}
		return nil
	})
	if err != nil {
		return HostView{}, err
	}
	return s.hostView(ctx, host), nil
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
	actorID := strings.TrimSpace(snapshot.ActorID)
	if actorID == "" {
		actorID = "system"
	}
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
		Updates(map[string]any{
			"status":     "disabled",
			"updated_at": time.Now().UTC(),
			"updated_by": actorID,
		})
	if result.Error != nil {
		return false, fmt.Errorf("disable host after ssh key change: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		return true, nil
	}
	var host model.Host
	if err := s.db.WithContext(ctx).Scopes(ActiveScope).First(&host, "id = ?", strings.TrimSpace(snapshot.ID)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, fmt.Errorf("%w: %q", ErrHostNotFound, snapshot.ID)
		}
		return false, fmt.Errorf("load host after ssh key change: %w", err)
	}
	return strings.EqualFold(strings.TrimSpace(host.Status), "disabled"), nil
}
