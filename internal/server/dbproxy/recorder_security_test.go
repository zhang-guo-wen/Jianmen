package dbproxy

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestRecorderPersistsOnlyRedactedSQLAndPairsTerminalEvents(t *testing.T) {
	tests := []struct {
		name  string
		close func(queryObserver)
	}{
		{
			name: "normal response",
			close: func(observer queryObserver) {
				observer.ObserveServerBytes(buildMySQLPacket(1, []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00}))
			},
		},
		{
			name: "relay abort",
			close: func(observer queryObserver) {
				observer.(queryObserverLifecycle).Abort(observerErrorRelay)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "queries.jsonl")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			audit := &queryCaptureAudit{}
			recorder := &connectionRecorder{
				id:             "connection-1",
				protocol:       "mysql",
				file:           file,
				startedAt:      time.Now(),
				audit:          audit,
				auditSessionID: "session-1",
			}
			observer := newQueryObserver("mysql", recorder)
			const secret = "persisted-secret-123456"
			sql := "INSERT INTO tokens(token, n) VALUES ('" + secret + "', 778899)"
			observer.ObserveClientBytes(buildMySQLPacket(0, append([]byte{0x03}, []byte(sql)...)))
			test.close(observer)
			if err := recorder.Close(); err != nil {
				t.Fatal(err)
			}

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			if strings.Contains(text, secret) || strings.Contains(text, "778899") {
				t.Fatalf("queries.jsonl exposed a literal: %s", text)
			}
			if strings.Count(text, `"type":"query_started"`) != 1 ||
				strings.Count(text, `"type":"query_finished"`) != 1 {
				t.Fatalf("query events are not paired: %s", text)
			}
			queries := audit.snapshot()
			if len(queries) != 1 {
				t.Fatalf("database audit rows = %#v", queries)
			}
			if strings.Contains(queries[0].SQLText, secret) || strings.Contains(queries[0].SQLText, "778899") {
				t.Fatalf("database audit exposed literal: %#v", queries[0])
			}
			if !strings.Contains(text, queries[0].SQLText) {
				t.Fatalf("file and database redaction differ: file=%s database=%q", text, queries[0].SQLText)
			}
		})
	}
}

type queryCaptureAudit struct {
	mu      sync.Mutex
	queries []model.AuditDBQuery
}

func (*queryCaptureAudit) CreateAuditSession(*model.AuditSession) error { return nil }
func (*queryCaptureAudit) EndAuditSession(string) error                 { return nil }
func (a *queryCaptureAudit) CreateAuditDBQuery(query *model.AuditDBQuery) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.queries = append(a.queries, *query)
	return nil
}

func (a *queryCaptureAudit) snapshot() []model.AuditDBQuery {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]model.AuditDBQuery(nil), a.queries...)
}

var _ auditWriter = (*queryCaptureAudit)(nil)
