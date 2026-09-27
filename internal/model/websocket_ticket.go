package model

import "time"

// WebSocketTicket is a short-lived, target-bound, single-use browser terminal
// credential.  Its raw value is intentionally never persisted.
type WebSocketTicket struct {
	ID           string     `gorm:"primaryKey;size:64"`
	SessionID    string     `gorm:"index;size:64;not null"`
	Purpose      string     `gorm:"index;size:32;not null"`
	TargetID     string     `gorm:"index;size:64;not null"`
	ConnectionID string     `gorm:"index;size:64"`
	SecretHash   string     `gorm:"uniqueIndex:idx_websocket_tickets_secret_hash_active,priority:1;size:64;not null"`
	ExpiresAt    time.Time  `gorm:"index;not null"`
	ConsumedAt   *time.Time `gorm:"index"`
	FullAudit
}

func (WebSocketTicket) TableName() string { return "websocket_tickets" }
