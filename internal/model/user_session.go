package model

import "time"

// UserSessionAuthDetail 用户会话授权详情，用于审计弹窗展示。
type UserSessionAuthDetail struct {
	ID                string     `json:"id"`
	SessionID         string     `json:"session_id"`
	SessionType       string     `json:"session_type"`
	AuthorizationType string     `json:"authorization_type"`
	UserID            string     `json:"user_id"`
	Username          string     `json:"username"`
	AuthorizedBy      string     `json:"authorized_by,omitempty"`
	StartsAt          AuditTime  `json:"starts_at"`
	ExpiresAt         *AuditTime `json:"expires_at,omitempty"`
	Remark            string     `json:"remark,omitempty"`
	Status            string     `json:"status"`
	EffectiveStatus   string     `json:"effective_status"`
}

type UserSession struct {
	ID         string     `gorm:"primaryKey;size:64" json:"id"`
	UserID     string     `gorm:"index;index:idx_user_sessions_user_type_status,priority:1;size:64;not null" json:"user_id"`
	User       User       `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	SessionSeq int        `gorm:"not null;uniqueIndex:idx_user_sessions_session_seq" json:"session_seq"`
	SessionID  string     `gorm:"size:5;not null;uniqueIndex:idx_user_sessions_session_id" json:"session_id"`
	Type       string     `gorm:"size:16;not null;default:permanent;index:idx_user_sessions_user_type_status,priority:2" json:"type"`
	Status     string     `gorm:"size:16;not null;default:active;index:idx_user_sessions_user_type_status,priority:3" json:"status"`
	ExpiresAt  *time.Time `gorm:"index" json:"expires_at,omitempty"`
	CreatedBy  string     `gorm:"size:128" json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (UserSession) TableName() string {
	return "user_sessions"
}
