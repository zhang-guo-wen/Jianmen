package storage

import (
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestHistoricalMigrationsDeferWebRDPSchema(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	for _, version := range []string{"202606290002", "202607180004"} {
		migration := webRDPMigrationForTest(t, version)
		if err := db.Transaction(migration.Run); err != nil {
			t.Fatalf("run historical migration %s: %v", version, err)
		}
	}

	assertWebRDPColumns(t, db, false)
	for _, table := range []string{"audit_artifacts", "audit_rdp_channel_events", "access_requests"} {
		if db.Migrator().HasTable(table) {
			t.Fatalf("historical migration unexpectedly created future table %q", table)
		}
	}
}

func TestWebRDPAuditMigrationUpgradesExistingSchema(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Transaction(webRDPMigrationForTest(t, "202606290002").Run); err != nil {
		t.Fatalf("create pre-RDP metadata schema: %v", err)
	}
	if err := db.AutoMigrate(&SchemaMigration{}); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	now := time.Now().UTC()
	if err := db.Exec(`INSERT INTO hosts
		(id, name, address, port, status, created_at, updated_at)
		VALUES ('host-1', 'windows', '10.0.0.8', 3389, 'active', ?, ?)`,
		now, now).Error; err != nil {
		t.Fatalf("seed host: %v", err)
	}
	if err := db.Exec(`INSERT INTO host_accounts
		(id, host_id, name, username, status, resource_seq, resource_id, created_at, updated_at)
		VALUES ('account-1', 'host-1', 'administrator', 'Administrator', 'active', 1, 'H001', ?, ?)`,
		now, now).Error; err != nil {
		t.Fatalf("seed host account: %v", err)
	}
	if err := db.Exec(`INSERT INTO websocket_tickets
		(id, session_id, target_id, secret_hash, expires_at, created_at)
		VALUES ('ticket-1', 'session-1', 'account-1', 'legacy-secret-hash', ?, ?)`,
		now.Add(time.Minute), now).Error; err != nil {
		t.Fatalf("seed websocket ticket: %v", err)
	}
	if err := db.Exec(`INSERT INTO audit_sessions
		(id, user_id, username, protocol, target_name, started_at, state, created_at, updated_at)
		VALUES ('audit-1', 'user-1', 'alice', 'ssh', 'legacy-target', ?, 'closed', ?, ?)`,
		now, now, now).Error; err != nil {
		t.Fatalf("seed audit session: %v", err)
	}
	for _, migration := range migrations {
		if migration.Version == webRDPAuditMigrationVersion {
			continue
		}
		if err := db.Create(&SchemaMigration{
			Version: migration.Version, Name: migration.Name, AppliedAt: now,
		}).Error; err != nil {
			t.Fatalf("record previous migration %s: %v", migration.Version, err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate Web RDP schema: %v", err)
	}
	assertWebRDPColumns(t, db, true)
	for _, table := range []string{"audit_artifacts", "audit_rdp_channel_events", "access_requests"} {
		if !db.Migrator().HasTable(table) {
			t.Fatalf("Web RDP migration did not create table %q", table)
		}
	}

	var ticketPurpose string
	if err := db.Table("websocket_tickets").
		Select("purpose").
		Where("id = ?", "ticket-1").
		Scan(&ticketPurpose).Error; err != nil {
		t.Fatalf("load migrated ticket: %v", err)
	}
	if ticketPurpose != "web-terminal" {
		t.Fatalf("legacy ticket purpose = %q, want web-terminal", ticketPurpose)
	}
	var hostProtocol string
	if err := db.Table("hosts").Select("protocol").Where("id = ?", "host-1").Scan(&hostProtocol).Error; err != nil {
		t.Fatalf("load migrated host: %v", err)
	}
	if hostProtocol != "ssh" {
		t.Fatalf("legacy host protocol = %q, want ssh", hostProtocol)
	}
	var accountDefaults struct {
		RDPSecurity         string
		RDPApprovalRequired bool
		RDPClipboardRead    bool
		RDPClipboardWrite   bool
		RDPFileUpload       bool
		RDPFileDownload     bool
		RDPDriveMapping     bool
	}
	if err := db.Table("host_accounts").Where("id = ?", "account-1").Scan(&accountDefaults).Error; err != nil {
		t.Fatalf("load migrated host account: %v", err)
	}
	if accountDefaults.RDPSecurity != "any" ||
		accountDefaults.RDPApprovalRequired ||
		accountDefaults.RDPClipboardRead ||
		accountDefaults.RDPClipboardWrite ||
		accountDefaults.RDPFileUpload ||
		accountDefaults.RDPFileDownload ||
		accountDefaults.RDPDriveMapping {
		t.Fatalf("unsafe migrated RDP account defaults: %#v", accountDefaults)
	}
	var migrationCount int64
	if err := db.Model(&SchemaMigration{}).
		Where("version = ?", webRDPAuditMigrationVersion).
		Count(&migrationCount).Error; err != nil {
		t.Fatalf("load Web RDP migration record: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("Web RDP migration records = %d, want 1", migrationCount)
	}
}

func assertWebRDPColumns(t *testing.T, db *gorm.DB, want bool) {
	t.Helper()
	columns := map[string][]string{
		"hosts": {"protocol"},
		"host_accounts": {
			"domain", "rdp_security", "rdp_ignore_certificate", "rdp_cert_fingerprints",
			"rdp_approval_required", "rdp_clipboard_read", "rdp_clipboard_write",
			"rdp_file_upload", "rdp_file_download", "rdp_drive_mapping",
		},
		"websocket_tickets": {"purpose", "connection_id"},
		"audit_sessions": {
			"resource_type", "resource_id", "host_id", "account_id", "access_request_id",
			"outcome", "failure_code", "failure_message", "policy_snapshot", "recording_status",
		},
		"audit_artifacts": {
			"audit_session_id", "kind", "format", "object_key", "content_type",
			"size_bytes", "sha256", "status", "error_message", "completed_at",
		},
		"audit_rdp_channel_events": {
			"audit_session_id", "timestamp", "channel", "direction", "operation",
			"bytes", "outcome", "reason",
		},
		"access_requests": {
			"requester_id", "resource_type", "resource_id", "protocol", "actions_json",
			"reason", "status", "requested_at", "access_starts_at", "access_expires_at",
			"decided_by", "decided_at", "decision_remark", "cancelled_at",
		},
	}
	for table, fields := range columns {
		for _, field := range fields {
			if got := db.Migrator().HasColumn(table, field); got != want {
				t.Fatalf("column %s.%s present = %v, want %v", table, field, got, want)
			}
		}
	}
}

func webRDPMigrationForTest(t *testing.T, version string) Migration {
	t.Helper()
	for _, migration := range migrations {
		if migration.Version == version {
			return migration
		}
	}
	t.Fatalf("missing migration %s", version)
	return Migration{}
}
