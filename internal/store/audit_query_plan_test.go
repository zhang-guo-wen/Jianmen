package store

import (
	"context"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

// explainAuditEventPlan 返回 ListAuditEvents 计数查询的执行计划文本，
// 用于断言 intent/result 配对折叠条件不会退化为逐行全表扫描（O(N²)）。
func explainAuditEventPlan(t *testing.T, st *DBStore) string {
	t.Helper()
	condition := logicalAuditEventRowsCondition(st.db.Dialector.Name())
	rows, err := st.db.Raw(
		"EXPLAIN QUERY PLAN SELECT COUNT(*) FROM audit_events WHERE "+condition,
	).Rows()
	if err != nil {
		t.Fatalf("explain audit events plan: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate explain rows: %v", err)
	}
	return plan.String()
}

func explainLoginAuditPlan(t *testing.T, st *DBStore) string {
	t.Helper()
	condition := logicalLoginAuditRowsCondition(st.db.Dialector.Name())
	rows, err := st.db.Raw(
		"EXPLAIN QUERY PLAN SELECT COUNT(*) FROM audit_login_logs WHERE "+condition,
	).Rows()
	if err != nil {
		t.Fatalf("explain login audit plan: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, notused int
		var detail string
		if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
			t.Fatalf("scan explain row: %v", err)
		}
		plan.WriteString(detail)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate explain rows: %v", err)
	}
	return plan.String()
}

// TestListAuditEventsPlanAvoidsPerRowFullScan 防止折叠条件回归为
// 「外层每一行都对内层做全表扫描」的 O(N²) 执行计划（数据量超过两千条后
// 操作日志页加载显著变慢）。配对解析必须走 intent_id 索引查找。
func TestListAuditEventsPlanAvoidsPerRowFullScan(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	st := NewDBStore(db)
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := 0; i < 20; i++ {
		intentID := "intent-" + string(rune('a'+i))
		if err := st.CreateAuditEvent(context.Background(), &model.AuditEvent{
			ID: intentID, ActorID: "u1", Action: "update", Phase: "intent", Result: "pending",
			Detail: `{"phase":"intent","result":"pending"}`, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("create intent: %v", err)
		}
		if err := st.CreateAuditEvent(context.Background(), &model.AuditEvent{
			ID: "result-" + intentID, ActorID: "u1", Action: "update", Phase: "result", Result: "success",
			IntentID: intentID, Detail: `{"phase":"result","result":"success"}`, CreatedAt: base.Add(time.Duration(i)*time.Minute + time.Second),
		}); err != nil {
			t.Fatalf("create result: %v", err)
		}
	}
	plan := explainAuditEventPlan(t, st)
	// 现代 result 配对分支必须走 (phase, intent_id) 复合索引查找，
	// 否则每行都会全表扫描配对表，数据量增大后查询呈 O(N²) 退化。
	if !strings.Contains(plan, "USING COVERING INDEX idx_audit_events_phase_intent") &&
		!strings.Contains(plan, "USING INDEX idx_audit_events_phase_intent") {
		t.Fatalf("审计事件折叠条件未通过 (phase, intent_id) 复合索引解析配对：\n%s", plan)
	}
}

// TestListLoginAuditLogsPlanAvoidsPerRowFullScan 与上面对应，守护登录审计列表。
func TestListLoginAuditLogsPlanAvoidsPerRowFullScan(t *testing.T) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	st := NewDBStore(db)
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := 0; i < 20; i++ {
		intentID := "login-intent-" + string(rune('a'+i))
		if err := st.CreateLoginAuditLog(context.Background(), &model.LoginAuditLog{
			ID: intentID, Username: "alice", Phase: "intent", Result: "pending",
			Outcome: "pending", Reason: "intent", CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("create login intent: %v", err)
		}
		if err := st.CreateLoginAuditLog(context.Background(), &model.LoginAuditLog{
			ID: "result-" + intentID, Username: "alice", Phase: "result", Result: "success",
			IntentID: intentID, Outcome: "success", StatusCode: 200,
			CreatedAt: base.Add(time.Duration(i)*time.Minute + time.Second),
		}); err != nil {
			t.Fatalf("create login result: %v", err)
		}
	}
	plan := explainLoginAuditPlan(t, st)
	// 现代 result 配对分支必须走 (phase, intent_id) 复合索引查找，
	// 否则每行都会全表扫描配对表，数据量增大后查询呈 O(N²) 退化。
	if !strings.Contains(plan, "USING COVERING INDEX idx_audit_login_logs_phase_intent") &&
		!strings.Contains(plan, "USING INDEX idx_audit_login_logs_phase_intent") {
		t.Fatalf("登录审计折叠条件未通过 (phase, intent_id) 复合索引解析配对：\n%s", plan)
	}
}
