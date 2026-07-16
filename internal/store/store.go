package store

import (
	"context"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/config"
	"jianmen/internal/model"
)

type LoginName struct {
	ResourceID string // 紧凑格式中的资源ID部分 (4位)
	SessionID  string // 紧凑格式中的会话ID部分 (5位)
}

type UserView struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type HostRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Group   string `json:"group"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Remark  string `json:"remark"`
	Status  string `json:"status"`
}

type HostView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Group        string `json:"group"`
	Address      string `json:"address"`
	Port         int    `json:"port"`
	Remark       string `json:"remark"`
	Status       string `json:"status"`
	AccountCount int    `json:"account_count"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	CanManage    bool   `json:"can_manage"`
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
	CanManage             bool     `json:"can_manage"`
}

// TargetConfig carries enough info to dial a target host via SSH.
type TargetConfig struct {
	ID                    string
	Name                  string
	HostName              string
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

// Expired reports whether the target account has passed its configured expiry.
func (t TargetConfig) Expired(now time.Time) bool {
	if strings.TrimSpace(t.ExpiresAt) == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, t.ExpiresAt)
	return err == nil && !now.Before(expiresAt)
}

type DatabaseInstanceView struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	Address      string `json:"address"`
	Port         int    `json:"port"`
	Group        string `json:"group,omitempty"`
	Remark       string `json:"remark,omitempty"`
	Status       string `json:"status"`
	AccountCount int    `json:"account_count"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	CanManage    bool   `json:"can_manage"`
}

type DatabaseAccountView struct {
	ID          string     `json:"id"`
	InstanceID  string     `json:"instance_id"`
	UniqueName  string     `json:"unique_name"`
	Username    string     `json:"username"`
	Group       string     `json:"group,omitempty"`
	Remark      string     `json:"remark,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Status      string     `json:"status"`
	ResourceID  string     `json:"resource_id,omitempty"`
	ResourceSeq int        `json:"resource_seq,omitempty"`
	CreatedAt   string     `json:"created_at,omitempty"`
	UpdatedAt   string     `json:"updated_at,omitempty"`
	CanManage   bool       `json:"can_manage"`
}

type ApplicationView struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	AppGroup       string `json:"group"`
	ListenPort     int    `json:"listen_port"`
	InternalScheme string `json:"internal_scheme"`
	InternalHost   string `json:"internal_host"`
	InternalPort   int    `json:"internal_port"`
	Remark         string `json:"remark,omitempty"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	CanManage      bool   `json:"can_manage"`
}

type PlatformAccountView struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	PlatformName string     `json:"platform_name"`
	URL          string     `json:"url,omitempty"`
	Category     string     `json:"category,omitempty"`
	Group        string     `json:"group,omitempty"`
	Username     string     `json:"username"`
	HasPassword  bool       `json:"has_password"`
	HasTOTP      bool       `json:"has_totp"`
	Remark       string     `json:"remark,omitempty"`
	OwnerID      string     `json:"owner_id"`
	OwnerName    string     `json:"owner_name,omitempty"`
	Visibility   string     `json:"visibility"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
}

type PlatformAccountShareView struct {
	ID                string     `json:"id"`
	PlatformAccountID string     `json:"platform_account_id"`
	UserID            string     `json:"user_id,omitempty"`
	Username          string     `json:"username,omitempty"`
	RoleID            string     `json:"role_id,omitempty"`
	RoleName          string     `json:"role_name,omitempty"`
	AccessLevel       string     `json:"access_level"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedAt         string     `json:"created_at"`
}

type SessionView struct {
	ID         string     `json:"id"`
	UserID     string     `json:"user_id"`
	Username   string     `json:"username"`
	SessionSeq int        `json:"session_seq"`
	SessionID  string     `json:"session_id"`
	Type       string     `json:"type"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	CreatedBy  string     `json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

var (
	ErrTargetNotFound          = errSentinel("target not found")
	ErrHostNotFound            = errSentinel("host not found")
	ErrDBProxyNotFound         = errSentinel("database proxy not found")
	ErrDBAccountNotFound       = errSentinel("database account not found")
	ErrDBInstanceNotFound      = errSentinel("database instance not found")
	ErrApplicationNotFound     = errSentinel("application not found")
	ErrPlatformAccountNotFound = errSentinel("platform account not found")
	ErrPlatformShareNotFound   = errSentinel("platform account share not found")
	ErrTargetUnavailable       = errSentinel("target unavailable")
)

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }

func errSentinel(msg string) error { return &sentinelError{msg: msg} }

// AuditListParams 审计列表查询参数。
type AuditListParams struct {
	Protocol string // 空表示不过滤，可逗号分隔多个协议
	Search   string // 模糊搜索用户名/目标名
	Date     string // YYYY-MM-DD 格式
	Page     int
	Size     int
}

// AuditSessionView 审计列表视图。
type AuditSessionView struct {
	ID              string `json:"id"`
	Username        string `json:"username"`
	Protocol        string `json:"protocol"`
	ProtocolSubtype string `json:"protocol_subtype,omitempty"`
	TargetName      string `json:"target_name"`
	TargetAddress   string `json:"target_address,omitempty"`
	AccountName     string `json:"account_name,omitempty"`
	AccountUsername string `json:"account_username,omitempty"`
	ClientIP        string `json:"client_ip"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at,omitempty"`
	State           string `json:"state"`
	ReplayDir       string `json:"replay_dir,omitempty"`
	LogCount        int64  `json:"log_count"`
}

// PageOpts 分页参数。
type PageOpts struct {
	Limit  int
	Offset int
}

// PlatformAccountListParams 平台账号列表查询参数。
type PlatformAccountListParams struct {
	Search     string // 模糊搜索名称、平台、用户名
	OwnerID    string // 按所有者过滤
	Visibility string // private / shared
	Platform   string // 按平台名称过滤
	Category   string // 按分类过滤
	Page       int
	PageSize   int
	UserID     string   // 当前用户 ID（用于可见性过滤）
	RoleIDs    []string // 当前用户角色 ID 列表
	IsAdmin    bool     // 是否管理员（可看全域）
}

// Store abstracts runtime data access. Implementations may back with
// JSON files (StaticAdapter) or a relational database (DBStore).
type Store interface {
	Authenticate(ctx context.Context, username, password string) (model.User, error)
	AuthenticatePublicKey(ctx context.Context, username string, key ssh.PublicKey) (model.User, error)
	Users() []UserView
	UserPreference(ctx context.Context, userID string) (model.UserPreference, error)
	SaveUserPreference(ctx context.Context, preference model.UserPreference) (model.UserPreference, error)
	CreateConnectionPassword(ctx context.Context, credential model.ConnectionPassword) error
	AuthenticateConnectionPassword(ctx context.Context, userID, resourceType, resourceID, password string) error
	AuthenticateMySQLConnectionPassword(ctx context.Context, userID, resourceID string, salt, response []byte) error
	CreateAIAccessToken(ctx context.Context, token model.AIAccessToken) error
	ListAIAccessTokens(ctx context.Context, userID string) ([]model.AIAccessToken, error)
	AuthenticateAIAccessToken(ctx context.Context, accessHash string, now time.Time) (model.AIAccessToken, error)
	RotateAIAccessToken(ctx context.Context, refreshHash string, replacement model.AIAccessToken, now time.Time) (model.AIAccessToken, error)
	RevokeAIAccessToken(ctx context.Context, userID, tokenID string, now time.Time) error

	Hosts() []HostView
	Host(id string) (HostView, error)
	AddHost(host HostRecord) (HostView, error)
	UpdateHost(id string, host HostRecord) (HostView, error)
	DeleteHost(id string) error

	HostAccounts(hostID string) ([]TargetView, error)
	Targets() []TargetView
	Target(id string) (TargetView, error)
	TargetConfig(id string) (TargetConfig, error)
	AddTarget(target config.Target) (TargetView, error)
	UpdateTarget(id string, target config.Target) (TargetView, error)
	DeleteTarget(id string) error

	DatabaseInstances() []DatabaseInstanceView
	DatabaseInstance(id string) (DatabaseInstanceView, error)
	AddDatabaseInstance(name, protocol, address string, port int, group, remark string) (DatabaseInstanceView, error)
	UpdateDatabaseInstance(id, name, protocol, address string, port int, group, remark, status string) (DatabaseInstanceView, error)
	DeleteDatabaseInstance(id string) error

	InstanceAccounts(instanceID string) ([]DatabaseAccountView, error)
	DatabaseAccounts() ([]DatabaseAccountView, error)
	DatabaseAccount(id string) (DatabaseAccountView, error)
	AddDatabaseAccount(instanceID, username, password, group, remark string, expiresAt *time.Time) (DatabaseAccountView, error)
	UpdateDatabaseAccount(id, username, password, group, remark string, expiresAt *time.Time, status string) (DatabaseAccountView, error)
	DeleteDatabaseAccount(id string) error

	// Application CRUD
	Applications() []ApplicationView
	Application(id string) (ApplicationView, error)
	AddApplication(name, scheme, host string, port, listenPort int, group, remark string) (ApplicationView, error)
	UpdateApplication(id, name, scheme, host string, port, listenPort int, group, remark, status string) (ApplicationView, error)
	DeleteApplication(id string) error

	// PlatformAccount CRUD
	PlatformAccounts(params PlatformAccountListParams) ([]PlatformAccountView, int64, error)
	PlatformAccount(id string) (PlatformAccountView, error)
	AddPlatformAccount(acc model.PlatformAccount) (PlatformAccountView, error)
	UpdatePlatformAccount(id string, acc model.PlatformAccount) (PlatformAccountView, error)
	DeletePlatformAccount(id string) error
	GetPlatformAccountPassword(id string) (string, error)

	// PlatformAccountShare
	PlatformAccountShares(accountID string) ([]PlatformAccountShareView, error)
	AddPlatformAccountShare(share model.PlatformAccountShare) (PlatformAccountShareView, error)
	DeletePlatformAccountShare(accountID, shareID string) error
	GetPlatformAccountSharesForUser(userID string, roleIDs []string) ([]PlatformAccountShareView, error)

	DatabaseAccountByUniqueName(uniqueName string) (*model.DatabaseAccount, error)
	AuthenticateDirect(ctx context.Context, username, password string) (model.User, error)

	DefaultTarget(ctx context.Context, user model.User) (TargetConfig, error)

	UserSessions(userID string) ([]SessionView, error)
	CreateUserSession(session model.UserSession) (*model.UserSession, error)
	DisableUserSession(id string) error
	EnableUserSession(id string) error
	UserSessionByID(sessionID string, userID string) (*model.UserSession, error)

	// -- audit --

	CreateAuditSession(session *model.AuditSession) error
	EndAuditSession(id string) error
	GetAuditSession(id string) (*model.AuditSession, error)
	ListAuditSessions(params AuditListParams) ([]AuditSessionView, int64, error)
	UpdateAuditProtocol(id string, protocol string) error

	CreateAuditSSHCommand(cmd *model.AuditSSHCommand) error
	ListAuditSSHCommands(sessionID string, opts PageOpts) ([]model.AuditSSHCommand, int64, error)

	CreateAuditSFTPEvent(event *model.AuditSFTPEvent) error
	ListAuditSFTPEvents(sessionID string, opts PageOpts) ([]model.AuditSFTPEvent, int64, error)

	CreateAuditDBQuery(query *model.AuditDBQuery) error
	ListAuditDBQueries(sessionID string, opts PageOpts) ([]model.AuditDBQuery, int64, error)
	ListAuditDBQueryEvents(sessionID string) ([]model.AuditDBQuery, error)

	FindUserSessionByCompactUsername(username string) (*model.UserSession, error)
}
