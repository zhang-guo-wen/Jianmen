package storage

import "testing"

func TestRemoveRDPApprovalMigrationDropsApprovalSchema(t *testing.T) {
	db, err := Open(Config{Driver: DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Transaction(migrateWebRDPAuditSchema); err != nil {
		t.Fatalf("create Web RDP schema: %v", err)
	}
	if err := db.Transaction(migrateRemoveRDPApproval); err != nil {
		t.Fatalf("remove RDP approval schema: %v", err)
	}

	if db.Migrator().HasTable(&accessRequestWebRDPSchema{}) {
		t.Fatal("access_requests table still exists")
	}
	for _, column := range []struct {
		table any
		name  string
	}{
		{table: &hostAccountWebRDPSchema{}, name: "rdp_approval_required"},
		{table: &auditSessionWebRDPSchema{}, name: "access_request_id"},
	} {
		if db.Migrator().HasColumn(column.table, column.name) {
			t.Fatalf("approval column %q still exists", column.name)
		}
	}
}
