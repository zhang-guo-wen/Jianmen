package model

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID              string     `gorm:"primaryKey;size:64" json:"id"`
	Username        string     `gorm:"uniqueIndex;index:idx_users_status_username,priority:2;size:128;not null" json:"username"`
	PasswordHash    string     `gorm:"size:255" json:"-"`
	MySQLNativeHash string     `gorm:"size:40" json:"-"`
	TokenHash       string     `gorm:"index;size:255" json:"-"`
	DisplayName     string     `gorm:"size:128" json:"display_name,omitempty"`
	Email           string     `gorm:"index;size:255" json:"email,omitempty"`
	Status          string     `gorm:"index:idx_users_status_username,priority:1;size:32;not null;default:active" json:"status"`
	LastLoginAt     *time.Time `json:"last_login_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	RequestedTargetID string `gorm:"-" json:"-"`
}

type Session struct {
	ID              string      `gorm:"primaryKey;size:64" json:"id"`
	SID             string      `gorm:"index;size:128" json:"sid,omitempty"`
	UserID          string      `gorm:"index;index:idx_sessions_user_started,priority:1;size:64" json:"user_id,omitempty"`
	User            User        `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"-"`
	UserSessionID   string      `gorm:"index;size:64" json:"user_session_id,omitempty"`
	UserSession     UserSession `gorm:"foreignKey:UserSessionID;constraint:OnUpdate:CASCADE,OnDelete:SET NULL;" json:"-"`
	HostID          string      `gorm:"index;size:64" json:"host_id,omitempty"`
	AccountID       string      `gorm:"index;index:idx_sessions_account_started,priority:1;size:64" json:"account_id,omitempty"`
	TargetID        string      `gorm:"index;index:idx_sessions_target_started,priority:1;size:64" json:"target_id,omitempty"`
	Target          string      `gorm:"size:255" json:"target,omitempty"`
	Protocol        string      `gorm:"index;index:idx_sessions_protocol_started,priority:1;size:32" json:"protocol,omitempty"`
	ProtocolSubtype string      `gorm:"size:64" json:"protocol_subtype,omitempty"`
	UserUsername    string      `gorm:"size:128" json:"user_username,omitempty"`
	AccountUsername string      `gorm:"size:128" json:"account_username,omitempty"`
	HostIP          string      `gorm:"size:128" json:"host_ip,omitempty"`
	ConnIP          string      `gorm:"size:128" json:"conn_ip,omitempty"`
	ConnPort        int         `json:"conn_port,omitempty"`
	ClientIP        string      `gorm:"size:128" json:"client_ip,omitempty"`
	StartedAt       time.Time   `gorm:"index;index:idx_sessions_user_started,priority:2;index:idx_sessions_account_started,priority:2;index:idx_sessions_target_started,priority:2;index:idx_sessions_protocol_started,priority:2;index:idx_sessions_state_started,priority:2" json:"started_at"`
	EndedAt         *time.Time  `gorm:"index" json:"ended_at,omitempty"`
	State           string      `gorm:"index;index:idx_sessions_state_started,priority:1;size:32" json:"state,omitempty"`
	CreatedAt       time.Time   `json:"created_at"`
	UpdatedAt       time.Time   `json:"updated_at"`
}

func NewSession(user User, targetID, target, clientIP string) Session {
	return Session{
		ID:           NewID(),
		UserID:       user.ID,
		User:         user,
		TargetID:     targetID,
		Target:       target,
		ClientIP:     clientIP,
		UserUsername: user.Username,
		StartedAt:    time.Now().UTC(),
		State:        "started",
	}
}

func (u *User) BeforeCreate(_ *gorm.DB) error {
	return ensureID(&u.ID)
}

func (s *Session) BeforeCreate(_ *gorm.DB) error {
	return ensureID(&s.ID)
}

func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}
