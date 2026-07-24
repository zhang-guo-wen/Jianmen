package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

func (s *DBStore) targetView(ctx context.Context, tx *gorm.DB, a model.HostAccount) TargetView {
	status := "disabled"
	if storedResourceStatusActive(a.Status) {
		status = "enabled"
	}
	hostStatus := strings.TrimSpace(a.Host.Status)
	if hostStatus == "" {
		hostStatus = "active"
	}
	authMethods := []string{"password"}
	if a.AuthType == "private_key" || a.AuthType == "key" {
		authMethods = []string{"private_key"}
	}
	name := strings.TrimSpace(a.Name)
	if name == "" {
		name = strings.TrimSpace(a.Username)
	}
	if name == "" {
		name = a.ID
	}
	host, port := s.hostAddressPort(ctx, tx, a.Host, a.HostID)
	hostKeyFingerprint, _ := effectiveHostIdentity(a)
	protocol := strings.ToLower(strings.TrimSpace(a.Host.Protocol))
	expiresAt := ""
	if a.ExpiresAt != nil {
		expiresAt = a.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return TargetView{
		ID: a.ID, HostID: a.HostID,
		ResourceType: model.ResourceTypeHostAccount, ResourceID: a.ResourceID,
		ResourceSeq: a.ResourceSeq,
		Name:        name, Group: a.GroupName, Remark: a.Remark, ExpiresAt: expiresAt,
		LifecycleStatus: strings.ToLower(strings.TrimSpace(a.Status)),
		HostStatus:      hostStatus,
		Host:            host, Port: port, Protocol: protocol,
		Username: a.Username, Domain: a.Domain, Status: status,
		AuthMethods:           authMethods,
		InsecureIgnoreHostKey: false,
		HostKeyFingerprint:    hostKeyFingerprint,
		KnownHostsPath:        "",
		RDPSecurity:           normalizedRDPSecurity(a.RDPSecurity),
		RDPIgnoreCertificate:  a.RDPIgnoreCertificate,
		RDPCertFingerprints:   a.RDPCertFingerprints,
		RDPClipboardRead:      a.RDPClipboardRead,
		RDPClipboardWrite:     a.RDPClipboardWrite,
		RDPFileUpload:         a.RDPFileUpload,
		RDPFileDownload:       a.RDPFileDownload,
		RDPDriveMapping:       a.RDPDriveMapping,
	}
}

func (s *DBStore) hostAddressPort(ctx context.Context, tx *gorm.DB, h model.Host, hostID string) (host string, port int) {
	host = h.Address
	port = h.Port
	if host == "" && hostID != "" {
		var loaded model.Host
		if err := queryWithContext(s.db, tx, ctx).Scopes(ActiveScope).First(&loaded, "id = ?", hostID).Error; err == nil {
			host = loaded.Address
			port = loaded.Port
		}
	}
	return
}

func (s *DBStore) targetConfig(ctx context.Context, tx *gorm.DB, a model.HostAccount) TargetConfig {
	host, port := s.hostAddressPort(ctx, tx, a.Host, a.HostID)
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 22
	}
	disabled := a.Status == "disabled" || a.Host.Status == "disabled"
	expiresAt := ""
	if a.ExpiresAt != nil {
		expiresAt = a.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	accountName := strings.TrimSpace(a.Name)
	if accountName == "" {
		accountName = a.Username
	}
	hostKeyFingerprint, knownHosts := effectiveHostIdentity(a)
	return TargetConfig{
		ID: a.ID, Username: a.Username,
		Name:                  accountName,
		HostName:              strings.TrimSpace(a.Host.Name),
		Host:                  host,
		Port:                  port,
		Protocol:              normalizedHostProtocol(a.Host.Protocol),
		Domain:                a.Domain,
		Password:              a.Password.GetPlaintext(),
		PrivateKeyPEM:         a.PrivateKeyPEM.GetPlaintext(),
		Passphrase:            a.Passphrase.GetPlaintext(),
		InsecureIgnoreHostKey: false,
		HostKeyFingerprint:    hostKeyFingerprint,
		KnownHosts:            knownHosts,
		KnownHostsPath:        "",
		HostKeyChangeHandler:  s.hostKeyChangeHandler(newSSHHostIdentitySnapshot(a.Host)),
		RDPSecurity:           normalizedRDPSecurity(a.RDPSecurity),
		RDPIgnoreCertificate:  a.RDPIgnoreCertificate,
		RDPCertFingerprints:   a.RDPCertFingerprints,
		RDPClipboardRead:      a.RDPClipboardRead,
		RDPClipboardWrite:     a.RDPClipboardWrite,
		RDPFileUpload:         a.RDPFileUpload,
		RDPFileDownload:       a.RDPFileDownload,
		RDPDriveMapping:       a.RDPDriveMapping,
		HostID:                a.HostID,
		Disabled:              disabled,
		ExpiresAt:             expiresAt,
	}
}

// effectiveHostIdentity uses host-level identity first. The legacy account
// fingerprint is accepted only as a strict verifier while an old host row has
// no identity; the legacy insecure flag and known_hosts file path are ignored.
func queryWithContext(db *gorm.DB, tx *gorm.DB, ctx context.Context) *gorm.DB {
	if tx != nil {
		return tx
	}
	return db.WithContext(ctx)
}

func (s *DBStore) ListHostAccounts(ctx context.Context, hostID string) ([]TargetView, error) {
	var accounts []model.HostAccount
	if err := s.db.WithContext(ctx).Scopes(activeHostAccountScope).Preload("Host").
		Where("host_accounts.host_id = ?", hostID).
		Order("host_accounts.username ASC").
		Find(&accounts).Error; err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	out := make([]TargetView, len(accounts))
	for i := range accounts {
		out[i] = s.targetView(ctx, nil, accounts[i])
	}
	return out, nil
}

func (s *DBStore) Targets(ctx context.Context) ([]TargetView, error) {
	var accounts []model.HostAccount
	if err := s.db.WithContext(ctx).Scopes(activeHostAccountScope).Preload("Host").
		Order("host_accounts.created_at DESC").
		Find(&accounts).Error; err != nil {
		return nil, err
	}
	out := make([]TargetView, len(accounts))
	for i := range accounts {
		out[i] = s.targetView(ctx, nil, accounts[i])
	}
	return out, nil
}

func (s *DBStore) Target(ctx context.Context, id string) (TargetView, error) {
	var a model.HostAccount
	if err := s.db.WithContext(ctx).Scopes(activeHostAccountScope).Preload("Host").
		First(&a, "host_accounts.id = ?", id).Error; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return TargetView{}, err
		}
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return s.targetView(ctx, nil, a), nil
}

func (s *DBStore) TargetConfig(ctx context.Context, id string) (TargetConfig, error) {
	var a model.HostAccount
	if err := s.db.WithContext(ctx).Scopes(activeHostAccountScope).Preload("Host").
		First(&a, "host_accounts.id = ?", id).Error; err != nil {
		return TargetConfig{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return s.targetConfig(ctx, nil, a), nil
}

func (s *DBStore) AddTarget(ctx context.Context, target config.Target) (TargetView, error) {
	target = normalizeConfigTarget(target)
	if err := validateRDPSecurity(target.RDPSecurity); err != nil {
		return TargetView{}, err
	}
	if target.Protocol == "rdp" {
		if err := validateNewRDPAccount(target); err != nil {
			return TargetView{}, err
		}
	}
	if target.HostID == "" {
		target.HostID = fmt.Sprintf("%s-%d", target.Host, target.Port)
	}
	if target.Username == "" {
		return TargetView{}, errors.New("username is required")
	}
	expiresAt, err := parseTargetExpiry(target.ExpiresAt)
	if err != nil {
		return TargetView{}, err
	}
	a := model.HostAccount{
		ID:                    target.ID,
		HostID:                target.HostID,
		Name:                  target.Name,
		Username:              target.Username,
		Domain:                target.Domain,
		AuthType:              "password",
		Password:              model.NewEncryptedField(target.Password),
		PrivateKeyPEM:         model.NewEncryptedField(target.PrivateKeyPEM),
		Passphrase:            model.NewEncryptedField(target.Passphrase),
		InsecureIgnoreHostKey: false,
		HostKeyFingerprint:    target.HostKeyFingerprint,
		KnownHostsPath:        target.KnownHostsPath,
		RDPSecurity:           normalizedRDPSecurity(target.RDPSecurity),
		RDPIgnoreCertificate:  target.RDPIgnoreCertificate,
		RDPCertFingerprints:   target.RDPCertFingerprints,
		RDPClipboardRead:      target.RDPClipboardRead,
		RDPClipboardWrite:     target.RDPClipboardWrite,
		RDPFileUpload:         target.RDPFileUpload,
		RDPFileDownload:       target.RDPFileDownload,
		RDPDriveMapping:       target.RDPDriveMapping,
		GroupName:             target.Group,
		Remark:                target.Remark,
		ExpiresAt:             expiresAt,
	}
	if target.PrivateKeyPEM != "" || target.PrivateKeyPath != "" {
		a.AuthType = "private_key"
		if target.PrivateKeyPath != "" && target.PrivateKeyPEM == "" {
			if pem, err := os.ReadFile(target.PrivateKeyPath); err == nil {
				a.PrivateKeyPEM = model.NewEncryptedField(string(pem))
			}
		}
	}
	if target.Disabled {
		a.Status = "disabled"
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		targetHost, createHost, err := s.lockTargetHost(tx, target.HostID, target.Host, target.Protocol, target.Port)
		if err != nil {
			return fmt.Errorf("load target host: %w", err)
		}
		target.Protocol = normalizedHostProtocol(targetHost.Protocol)
		if target.Protocol == "rdp" {
			if err := validateNewRDPAccount(target); err != nil {
				return err
			}
		}
		if err := s.ensureHost(tx, &targetHost, createHost); err != nil {
			return fmt.Errorf("ensure host: %w", err)
		}
		if err := s.ensureHostAccountUsernameAvailable(tx, target.HostID, target.Username, ""); err != nil {
			return err
		}
		seq, err := s.nextHostResourceSeq(tx)
		if err != nil {
			return err
		}
		a.ResourceSeq = seq
		a.ResourceID = util.ResourceIDFromSeq(util.PrefixHost, seq)
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		if err := ensureAccountGroup(tx, target.Group); err != nil {
			return fmt.Errorf("create target group: %w", err)
		}
		a.Host = targetHost
		if err := s.syncResourceTx(tx, model.ResourceTypeHostAccount, a.ID, hostAccountResourceName(a), a.HostID); err != nil {
			return fmt.Errorf("sync target resource: %w", err)
		}
		return nil
	}); err != nil {
		return TargetView{}, fmt.Errorf("create target: %w", err)
	}
	return s.targetView(ctx, nil, a), nil
}

func (s *DBStore) UpdateTarget(ctx context.Context, id string, target config.Target) (TargetView, error) {
	target = normalizeConfigTargetUpdate(target)
	if err := validateRDPSecurity(target.RDPSecurity); err != nil {
		return TargetView{}, err
	}
	var a model.HostAccount
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Scopes(activeHostAccountScope).First(&a, "host_accounts.id = ?", id).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrTargetNotFound, id)
		}
		if target.HostID != "" && target.HostID != a.HostID {
			return errors.New("host_id cannot be changed through target update")
		}
		var targetHost model.Host
		if err := tx.Scopes(ActiveScope).First(&targetHost, "id = ?", a.HostID).Error; err != nil {
			return fmt.Errorf("load target host: %w", err)
		}
		protocol := normalizedHostProtocol(targetHost.Protocol)
		if protocol == "rdp" {
			if target.PrivateKeyPEM != "" || target.PrivateKeyPath != "" || target.Passphrase != "" {
				return errors.New("RDP accounts only support password authentication")
			}
			if target.Password == "" && a.Password.GetPlaintext() == "" {
				return errors.New("RDP account password is required")
			}
			if err := validateRDPFilePolicy(target); err != nil {
				return err
			}
			a.AuthType = "password"
			a.PrivateKeyPEM = model.EncryptedField{}
			a.Passphrase = model.EncryptedField{}
		}
		if target.Username == "" {
			return errors.New("username is required")
		}
		if err := s.ensureHostAccountUsernameAvailable(tx, a.HostID, target.Username, a.ID); err != nil {
			return err
		}

		a.Username = target.Username
		a.Domain = target.Domain
		a.Name = target.Name
		if a.Name == "" {
			a.Name = a.Username
		}
		a.GroupName = target.Group
		a.Remark = target.Remark
		a.InsecureIgnoreHostKey = false
		a.HostKeyFingerprint = target.HostKeyFingerprint
		a.KnownHostsPath = target.KnownHostsPath
		a.RDPSecurity = normalizedRDPSecurity(target.RDPSecurity)
		a.RDPIgnoreCertificate = target.RDPIgnoreCertificate
		a.RDPCertFingerprints = target.RDPCertFingerprints
		a.RDPClipboardRead = target.RDPClipboardRead
		a.RDPClipboardWrite = target.RDPClipboardWrite
		a.RDPFileUpload = target.RDPFileUpload
		a.RDPFileDownload = target.RDPFileDownload
		a.RDPDriveMapping = target.RDPDriveMapping

		expiresAt, err := parseTargetExpiry(target.ExpiresAt)
		if err != nil {
			return err
		}
		a.ExpiresAt = expiresAt

		if target.Password != "" {
			a.AuthType = "password"
			a.Password = model.NewEncryptedField(target.Password)
		}
		if target.PrivateKeyPEM != "" {
			a.AuthType = "private_key"
			a.PrivateKeyPEM = model.NewEncryptedField(target.PrivateKeyPEM)
		}
		if target.Passphrase != "" {
			a.Passphrase = model.NewEncryptedField(target.Passphrase)
		}
		if target.PrivateKeyPath != "" {
			if pem, err := os.ReadFile(target.PrivateKeyPath); err == nil {
				if pemStr := string(pem); pemStr != "" {
					a.AuthType = "private_key"
					a.PrivateKeyPEM = model.NewEncryptedField(pemStr)
				}
			}
		}
		a.Status = "active"
		if target.Disabled {
			a.Status = "disabled"
		}

		if err := ensureAccountGroup(tx, target.Group); err != nil {
			return fmt.Errorf("update target: %w", err)
		}

		if err := tx.Save(&a).Error; err != nil {
			return fmt.Errorf("update target: %w", err)
		}
		if err := s.syncResourceTx(tx, model.ResourceTypeHostAccount, a.ID, hostAccountResourceName(a), a.HostID); err != nil {
			return fmt.Errorf("sync target resource: %w", err)
		}
		if err := tx.Scopes(activeHostAccountScope).Preload("Host").
			First(&a, "host_accounts.id = ?", id).Error; err != nil {
			return err
		}
		return nil
	}); err != nil {
		return TargetView{}, err
	}
	return s.targetView(ctx, nil, a), nil
}

func (s *DBStore) DeleteTarget(ctx context.Context, id string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account model.HostAccount
		if err := tx.Scopes(ActiveScope).First(&account, "id = ?", id).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrTargetNotFound, id)
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeHostAccount, account.ID); err != nil {
			return err
		}
		return SoftDelete(ctx, tx, "host_accounts", account.ID)
	})
}

func (s *DBStore) DefaultTarget(ctx context.Context, user model.User) (TargetConfig, error) {
	now := time.Now().UTC()
	if user.RequestedTargetID != "" {
		var a model.HostAccount
		if err := s.db.WithContext(ctx).Scopes(activeHostAccountScope).Preload("Host").
			Where("host_accounts.id = ? AND host_accounts.status = ?", user.RequestedTargetID, "active").
			Where("host_accounts.expires_at IS NULL OR host_accounts.expires_at > ?", now).
			First(&a).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return TargetConfig{}, fmt.Errorf("%w: target %q is not available", ErrTargetUnavailable, user.RequestedTargetID)
			}
			return TargetConfig{}, fmt.Errorf("target %q not found", user.RequestedTargetID)
		}
		if a.Host.Status == "disabled" {
			return TargetConfig{}, fmt.Errorf("%w: host %q is disabled", ErrTargetUnavailable, a.HostID)
		}
		if normalizedHostProtocol(a.Host.Protocol) != "ssh" {
			return TargetConfig{}, fmt.Errorf("%w: target %q is not an SSH account", ErrTargetUnavailable, user.RequestedTargetID)
		}
		return s.targetConfig(ctx, nil, a), nil
	}

	var account model.HostAccount
	if err := s.db.WithContext(ctx).Preload("Host").
		Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
		Where("host_accounts.active_marker = ?", model.ActiveMarkerValue).
		Where("hosts.active_marker = ?", model.ActiveMarkerValue).
		Where("host_accounts.status = ?", "active").
		Where("host_accounts.expires_at IS NULL OR host_accounts.expires_at > ?", now).
		Where("hosts.status IS NULL OR hosts.status <> ?", "disabled").
		Where("LOWER(COALESCE(hosts.protocol, 'ssh')) = ?", "ssh").
		Order("host_accounts.created_at ASC").
		First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TargetConfig{}, errors.New("no target accounts available")
		}
		return TargetConfig{}, fmt.Errorf("find default target: %w", err)
	}
	return s.targetConfig(ctx, nil, account), nil
}
