package model

import (
	"time"
)

// UserSession 用户身份会话，用于连接用户名中的会话ID部分
type UserSession struct {
	ID         string     `gorm:"primaryKey;size:64" json:"id"`
	UserID     string     `gorm:"index;size:64;not null" json:"user_id"`
	User       User       `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"-"`
	SessionSeq int        `gorm:"not null" json:"session_seq"`       // 用户维度自增序号
	SessionID  string     `gorm:"size:5;not null" json:"session_id"` // 5位62进制
	Type       string     `gorm:"size:16;not null;default:permanent" json:"type"` // permanent / temporary
	Status     string     `gorm:"size:16;not null;default:active" json:"status"`   // active / disabled / expired
	ExpiresAt  *time.Time `gorm:"index" json:"expires_at,omitempty"`
	CreatedBy  string     `gorm:"size:128" json:"created_by,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (UserSession) TableName() string {
	return "user_sessions"
}
