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

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"jianmen/internal/config"
	"jianmen/internal/model"
	rbaccheck "jianmen/internal/rbac"
)

type StaticStore struct {
	cfg              *config.Config
	mu               sync.RWMutex
	users            map[string]config.User
	userPublicKeys   map[string][]ssh.PublicKey
	hosts            map[string]HostRecord
	runtimeHosts     map[string]struct{}
	hostsFile        string
	dbProxies        map[string]config.DatabaseProxyConfig
	runtimeDBProxies map[string]struct{}
	dbProxiesFile    string
	targets          map[string]config.Target
	runtimeTargets   map[string]struct{}
}

var (
	ErrTargetNotFound    = errors.New("target not found")
	ErrHostNotFound      = errors.New("host not found")
	ErrDBProxyNotFound   = errors.New("database proxy not found")
	ErrDBAccountNotFound = errors.New("database account not found")
	ErrTargetUnavailable = errors.New("target unavailable")
)

type UserView struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type LoginName struct {
	Username string
	TargetID string
}

type TargetView struct {
	ID                    string   `json:"id"`
	HostID                string   `json:"host_id,omitempty"`
	ResourceType          string   `json:"resource_type"`
	ResourceID            string   `json:"resource_id"`
	HostResourceID        string   `json:"host_resource_id"`
	Name                  string   `json:"name"`
	Group                 string   `json:"group,omitempty"`
	Remark                string   `json:"remark,omitempty"`
	Disabled              bool     `json:"disabled"`
	ExpiresAt             string   `json:"expires_at,omitempty"`
	Status                string   `json:"status"`
	Host                  string   `json:"host"`
	Port                  int      `json:"port"`
	Username              string   `json:"username"`
	AuthMethods           []string `json:"auth_methods"`
	InsecureIgnoreHostKey bool     `json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string   `json:"host_key_fingerprint"`
	KnownHostsPath        string   `json:"known_hosts_path"`
	Static                bool     `json:"static"`
}

type HostRecord struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Group    string `json:"group,omitempty"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Remark   string `json:"remark,omitempty"`
	Disabled bool   `json:"disabled"`
}

type HostView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Group        string `json:"group,omitempty"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Remark       string `json:"remark,omitempty"`
	Disabled     bool   `json:"disabled"`
	Status       string `json:"status"`
	AccountCount int    `json:"account_count"`
	Static       bool   `json:"static"`
}

type DatabaseProxyView struct {
	Name                 string                           `json:"name"`
	Enabled              bool                             `json:"enabled"`
	Protocol             string                           `json:"protocol"`
	ListenAddr           string                           `json:"listen_addr"`
	UpstreamAddr         string                           `json:"upstream_addr"`
	Remark               string                           `json:"remark,omitempty"`
	AccountCount         int                              `json:"account_count"`
	AllowedUsersEnforced bool                             `json:"allowed_users_enforced"`
	AllowedUsers         []string                         `json:"allowed_users,omitempty"`
	QueryPolicy          config.DatabaseQueryPolicyConfig `json:"query_policy"`
	Static               bool                             `json:"static"`
}

type DatabaseAccountView struct {
	Username     string `json:"username"`
	Database     string `json:"database,omitempty"`
	Remark       string `json:"remark,omitempty"`
	Disabled     bool   `json:"disabled"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Static       bool   `json:"static"`
}

func NewStaticStore(cfg *config.Config) (*StaticStore, error) {
	store := &StaticStore{
		cfg:              cfg,
		users:            make(map[string]config.User, len(cfg.Users)),
		userPublicKeys:   make(map[string][]ssh.PublicKey, len(cfg.Users)),
		hosts:            make(map[string]HostRecord),
		runtimeHosts:     make(map[string]struct{}),
		hostsFile:        hostsFileForTargets(cfg.TargetsFile),
		dbProxies:        make(map[string]config.DatabaseProxyConfig),
		runtimeDBProxies: make(map[string]struct{}),
		dbProxiesFile:    dbProxiesFileForTargets(cfg.TargetsFile),
		targets:          make(map[string]config.Target),
		runtimeTargets:   make(map[string]struct{}),
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
	runtimeDBProxies, err := loadRuntimeDBProxies(store.dbProxiesFile)
	if err != nil {
		return nil, err
	}
	for _, proxy := range runtimeDBProxies {
		proxy = normalizeDatabaseProxy(proxy)
		if err := validateDatabaseProxy(proxy); err != nil {
			return nil, err
		}
		store.dbProxies[proxy.Name] = proxy
		store.runtimeDBProxies[proxy.Name] = struct{}{}
	}
	store.syncConfigDBProxiesLocked()
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
	login := ParseLoginName(username)
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[login.Username]
	if !ok || user.Password == "" || user.Password != password {
		return model.User{}, errors.New("invalid username or password")
	}
	return s.userForLoginLocked(login, user)
}

func (s *StaticStore) AuthenticatePublicKey(_ context.Context, username string, key ssh.PublicKey) (model.User, error) {
	login := ParseLoginName(username)
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[login.Username]
	if !ok {
		return model.User{}, errors.New("invalid username or public key")
	}
	for _, allowed := range s.userPublicKeys[login.Username] {
		if publicKeysEqual(allowed, key) {
			return s.userForLoginLocked(login, user)
		}
	}
	return model.User{}, errors.New("invalid username or public key")
}

func (s *StaticStore) userForLoginLocked(login LoginName, user config.User) (model.User, error) {
	if login.TargetID != "" {
		if _, ok := s.targets[login.TargetID]; !ok {
			return model.User{}, fmt.Errorf("target %q not found", login.TargetID)
		}
	}
	return model.User{ID: configUserID(user), Username: user.Username, RequestedTargetID: login.TargetID}, nil
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

func (s *StaticStore) DatabaseProxies() []DatabaseProxyView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]DatabaseProxyView, 0, len(s.dbProxies))
	for _, proxy := range s.dbProxies {
		out = append(out, s.databaseProxyViewLocked(proxy))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *StaticStore) DatabaseProxyConfigs() []config.DatabaseProxyConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	proxies := make([]config.DatabaseProxyConfig, 0, len(s.dbProxies))
	for _, proxy := range s.dbProxies {
		proxies = append(proxies, proxy)
	}
	sort.Slice(proxies, func(i, j int) bool { return proxies[i].Name < proxies[j].Name })
	return proxies
}

func (s *StaticStore) DatabaseProxy(name string) (DatabaseProxyView, error) {
	name = strings.TrimSpace(name)
	s.mu.RLock()
	defer s.mu.RUnlock()
	proxy, ok := s.dbProxies[name]
	if !ok {
		return DatabaseProxyView{}, fmt.Errorf("%w: %q", ErrDBProxyNotFound, name)
	}
	return s.databaseProxyViewLocked(proxy), nil
}

func (s *StaticStore) AddDatabaseProxy(proxy config.DatabaseProxyConfig) (DatabaseProxyView, error) {
	proxy = normalizeDatabaseProxy(proxy)
	if err := validateDatabaseProxy(proxy); err != nil {
		return DatabaseProxyView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.dbProxies[proxy.Name]; exists {
		return DatabaseProxyView{}, fmt.Errorf("database proxy %q already exists", proxy.Name)
	}
	s.dbProxies[proxy.Name] = proxy
	s.runtimeDBProxies[proxy.Name] = struct{}{}
	if err := s.saveRuntimeDBProxiesLocked(); err != nil {
		delete(s.dbProxies, proxy.Name)
		delete(s.runtimeDBProxies, proxy.Name)
		return DatabaseProxyView{}, err
	}
	s.syncConfigDBProxiesLocked()
	return s.databaseProxyViewLocked(proxy), nil
}

func (s *StaticStore) UpdateDatabaseProxy(name string, proxy config.DatabaseProxyConfig) (DatabaseProxyView, error) {
	name = strings.TrimSpace(name)
	proxy.Name = strings.TrimSpace(proxy.Name)
	if proxy.Name == "" {
		proxy.Name = name
	}
	proxy = normalizeDatabaseProxy(proxy)
	if proxy.Name != name {
		return DatabaseProxyView{}, fmt.Errorf("database proxy name mismatch: path %q, body %q", name, proxy.Name)
	}
	if err := validateDatabaseProxy(proxy); err != nil {
		return DatabaseProxyView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, exists := s.dbProxies[name]
	if !exists {
		return DatabaseProxyView{}, fmt.Errorf("%w: %q", ErrDBProxyNotFound, name)
	}
	if len(proxy.AllowedUsers) == 0 && len(proxy.Accounts) == 0 {
		proxy.AllowedUsers = append([]string(nil), existing.AllowedUsers...)
		proxy.Accounts = append([]config.DatabaseAccountConfig(nil), existing.Accounts...)
	}
	_, wasRuntime := s.runtimeDBProxies[name]
	s.dbProxies[name] = proxy
	s.runtimeDBProxies[name] = struct{}{}
	if err := s.saveRuntimeDBProxiesLocked(); err != nil {
		s.dbProxies[name] = existing
		if !wasRuntime {
			delete(s.runtimeDBProxies, name)
		}
		return DatabaseProxyView{}, err
	}
	s.syncConfigDBProxiesLocked()
	return s.databaseProxyViewLocked(proxy), nil
}

func (s *StaticStore) DeleteDatabaseProxy(name string) error {
	name = strings.TrimSpace(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	proxy, exists := s.dbProxies[name]
	if !exists {
		return fmt.Errorf("%w: %q", ErrDBProxyNotFound, name)
	}
	delete(s.dbProxies, name)
	delete(s.runtimeDBProxies, name)
	if err := s.saveRuntimeDBProxiesLocked(); err != nil {
		s.dbProxies[name] = proxy
		s.runtimeDBProxies[name] = struct{}{}
		return err
	}
	s.syncConfigDBProxiesLocked()
	return nil
}

func (s *StaticStore) DatabaseAccounts(proxyName string) ([]DatabaseAccountView, error) {
	proxyName = strings.TrimSpace(proxyName)
	s.mu.RLock()
	defer s.mu.RUnlock()
	proxy, ok := s.dbProxies[proxyName]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrDBProxyNotFound, proxyName)
	}
	return databaseAccountViews(proxy, false), nil
}

func (s *StaticStore) AddDatabaseAccount(proxyName string, account config.DatabaseAccountConfig) (DatabaseAccountView, error) {
	return s.upsertDatabaseAccount(proxyName, "", account)
}

func (s *StaticStore) UpdateDatabaseAccount(proxyName, username string, account config.DatabaseAccountConfig) (DatabaseAccountView, error) {
	return s.upsertDatabaseAccount(proxyName, username, account)
}

func (s *StaticStore) upsertDatabaseAccount(proxyName, username string, account config.DatabaseAccountConfig) (DatabaseAccountView, error) {
	proxyName = strings.TrimSpace(proxyName)
	username = strings.TrimSpace(username)
	account = normalizeDatabaseAccount(account)
	if account.Username == "" {
		account.Username = username
	}
	if username != "" && account.Username != username {
		return DatabaseAccountView{}, fmt.Errorf("database account username mismatch: path %q, body %q", username, account.Username)
	}
	if err := validateDatabaseAccount(account); err != nil {
		return DatabaseAccountView{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	proxy, exists := s.dbProxies[proxyName]
	if !exists {
		return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBProxyNotFound, proxyName)
	}
	previous := proxy
	accounts := databaseAccounts(proxy)
	index := -1
	for i := range accounts {
		if accounts[i].Username == account.Username {
			index = i
			break
		}
	}
	if username != "" && index < 0 {
		return DatabaseAccountView{}, fmt.Errorf("%w: %q", ErrDBAccountNotFound, username)
	}
	if index >= 0 {
		accounts[index] = account
	} else {
		accounts = append(accounts, account)
	}
	proxy.Accounts = accounts
	proxy.AllowedUsers = allowedUsersFromAccounts(accounts)
	s.dbProxies[proxyName] = proxy
	s.runtimeDBProxies[proxyName] = struct{}{}
	if err := s.saveRuntimeDBProxiesLocked(); err != nil {
		s.dbProxies[proxyName] = previous
		return DatabaseAccountView{}, err
	}
	s.syncConfigDBProxiesLocked()
	return databaseAccountView(proxy, account, false), nil
}

func (s *StaticStore) DeleteDatabaseAccount(proxyName, username string) error {
	proxyName = strings.TrimSpace(proxyName)
	username = strings.TrimSpace(username)
	s.mu.Lock()
	defer s.mu.Unlock()
	proxy, exists := s.dbProxies[proxyName]
	if !exists {
		return fmt.Errorf("%w: %q", ErrDBProxyNotFound, proxyName)
	}
	previous := proxy
	accounts := databaseAccounts(proxy)
	next := accounts[:0]
	removed := false
	for _, account := range accounts {
		if account.Username == username {
			removed = true
			continue
		}
		next = append(next, account)
	}
	if !removed {
		return fmt.Errorf("%w: %q", ErrDBAccountNotFound, username)
	}
	proxy.Accounts = next
	proxy.AllowedUsers = allowedUsersFromAccounts(next)
	s.dbProxies[proxyName] = proxy
	s.runtimeDBProxies[proxyName] = struct{}{}
	if err := s.saveRuntimeDBProxiesLocked(); err != nil {
		s.dbProxies[proxyName] = previous
		return err
	}
	s.syncConfigDBProxiesLocked()
	return nil
}

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

func (s *StaticStore) saveRuntimeTargetsLocked() error {
	return saveRuntimeTargets(s.cfg.TargetsFile, s.runtimeTargetsSnapshotLocked())
}

func (s *StaticStore) saveRuntimeHostsLocked() error {
	return saveRuntimeHosts(s.hostsFile, s.runtimeHostsSnapshotLocked())
}

func (s *StaticStore) saveRuntimeDBProxiesLocked() error {
	return saveRuntimeDBProxies(s.dbProxiesFile, s.runtimeDBProxiesSnapshotLocked())
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

func (s *StaticStore) runtimeDBProxiesSnapshotLocked() []config.DatabaseProxyConfig {
	proxies := make([]config.DatabaseProxyConfig, 0, len(s.runtimeDBProxies))
	for name := range s.runtimeDBProxies {
		proxy, ok := s.dbProxies[name]
		if ok {
			proxies = append(proxies, proxy)
		}
	}
	sort.Slice(proxies, func(i, j int) bool { return proxies[i].Name < proxies[j].Name })
	return proxies
}

func (s *StaticStore) databaseProxyViewLocked(proxy config.DatabaseProxyConfig) DatabaseProxyView {
	accounts := databaseAccountViews(proxy, false)
	return DatabaseProxyView{
		Name:                 proxy.Name,
		Enabled:              proxy.Enabled,
		Protocol:             proxy.Protocol,
		ListenAddr:           proxy.ListenAddr,
		UpstreamAddr:         proxy.UpstreamAddr,
		Remark:               proxy.Remark,
		AccountCount:         len(accounts),
		AllowedUsersEnforced: len(accounts) > 0,
		AllowedUsers:         allowedUsersFromAccounts(databaseAccounts(proxy)),
		QueryPolicy:          proxy.QueryPolicy,
		Static:               false,
	}
}

func (s *StaticStore) syncConfigDBProxiesLocked() {
	proxies := make([]config.DatabaseProxyConfig, 0, len(s.dbProxies))
	for _, proxy := range s.dbProxies {
		proxies = append(proxies, proxy)
	}
	sort.Slice(proxies, func(i, j int) bool { return proxies[i].Name < proxies[j].Name })
	s.cfg.DatabaseProxies = proxies
}

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
	return ok && host.Disabled
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

func ParseLoginName(username string) LoginName {
	for _, sep := range []string{"+", "#", "@"} {
		if left, right, ok := strings.Cut(username, sep); ok && left != "" && right != "" {
			return LoginName{Username: left, TargetID: right}
		}
	}
	return LoginName{Username: username}
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

func loadRuntimeDBProxies(path string) ([]config.DatabaseProxyConfig, error) {
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
	var proxies []config.DatabaseProxyConfig
	if err := json.Unmarshal(raw, &proxies); err != nil {
		return nil, fmt.Errorf("load runtime database proxies %q: %w", path, err)
	}
	return proxies, nil
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

func saveRuntimeDBProxies(path string, proxies []config.DatabaseProxyConfig) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(proxies, "", "  ")
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

func dbProxiesFileForTargets(targetsFile string) string {
	if strings.TrimSpace(targetsFile) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(targetsFile), "database_proxies.json")
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

func normalizeDatabaseProxy(proxy config.DatabaseProxyConfig) config.DatabaseProxyConfig {
	proxy.Name = strings.TrimSpace(proxy.Name)
	proxy.Protocol = strings.ToLower(strings.TrimSpace(proxy.Protocol))
	proxy.ListenAddr = strings.TrimSpace(proxy.ListenAddr)
	proxy.UpstreamAddr = strings.TrimSpace(proxy.UpstreamAddr)
	proxy.Remark = strings.TrimSpace(proxy.Remark)
	if proxy.Protocol == "postgresql" || proxy.Protocol == "pg" {
		proxy.Protocol = "postgres"
	}
	if proxy.Protocol == "" {
		proxy.Protocol = "mysql"
	}
	if proxy.Name == "" {
		proxy.Name = HostResourceID(proxy.UpstreamAddr, 0, "")
	}
	proxy.Accounts = databaseAccounts(proxy)
	proxy.AllowedUsers = allowedUsersFromAccounts(proxy.Accounts)
	return proxy
}

func validateDatabaseProxy(proxy config.DatabaseProxyConfig) error {
	if proxy.Name == "" || proxy.ListenAddr == "" || proxy.UpstreamAddr == "" {
		return fmt.Errorf("database proxy %q is missing name, listen_addr, or upstream_addr", proxy.Name)
	}
	if strings.ContainsAny(proxy.Name, `/\.`) {
		return fmt.Errorf("database proxy name %q contains unsupported characters", proxy.Name)
	}
	switch proxy.Protocol {
	case "mysql", "postgres", "tcp":
	default:
		return fmt.Errorf("database proxy %q has unsupported protocol %q", proxy.Name, proxy.Protocol)
	}
	if _, _, err := net.SplitHostPort(proxy.ListenAddr); err != nil {
		return fmt.Errorf("database proxy %q has invalid listen_addr %q: %w", proxy.Name, proxy.ListenAddr, err)
	}
	if _, _, err := net.SplitHostPort(proxy.UpstreamAddr); err != nil {
		return fmt.Errorf("database proxy %q has invalid upstream_addr %q: %w", proxy.Name, proxy.UpstreamAddr, err)
	}
	for _, account := range databaseAccounts(proxy) {
		if err := validateDatabaseAccount(account); err != nil {
			return fmt.Errorf("database proxy %q: %w", proxy.Name, err)
		}
	}
	return nil
}

func normalizeDatabaseAccount(account config.DatabaseAccountConfig) config.DatabaseAccountConfig {
	account.Username = strings.TrimSpace(account.Username)
	account.Database = strings.TrimSpace(account.Database)
	account.Remark = strings.TrimSpace(account.Remark)
	return account
}

func validateDatabaseAccount(account config.DatabaseAccountConfig) error {
	if account.Username == "" {
		return errors.New("database account username is required")
	}
	if strings.ContainsAny(account.Username, `/\`) {
		return fmt.Errorf("database account username %q contains unsupported characters", account.Username)
	}
	return nil
}

func databaseAccounts(proxy config.DatabaseProxyConfig) []config.DatabaseAccountConfig {
	seen := make(map[string]struct{}, len(proxy.AllowedUsers)+len(proxy.Accounts))
	accounts := make([]config.DatabaseAccountConfig, 0, len(proxy.AllowedUsers)+len(proxy.Accounts))
	for _, account := range proxy.Accounts {
		account = normalizeDatabaseAccount(account)
		if account.Username == "" {
			continue
		}
		if _, ok := seen[account.Username]; ok {
			continue
		}
		seen[account.Username] = struct{}{}
		accounts = append(accounts, account)
	}
	for _, username := range proxy.AllowedUsers {
		account := normalizeDatabaseAccount(config.DatabaseAccountConfig{Username: username})
		if account.Username == "" {
			continue
		}
		if _, ok := seen[account.Username]; ok {
			continue
		}
		seen[account.Username] = struct{}{}
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].Username < accounts[j].Username })
	return accounts
}

func allowedUsersFromAccounts(accounts []config.DatabaseAccountConfig) []string {
	users := make([]string, 0, len(accounts))
	for _, account := range accounts {
		account = normalizeDatabaseAccount(account)
		if account.Username != "" && !account.Disabled {
			users = append(users, account.Username)
		}
	}
	sort.Strings(users)
	return users
}

func databaseAccountViews(proxy config.DatabaseProxyConfig, static bool) []DatabaseAccountView {
	accounts := databaseAccounts(proxy)
	out := make([]DatabaseAccountView, 0, len(accounts))
	for _, account := range accounts {
		out = append(out, databaseAccountView(proxy, account, static))
	}
	return out
}

func databaseAccountView(proxy config.DatabaseProxyConfig, account config.DatabaseAccountConfig, static bool) DatabaseAccountView {
	return DatabaseAccountView{
		Username:     account.Username,
		Database:     account.Database,
		Remark:       account.Remark,
		Disabled:     account.Disabled,
		ResourceType: model.ResourceTypeDatabaseAccount,
		ResourceID:   rbaccheck.DatabaseAccountResourceID(proxy.Name, proxy.ListenAddr, account.Username),
		Static:       static,
	}
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
		Disabled: host.Disabled,
		Status:   status,
		Static:   static,
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
		Disabled:              target.Disabled,
		ExpiresAt:             target.ExpiresAt,
		Status:                status,
		Host:                  target.Host,
		Port:                  target.Port,
		Username:              target.Username,
		AuthMethods:           targetAuthMethods(target),
		InsecureIgnoreHostKey: target.InsecureIgnoreHostKey,
		HostKeyFingerprint:    target.HostKeyFingerprint,
		KnownHostsPath:        target.KnownHostsPath,
		Static:                static,
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
