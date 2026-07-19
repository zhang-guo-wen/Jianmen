package model

import "time"

const (
	AccessRequestPending   = "pending"
	AccessRequestApproved  = "approved"
	AccessRequestRejected  = "rejected"
	AccessRequestCancelled = "cancelled"
)

// AccessRequest is an explicit, time-bounded approval gate. Approval is
// additional to RBAC and never replaces normal action/resource authorization.
type AccessRequest struct {
	ID              string     `gorm:"primaryKey;size:64" json:"id"`
	RequesterID     string     `gorm:"index;size:64;not null" json:"requester_id"`
	ResourceType    string     `gorm:"index:idx_access_requests_resource,priority:1;size:64;not null" json:"resource_type"`
	ResourceID      string     `gorm:"index:idx_access_requests_resource,priority:2;size:64;not null" json:"resource_id"`
	Protocol        string     `gorm:"index;size:32;not null" json:"protocol"`
	ActionsJSON     string     `gorm:"type:text;not null" json:"-"`
	Reason          string     `gorm:"type:text;not null" json:"reason"`
	Status          string     `gorm:"index;size:32;not null" json:"status"`
	RequestedAt     time.Time  `gorm:"index;not null" json:"requested_at"`
	AccessStartsAt  *time.Time `gorm:"index" json:"access_starts_at,omitempty"`
	AccessExpiresAt *time.Time `gorm:"index" json:"access_expires_at,omitempty"`
	DecidedBy       string     `gorm:"index;size:64" json:"decided_by,omitempty"`
	DecidedAt       *time.Time `json:"decided_at,omitempty"`
	DecisionRemark  string     `gorm:"type:text" json:"decision_remark,omitempty"`
	CancelledAt     *time.Time `json:"cancelled_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}
