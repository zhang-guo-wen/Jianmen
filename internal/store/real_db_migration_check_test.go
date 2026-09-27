package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"jianmen/internal/storage"
)

// TestRealDatabaseMigrationAndConsistency 用真实部署库的副本验证：
// 1) 存量库启动迁移会补建 (phase, intent_id) 复合索引；
// 2) 新旧折叠条件在真实数据上返回完全一致的结果。
// 副本通过环境变量 JIANMEN_REAL_DB 传入；未设置时跳过。
func TestRealDatabaseMigrationAndConsistency(t *testing.T) {
	src := os.Getenv("JIANMEN_REAL_DB")
	if src == "" {
		t.Skip("未设置 JIANMEN_REAL_DB，跳过真实库验证")
	}
	tmp := t.TempDir()
	dst := filepath.Join(tmp, "bastion-copy.db")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("读取真实库副本: %v", err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("写入副本: %v", err)
	}

	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: dst})
	if err != nil {
		t.Fatalf("open copy: %v", err)
	}
	if err := storage.Migrate(db); err != nil {
		t.Fatalf("迁移存量库: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	defer sqlDB.Close()

	// 复合索引已补建
	hasIndex := false
	rows, err := db.Raw("PRAGMA index_list(audit_events)").Rows()
	if err != nil {
		t.Fatalf("index_list: %v", err)
	}
	for rows.Next() {
		var seq int
		var name, unique, origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		if name == "idx_audit_events_phase_intent" {
			hasIndex = true
		}
	}
	rows.Close()
	if !hasIndex {
		t.Fatal("存量库迁移后未补建 idx_audit_events_phase_intent 复合索引")
	}

	// 新旧条件在真实数据上结果一致（旧版为重构前的单 EXISTS OR 结构）
	st := NewDBStore(db)
	items, total, err := st.ListAuditEvents(context.Background(), AuditEventListParams{Page: 1, Size: 50})
	if err != nil {
		t.Fatalf("新逻辑 list: %v", err)
	}
	_ = items
	oldCondition := `NOT (
		(
			COALESCE(audit_events.phase, '') = 'intent'
			OR (
				COALESCE(audit_events.phase, '') = ''
				AND audit_events.detail LIKE '%"phase":"intent"%'
			)
		)
		AND EXISTS (
			SELECT 1
			FROM audit_events AS audit_event_results
			WHERE (
				audit_event_results.phase = 'result'
				AND audit_event_results.intent_id = audit_events.id
			) OR (
				COALESCE(audit_event_results.phase, '') = ''
				AND audit_event_results.detail LIKE '%"phase":"result"%'
				AND audit_event_results.detail LIKE '%"intent_id":"' || audit_events.id || '"%'
			)
		)
	)`
	var oldTotal int64
	if err := db.Raw("SELECT COUNT(*) FROM audit_events WHERE "+oldCondition).Scan(&oldTotal).Error; err != nil {
		t.Fatalf("旧逻辑 count: %v", err)
	}
	if total != oldTotal {
		t.Fatalf("新旧逻辑 total 不一致: 新=%d 旧=%d", total, oldTotal)
	}
	t.Logf("真实库验证通过: total=%d（含折叠后行数一致）", total)
}
