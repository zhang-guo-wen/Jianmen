package store

import (
	"strings"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/sshhost"
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
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Group              string `json:"group"`
	Address            string `json:"address"`
	Port               int    `json:"port"`
	Protocol           string `json:"protocol"`
	Remark             string `json:"remark"`
	Status             string `json:"status"`
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	KnownHosts         string `json:"known_hosts,omitempty"`
}

type HostView struct {
	ID                   string                                                     `json:"id"`
	Name                 string                                                     `json:"name"`
	Group                string                                                     `json:"group"`
	Address              string                                                     `json:"address"`
	Port                 int                                                        `json:"port"`
	Protocol             string                                                     `json:"protocol"`
	Remark               string                                                     `json:"remark"`
	Status               string                                                     `json:"status"`
	LifecycleStatus      string                                                     `json:"-"`
	HostKeyFingerprint   string                                                     `json:"host_key_fingerprint,omitempty"`
	KnownHosts           string                                                     `json:"known_hosts,omitempty"`
	IdentityStatus       string                                                     `json:"identity_status"`
	HostKeyChangeHandler func(change sshhost.Change) (hostDisabled bool, err error) `json:"-"`
	AccountCount         int                                                        `json:"account_count"`
	CreatedAt            string                                                     `json:"created_at"`
	UpdatedAt            string                                                     `json:"updated_at"`
	CanManage            bool                                                       `json:"can_manage"`
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
	LifecycleStatus       string   `json:"-"`
	HostStatus            string   `json:"-"`
	Host                  string   `json:"host"`
	Port                  int      `json:"port"`
	Protocol              string   `json:"protocol"`
	Username              string   `json:"username"`
	Domain                string   `json:"domain,omitempty"`
	AuthMethods           []string `json:"auth_methods"`
	InsecureIgnoreHostKey bool     `json:"insecure_ignore_host_key"`
	HostKeyFingerprint    string   `json:"host_key_fingerprint"`
	KnownHostsPath        string   `json:"known_hosts_path"`
	RDPSecurity           string   `json:"rdp_security,omitempty"`
	RDPIgnoreCertificate  bool     `json:"rdp_ignore_certificate"`
	RDPCertFingerprints   string   `json:"rdp_cert_fingerprints,omitempty"`
	RDPApprovalRequired   bool     `json:"rdp_approval_required"`
	RDPClipboardRead      bool     `json:"rdp_clipboard_read"`
	RDPClipboardWrite     bool     `json:"rdp_clipboard_write"`
	RDPFileUpload         bool     `json:"rdp_file_upload"`
	RDPFileDownload       bool     `json:"rdp_file_download"`
	RDPDriveMapping       bool     `json:"rdp_drive_mapping"`
	CanManage             bool     `json:"can_manage"`
}

// TargetConfig carries secret-bearing connection data for a single target
// account. It must never be serialized to an API response.
type TargetConfig struct {
	ID                    string
	Name                  string
	HostName              string
	Host                  string
	Port                  int
	Protocol              string
	Username              string
	Domain                string
	Password              string
	PrivateKeyPath        string
	PrivateKeyPEM         string
	Passphrase            string
	InsecureIgnoreHostKey bool
	HostKeyFingerprint    string
	KnownHosts            string
	KnownHostsPath        string
	HostKeyChangeHandler  func(change sshhost.Change) (hostDisabled bool, err error)
	RDPSecurity           string
	RDPIgnoreCertificate  bool
	RDPCertFingerprints   string
	RDPApprovalRequired   bool
	RDPClipboardRead      bool
	RDPClipboardWrite     bool
	RDPFileUpload         bool
	RDPFileDownload       bool
	RDPDriveMapping       bool
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
	ID            string `json:"id"`
	Name          string `json:"name"`
	Protocol      string `json:"protocol"`
	Address       string `json:"address"`
	Port          int    `json:"port"`
	TLSMode       string `json:"tls_mode"`
	TLSServerName string `json:"tls_server_name,omitempty"`
	HasTLSCA      bool   `json:"has_tls_ca"`
	Group         string `json:"group,omitempty"`
	Remark        string `json:"remark,omitempty"`
	Status        string `json:"status"`
	AccountCount  int    `json:"account_count"`
	CreatedAt     string `json:"created_at,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
	CanManage     bool   `json:"can_manage"`
}

type DatabaseInstanceInput struct {
	Name          string
	Protocol      string
	Address       string
	Port          int
	TLSMode       string
	TLSServerName string
	TLSCAPEM      *string
	ClearTLSCA    bool
	Group         string
	Remark        string
	Status        string
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

// DatabaseAccountProbeMetadata is deliberately credential-free. Callers must
// authorize against the account before asking for the associated secret.
type DatabaseAccountProbeMetadata struct {
	ID         string
	InstanceID string
	Username   string
	Status     string
	ExpiresAt  *time.Time
	Instance   model.DatabaseInstance
}

type ApplicationView struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	AppGroup       string `json:"group"`
	ListenPort     int    `json:"listen_port"`
	Address        string `json:"address"`
	EntryPath      string `json:"entry_path"`
	InternalScheme string `json:"internal_scheme"`
	InternalHost   string `json:"internal_host"`
	InternalPort   int    `json:"internal_port"`
	Remark         string `json:"remark,omitempty"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	CanManage      bool   `json:"can_manage"`
}

type ApplicationInput struct {
	Name           string
	Address        string
	EntryPath      string
	InternalScheme string
	InternalHost   string
	InternalPort   int
	ListenPort     int
	AppGroup       string
	Remark         string
	Status         string
}

type ContainerEndpointView struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Group           string `json:"group,omitempty"`
	Runtime         string `json:"runtime"`
	ConnectionMode  string `json:"connection_mode"`
	Address         string `json:"address"`
	Port            int    `json:"port,omitempty"`
	HostID          string `json:"host_id,omitempty"`
	HostName        string `json:"host_name,omitempty"`
	HostAddress     string `json:"host_address,omitempty"`
	HostGroup       string `json:"host_group,omitempty"`
	HostRemark      string `json:"host_remark,omitempty"`
	HostAccountID   string `json:"host_account_id,omitempty"`
	HostAccountName string `json:"host_account_name,omitempty"`
	Remark          string `json:"remark,omitempty"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
	CanManage       bool   `json:"can_manage"`
}

type ContainerEndpointListParams struct {
	Page   int
	Size   int
	Query  string
	Status string
}

type ContainerEndpointInput struct {
	ID             string
	Name           string
	Group          string
	Runtime        string
	ConnectionMode string
	Address        string
	Port           int
	HostID         string
	HostAccountID  string
	Remark         string
	Status         string
}

type ContainerRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image,omitempty"`
	State   string `json:"state,omitempty"`
	Status  string `json:"status,omitempty"`
	Ports   string `json:"ports,omitempty"`
	Created string `json:"created,omitempty"`
}

type PlatformAccountView struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	PlatformName string     `json:"platform_name"`
	URL          string     `json:"url,omitempty"`
	Group        string     `json:"group,omitempty"`
	Username     string     `json:"username"`
	HasPassword  bool       `json:"has_password"`
	Remark       string     `json:"remark,omitempty"`
	OwnerID      string     `json:"owner_id"`
	OwnerName    string     `json:"owner_name,omitempty"`
	Status       string     `json:"status"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	CreatedAt    string     `json:"created_at"`
	UpdatedAt    string     `json:"updated_at"`
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
	ErrTargetNotFound                        = errSentinel("target not found")
	ErrHostNotFound                          = errSentinel("host not found")
	ErrDBProxyNotFound                       = errSentinel("database proxy not found")
	ErrDBAccountNotFound                     = errSentinel("database account not found")
	ErrDBInstanceNotFound                    = errSentinel("database instance not found")
	ErrDatabaseProvisioningOperationNotFound = errSentinel("database provisioning operation not found")
	ErrApplicationNotFound                   = errSentinel("application not found")
	ErrContainerEndpointNotFound             = errSentinel("container endpoint not found")
	ErrPlatformAccountNotFound               = errSentinel("platform account not found")
	ErrPlatformShareNotFound                 = errSentinel("platform account share not found")
	ErrTargetUnavailable                     = errSentinel("target unavailable")
)

type sentinelError struct{ msg string }

func (e *sentinelError) Error() string { return e.msg }

func errSentinel(msg string) error { return &sentinelError{msg: msg} }

// AuditListParams 审计列表查询参数。
type AuditListParams struct {
	Protocol        string // 空表示不过滤，可逗号分隔多个协议
	Search          string // 模糊搜索用户名/目标名
	Date            string // 兼容旧接口的 YYYY-MM-DD 过滤
	UserID          string
	AccountID       string
	Outcome         string
	RecordingStatus string
	StartedFrom     *time.Time
	StartedTo       *time.Time
	Page            int
	Size            int
}

// AuditSessionAccessMetadata contains only the fields needed to authorize an
// audit session read. Sensitive replay and session detail fields are excluded.
type AuditSessionAccessMetadata struct {
	ID              string
	Protocol        string
	ProtocolSubtype string
	State           string
}

// AuditSessionView 审计列表视图。
type AuditSessionView struct {
	ID              string `json:"id"`
	UserID          string `json:"user_id,omitempty"`
	Username        string `json:"username"`
	Protocol        string `json:"protocol"`
	ProtocolSubtype string `json:"protocol_subtype,omitempty"`
	ResourceType    string `json:"resource_type,omitempty"`
	ResourceID      string `json:"resource_id,omitempty"`
	HostID          string `json:"host_id,omitempty"`
	AccountID       string `json:"account_id,omitempty"`
	TargetName      string `json:"target_name"`
	TargetAddress   string `json:"target_address,omitempty"`
	AccountName     string `json:"account_name,omitempty"`
	AccountUsername string `json:"account_username,omitempty"`
	ClientIP        string `json:"client_ip"`
	StartedAt       string `json:"started_at"`
	EndedAt         string `json:"ended_at,omitempty"`
	State           string `json:"state"`
	Outcome         string `json:"outcome,omitempty"`
	FailureCode     string `json:"failure_code,omitempty"`
	FailureMessage  string `json:"failure_message,omitempty"`
	RecordingStatus string `json:"recording_status,omitempty"`
	HasReplay       bool   `json:"has_replay"`
	LogCount        int64  `json:"log_count"`
}

// AuditEventListParams controls operation audit log filtering and pagination.
type AuditEventListParams struct {
	Search       string
	Action       string
	ResourceType string
	Date         string
	Page         int
	Size         int
}

// LoginAuditListParams controls login audit log filtering and pagination.
type LoginAuditListParams struct {
	Search  string
	Outcome string
	Date    string
	Page    int
	Size    int
}

// PageOpts 分页参数。
type PageOpts struct {
	Limit  int
	Offset int
}

// PlatformAccountListParams 平台账号列表查询参数。
type PlatformAccountListParams struct {
	Search   string
	Platform string
	Page     int
	PageSize int
	Unpaged  bool
}
