package admin

type sessionListItem struct {
	ID              string  `json:"id"`
	User            string  `json:"user"`
	Target          string  `json:"target"`
	AccountUsername string  `json:"account_username,omitempty"`
	ClientIP        string  `json:"client_ip"`
	StartedAt       string  `json:"started_at"`
	EndedAt         string  `json:"ended_at,omitempty"`
	DurationSeconds float64 `json:"duration_seconds"`
	Protocol        string  `json:"protocol"`
	ProtocolSubtype string  `json:"protocol_subtype"`
	Path            string  `json:"path"`
	HasReplay       bool    `json:"has_replay"`
	ReplaySize      int64   `json:"replay_size"`
}

type dbConnectionListItem struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Protocol     string `json:"protocol"`
	ClientAddr   string `json:"client_addr"`
	UpstreamAddr string `json:"upstream_addr"`
	StartedAt    string `json:"started_at"`
	EndedAt      string `json:"ended_at,omitempty"`
	DurationMs   int64  `json:"duration_ms,omitempty"`
	AccountName  string `json:"account_name,omitempty"`
	InstanceName string `json:"instance_name,omitempty"`
	AuthUser     string `json:"auth_user,omitempty"`
	Path         string `json:"path"`
}

type pageResponse struct {
	Items    any `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

type createUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
}

type updateUserRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Email       *string `json:"email,omitempty"`
	Status      *string `json:"status,omitempty"`
}
