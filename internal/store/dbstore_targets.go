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
	status := "enabled"
	if a.Status == "disabled" {
		status = "disabled"
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
	protocol := normalizedHostProtocol(a.Host.Protocol)
	expiresAt := ""
	if a.ExpiresAt != nil {
		expiresAt = a.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return TargetView{
		ID: a.ID, HostID: a.HostID,
		ResourceType: model.ResourceTypeHostAccount, ResourceID: a.ResourceID,
		ResourceSeq: a.ResourceSeq,
		Name:        name, Group: a.GroupName, Remark: a.Remark, ExpiresAt: expiresAt,
		Host: host, Port: port, Protocol: protocol,
		Username: a.Username, Domain: a.Domain, Status: status,
		AuthMethods:           authMethods,
		InsecureIgnoreHostKey: a.InsecureIgnoreHostKey,
		HostKeyFingerprint:    a.HostKeyFingerprint,
		KnownHostsPath:        a.KnownHostsPath,
		RDPSecurity:           normalizedRDPSecurity(a.RDPSecurity),
		RDPIgnoreCertificate:  a.RDPIgnoreCertificate,
		RDPCertFingerprints:   a.RDPCertFingerprints,
		RDPApprovalRequired:   a.RDPApprovalRequired,
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
		if err := queryWithContext(s.db, tx, ctx).First(&loaded, "id = ?", hostID).Error; err == nil {
			host = loaded.Address
			port = loaded.Port
		}
	}
	return
}

func (s *DBStore) ensureHost(ctx context.Context, tx *gorm.DB, hostID, address string, protocol string, port int) error {
	db := queryWithContext(s.db, tx, ctx)
	var existing model.Host
	if err := db.First(&existing, "id = ?", hostID).Error; err == nil {
		return s.syncResourceTx(db, model.ResourceTypeHost, existing.ID, hostResourceName(existing), "")
	}
	name := address
	if port != 0 && port != 22 {
		name = fmt.Sprintf("%s:%d", address, port)
	}
	if name == "" {
		name = hostID
	}
	host := model.Host{
		ID:       hostID,
		Name:     name,
		Address:  address,
		Port:     port,
		Protocol: normalizedHostProtocol(protocol),
	}
	return db.Transaction(func(txLocal *gorm.DB) error {
		if err := txLocal.Create(&host).Error; err != nil {
			return err
		}
		return s.syncResourceTx(txLocal, model.ResourceTypeHost, host.ID, hostResourceName(host), "")
	})
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
		InsecureIgnoreHostKey: a.InsecureIgnoreHostKey,
		HostKeyFingerprint:    a.HostKeyFingerprint,
		KnownHostsPath:        a.KnownHostsPath,
		RDPSecurity:           normalizedRDPSecurity(a.RDPSecurity),
		RDPIgnoreCertificate:  a.RDPIgnoreCertificate,
		RDPCertFingerprints:   a.RDPCertFingerprints,
		RDPApprovalRequired:   a.RDPApprovalRequired,
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

func queryWithContext(db *gorm.DB, tx *gorm.DB, ctx context.Context) *gorm.DB {
	if tx != nil {
		return tx
	}
	return db.WithContext(ctx)
}

func (s *DBStore) ListHostAccounts(ctx context.Context, hostID string) ([]TargetView, error) {
	var accounts []model.HostAccount
	if err := s.db.WithContext(ctx).Preload("Host").Where("host_id = ?", hostID).Order("username ASC").Find(&accounts).Error; err != nil {
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
	if err := s.db.WithContext(ctx).Preload("Host").Order("created_at DESC").Find(&accounts).Error; err != nil {
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
	if err := s.db.WithContext(ctx).Preload("Host").First(&a, "id = ?", id).Error; err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return TargetView{}, err
		}
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return s.targetView(ctx, nil, a), nil
}

func (s *DBStore) TargetConfig(ctx context.Context, id string) (TargetConfig, error) {
	var a model.HostAccount
	if err := s.db.WithContext(ctx).Preload("Host").First(&a, "id = ?", id).Error; err != nil {
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
	if err := s.ensureHost(ctx, nil, target.HostID, target.Host, target.Protocol, target.Port); err != nil {
		return TargetView{}, fmt.Errorf("ensure host: %w", err)
	}
	var targetHost model.Host
	if err := s.db.WithContext(ctx).First(&targetHost, "id = ?", target.HostID).Error; err != nil {
		return TargetView{}, fmt.Errorf("load target host: %w", err)
	}
	target.Protocol = normalizedHostProtocol(targetHost.Protocol)
	if target.Protocol == "rdp" {
		if err := validateNewRDPAccount(target); err != nil {
			return TargetView{}, err
		}
	}
	if err := s.ensureHostAccountUsernameAvailable(ctx, nil, target.HostID, target.Username, ""); err != nil {
		return TargetView{}, err
	}

	seq, err := s.nextHostResourceSeq(ctx)
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
		InsecureIgnoreHostKey: target.InsecureIgnoreHostKey,
		HostKeyFingerprint:    target.HostKeyFingerprint,
		KnownHostsPath:        target.KnownHostsPath,
		RDPSecurity:           normalizedRDPSecurity(target.RDPSecurity),
		RDPIgnoreCertificate:  target.RDPIgnoreCertificate,
		RDPCertFingerprints:   target.RDPCertFingerprints,
		RDPApprovalRequired:   target.RDPApprovalRequired,
		RDPClipboardRead:      target.RDPClipboardRead,
		RDPClipboardWrite:     target.RDPClipboardWrite,
		RDPFileUpload:         target.RDPFileUpload,
		RDPFileDownload:       target.RDPFileDownload,
		RDPDriveMapping:       target.RDPDriveMapping,
		GroupName:             target.Group,
		Remark:                target.Remark,
		ResourceSeq:           seq,
		ResourceID:            util.ResourceIDFromSeq(util.PrefixHost, seq),
	}
	expiresAt, err := parseTargetExpiry(target.ExpiresAt)
	if err != nil {
		return TargetView{}, err
	}
	a.ExpiresAt = expiresAt
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
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		if err := tx.First(&a.Host, "id = ?", a.HostID).Error; err != nil {
			return err
		}
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
		if err := tx.First(&a, "id = ?", id).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrTargetNotFound, id)
		}
		var targetHost model.Host
		if err := tx.First(&targetHost, "id = ?", a.HostID).Error; err != nil {
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
		if err := s.ensureHostAccountUsernameAvailable(ctx, tx, a.HostID, target.Username, a.ID); err != nil {
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
		a.InsecureIgnoreHostKey = target.InsecureIgnoreHostKey
		a.HostKeyFingerprint = target.HostKeyFingerprint
		a.KnownHostsPath = target.KnownHostsPath
		a.RDPSecurity = normalizedRDPSecurity(target.RDPSecurity)
		a.RDPIgnoreCertificate = target.RDPIgnoreCertificate
		a.RDPCertFingerprints = target.RDPCertFingerprints
		a.RDPApprovalRequired = target.RDPApprovalRequired
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

		hostID := target.HostID
		if hostID == "" {
			hostID = a.HostID
		}
		if err := s.updateHostIfChanged(ctx, tx, hostID, target.Host, target.Port); err != nil {
			return err
		}

		if err := tx.Save(&a).Error; err != nil {
			return fmt.Errorf("update target: %w", err)
		}
		if err := s.syncResourceTx(tx, model.ResourceTypeHostAccount, a.ID, hostAccountResourceName(a), a.HostID); err != nil {
			return fmt.Errorf("sync target resource: %w", err)
		}
		if err := tx.Preload("Host").First(&a, "id = ?", id).Error; err != nil {
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
		if err := tx.First(&account, "id = ?", id).Error; err != nil {
			return fmt.Errorf("%w: %q", ErrTargetNotFound, id)
		}
		if err := s.deleteResourceTx(tx, model.ResourceTypeHostAccount, account.ID); err != nil {
			return err
		}
		return tx.Delete(&account).Error
	})
}

func (s *DBStore) DefaultTarget(ctx context.Context, user model.User) (TargetConfig, error) {
	now := time.Now().UTC()
	if user.RequestedTargetID != "" {
		var a model.HostAccount
		if err := s.db.WithContext(ctx).Preload("Host").
			Where("id = ? AND status = ?", user.RequestedTargetID, "active").
			Where("expires_at IS NULL OR expires_at > ?", now).
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

func (s *DBStore) ensureHostAccountUsernameAvailable(ctx context.Context, tx *gorm.DB, hostID, username, exceptID string) error {
	var count int64
	q := queryWithContext(s.db, tx, ctx).Model(&model.HostAccount{}).
		Where("host_id = ? AND username = ?", hostID, username)
	if exceptID != "" {
		q = q.Where("id <> ?", exceptID)
	}
	if err := q.Count(&count).Error; err != nil {
		return fmt.Errorf("check host account duplicate: %w", err)
	}
	if count > 0 {
		return fmt.Errorf("host account %q already exists on host %q", username, hostID)
	}
	return nil
}

func (s *DBStore) updateHostIfChanged(ctx context.Context, tx *gorm.DB, hostID string, host string, port int) error {
	if host == "" && port == 0 {
		return nil
	}
	if tx == nil {
		return s.db.WithContext(ctx).Transaction(func(inner *gorm.DB) error {
			return s.updateHostIfChanged(ctx, inner, hostID, host, port)
		})
	}
	var h model.Host
	if err := tx.First(&h, "id = ?", hostID).Error; err != nil {
		return nil
	}
	if host != "" {
		h.Address = host
	}
	if port != 0 {
		h.Port = port
	}
	if err := tx.Save(&h).Error; err != nil {
		return err
	}
	return s.syncResourceTx(tx, model.ResourceTypeHost, h.ID, hostResourceName(h), "")
}
