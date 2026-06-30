package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

func (s *DBStore) targetView(a model.HostAccount) TargetView {
	status := "enabled"
	if a.Status == "disabled" {
		status = "disabled"
	}
	authMethods := []string{"password"}
	if a.AuthType == "private_key" || a.AuthType == "key" {
		authMethods = []string{"private_key"}
	}
	name := a.Username
	if name == "" {
		name = a.ID
	}
	host, port := s.hostAddressPort(a.Host, a.HostID)
	return TargetView{
		ID: a.ID, HostID: a.HostID,
		ResourceType: model.ResourceTypeHostAccount, ResourceID: a.ResourceID,
		ResourceSeq: a.ResourceSeq,
		Name:        name, Group: a.GroupName, Remark: a.Remark,
		Host: host, Port: port,
		Username: a.Username, Status: status,
		AuthMethods:           authMethods,
		InsecureIgnoreHostKey: a.InsecureIgnoreHostKey,
		HostKeyFingerprint:    a.HostKeyFingerprint,
		KnownHostsPath:        a.KnownHostsPath,
	}
}

func (s *DBStore) hostAddressPort(h model.Host, hostID string) (host string, port int) {
	host = h.Address
	port = h.Port
	if host == "" && hostID != "" {
		var loaded model.Host
		if err := s.db.First(&loaded, "id = ?", hostID).Error; err == nil {
			host = loaded.Address
			port = loaded.Port
		}
	}
	return
}

func (s *DBStore) ensureHost(hostID, address string, port int) error {
	var existing model.Host
	if err := s.db.First(&existing, "id = ?", hostID).Error; err == nil {
		return s.syncResource(model.ResourceTypeHost, existing.ID, hostResourceName(existing), "")
	}
	name := address
	if port != 0 && port != 22 {
		name = fmt.Sprintf("%s:%d", address, port)
	}
	if name == "" {
		name = hostID
	}
	host := model.Host{
		ID:      hostID,
		Name:    name,
		Address: address,
		Port:    port,
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&host).Error; err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeHost, host.ID, hostResourceName(host), "")
	})
}

func (s *DBStore) targetConfig(a model.HostAccount) TargetConfig {
	host, port := s.hostAddressPort(a.Host, a.HostID)
	if host == "" {
		host = "127.0.0.1"
	}
	if port == 0 {
		port = 22
	}
	disabled := a.Status == "disabled" || a.Host.Status == "disabled"
	return TargetConfig{
		ID: a.ID, Username: a.Username,
		Name:                  a.Username + "@" + formatHostAddress(host, port),
		Host:                  host,
		Port:                  port,
		Password:              a.Password.GetPlaintext(),
		PrivateKeyPEM:         a.PrivateKeyPEM.GetPlaintext(),
		Passphrase:            a.Passphrase.GetPlaintext(),
		InsecureIgnoreHostKey: a.InsecureIgnoreHostKey,
		HostKeyFingerprint:    a.HostKeyFingerprint,
		KnownHostsPath:        a.KnownHostsPath,
		HostID:                a.HostID,
		Disabled:              disabled,
	}
}

func (s *DBStore) HostAccounts(hostID string) ([]TargetView, error) {
	var accounts []model.HostAccount
	if err := s.db.Preload("Host").Where("host_id = ?", hostID).Order("username ASC").Find(&accounts).Error; err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	out := make([]TargetView, len(accounts))
	for i := range accounts {
		out[i] = s.targetView(accounts[i])
	}
	return out, nil
}

func (s *DBStore) Targets() []TargetView {
	var accounts []model.HostAccount
	if err := s.db.Preload("Host").Order("created_at DESC").Find(&accounts).Error; err != nil {
		return nil
	}
	out := make([]TargetView, len(accounts))
	for i := range accounts {
		out[i] = s.targetView(accounts[i])
	}
	return out
}

func (s *DBStore) Target(id string) (TargetView, error) {
	var a model.HostAccount
	if err := s.db.Preload("Host").First(&a, "id = ?", id).Error; err != nil {
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return s.targetView(a), nil
}

func (s *DBStore) TargetConfig(id string) (TargetConfig, error) {
	var a model.HostAccount
	if err := s.db.Preload("Host").First(&a, "id = ?", id).Error; err != nil {
		return TargetConfig{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return s.targetConfig(a), nil
}

func (s *DBStore) AddTarget(target config.Target) (TargetView, error) {
	target = normalizeConfigTarget(target)
	if target.HostID == "" {
		target.HostID = fmt.Sprintf("%s-%d", target.Host, target.Port)
	}
	if target.Username == "" {
		return TargetView{}, errors.New("username is required")
	}
	if err := s.ensureHost(target.HostID, target.Host, target.Port); err != nil {
		return TargetView{}, fmt.Errorf("ensure host: %w", err)
	}
	if err := s.ensureHostAccountUsernameAvailable(target.HostID, target.Username, ""); err != nil {
		return TargetView{}, err
	}

	seq, err := s.nextHostResourceSeq()
	if err != nil {
		return TargetView{}, err
	}
	a := model.HostAccount{
		ID:                    target.ID,
		HostID:                target.HostID,
		Username:              target.Username,
		AuthType:              "password",
		Password:              model.NewEncryptedField(target.Password),
		PrivateKeyPEM:         model.NewEncryptedField(target.PrivateKeyPEM),
		Passphrase:            model.NewEncryptedField(target.Passphrase),
		InsecureIgnoreHostKey: target.InsecureIgnoreHostKey,
		HostKeyFingerprint:    target.HostKeyFingerprint,
		KnownHostsPath:        target.KnownHostsPath,
		GroupName:             target.Group,
		Remark:                target.Remark,
		ResourceSeq:           seq,
		ResourceID:            util.ResourceIDFromSeq(util.PrefixHost, seq),
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
	if err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&a).Error; err != nil {
			return err
		}
		var host model.Host
		if err := tx.First(&host, "id = ?", a.HostID).Error; err == nil {
			a.Host = host
		}
		return s.syncResourceTx(tx, model.ResourceTypeHostAccount, a.ID, hostAccountResourceName(a), a.HostID)
	}); err != nil {
		return TargetView{}, fmt.Errorf("create target: %w", err)
	}
	return s.targetView(a), nil
}

func (s *DBStore) UpdateTarget(id string, target config.Target) (TargetView, error) {
	target = normalizeConfigTargetUpdate(target)
	var a model.HostAccount
	if err := s.db.First(&a, "id = ?", id).Error; err != nil {
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	if target.Username == "" {
		return TargetView{}, errors.New("username is required")
	}
	if err := s.ensureHostAccountUsernameAvailable(a.HostID, target.Username, a.ID); err != nil {
		return TargetView{}, err
	}
	a.Username = target.Username
	a.GroupName = target.Group
	a.Remark = target.Remark
	a.InsecureIgnoreHostKey = target.InsecureIgnoreHostKey
	a.HostKeyFingerprint = target.HostKeyFingerprint
	a.KnownHostsPath = target.KnownHostsPath
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
	if err := s.db.Save(&a).Error; err != nil {
		return TargetView{}, fmt.Errorf("update target: %w", err)
	}

	hostID := target.HostID
	if hostID == "" {
		hostID = a.HostID
	}
	if err := s.updateHostIfChanged(hostID, target.Host, target.Port); err != nil {
		return TargetView{}, err
	}
	var host model.Host
	if err := s.db.First(&host, "id = ?", a.HostID).Error; err == nil {
		a.Host = host
	}
	if err := s.syncResource(model.ResourceTypeHostAccount, a.ID, hostAccountResourceName(a), a.HostID); err != nil {
		return TargetView{}, fmt.Errorf("sync target resource: %w", err)
	}
	return s.targetView(a), nil
}

func (s *DBStore) updateHostIfChanged(hostID string, host string, port int) error {
	if host == "" && port == 0 {
		return nil
	}
	var h model.Host
	if err := s.db.First(&h, "id = ?", hostID).Error; err != nil {
		return nil
	}
	if host != "" {
		h.Address = host
	}
	if port != 0 {
		h.Port = port
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&h).Error; err != nil {
			return err
		}
		return s.syncResourceTx(tx, model.ResourceTypeHost, h.ID, hostResourceName(h), "")
	})
}

func (s *DBStore) DeleteTarget(id string) error {
	return s.db.Transaction(func(tx *gorm.DB) error {
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

func (s *DBStore) DefaultTarget(_ context.Context, user model.User) (TargetConfig, error) {
	if user.RequestedTargetID != "" {
		var a model.HostAccount
		if err := s.db.Preload("Host").Where("id = ? AND status = ?", user.RequestedTargetID, "active").First(&a).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return TargetConfig{}, fmt.Errorf("%w: target %q is not available", ErrTargetUnavailable, user.RequestedTargetID)
			}
			return TargetConfig{}, fmt.Errorf("target %q not found", user.RequestedTargetID)
		}
		if a.Host.Status == "disabled" {
			return TargetConfig{}, fmt.Errorf("%w: host %q is disabled", ErrTargetUnavailable, a.HostID)
		}
		return s.targetConfig(a), nil
	}

	var account model.HostAccount
	if err := s.db.Preload("Host").
		Joins("JOIN hosts ON hosts.id = host_accounts.host_id").
		Where("host_accounts.status = ?", "active").
		Where("hosts.status IS NULL OR hosts.status <> ?", "disabled").
		Order("host_accounts.created_at ASC").
		First(&account).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return TargetConfig{}, errors.New("no target accounts available")
		}
		return TargetConfig{}, fmt.Errorf("find default target: %w", err)
	}
	return s.targetConfig(account), nil
}

func (s *DBStore) ensureHostAccountUsernameAvailable(hostID, username, exceptID string) error {
	var count int64
	q := s.db.Model(&model.HostAccount{}).
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

func normalizeConfigTarget(t config.Target) config.Target {
	t.ID = strings.TrimSpace(t.ID)
	t.Name = strings.TrimSpace(t.Name)
	t.HostID = strings.TrimSpace(t.HostID)
	t.Group = strings.TrimSpace(t.Group)
	t.Remark = strings.TrimSpace(t.Remark)
	t.Host = strings.TrimSpace(t.Host)
	t.Username = strings.TrimSpace(t.Username)
	t.Password = strings.TrimSpace(t.Password)
	t.PrivateKeyPEM = strings.TrimSpace(t.PrivateKeyPEM)
	t.PrivateKeyPath = strings.TrimSpace(t.PrivateKeyPath)
	t.HostKeyFingerprint = strings.TrimSpace(t.HostKeyFingerprint)
	t.KnownHostsPath = strings.TrimSpace(t.KnownHostsPath)
	if t.Port == 0 {
		t.Port = 22
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
	t.Group = strings.TrimSpace(t.Group)
	t.Remark = strings.TrimSpace(t.Remark)
	t.Host = strings.TrimSpace(t.Host)
	t.Username = strings.TrimSpace(t.Username)
	t.Password = strings.TrimSpace(t.Password)
	t.PrivateKeyPEM = strings.TrimSpace(t.PrivateKeyPEM)
	t.PrivateKeyPath = strings.TrimSpace(t.PrivateKeyPath)
	t.HostKeyFingerprint = strings.TrimSpace(t.HostKeyFingerprint)
	t.KnownHostsPath = strings.TrimSpace(t.KnownHostsPath)
	return t
}
