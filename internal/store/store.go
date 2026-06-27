package store

import (
	"context"
	"time"

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
	ResourceSeq           int      `json:"resource_seq"`
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

type DatabaseInstanceView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	Address      string `json:"address"`
	GroupName    string `json:"group_name,omitempty"`
	Remark       string `json:"remark,omitempty"`
	Disabled     bool   `json:"disabled"`
	AccountCount int    `json:"account_count"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
}

type DatabaseAccountView struct {
	ID               string     `json:"id"`
	InstanceID       string     `json:"instance_id"`
	UniqueName       string     `json:"unique_name"`
	UpstreamUsername string     `json:"upstream_username"`
	GroupName        string     `json:"group_name,omitempty"`
	Remark           string     `json:"remark,omitempty"`
	ExpiresAt        *time.Time `json:"expires_at,omitempty"`
	Disabled         bool       `json:"disabled"`
	CreatedAt        string     `json:"created_at,omitempty"`
	UpdatedAt        string     `json:"updated_at,omitempty"`
}

var (
	ErrTargetNotFound      = errSentinel("target not found")
	ErrHostNotFound        = errSentinel("host not found")
	ErrDBProxyNotFound     = errSentinel("database proxy not found")
	ErrDBAccountNotFound   = errSentinel("database account not found")
	ErrDBInstanceNotFound  = errSentinel("database instance not found")
	ErrTargetUnavailable   = errSentinel("target unavailable")
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

	DatabaseInstances() []DatabaseInstanceView
	DatabaseInstance(id string) (DatabaseInstanceView, error)
	AddDatabaseInstance(name, protocol, address, groupName, remark string) (DatabaseInstanceView, error)
	UpdateDatabaseInstance(id, name, protocol, address, groupName, remark string, disabled bool) (DatabaseInstanceView, error)
	DeleteDatabaseInstance(id string) error

	InstanceAccounts(instanceID string) ([]DatabaseAccountView, error)
	DatabaseAccount(id string) (DatabaseAccountView, error)
	AddDatabaseAccount(instanceID, upstreamUsername, upstreamPassword, groupName, remark string, expiresAt *time.Time) (DatabaseAccountView, error)
	UpdateDatabaseAccount(id, upstreamUsername, upstreamPassword, groupName, remark string, expiresAt *time.Time, disabled bool) (DatabaseAccountView, error)
	DeleteDatabaseAccount(id string) error

	DatabaseAccountByUniqueName(uniqueName string) (*model.DatabaseAccount, error)
	AuthenticateDirect(ctx context.Context, username, password string) (model.User, error)

	DefaultTarget(ctx context.Context, user model.User) (TargetConfig, error)
}
