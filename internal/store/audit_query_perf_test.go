package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"jianmen/internal/model"
	"jianmen/internal/storage"
)

// benchmarkAuditEventDataset 构造与真实写入模式一致的数据：
// 每次管理操作写入 intent + result 两行。
func benchmarkAuditEventDataset(b *testing.B, pairs int) (*DBStore, func()) {
	db, err := storage.Open(storage.Config{Driver: storage.DriverSQLite, DSN: ":memory:"})
	if err != nil {
		b.Fatalf("open sqlite: %v", err)
	}
	if err := storage.AutoMigrate(db); err != nil {
		b.Fatalf("auto migrate: %v", err)
	}
	st := NewDBStore(db)
	base := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := 0; i < pairs; i++ {
		intentID := fmt.Sprintf("intent-%d", i)
		if err := st.CreateAuditEvent(context.Background(), &model.AuditEvent{
			ID: intentID, ActorID: "u1", ActorUsername: "alice", Action: "update",
			ResourceType: "hosts", ResourceName: "host-" + fmt.Sprint(i), Phase: "intent", Result: "pending",
			Detail: `{"method":"PUT","phase":"intent","result":"pending","request_id":"req-` + fmt.Sprint(i) + `","user_agent":"Mozilla/5.0"}`,
			ClientIP: "127.0.0.1", CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			b.Fatalf("create intent: %v", err)
		}
		if err := st.CreateAuditEvent(context.Background(), &model.AuditEvent{
			ID: "result-" + intentID, ActorID: "u1", ActorUsername: "alice", Action: "update",
			ResourceType: "hosts", ResourceName: "host-" + fmt.Sprint(i), Phase: "result", Result: "success",
			IntentID: intentID, RequestID: "req-" + fmt.Sprint(i), StatusCode: 200,
			Detail: `{"method":"PUT","phase":"result","status":200,"result":"success","intent_id":"` + intentID + `","request_id":"req-` + fmt.Sprint(i) + `","user_agent":"Mozilla/5.0"}`,
			ClientIP: "127.0.0.1", CreatedAt: base.Add(time.Duration(i)*time.Minute + time.Second),
		}); err != nil {
			b.Fatalf("create result: %v", err)
		}
	}
	return st, func() {}
}

// BenchmarkListAuditEventsLargeDataset 模拟 2000 条操作记录（4000 行）
// 时的分页加载，防止 O(N²) 退化回归。
func BenchmarkListAuditEventsLargeDataset(b *testing.B) {
	st, cleanup := benchmarkAuditEventDataset(b, 2000)
	defer cleanup()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListAuditEvents(context.Background(), AuditEventListParams{Page: 1, Size: 50}); err != nil {
			b.Fatalf("list audit events: %v", err)
		}
	}
}

// BenchmarkListAuditEventsDeepPage 深层分页（第 40 页）场景。
func BenchmarkListAuditEventsDeepPage(b *testing.B) {
	st, cleanup := benchmarkAuditEventDataset(b, 2000)
	defer cleanup()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := st.ListAuditEvents(context.Background(), AuditEventListParams{Page: 40, Size: 50}); err != nil {
			b.Fatalf("list audit events: %v", err)
		}
	}
}
