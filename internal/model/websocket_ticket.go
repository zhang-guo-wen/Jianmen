package model

import "time"

// WebSocketTicket is a short-lived, target-bound, single-use browser terminal
// credential.  Its raw value is intentionally never persisted.
type WebSocketTicket struct {
	ID         string     `gorm:"primaryKey;size:64"`
	SessionID  string     `gorm:"index;size:64;not null"`
	TargetID   string     `gorm:"index;size:64;not null"`
	SecretHash string     `gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt  time.Time  `gorm:"index;not null"`
	ConsumedAt *time.Time `gorm:"index"`
	CreatedAt  time.Time
}

func (WebSocketTicket) TableName() string { return "websocket_tickets" }
