package store

import (
	"context"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

type LoginName struct {
	Username string
	TargetID string
}

type UserView struct {
	ID       string `json:"id"`
	Username string `json:"username"`
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

// TargetConfig carries enough info to dial a target host via SSH.
type TargetConfig struct {
	ID                    string
	Name                  string
	Host                  string
	Port                  int
	Username              string
	Password              string
	PrivateKeyPath        string
	PrivateKeyPEM         string
	Passphrase            string
	InsecureIgnoreHostKey bool
	HostKeyFingerprint    string
	KnownHostsPath        string
	Disabled              bool
	ExpiresAt             string
	HostID                string
}

func (t TargetConfig) Addr() string {
	return formatHostAddress(t.Host, t.Port)
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

var (
	ErrTargetNotFound    = errSentinel("target not found")
	ErrHostNotFound      = errSentinel("host not found")
	ErrDBProxyNotFound   = errSentinel("database proxy not found")
	ErrDBAccountNotFound = errSentinel("database account not found")
	ErrTargetUnavailable = errSentinel("target unavailable")
)

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }

func errSentinel(msg string) error { return &sentinelError{msg: msg} }

// Store abstracts runtime data access. Implementations may back with
// JSON files (StaticAdapter) or a relational database (DBStore).
type Store interface {
	Authenticate(ctx context.Context, username, password string) (model.User, error)
	AuthenticatePublicKey(ctx context.Context, username string, key ssh.PublicKey) (model.User, error)
	Users() []UserView

	Hosts() []HostView
	Host(id string) (HostView, error)
	AddHost(host HostRecord) (HostView, error)
	UpdateHost(id string, host HostRecord) (HostView, error)
	DeleteHost(id string) error

	HostAccounts(hostID string) ([]TargetView, error)
	Targets() []TargetView
	Target(id string) (TargetView, error)
	AddTarget(target config.Target) (TargetView, error)
	UpdateTarget(id string, target config.Target) (TargetView, error)
	DeleteTarget(id string) error

	DatabaseProxies() []DatabaseProxyView
	DatabaseProxyConfigs() []config.DatabaseProxyConfig
	DatabaseProxy(name string) (DatabaseProxyView, error)
	AddDatabaseProxy(proxy config.DatabaseProxyConfig) (DatabaseProxyView, error)
	UpdateDatabaseProxy(name string, proxy config.DatabaseProxyConfig) (DatabaseProxyView, error)
	DeleteDatabaseProxy(name string) error

	DatabaseAccounts(proxyName string) ([]DatabaseAccountView, error)
	AddDatabaseAccount(proxyName string, account config.DatabaseAccountConfig) (DatabaseAccountView, error)
	UpdateDatabaseAccount(proxyName, username string, account config.DatabaseAccountConfig) (DatabaseAccountView, error)
	DeleteDatabaseAccount(proxyName, username string) error

	DefaultTarget(ctx context.Context, user model.User) (TargetConfig, error)
}
