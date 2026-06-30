package model

import "time"

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
