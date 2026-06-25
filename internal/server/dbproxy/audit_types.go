package dbproxy

import "time"

const (
	queryEventTypeStarted  = "query_started"
	queryEventTypeFinished = "query_finished"

	queryStatusSuccess      = "success"
	queryStatusError        = "error"
	queryStatusUnknown      = "unknown"
	queryStatusPolicyDenied = "policy_denied"
)

type DBConnectionMeta struct {
	ID                   string            `json:"id"`
	Name                 string            `json:"name"`
	Protocol             string            `json:"protocol"`
	ClientAddr           string            `json:"client_addr"`
	UpstreamAddr         string            `json:"upstream_addr"`
	StartedAt            string            `json:"started_at"`
	AuthUser             string            `json:"auth_user,omitempty"`
	Database             string            `json:"database,omitempty"`
	ApplicationName      string            `json:"application_name,omitempty"`
	MySQLConnectAttrs    map[string]string `json:"mysql_connect_attrs,omitempty"`
	AuthObservation      string            `json:"auth_observation,omitempty"`
	AllowedUsersEnforced bool              `json:"allowed_users_enforced"`
}

type DBQueryEvent struct {
	Type         string         `json:"type"`
	ConnectionID string         `json:"connection_id"`
	Seq          int64          `json:"seq"`
	Protocol     string         `json:"protocol"`
	SQL          string         `json:"sql,omitempty"`
	QueryKind    string         `json:"query_kind,omitempty"`
	Detail       map[string]any `json:"detail,omitempty"`
	StartedAt    int64          `json:"started_at,omitempty"`
	CompletedAt  int64          `json:"completed_at,omitempty"`
	DurationMs   int64          `json:"duration_ms,omitempty"`
	Status       string         `json:"status,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	RowsAffected *int64         `json:"rows_affected,omitempty"`
	Rows         *int64         `json:"rows,omitempty"`
}

type queryRecord struct {
	seq       int64
	protocol  string
	sql       string
	queryKind string
	detail    map[string]any
	startedAt time.Time
}

type queryFinish struct {
	Status       string
	ErrorCode    string
	ErrorMessage string
	RowsAffected *int64
	Rows         *int64
	Detail       map[string]any
}

type loginObservation struct {
	User            string
	Database        string
	ApplicationName string
	ConnectAttrs    map[string]string
	TLSRequested    bool
	MetadataVisible bool
	Observation     string
}
