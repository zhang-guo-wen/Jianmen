package access

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"gorm.io/gorm"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/util"
)

type StaticStore struct {
	cfg              *config.Config
	db               *gorm.DB
	mu               sync.RWMutex
	users            map[string]config.User
	userPublicKeys   map[string][]ssh.PublicKey
	hosts            map[string]HostRecord
	runtimeHosts     map[string]struct{}
	hostsFile        string
	targets          map[string]config.Target
	runtimeTargets   map[string]struct{}
}

var (
	ErrTargetNotFound    = errors.New("target not found")
	ErrHostNotFound      = errors.New("host not found")
	ErrDBProxyNotFound   = errors.New("database proxy not found")
	ErrDBAccountNotFound = errors.New("database account not found")
	ErrTargetUnavailable = errors.New("target unavailable")
	ErrDBInstanceNotFound = errors.New("database instance not found")
)

type UserView struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type LoginName struct {
	ResourceID string // 紧凑格式中的资源ID部分 (4位)
	SessionID  string // 紧凑格式中的会话ID部分 (5位)
}

type TargetView struct {
	ID                    string   `json:"id"`
	HostID                string   `json:"host_id,omitempty"`
	ResourceType          string   `json:"resource_type"`
	ResourceID            string   `json:"resource_id"`
	ResourceSeq           int      `json:"resource_seq"`
	HostResourceID        string   `json:"host_resource_id"`
	Name                  string   `json:"name"`
	Group                 string   `json:"group"`
	Remark                string   `json:"remark,omitempty"`
	ExpiresAt             string   `json:"expires_at,omitempty"`
	Status                string   `json:"status"`
	Host                  string   `json:"host"`
	Port                  int      `json:"port"`
	Username              string   `json:"username"`
	AuthMethods           []string `json:"auth_methods"`
	InsecureIgnoreHostKey bool     `json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string   `json:"host_key_fingerprint"`
	KnownHostsPath        string   `json:"known_hosts_path"`
}

type HostRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Group   string `json:"group,omitempty"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Remark  string `json:"remark,omitempty"`
	Disabled bool   `json:"disabled"`
}

type HostView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Group        string `json:"group,omitempty"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Remark       string `json:"remark,omitempty"`
	Status       string `json:"status"`
	AccountCount int    `json:"account_count"`
}

type DatabaseInstanceView struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Protocol     string    `json:"protocol"`
	Address      string    `json:"address"`
	Port         int       `json:"port"`
	Group        string    `json:"group,omitempty"`
	Remark       string    `json:"remark,omitempty"`
	Status       string    `json:"status"`
	AccountCount int64     `json:"account_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type DatabaseAccountView struct {
	ID               string     `json:"id"`
	InstanceID       string     `json:"instance_id"`
	UniqueName       string     `json:"unique_name"`
	Username   string     `json:"username"`
	Group      string     `json:"group,omitempty"`
	Remark           string     `json:"remark,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Status     string     `json:"status"`
	ResourceID       string     `json:"resource_id,omitempty"`
	ResourceSeq      int        `json:"resource_seq,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

func NewStaticStore(cfg *config.Config, db *gorm.DB) (*StaticStore, error) {
	store := &StaticStore{
		cfg:            cfg,
		db:             db,
		users:          make(map[string]config.User, len(cfg.Users)),
		userPublicKeys: make(map[string][]ssh.PublicKey, len(cfg.Users)),
		hosts:          make(map[string]HostRecord),
		runtimeHosts:   make(map[string]struct{}),
		hostsFile:      hostsFileForTargets(cfg.TargetsFile),
		targets:        make(map[string]config.Target),
		runtimeTargets: make(map[string]struct{}),
	}
	for _, user := range cfg.Users {
		if user.Username == "" {
			return nil, errors.New("user username is required")
		}
		publicKeys, err := loadUserPublicKeys(user)
		if err != nil {
			return nil, fmt.Errorf("load public keys for user %q: %w", user.Username, err)
		}
		store.users[user.Username] = user
		store.userPublicKeys[user.Username] = publicKeys
	}
	runtimeHosts, err := loadRuntimeHosts(store.hostsFile)
	if err != nil {
		return nil, err
	}
	for _, host := range runtimeHosts {
		host = normalizeHost(host)
		if err := validateHost(host); err != nil {
			return nil, err
		}
		store.hosts[host.ID] = host
		store.runtimeHosts[host.ID] = struct{}{}
	}
	runtimeTargets, err := loadRuntimeTargets(cfg.TargetsFile)
	if err != nil {
		return nil, err
	}
	for _, target := range runtimeTargets {
		target = normalizeTarget(target)
		if err := validateTarget(target); err != nil {
			return nil, err
		}
		store.targets[target.ID] = normalizeTarget(target)
		store.runtimeTargets[target.ID] = struct{}{}
	}
	return store, nil
}

func (s *StaticStore) Authenticate(_ context.Context, username, password string) (model.User, error) {
	login, err := ParseLoginName(username)
	if err != nil {
		return model.User{}, err
	}
	return s.authenticateCompact(login, password)
}

func (s *StaticStore) AuthenticateDirect(_ context.Context, username, password string) (model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[username]
	if !ok || user.Password == "" || user.Password != password {
		return model.User{}, errors.New("invalid username or password")
	}
	return model.User{ID: configUserID(user), Username: user.Username, RequestedTargetID: ""}, nil
}

func (s *StaticStore) AuthenticatePublicKey(_ context.Context, username string, key ssh.PublicKey) (model.User, error) {
	login, err := ParseLoginName(username)
	if err != nil {
		return model.User{}, err
	}
	return s.authenticateCompactPublicKey(login, key)
}

func (s *StaticStore) authenticateCompact(login LoginName, password string) (model.User, error) {
	// 1. 通过 sessionID 找到用户会话
	var userSession model.UserSession
	if err := s.db.Where("session_id = ? AND status = ?", login.SessionID, "active").First(&userSession).Error; err != nil {
		return model.User{}, fmt.Errorf("invalid session: %w", err)
	}
	// 2. 检查过期
	if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
		s.db.Model(&userSession).Update("status", "expired")
		return model.User{}, errors.New("session expired")
	}
	// 3. 验证用户密码
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
	// 4. 查找目标资源
	if login.ResourceID != "" {
		var account model.HostAccount
		if err := s.db.Where("resource_id = ?", login.ResourceID).First(&account).Error; err == nil {
			user.RequestedTargetID = account.ID
		}
	}
	return user, nil
}

func (s *StaticStore) authenticateCompactPublicKey(login LoginName, key ssh.PublicKey) (model.User, error) {
	// 1. 通过 sessionID 找到用户会话
	var userSession model.UserSession
	if err := s.db.Where("session_id = ? AND status = ?", login.SessionID, "active").First(&userSession).Error; err != nil {
		return model.User{}, fmt.Errorf("invalid session: %w", err)
	}
	// 2. 检查过期
	if userSession.ExpiresAt != nil && time.Now().After(*userSession.ExpiresAt) {
		s.db.Model(&userSession).Update("status", "expired")
		return model.User{}, errors.New("session expired")
	}
	// 3. 查找用户并验证公钥
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
	// 4. 查找目标资源
	if login.ResourceID != "" {
		var account model.HostAccount
		if err := s.db.Where("resource_id = ?", login.ResourceID).First(&account).Error; err == nil {
			user.RequestedTargetID = account.ID
		}
	}
	return user, nil
}

func (s *StaticStore) userForLoginLocked(login LoginName, user config.User) (model.User, error) {
	if login.ResourceID != "" {
		// ResourceID-based lookup replaces old TargetID lookup
		var account model.HostAccount
		if err := s.db.Where("resource_id = ?", login.ResourceID).First(&account).Error; err != nil {
			return model.User{}, fmt.Errorf("target account for resource %q not found", login.ResourceID)
		}
		return model.User{ID: configUserID(user), Username: user.Username, RequestedTargetID: account.ID}, nil
	}
	return model.User{ID: configUserID(user), Username: user.Username, RequestedTargetID: ""}, nil
}

func (s *StaticStore) DefaultTarget(_ context.Context, user model.User) (config.Target, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targetID := strings.TrimSpace(user.RequestedTargetID)
	if targetID == "" {
		targetID = strings.TrimSpace(s.cfg.DefaultTarget)
	}
	if targetID == "" {
		for id := range s.targets {
			if targetID == "" || id < targetID {
				targetID = id
			}
		}
	}
	if targetID == "" {
		return config.Target{}, errors.New("no target accounts are configured")
	}
	target, ok := s.targets[targetID]
	if !ok {
		return config.Target{}, fmt.Errorf("target %q not found", targetID)
	}
	if err := s.targetAvailableLocked(target); err != nil {
		return config.Target{}, err
	}
	return target, nil
}

func (s *StaticStore) Users() []UserView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]UserView, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, UserView{
			ID:       configUserID(user),
			Username: user.Username,
		})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	return users
}

func (s *StaticStore) Hosts() []HostView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hosts := s.hostViewsLocked()
	out := make([]HostView, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, host)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *StaticStore) Host(id string) (HostView, error) {
	id = strings.TrimSpace(id)
	s.mu.RLock()
	defer s.mu.RUnlock()
	hosts := s.hostViewsLocked()
	host, ok := hosts[id]
	if !ok {
		return HostView{}, fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	return host, nil
}

// --- Database Instances (GORM-backed) ---

func (s *StaticStore) DatabaseInstances() []DatabaseInstanceView {
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
			Group:    inst.GroupName,
			Remark:       inst.Remark,
			Status:       inst.Status,
			AccountCount: count,
			CreatedAt:    inst.CreatedAt,
			UpdatedAt:    inst.UpdatedAt,
		})
	}
	return views
}

func (s *StaticStore) DatabaseInstance(id string) (DatabaseInstanceView, error) {
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
		Group:    inst.GroupName,
		Remark:       inst.Remark,
		Status:       inst.Status,
		AccountCount: count,
		CreatedAt:    inst.CreatedAt,
		UpdatedAt:    inst.UpdatedAt,
	}, nil
}

func (s *StaticStore) AddDatabaseInstance(name, protocol, address string, port int, group, remark string) (DatabaseInstanceView, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" || protocol == "pg" || protocol == "postgresql" {
		protocol = "postgres"
	}
	if protocol != "mysql" && protocol != "postgres" && protocol != "tcp" {
		return DatabaseInstanceView{}, fmt.Errorf("unsupported database protocol %q", protocol)
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		return DatabaseInstanceView{}, fmt.Errorf("invalid address %q: %w", address, err)
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
		Group: inst.GroupName,
		Remark:    inst.Remark,
		Status:       inst.Status,
		CreatedAt: inst.CreatedAt,
		UpdatedAt: inst.UpdatedAt,
	}, nil
}

func (s *StaticStore) UpdateDatabaseInstance(id, name, protocol, address string, port int, group, remark, status string) (DatabaseInstanceView, error) {
	id = strings.TrimSpace(id)
	var inst model.DatabaseInstance
	if err := s.db.First(&inst, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return DatabaseInstanceView{}, fmt.Errorf("%w: %q", ErrDBInstanceNotFound, id)
		}
		return DatabaseInstanceView{}, err
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" || protocol == "pg" || protocol == "postgresql" {
		protocol = "postgres"
	}
	if protocol != "mysql" && protocol != "postgres" && protocol != "tcp" {
		return DatabaseInstanceView{}, fmt.Errorf("unsupported database protocol %q", protocol)
	}
	if _, _, err := net.SplitHostPort(address); err != nil {
		return DatabaseInstanceView{}, fmt.Errorf("invalid address %q: %w", address, err)
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
		Group:    inst.GroupName,
		Remark:       inst.Remark,
		Status:       inst.Status,
		AccountCount: count,
		CreatedAt:    inst.CreatedAt,
		UpdatedAt:    inst.UpdatedAt,
	}, nil
}

func (s *StaticStore) DeleteDatabaseInstance(id string) error {
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

// --- Database Accounts (GORM-backed) ---

func (s *StaticStore) DatabaseAccounts(instanceID string) ([]DatabaseAccountView, error) {
	instanceID = strings.TrimSpace(instanceID)
	var accounts []model.DatabaseAccount
	if err := s.db.Where("instance_id = ?", instanceID).Order("upstream_username ASC").Find(&accounts).Error; err != nil {
		return nil, err
	}
	views := make([]DatabaseAccountView, 0, len(accounts))
	for _, acct := range accounts {
		views = append(views, s.databaseAccountView(acct))
	}
	return views, nil
}

func (s *StaticStore) DatabaseAccount(id string) (DatabaseAccountView, error) {
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

func (s *StaticStore) AddDatabaseAccount(instanceID, username, password, group, remark string, expiresAt *time.Time) (DatabaseAccountView, error) {
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
	acct := model.DatabaseAccount{
		InstanceID:       instanceID,
		UniqueName:       uniqueName,
		Username:   username,
		Password:   model.NewEncryptedField(password),
		GroupName:        strings.TrimSpace(group),
		Remark:           strings.TrimSpace(remark),
		ExpiresAt:        expiresAt,
	}
	// 分配资源ID
	var maxSeq int
	s.db.Model(&model.DatabaseAccount{}).Select("COALESCE(MAX(resource_seq), 0)").Scan(&maxSeq)
	acct.ResourceSeq = maxSeq + 1
	acct.ResourceID = util.ResourceIDFromSeq(util.PrefixDatabase, acct.ResourceSeq)
	if err := s.db.Create(&acct).Error; err != nil {
		return DatabaseAccountView{}, err
	}
	return s.databaseAccountView(acct), nil
}

func (s *StaticStore) UpdateDatabaseAccount(id, username, password, group, remark string, expiresAt *time.Time, status string) (DatabaseAccountView, error) {
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

func (s *StaticStore) DeleteDatabaseAccount(id string) error {
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

// --- Database lookups for proxy/rbac ---

func (s *StaticStore) DatabaseAccountByUniqueName(uniqueName string) (*model.DatabaseAccount, error) {
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

func (s *StaticStore) generateUniqueName() (string, error) {
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

func (s *StaticStore) databaseAccountView(acct model.DatabaseAccount) DatabaseAccountView {
	return DatabaseAccountView{
		ID:               acct.ID,
		InstanceID:       acct.InstanceID,
		UniqueName:       acct.UniqueName,
		Username: acct.Username,
		Group:        acct.GroupName,
		Remark:           acct.Remark,
		ExpiresAt:        acct.ExpiresAt,
		Status:         acct.Status,
		ResourceID:       acct.ResourceID,
		ResourceSeq:      acct.ResourceSeq,
		CreatedAt:        acct.CreatedAt,
		UpdatedAt:        acct.UpdatedAt,
	}
}

// --- Host CRUD (JSON-file-backed) ---

func (s *StaticStore) AddHost(host HostRecord) (HostView, error) {
	host = normalizeHost(host)
	if err := validateHost(host); err != nil {
		return HostView{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.hosts[host.ID]; exists {
		return HostView{}, fmt.Errorf("host %q already exists", host.ID)
	}
	s.hosts[host.ID] = host
	s.runtimeHosts[host.ID] = struct{}{}
	if err := s.saveRuntimeHostsLocked(); err != nil {
		delete(s.hosts, host.ID)
		delete(s.runtimeHosts, host.ID)
		return HostView{}, err
	}
	view, _ := s.hostViewLocked(host.ID)
	return view, nil
}

func (s *StaticStore) UpdateHost(id string, host HostRecord) (HostView, error) {
	id = strings.TrimSpace(id)
	host = normalizeHost(host)
	if host.ID == "" {
		host.ID = id
	}
	if host.ID != id {
		return HostView{}, fmt.Errorf("host id mismatch: path %q, body %q", id, host.ID)
	}
	if err := validateHost(host); err != nil {
		return HostView{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	previous, hadRuntimeHost := s.hosts[id]
	previousRuntimeTargets := make(map[string]struct {
		target     config.Target
		wasRuntime bool
	})
	restorePrevious := func() {
		if hadRuntimeHost {
			s.hosts[id] = previous
			s.runtimeHosts[id] = struct{}{}
		} else {
			delete(s.hosts, id)
			delete(s.runtimeHosts, id)
		}
		for targetID, state := range previousRuntimeTargets {
			s.targets[targetID] = state.target
			if state.wasRuntime {
				s.runtimeTargets[targetID] = struct{}{}
			} else {
				delete(s.runtimeTargets, targetID)
			}
		}
	}
	derived, exists := s.hostViewLocked(id)
	if !exists {
		return HostView{}, fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	if host.Host != derived.Host || host.Port != derived.Port {
		for targetID, target := range s.targets {
			if targetHostID(target) == id {
				_, wasRuntime := s.runtimeTargets[targetID]
				previousRuntimeTargets[targetID] = struct {
					target     config.Target
					wasRuntime bool
				}{target: target, wasRuntime: wasRuntime}
				target.HostID = id
				target.Host = host.Host
				target.Port = host.Port
				s.targets[targetID] = normalizeTarget(target)
				s.runtimeTargets[targetID] = struct{}{}
			}
		}
	}

	s.hosts[id] = host
	s.runtimeHosts[id] = struct{}{}
	if err := s.saveRuntimeTargetsLocked(); err != nil {
		restorePrevious()
		return HostView{}, err
	}
	if err := s.saveRuntimeHostsLocked(); err != nil {
		restorePrevious()
		_ = s.saveRuntimeTargetsLocked()
		return HostView{}, err
	}
	view, _ := s.hostViewLocked(id)
	return view, nil
}

func (s *StaticStore) DeleteHost(id string) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.hostViewLocked(id); !exists {
		return fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}

	previousHost, hadRuntimeHost := s.hosts[id]
	previousRuntimeTargets := make(map[string]config.Target)
	for targetID, target := range s.targets {
		if targetHostID(target) == id {
			previousRuntimeTargets[targetID] = target
			delete(s.targets, targetID)
			delete(s.runtimeTargets, targetID)
		}
	}
	delete(s.hosts, id)
	delete(s.runtimeHosts, id)

	if err := s.saveRuntimeTargetsLocked(); err != nil {
		for targetID, target := range previousRuntimeTargets {
			s.targets[targetID] = target
			s.runtimeTargets[targetID] = struct{}{}
		}
		if hadRuntimeHost {
			s.hosts[id] = previousHost
			s.runtimeHosts[id] = struct{}{}
		}
		return err
	}
	if err := s.saveRuntimeHostsLocked(); err != nil {
		for targetID, target := range previousRuntimeTargets {
			s.targets[targetID] = target
			s.runtimeTargets[targetID] = struct{}{}
		}
		if hadRuntimeHost {
			s.hosts[id] = previousHost
			s.runtimeHosts[id] = struct{}{}
		}
		return err
	}
	return nil
}

// --- Target CRUD (JSON-file-backed) ---

func (s *StaticStore) Targets() []TargetView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	targets := make([]TargetView, 0, len(s.targets))
	for _, target := range s.targets {
		targets = append(targets, targetView(target, false, s.hostDisabledLocked(targetHostID(target))))
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets
}

func (s *StaticStore) HostAccounts(id string) ([]TargetView, error) {
	id = strings.TrimSpace(id)
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, exists := s.hostViewLocked(id); !exists {
		return nil, fmt.Errorf("%w: %q", ErrHostNotFound, id)
	}
	targets := make([]TargetView, 0)
	for _, target := range s.targets {
		if targetHostID(target) == id {
			targets = append(targets, targetView(target, false, s.hostDisabledLocked(id)))
		}
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Username == targets[j].Username {
			return targets[i].ID < targets[j].ID
		}
		return targets[i].Username < targets[j].Username
	})
	return targets, nil
}

func (s *StaticStore) Target(id string) (TargetView, error) {
	id = strings.TrimSpace(id)
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, ok := s.targets[id]
	if !ok {
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return targetView(target, false, s.hostDisabledLocked(targetHostID(target))), nil
}

func (s *StaticStore) AddTarget(target config.Target) (TargetView, error) {
	target = normalizeTarget(target)

	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	target, err = s.applyHostResourceToTargetLocked(target)
	if err != nil {
		return TargetView{}, err
	}
	if err := validateTarget(target); err != nil {
		return TargetView{}, err
	}
	if target.Password == "" && target.PrivateKeyPath == "" && target.PrivateKeyPEM == "" {
		return TargetView{}, errors.New("target requires password or private key")
	}
	if _, exists := s.targets[target.ID]; exists {
		return TargetView{}, fmt.Errorf("target %q already exists", target.ID)
	}
	s.targets[target.ID] = target
	s.runtimeTargets[target.ID] = struct{}{}
	if err := s.saveRuntimeTargetsLocked(); err != nil {
		delete(s.targets, target.ID)
		delete(s.runtimeTargets, target.ID)
		return TargetView{}, err
	}
	return targetView(target, false, s.hostDisabledLocked(targetHostID(target))), nil
}

func (s *StaticStore) UpdateTarget(id string, target config.Target) (TargetView, error) {
	id = strings.TrimSpace(id)
	target.ID = strings.TrimSpace(target.ID)
	if target.ID == "" {
		target.ID = id
	}
	target = normalizeTarget(target)
	if target.ID != id {
		return TargetView{}, fmt.Errorf("target id mismatch: path %q, body %q", id, target.ID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.targets[id]
	if !exists {
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	if target.Password == "" && target.PrivateKeyPath == "" && target.PrivateKeyPEM == "" {
		target.Password = existing.Password
		target.PrivateKeyPath = existing.PrivateKeyPath
		target.PrivateKeyPEM = existing.PrivateKeyPEM
		target.Passphrase = existing.Passphrase
	}
	var err error
	target, err = s.applyHostResourceToTargetLocked(target)
	if err != nil {
		return TargetView{}, err
	}
	if err := validateTarget(target); err != nil {
		return TargetView{}, err
	}
	if target.Password == "" && target.PrivateKeyPath == "" && target.PrivateKeyPEM == "" {
		return TargetView{}, errors.New("target requires password or private key")
	}

	_, wasRuntime := s.runtimeTargets[id]
	s.targets[id] = target
	s.runtimeTargets[id] = struct{}{}
	if err := s.saveRuntimeTargetsLocked(); err != nil {
		s.targets[id] = existing
		if !wasRuntime {
			delete(s.runtimeTargets, id)
		}
		return TargetView{}, err
	}
	return targetView(target, false, s.hostDisabledLocked(targetHostID(target))), nil
}

func (s *StaticStore) DeleteTarget(id string) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	target, exists := s.targets[id]
	if !exists {
		return fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	_, wasRuntime := s.runtimeTargets[id]
	delete(s.targets, id)
	delete(s.runtimeTargets, id)
	if err := s.saveRuntimeTargetsLocked(); err != nil {
		s.targets[id] = target
		if wasRuntime {
			s.runtimeTargets[id] = struct{}{}
		}
		return err
	}
	return nil
}

// --- Internal persistence helpers ---

func (s *StaticStore) saveRuntimeTargetsLocked() error {
	return saveRuntimeTargets(s.cfg.TargetsFile, s.runtimeTargetsSnapshotLocked())
}

func (s *StaticStore) saveRuntimeHostsLocked() error {
	return saveRuntimeHosts(s.hostsFile, s.runtimeHostsSnapshotLocked())
}

func (s *StaticStore) runtimeTargetsSnapshotLocked() []config.Target {
	targets := make([]config.Target, 0, len(s.runtimeTargets))
	for id := range s.runtimeTargets {
		target, ok := s.targets[id]
		if ok {
			targets = append(targets, target)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets
}

func (s *StaticStore) runtimeHostsSnapshotLocked() []HostRecord {
	hosts := make([]HostRecord, 0, len(s.runtimeHosts))
	for id := range s.runtimeHosts {
		host, ok := s.hosts[id]
		if ok {
			hosts = append(hosts, host)
		}
	}
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].ID < hosts[j].ID })
	return hosts
}

// --- Internal view helpers ---

func (s *StaticStore) hostViewsLocked() map[string]HostView {
	hosts := make(map[string]HostView, len(s.hosts)+len(s.targets))
	for _, host := range s.hosts {
		hosts[host.ID] = hostView(host, false)
	}
	for _, target := range s.targets {
		id := targetHostID(target)
		view, exists := hosts[id]
		if !exists {
			view = HostView{
				ID:   id,
				Name: target.Host,
				Host: target.Host,
				Port: normalizedPort(target.Port),
			}
		}
		view.AccountCount++
		hosts[id] = view
	}
	return hosts
}

func (s *StaticStore) hostViewLocked(id string) (HostView, bool) {
	views := s.hostViewsLocked()
	if view, ok := views[id]; ok {
		return view, true
	}
	return HostView{}, false
}

func (s *StaticStore) hostDisabledLocked(id string) bool {
	host, ok := s.hostViewLocked(id)
	return ok && host.Status == "disabled"
}

func (s *StaticStore) targetAvailableLocked(target config.Target) error {
	status := targetStatus(target, s.hostDisabledLocked(targetHostID(target)))
	if status != "enabled" {
		return fmt.Errorf("%w: target %q is %s", ErrTargetUnavailable, target.ID, status)
	}
	return nil
}

func (s *StaticStore) applyHostResourceToTargetLocked(target config.Target) (config.Target, error) {
	if target.HostID == "" {
		return target, nil
	}
	host, exists := s.hostViewLocked(target.HostID)
	if !exists {
		return config.Target{}, fmt.Errorf("%w: %q", ErrHostNotFound, target.HostID)
	}
	target.Host = host.Host
	target.Port = host.Port
	return normalizeTarget(target), nil
}

// --- Standalone helpers ---

func ParseLoginName(username string) (LoginName, error) {
	if len(username) != 10 {
		return LoginName{}, fmt.Errorf("connection username must be 10 characters, got %d", len(username))
	}
	prefix, _, _, err := util.ParseCompactUsername(username)
	if err != nil {
		return LoginName{}, err
	}
	if prefix != util.PrefixHost && prefix != util.PrefixDatabase {
		return LoginName{}, fmt.Errorf("unknown resource prefix: %s", prefix)
	}
	return LoginName{
		ResourceID: username[1:5],
		SessionID:  username[5:10],
	}, nil
}

func loadRuntimeHosts(path string) ([]HostRecord, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var hosts []HostRecord
	if err := json.Unmarshal(raw, &hosts); err != nil {
		return nil, fmt.Errorf("load runtime hosts %q: %w", path, err)
	}
	return hosts, nil
}

func saveRuntimeHosts(path string, hosts []HostRecord) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(hosts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func hostsFileForTargets(targetsFile string) string {
	if strings.TrimSpace(targetsFile) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(targetsFile), "hosts.json")
}

func loadRuntimeTargets(path string) ([]config.Target, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var targets []config.Target
	if err := json.Unmarshal(raw, &targets); err != nil {
		return nil, fmt.Errorf("load runtime targets %q: %w", path, err)
	}
	return targets, nil
}

func saveRuntimeTargets(path string, targets []config.Target) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(targets, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

func normalizeHost(host HostRecord) HostRecord {
	host.ID = strings.TrimSpace(host.ID)
	host.Name = strings.TrimSpace(host.Name)
	host.Group = strings.TrimSpace(host.Group)
	host.Host = strings.TrimSpace(host.Host)
	host.Remark = strings.TrimSpace(host.Remark)
	if address, port, ok := splitAddressPort(host.Host); ok && (host.Port == 0 || host.Port == port) {
		host.Host = address
		host.Port = port
	}
	host.Port = normalizedPort(host.Port)
	if host.ID == "" {
		host.ID = HostResourceID(host.Host, host.Port, "")
	}
	if host.Name == "" {
		host.Name = formatAddressPort(host.Host, host.Port)
	}
	return host
}

func validateHost(host HostRecord) error {
	if host.ID == "" || host.Name == "" || host.Host == "" {
		return fmt.Errorf("host %q is missing id, name, or host", host.Name)
	}
	if strings.ContainsAny(host.ID, `/\.`) {
		return fmt.Errorf("host id %q contains unsupported characters", host.ID)
	}
	if _, embeddedPort, ok := splitAddressPort(host.Host); ok && embeddedPort != normalizedPort(host.Port) {
		return fmt.Errorf("host %q has port %d in host field conflicting with port %d", host.Name, embeddedPort, normalizedPort(host.Port))
	}
	if host.Port <= 0 || host.Port > 65535 {
		return fmt.Errorf("host %q has invalid port %d", host.Name, host.Port)
	}
	return nil
}

func normalizeTarget(target config.Target) config.Target {
	target.ID = strings.TrimSpace(target.ID)
	target.HostID = strings.TrimSpace(target.HostID)
	target.Name = strings.TrimSpace(target.Name)
	target.Group = strings.TrimSpace(target.Group)
	target.Remark = strings.TrimSpace(target.Remark)
	target.ExpiresAt = strings.TrimSpace(target.ExpiresAt)
	target.Host = strings.TrimSpace(target.Host)
	target.Username = strings.TrimSpace(target.Username)
	target.PrivateKeyPath = strings.TrimSpace(target.PrivateKeyPath)
	target.HostKeyFingerprint = strings.TrimSpace(target.HostKeyFingerprint)
	target.KnownHostsPath = strings.TrimSpace(target.KnownHostsPath)
	if address, port, ok := splitAddressPort(target.Host); ok && (target.Port == 0 || target.Port == port) {
		target.Host = address
		target.Port = port
	}
	if target.Port == 0 {
		target.Port = 22
	}
	if target.Name == "" {
		target.Name = target.Username
	}
	if target.Name == "" {
		target.Name = target.ID
	}
	if !target.InsecureIgnoreHostKey && target.HostKeyFingerprint == "" && target.KnownHostsPath == "" {
		target.InsecureIgnoreHostKey = true
	}
	return target
}

func validateTarget(target config.Target) error {
	if target.ID == "" || target.Host == "" || target.Username == "" {
		return fmt.Errorf("target %q is missing id, host, or username", target.Name)
	}
	if strings.ContainsAny(target.ID, `/\.`) {
		return fmt.Errorf("target id %q contains unsupported characters", target.ID)
	}
	if _, embeddedPort, ok := splitAddressPort(target.Host); ok && embeddedPort != normalizedPort(target.Port) {
		return fmt.Errorf("target %q has port %d in host field conflicting with port %d", target.Name, embeddedPort, normalizedPort(target.Port))
	}
	if _, err := parseExpiry(target.ExpiresAt); err != nil {
		return fmt.Errorf("target %q has invalid expires_at: %w", target.Name, err)
	}
	return nil
}

func hostView(host HostRecord, static bool) HostView {
	status := "enabled"
	if host.Disabled {
		status = "disabled"
	}
	return HostView{
		ID:       host.ID,
		Name:     host.Name,
		Group:    host.Group,
		Host:     host.Host,
		Port:     normalizedPort(host.Port),
		Remark:   host.Remark,
		Status:   status,
	}
}

func targetView(target config.Target, static bool, hostDisabled bool) TargetView {
	hostResourceID := targetHostID(target)
	status := targetStatus(target, hostDisabled)
	return TargetView{
		ID:                    target.ID,
		HostID:                hostResourceID,
		ResourceType:          model.ResourceTypeHostAccount,
		ResourceID:            target.ID,
		HostResourceID:        hostResourceID,
		Name:                  target.Name,
		Group:                 target.Group,
		Remark:                target.Remark,
		ExpiresAt:             target.ExpiresAt,
		Status:                status,
		Host:                  target.Host,
		Port:                  target.Port,
		Username:              target.Username,
		AuthMethods:           targetAuthMethods(target),
		InsecureIgnoreHostKey: target.InsecureIgnoreHostKey,
		HostKeyFingerprint:    target.HostKeyFingerprint,
		KnownHostsPath:        target.KnownHostsPath,
	}
}

func targetStatus(target config.Target, hostDisabled bool) string {
	switch {
	case hostDisabled:
		return "host_disabled"
	case target.Disabled:
		return "disabled"
	case targetExpired(target):
		return "expired"
	default:
		return "enabled"
	}
}

func targetExpired(target config.Target) bool {
	expiresAt, err := parseExpiry(target.ExpiresAt)
	if err != nil || expiresAt.IsZero() {
		return false
	}
	return !time.Now().Before(expiresAt)
}

func parseExpiry(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func configUserID(user config.User) string {
	if id := strings.TrimSpace(user.ID); id != "" {
		return id
	}
	return strings.TrimSpace(user.Username)
}

func normalizedPort(port int) int {
	if port == 0 {
		return 22
	}
	return port
}

func hostResourceIDForTarget(target config.Target) string {
	return HostResourceID(target.Host, target.Port, target.ID)
}

func targetHostID(target config.Target) string {
	if id := strings.TrimSpace(target.HostID); id != "" {
		return id
	}
	return hostResourceIDForTarget(target)
}

func HostResourceID(host string, port int, fallback string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	port = normalizedPort(port)
	value := fmt.Sprintf("%s-%d", host, port)
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, value)
	value = strings.Trim(value, "-")
	if value == "" {
		return strings.TrimSpace(fallback)
	}
	return value
}

func splitAddressPort(value string) (string, int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, false
	}
	host, portText, err := net.SplitHostPort(value)
	if err == nil {
		port, ok := parsePort(portText)
		if ok && strings.TrimSpace(host) != "" {
			return strings.TrimSpace(host), port, true
		}
		return value, 0, false
	}
	if strings.Count(value, ":") != 1 {
		return value, 0, false
	}
	hostText, portText, ok := strings.Cut(value, ":")
	if !ok || strings.TrimSpace(hostText) == "" {
		return value, 0, false
	}
	port, ok := parsePort(portText)
	if !ok {
		return value, 0, false
	}
	return strings.TrimSpace(hostText), port, true
}

func parsePort(value string) (int, bool) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || port <= 0 || port > 65535 {
		return 0, false
	}
	return port, true
}

func formatAddressPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	return fmt.Sprintf("%s:%d", host, normalizedPort(port))
}

func targetAuthMethods(target config.Target) []string {
	methods := make([]string, 0, 2)
	if target.Password != "" {
		methods = append(methods, "password")
	}
	if target.PrivateKeyPath != "" || target.PrivateKeyPEM != "" {
		methods = append(methods, "private_key")
	}
	return methods
}

func loadUserPublicKeys(user config.User) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	for i, publicKey := range user.PublicKeys {
		parsed, err := parseAuthorizedKeys([]byte(publicKey))
		if err != nil {
			return nil, fmt.Errorf("public_keys[%d]: %w", i, err)
		}
		keys = append(keys, parsed...)
	}
	if user.AuthorizedKeysPath != "" {
		raw, err := os.ReadFile(user.AuthorizedKeysPath)
		if err != nil {
			return nil, err
		}
		parsed, err := parseAuthorizedKeys(raw)
		if err != nil {
			return nil, fmt.Errorf("authorized_keys_path %q: %w", user.AuthorizedKeysPath, err)
		}
		keys = append(keys, parsed...)
	}
	return keys, nil
}

func parseAuthorizedKeys(raw []byte) ([]ssh.PublicKey, error) {
	var keys []ssh.PublicKey
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		keys = append(keys, key)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return keys, nil
}

func publicKeysEqual(a, b ssh.PublicKey) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Type() == b.Type() && bytes.Equal(a.Marshal(), b.Marshal())
}

func ClientConfigForTarget(target config.Target) (*ssh.ClientConfig, error) {
	authMethods := make([]ssh.AuthMethod, 0, 2)
	if target.Password != "" {
		authMethods = append(authMethods, ssh.Password(target.Password))
		authMethods = append(authMethods, ssh.KeyboardInteractive(func(_ string, _ string, questions []string, _ []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range answers {
				answers[i] = target.Password
			}
			return answers, nil
		}))
	}
	if target.PrivateKeyPath != "" || target.PrivateKeyPEM != "" {
		signer, err := loadSigner(target.PrivateKeyPath, target.PrivateKeyPEM, target.Passphrase)
		if err != nil {
			return nil, err
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}
	if len(authMethods) == 0 {
		return nil, errors.New("target has no usable auth method")
	}

	hostKeyCallback, err := hostKeyCallbackForTarget(target)
	if err != nil {
		return nil, err
	}

	return &ssh.ClientConfig{
		User:            target.Username,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
	}, nil
}

func hostKeyCallbackForTarget(target config.Target) (ssh.HostKeyCallback, error) {
	if target.InsecureIgnoreHostKey {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	if target.HostKeyFingerprint != "" {
		expected := normalizeHostKeyFingerprint(target.HostKeyFingerprint)
		return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			actual := normalizeHostKeyFingerprint(ssh.FingerprintSHA256(key))
			if actual != expected {
				return fmt.Errorf("target host key mismatch for %s: got %s, want %s", hostname, actual, expected)
			}
			return nil
		}, nil
	}
	if target.KnownHostsPath != "" {
		callback, err := knownhosts.New(target.KnownHostsPath)
		if err != nil {
			return nil, err
		}
		return callback, nil
	}
	return nil, errors.New("strict target host key verification requires host_key_fingerprint or known_hosts_path")
}

func normalizeHostKeyFingerprint(fingerprint string) string {
	fingerprint = strings.TrimSpace(fingerprint)
	fingerprint = strings.TrimPrefix(fingerprint, "SHA256:")
	return "SHA256:" + fingerprint
}

func loadSigner(path, pemText, passphrase string) (ssh.Signer, error) {
	var key []byte
	var err error
	if pemText != "" {
		key = []byte(pemText)
	} else {
		key, err = os.ReadFile(path)
		if err != nil {
			return nil, err
		}
	}
	if passphrase != "" {
		return ssh.ParsePrivateKeyWithPassphrase(key, []byte(passphrase))
	}
	return ssh.ParsePrivateKey(key)
}
