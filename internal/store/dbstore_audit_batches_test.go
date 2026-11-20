package store

import (
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"jianmen/internal/service"
)

func TestAuditBatchMySQLQueriesUsePortableSQL(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "test@tcp(127.0.0.1:1)/test?parseTime=true", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	filter := service.AuditBatchFilter{Protocol: "ssh,sftp", BatchID: "b"}
	base := auditBatchScope(db.Table("audit_sessions"), filter)
	var summaries []auditBatchSummary
	list := auditBatchSummariesQuery(base, 20, 0).Find(&summaries).Statement.SQL.String()
	if !strings.Contains(list, "CAST(MAX(ended_at) AS CHAR)") || strings.Contains(list, "ROW_NUMBER") {
		t.Fatalf("bad MySQL batch list SQL: %s", list)
	}
	var rows []service.AuditBatchLogItem
	logs := auditBatchSSHLogQuery(db, filter, "needle").Find(&rows).Statement.SQL.String()
	if strings.Contains(logs, "((SELECT") || strings.Contains(logs, "UNION ALL (SELECT") || !strings.Contains(logs, "UNION ALL SELECT") {
		t.Fatalf("bad MySQL log union: %s", logs)
	}
}
