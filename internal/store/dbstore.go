package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

// DBStore implements Store backed by a GORM database.
// Database proxies still use config-based management (not yet migrated to DB).
type DBStore struct {
	db *gorm.DB
}

func NewDBStore(db *gorm.DB) *DBStore {
	return &DBStore{db: db}
}

// -- auth --

func (s *DBStore) Authenticate(_ context.Context, username, password string) (model.User, error) {
	// Try token-based auth first.
	hash := sha256.Sum256([]byte(password))
	hashStr := hex.EncodeToString(hash[:])

	var user model.User
	if err := s.db.Where("token_hash = ?", hashStr).First(&user).Error; err == nil {
		return user, nil
	}

	// Parse compact username and authenticate via session.
	login, err := parseLoginName(username)
	if err != nil {
		return model.User{}, err
	}
	return s.authenticateCompact(login, password)
}

func (s *DBStore) AuthenticatePublicKey(_ context.Context, username string, key ssh.PublicKey) (model.User, error) {
	login, err := parseLoginName(username)
	if err != nil {
		return model.User{}, err
	}
	return s.authenticateCompactPublicKey(login, key)
}

func (s *DBStore) authenticateCompact(login LoginName, password string) (model.User, error) {
	// 1. 閫氳繃 sessionID 鎵惧埌鐢ㄦ埛浼氳瘽
	var userSession model.UserSession
	if err := s.db.Where("session_id = ? AND status = ?", login.SessionID, "active").First(&userSession).Error; err != nil {
		return model.User{}, fmt.Errorf("invalid session: %w", err)
	}
	// 2. 妫€鏌ヨ繃鏈?
	if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
		s.db.Model(&userSession).Update("status", "expired")
		return model.User{}, errors.New("session expired")
	}
	// 3. 楠岃瘉鐢ㄦ埛瀵嗙爜
	var user model.User
	if err := s.db.Where("id = ? AND status = ?", userSession.UserID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("user is disabled or not found")
		}
		return model.User{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return model.User{}, errors.New("authentication failed")
	}
	// 4. 鏌ユ壘鐩爣璧勬簮
	if login.ResourceID != "" {
		var account model.HostAccount
		if err := s.db.Where("resource_id = ?", login.ResourceID).First(&account).Error; err == nil {
			user.RequestedTargetID = account.ID
		}
	}
	return user, nil
}

func (s *DBStore) authenticateCompactPublicKey(login LoginName, key ssh.PublicKey) (model.User, error) {
	// 1. 閫氳繃 sessionID 鎵惧埌鐢ㄦ埛浼氳瘽
	var userSession model.UserSession
	if err := s.db.Where("session_id = ? AND status = ?", login.SessionID, "active").First(&userSession).Error; err != nil {
		return model.User{}, fmt.Errorf("invalid session: %w", err)
	}
	// 2. 妫€鏌ヨ繃鏈?
	if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
		s.db.Model(&userSession).Update("status", "expired")
		return model.User{}, errors.New("session expired")
	}
	// 3. 鏌ユ壘鐢ㄦ埛骞堕獙璇佸叕閽?
	var user model.User
	if err := s.db.Where("id = ? AND status = ?", userSession.UserID, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("user is disabled or not found")
		}
		return model.User{}, err
	}
	var pubKeys []model.UserPublicKey
	if err := s.db.Where("user_id = ? AND revoked_at IS NULL", user.ID).Find(&pubKeys).Error; err != nil {
		return model.User{}, fmt.Errorf("load public keys: %w", err)
	}
	keyMatched := false
	for _, pk := range pubKeys {
		parsed, _, _, _, err := ssh.ParseAuthorizedKey([]byte(pk.PublicKey))
		if err != nil {
			continue
		}
		if publicKeysEqual(key, parsed) {
			keyMatched = true
			break
		}
	}
	if !keyMatched {
		return model.User{}, errors.New("invalid username or public key")
	}
	// 4. 鏌ユ壘鐩爣璧勬簮
	if login.ResourceID != "" {
		var account model.HostAccount
		if err := s.db.Where("resource_id = ?", login.ResourceID).First(&account).Error; err == nil {
			user.RequestedTargetID = account.ID
		}
	}
	return user, nil
}

func (s *DBStore) Users() []UserView {
	var users []model.User
	if err := s.db.Where("status = ?", "active").Order("username ASC").Find(&users).Error; err != nil {
		return nil
	}
	out := make([]UserView, len(users))
	for i := range users {
		out[i] = UserView{ID: users[i].ID, Username: users[i].Username}
	}
	return out
}

// -- hosts --

func (s *DBStore) hostView(m model.Host) HostView {
	status := m.Status
	if status == "" {
		status = "active"
	}
	var count int64
	s.db.Model(&model.HostAccount{}).Where("host_id = ?", m.ID).Count(&count)
	return HostView{
		ID: m.ID, Name: m.Name, Group: m.GroupName, Address: m.Address,
		Port: m.Port, Remark: m.Remark, Status: status,
		AccountCount: int(count),
		CreatedAt:    m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    m.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *DBStore) Hosts() []HostView {
	var hosts []model.Host
	if err := s.db.Order("created_at DESC").Find(&hosts).Error; err != nil {
		return nil
	}
	out := make([]HostView, len(hosts))
	for i := range hosts {
		out[i] = s.hostView(hosts[i])
	}
	return out
}

func (s *DBStore) Host(id string) (HostView, error) {
	var m model.Host
	if err := s.db.First(&m, "id = ?", id).Error; err != nil {
		return HostView{}, fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	return s.hostView(m), nil
}

func (s *DBStore) AddHost(host HostRecord) (HostView, error) {
	normalized := normalizeHostRecord(host)
	m := model.Host{
		ID:        normalized.ID,
		Name:      normalized.Name,
		Address:   normalized.Address,
		Port:      normalized.Port,
		GroupName: normalized.Group,
		Remark:    normalized.Remark,
	}
	if normalized.Status == "disabled" {
		m.Status = "disabled"
	}
	if err := s.db.Create(&m).Error; err != nil {
		return HostView{}, fmt.Errorf("create host: %w", err)
	}
	return s.hostView(m), nil
}

func (s *DBStore) UpdateHost(id string, host HostRecord) (HostView, error) {
	var m model.Host
	if err := s.db.First(&m, "id = ?", id).Error; err != nil {
		return HostView{}, fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	normalized := normalizeHostRecord(host)
	m.Name = normalized.Name
	m.Address = normalized.Address
	m.Port = normalized.Port
	m.GroupName = normalized.Group
	m.Remark = normalized.Remark
	m.Status = "active"
	if normalized.Status == "disabled" {
		m.Status = "disabled"
	}
	if err := s.db.Save(&m).Error; err != nil {
		return HostView{}, fmt.Errorf("update host: %w", err)
	}
	return s.hostView(m), nil
}

func (s *DBStore) DeleteHost(id string) error {
	result := s.db.Delete(&model.Host{}, "id = ?", id)
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	s.db.Delete(&model.HostAccount{}, "host_id = ?", id)
	return result.Error
}

// -- targets / host accounts --

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
	return TargetView{
		ID: a.ID, HostID: a.HostID,
		ResourceType: model.ResourceTypeHostAccount, ResourceID: a.ResourceID,
		ResourceSeq: a.ResourceSeq,
		Name: name, Group: a.GroupName, Remark: a.Remark,
		Username: a.Username, Status: status,
		AuthMethods: authMethods,
	}
}

func (s *DBStore) targetConfig(a model.HostAccount) TargetConfig {
	host, port := a.Host.Address, a.Host.Port
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
		InsecureIgnoreHostKey: true,
		HostID:                a.HostID,
		Disabled:              disabled,
	}
}

func (s *DBStore) HostAccounts(hostID string) ([]TargetView, error) {
	var accounts []model.HostAccount
	if err := s.db.Where("host_id = ?", hostID).Order("username ASC").Find(&accounts).Error; err != nil {
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
	if err := s.db.Order("created_at DESC").Find(&accounts).Error; err != nil {
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
	if err := s.db.First(&a, "id = ?", id).Error; err != nil {
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return s.targetView(a), nil
}

func (s *DBStore) AddTarget(target config.Target) (TargetView, error) {
	target = normalizeConfigTarget(target)
	if target.HostID == "" {
		target.HostID = fmt.Sprintf("%s-%d", target.Host, target.Port)
	}
	// 鍒嗛厤璧勬簮ID
	seq, err := s.nextHostResourceSeq()
	if err != nil {
		return TargetView{}, err
	}
	a := model.HostAccount{
		ID:           target.ID,
		HostID:       target.HostID,
		Username:     target.Username,
		AuthType:     "password",
		Password:     model.NewEncryptedField(target.Password),
		PrivateKeyPEM: model.NewEncryptedField(target.PrivateKeyPEM),
		Passphrase:   model.NewEncryptedField(target.Passphrase),
		GroupName:    target.Group,
		Remark:       target.Remark,
		ResourceSeq:  seq,
		ResourceID:   util.ResourceIDFromSeq(util.PrefixHost, seq),
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
	if err := s.db.Create(&a).Error; err != nil {
		return TargetView{}, fmt.Errorf("create target: %w", err)
	}
	return s.targetView(a), nil
}

func (s *DBStore) UpdateTarget(id string, target config.Target) (TargetView, error) {
	var a model.HostAccount
	if err := s.db.First(&a, "id = ?", id).Error; err != nil {
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	a.Username = target.Username
	a.GroupName = target.Group
	a.Remark = target.Remark
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
	return s.targetView(a), nil
}

func (s *DBStore) DeleteTarget(id string) error {
	result := s.db.Delete(&model.HostAccount{}, "id = ?", id)
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return result.Error
}

// -- db instances (DB-backed) --

func (s *DBStore) DatabaseInstances() []DatabaseInstanceView {
	var instances []model.DatabaseInstance
	if err := s.db.Order("name ASC").Find(&instances).Error; err != nil {
		return nil
	}
	views := make([]DatabaseInstanceView, 0, len(instances))
	for _, inst := range instances {
		var count int64
		s.db.Model(&model.DatabaseAccount{}).Where("instance_id = ?", inst.ID).Count(&count)
		views = append(views, DatabaseInstanceView{
			ID:           inst.ID,
			Name:         inst.Name,
			Protocol:     inst.Protocol,
			Address:      inst.Address,
			Port:         inst.Port,
			Group:        inst.GroupName,
			Remark:       inst.Remark,
			Status:       inst.Status,
			AccountCount: int(count),
			CreatedAt:    inst.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    inst.UpdatedAt.Format(time.RFC3339),
		})
	}
	return views
}

func (s *DBStore) DatabaseInstance(id string) (DatabaseInstanceView, error) {
	id = strings.TrimSpace(id)
	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseInstanceView{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
		}
		return DatabaseInstanceView{}, err
	}
	var count int64
	s.db.Model(&model.DatabaseAccount{}).Where("instance_id = ?", inst.ID).Count(&count)
	return DatabaseInstanceView{
		ID:           inst.ID,
		Name:         inst.Name,
		Protocol:     inst.Protocol,
		Address:      inst.Address,
			Port:         inst.Port,
		Group:        inst.GroupName,
		Remark:       inst.Remark,
		Status:       inst.Status,
		AccountCount: int(count),
		CreatedAt:    inst.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    inst.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func normalizeDBProtocol(protocol string) (string, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" || protocol == "pg" || protocol == "postgresql" {
		protocol = "postgres"
	}
	if protocol != "mysql" && protocol != "postgres" && protocol != "tcp" {
		return "", fmt.Errorf("unsupported database protocol %q", protocol)
	}
	return protocol, nil
}

func (s *DBStore) AddDatabaseInstance(name, protocol, address string, port int, group, remark string) (DatabaseInstanceView, error) {
	protocol, err := normalizeDBProtocol(protocol)
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	if strings.TrimSpace(address) == "" {
		return DatabaseInstanceView{}, fmt.Errorf("address is required")
	}
	inst := model.DatabaseInstance{
		Name:      strings.TrimSpace(name),
		Protocol:  protocol,
		Address:   strings.TrimSpace(address),
			Port:      port,
		GroupName: strings.TrimSpace(group),
		Remark:    strings.TrimSpace(remark),
	}
	if inst.Name == "" {
		inst.Name = inst.Address
	}
	if err := s.db.Create(&inst).Error; err != nil {
		return DatabaseInstanceView{}, err
	}
	return DatabaseInstanceView{
		ID:        inst.ID,
		Name:      inst.Name,
		Protocol:  inst.Protocol,
		Address:   inst.Address,
			Port:         inst.Port,
		Group:        inst.GroupName,
		Remark:    inst.Remark,
		Status:       inst.Status,
		CreatedAt: inst.CreatedAt.Format(time.RFC3339),
		UpdatedAt: inst.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *DBStore) UpdateDatabaseInstance(id, name, protocol, address string, port int, group, remark, status string) (DatabaseInstanceView, error) {
	id = strings.TrimSpace(id)
	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseInstanceView{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
		}
		return DatabaseInstanceView{}, err
	}
	protocol, err := normalizeDBProtocol(protocol)
	if err != nil {
		return DatabaseInstanceView{}, err
	}
	if strings.TrimSpace(address) == "" {
		return DatabaseInstanceView{}, fmt.Errorf("address is required")
	}
	inst.Name = strings.TrimSpace(name)
	inst.Protocol = protocol
	inst.Address = strings.TrimSpace(address)
	inst.Port = port
	inst.GroupName = strings.TrimSpace(group)
	inst.Remark = strings.TrimSpace(remark)
	inst.Status = status
	if inst.Name == "" {
		inst.Name = inst.Address
	}
	if err := s.db.Save(&inst).Error; err != nil {
		return DatabaseInstanceView{}, err
	}
	var count int64
	s.db.Model(&model.DatabaseAccount{}).Where("instance_id = ?", inst.ID).Count(&count)
	return DatabaseInstanceView{
		ID:           inst.ID,
		Name:         inst.Name,
		Protocol:     inst.Protocol,
		Address:      inst.Address,
			Port:         inst.Port,
		Group:        inst.GroupName,
		Remark:       inst.Remark,
		Status:       inst.Status,
		AccountCount: int(count),
		CreatedAt:    inst.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    inst.UpdatedAt.Format(time.RFC3339),
	}, nil
}

func (s *DBStore) DeleteDatabaseInstance(id string) error {
	id = strings.TrimSpace(id)
	return s.db.Transaction(func(tx *gorm.DB) error {
		var inst model.DatabaseInstance
		if err := tx.First(&inst, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
			}
			return err
		}
		if err := tx.Where("instance_id = ?", id).Delete(&model.DatabaseAccount{}).Error; err != nil {
			return err
		}
		return tx.Delete(&inst).Error
	})
}

func (s *DBStore) InstanceAccounts(instanceID string) ([]DatabaseAccountView, error) {
	var accounts []model.DatabaseAccount
	if err := s.db.Where("instance_id = ?", instanceID).Order("username ASC").Find(&accounts).Error; err != nil {
		return nil, err
	}
	views := make([]DatabaseAccountView, 0, len(accounts))
	for _, acct := range accounts {
		views = append(views, s.databaseAccountView(acct))
	}
	return views, nil
}

func (s *DBStore) DatabaseAccount(id string) (DatabaseAccountView, error) {
	id = strings.TrimSpace(id)
	var acct model.DatabaseAccount
	if err := s.db.First(&acct, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(acct), nil
}

func (s *DBStore) AddDatabaseAccount(instanceID, username, password, group, remark string, expiresAt *time.Time) (DatabaseAccountView, error) {
	instanceID = strings.TrimSpace(instanceID)
	username = strings.TrimSpace(username)
	if username == "" {
		return DatabaseAccountView{}, errors.New("username is required")
	}
	// Verify instance exists
	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", instanceID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, instanceID)
		}
		return DatabaseAccountView{}, err
	}
	uniqueName, err := s.generateUniqueName()
	if err != nil {
		return DatabaseAccountView{}, err
	}
	// 鍒嗛厤璧勬簮ID
	seq, err := s.nextDBResourceSeq()
	if err != nil {
		return DatabaseAccountView{}, err
	}
	acct := model.DatabaseAccount{
		InstanceID:       instanceID,
		UniqueName:       uniqueName,
		Username:   username,
		Password:   model.NewEncryptedField(password),
		GroupName: strings.TrimSpace(group),
		Remark:           strings.TrimSpace(remark),
		ExpiresAt:        expiresAt,
		ResourceSeq:      seq,
		ResourceID:       util.ResourceIDFromSeq(util.PrefixDatabase, seq),
	}
	if err := s.db.Create(&acct).Error; err != nil {
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(acct), nil
}

func (s *DBStore) UpdateDatabaseAccount(id, username, password, group, remark string, expiresAt *time.Time, status string) (DatabaseAccountView, error) {
	id = strings.TrimSpace(id)
	username = strings.TrimSpace(username)
	var acct model.DatabaseAccount
	if err := s.db.First(&acct, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
		}
		return DatabaseAccountView{}, err
	}
	if username != "" {
		acct.Username = username
	}
	if password != "" {
		acct.Password = model.NewEncryptedField(password)
	}
	acct.GroupName = strings.TrimSpace(group)
	acct.Remark = strings.TrimSpace(remark)
	acct.ExpiresAt = expiresAt
	acct.Status = status
	if err := s.db.Save(&acct).Error; err != nil {
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(acct), nil
}

func (s *DBStore) DeleteDatabaseAccount(id string) error {
	id = strings.TrimSpace(id)
	result := s.db.Delete(&model.DatabaseAccount{}, "id = ?", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: %q", ErrDBAccountNotFound, id)
	}
	return nil
}

func (s *DBStore) DatabaseAccountByUniqueName(uniqueName string) (*model.DatabaseAccount, error) {
	uniqueName = strings.TrimSpace(uniqueName)
	var acct model.DatabaseAccount
	if err := s.db.Preload("Instance").First(&acct, "unique_name = ?", uniqueName).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrDBAccountNotFound, uniqueName)
		}
		return nil, err
	}
	return &acct, nil
}
func (s *DBStore) AuthenticateDirect(_ context.Context, username, password string) (model.User, error) {
	// 鎸夌敤鎴峰悕鏌ユ壘娲昏穬鐢ㄦ埛
	var user model.User
	if err := s.db.Where("username = ? AND status = ?", username, "active").First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.User{}, errors.New("invalid username or password")
		}
		return model.User{}, err
	}
	// 浣跨敤 bcrypt 楠岃瘉瀵嗙爜
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return model.User{}, errors.New("invalid username or password")
	}
	return user, nil
}

// -- connection --

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

	// Pick first active account whose host is also active.
	var accounts []model.HostAccount
	if err := s.db.Preload("Host").Where("status = ?", "active").Find(&accounts).Error; err != nil {
		return TargetConfig{}, fmt.Errorf("list accounts: %w", err)
	}
	for _, a := range accounts {
		if a.Host.Status != "disabled" {
			return s.targetConfig(a), nil
		}
	}
	return TargetConfig{}, errors.New("no target accounts available")
}

// -- user sessions --

func (s *DBStore) UserSessions(userID string) ([]SessionView, error) {
	var sessions []model.UserSession
	q := s.db.Order("session_seq DESC")
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Find(&sessions).Error; err != nil {
		return nil, err
	}
	views := make([]SessionView, len(sessions))
	for i, sess := range sessions {
		views[i] = s.sessionView(sess)
	}
	return views, nil
}

func (s *DBStore) sessionView(sess model.UserSession) SessionView {
	username := ""
	var user model.User
	if s.db.Where("id = ?", sess.UserID).First(&user).Error == nil {
		username = user.Username
	}
	return SessionView{
		ID: sess.ID, UserID: sess.UserID, Username: username,
		SessionSeq: sess.SessionSeq, SessionID: sess.SessionID,
		Type: sess.Type, Status: sess.Status,
		ExpiresAt: sess.ExpiresAt, CreatedBy: sess.CreatedBy,
		CreatedAt: sess.CreatedAt,
	}
}

func (s *DBStore) CreateUserSession(sess model.UserSession) (*model.UserSession, error) {
	// 鐢ㄦ埛缁村害鑷
	var maxSeq int
	s.db.Model(&model.UserSession{}).
		Where("user_id = ?", sess.UserID).
		Select("COALESCE(MAX(session_seq), 0)").Scan(&maxSeq)
	sess.SessionSeq = maxSeq + 1
	sess.SessionID = util.EncodeBase62Padded(uint64(sess.SessionSeq), 5)
	if err := s.db.Create(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *DBStore) DisableUserSession(id string) error {
	return s.db.Model(&model.UserSession{}).Where("id = ?", id).Update("status", "disabled").Error
}

func (s *DBStore) EnableUserSession(id string) error {
	return s.db.Model(&model.UserSession{}).Where("id = ?", id).Update("status", "active").Error
}

func (s *DBStore) UserSessionByID(sessionID string, userID string) (*model.UserSession, error) {
	var sess model.UserSession
	q := s.db.Where("session_id = ?", sessionID)
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.First(&sess).Error; err != nil {
		return nil, err
	}
	return &sess, nil
}

// ---- DBStore helpers ----

func (s *DBStore) databaseAccountView(acct model.DatabaseAccount) DatabaseAccountView {
	return DatabaseAccountView{
		ID:               acct.ID,
		InstanceID:       acct.InstanceID,
		UniqueName:       acct.UniqueName,
		Username:   acct.Username,
		Group:        acct.GroupName,
		Remark:           acct.Remark,
		ExpiresAt:        acct.ExpiresAt,
		Status:       acct.Status,
		ResourceID:       acct.ResourceID,
		ResourceSeq:      acct.ResourceSeq,
		CreatedAt:        acct.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        acct.UpdatedAt.Format(time.RFC3339),
	}
}

func (s *DBStore) generateUniqueName() (string, error) {
	for i := 0; i < 10; i++ {
		name := "db-" + model.NewID()[:12]
		var count int64
		s.db.Model(&model.DatabaseAccount{}).Where("unique_name = ?", name).Count(&count)
		if count == 0 {
			return name, nil
		}
	}
	return "", errors.New("failed to generate unique database account name after 10 attempts")
}

// ---- normalize helpers (subset, for DB entries) ----

func normalizeHostRecord(h HostRecord) HostRecord {
	h.ID = strings.TrimSpace(h.ID)
	h.Name = strings.TrimSpace(h.Name)
	h.Group = strings.TrimSpace(h.Group)
	h.Address = strings.TrimSpace(h.Address)
	h.Remark = strings.TrimSpace(h.Remark)
	if h.Port == 0 {
		h.Port = 22
	}
	if h.ID == "" {
		h.ID = fmt.Sprintf("%s-%d", strings.ToLower(h.Address), h.Port)
	}
	if h.Name == "" {
		h.Name = formatHostAddress(h.Address, h.Port)
	}
	return h
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
	if t.Port == 0 {
		t.Port = 22
	}
	if t.Name == "" {
		t.Name = t.Username
	}
	if !t.InsecureIgnoreHostKey && t.HostKeyFingerprint == "" && t.KnownHostsPath == "" {
		t.InsecureIgnoreHostKey = true
	}
	return t
}
