package model

import "time"

const (
	AuditArtifactKindRecording = "recording"
	AuditArtifactFormatGuac    = "guac"
)

// AuditArtifact indexes an immutable object held by object storage.
// ObjectKey is never serialized to clients; playback is always mediated by an
// authorized API.
type AuditArtifact struct {
	ID             string     `gorm:"primaryKey;size:64" json:"id"`
	AuditSessionID string     `gorm:"uniqueIndex:uidx_audit_artifacts_session_kind,priority:1;index;size:64;not null" json:"audit_session_id"`
	Kind           string     `gorm:"uniqueIndex:uidx_audit_artifacts_session_kind,priority:2;size:32;not null" json:"kind"`
	Format         string     `gorm:"size:32;not null" json:"format"`
	ObjectKey      string     `gorm:"uniqueIndex;size:1024;not null" json:"-"`
	ContentType    string     `gorm:"size:255" json:"content_type"`
	SizeBytes      int64      `json:"size_bytes"`
	SHA256         string     `gorm:"size:64" json:"sha256,omitempty"`
	Status         string     `gorm:"index;size:32;not null" json:"status"`
	ErrorMessage   string     `gorm:"type:text" json:"-"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}
