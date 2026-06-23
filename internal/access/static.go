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
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

type StaticStore struct {
	cfg            *config.Config
	mu             sync.RWMutex
	users          map[string]config.User
	userPublicKeys map[string][]ssh.PublicKey
	targets        map[string]config.Target
	staticTargets  map[string]struct{}
	runtimeTargets map[string]struct{}
}

var (
	ErrTargetNotFound     = errors.New("target not found")
	ErrStaticTargetDelete = errors.New("static target cannot be deleted")
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
	Name                  string   `json:"name"`
	Host                  string   `json:"host"`
	Port                  int      `json:"port"`
	Username              string   `json:"username"`
	AuthMethods           []string `json:"auth_methods"`
	InsecureIgnoreHostKey bool     `json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string   `json:"host_key_fingerprint"`
	KnownHostsPath        string   `json:"known_hosts_path"`
	Static                bool     `json:"static"`
}

func NewStaticStore(cfg *config.Config) (*StaticStore, error) {
	store := &StaticStore{
		cfg:            cfg,
		users:          make(map[string]config.User, len(cfg.Users)),
		userPublicKeys: make(map[string][]ssh.PublicKey, len(cfg.Users)),
		targets:        make(map[string]config.Target, len(cfg.Targets)),
		staticTargets:  make(map[string]struct{}, len(cfg.Targets)),
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
	for _, target := range cfg.Targets {
		if err := validateTarget(normalizeTarget(target)); err != nil {
			return nil, err
		}
		target = normalizeTarget(target)
		store.targets[target.ID] = target
		store.staticTargets[target.ID] = struct{}{}
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
	return model.User{ID: user.ID, Username: user.Username, RequestedTargetID: login.TargetID}, nil
}

func (s *StaticStore) DefaultTarget(_ context.Context, user model.User) (config.Target, error) {
	targetID := s.cfg.DefaultTarget
	if user.RequestedTargetID != "" {
		targetID = user.RequestedTargetID
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, ok := s.targets[targetID]
	if !ok {
		return config.Target{}, fmt.Errorf("target %q not found", targetID)
	}
	return target, nil
}

func (s *StaticStore) Users() []UserView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	users := make([]UserView, 0, len(s.users))
	for _, user := range s.users {
		users = append(users, UserView{
			ID:       user.ID,
			Username: user.Username,
		})
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	return users
}

func (s *StaticStore) Targets() []TargetView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	targets := make([]TargetView, 0, len(s.targets))
	for _, target := range s.targets {
		targets = append(targets, targetView(target, s.isStaticTargetLocked(target.ID)))
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	return targets
}

func (s *StaticStore) Target(id string) (TargetView, error) {
	id = strings.TrimSpace(id)
	s.mu.RLock()
	defer s.mu.RUnlock()
	target, ok := s.targets[id]
	if !ok {
		return TargetView{}, fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	return targetView(target, s.isStaticTargetLocked(id)), nil
}

func (s *StaticStore) AddTarget(target config.Target) (TargetView, error) {
	target = normalizeTarget(target)
	if err := validateTarget(target); err != nil {
		return TargetView{}, err
	}
	if target.Password == "" && target.PrivateKeyPath == "" && target.PrivateKeyPEM == "" {
		return TargetView{}, errors.New("target requires password, private_key_path, or private_key_pem")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
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
	return targetView(target, false), nil
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
	if err := validateTarget(target); err != nil {
		return TargetView{}, err
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
	if target.Password == "" && target.PrivateKeyPath == "" && target.PrivateKeyPEM == "" {
		return TargetView{}, errors.New("target requires password, private_key_path, or private_key_pem")
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
	return targetView(target, s.isStaticTargetLocked(id)), nil
}

func (s *StaticStore) DeleteTarget(id string) error {
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	target, exists := s.targets[id]
	if !exists {
		return fmt.Errorf("%w: %q", ErrTargetNotFound, id)
	}
	if s.isStaticTargetLocked(id) {
		return fmt.Errorf("%w: %q", ErrStaticTargetDelete, id)
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

func (s *StaticStore) isStaticTargetLocked(id string) bool {
	_, ok := s.staticTargets[id]
	return ok
}

func ParseLoginName(username string) LoginName {
	for _, sep := range []string{"+", "#", "@"} {
		if left, right, ok := strings.Cut(username, sep); ok && left != "" && right != "" {
			return LoginName{Username: left, TargetID: right}
		}
	}
	return LoginName{Username: username}
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

func normalizeTarget(target config.Target) config.Target {
	target.ID = strings.TrimSpace(target.ID)
	target.Name = strings.TrimSpace(target.Name)
	target.Host = strings.TrimSpace(target.Host)
	target.Username = strings.TrimSpace(target.Username)
	target.PrivateKeyPath = strings.TrimSpace(target.PrivateKeyPath)
	target.HostKeyFingerprint = strings.TrimSpace(target.HostKeyFingerprint)
	target.KnownHostsPath = strings.TrimSpace(target.KnownHostsPath)
	if target.Port == 0 {
		target.Port = 22
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
	return nil
}

func targetView(target config.Target, static bool) TargetView {
	return TargetView{
		ID:                    target.ID,
		Name:                  target.Name,
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

func targetAuthMethods(target config.Target) []string {
	methods := make([]string, 0, 3)
	if target.Password != "" {
		methods = append(methods, "password")
	}
	if target.PrivateKeyPath != "" {
		methods = append(methods, "private_key_path")
	}
	if target.PrivateKeyPEM != "" {
		methods = append(methods, "private_key_pem")
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
