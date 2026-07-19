package dbproxy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"jianmen/internal/model"
)

func TestRecorderThreadsConnectionContextAndReportsQueryFailureOnce(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "queries.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	audit := &queryCaptureAudit{queryErr: errors.New("audit query unavailable")}
	client := newAuditGateConn()
	upstream := newAuditGateConn()
	fatalCalls := 0
	ctx := context.WithValue(context.Background(), recorderContextKey{}, "connection-value")
	recorder := &connectionRecorder{
		ctx:            ctx,
		id:             "connection-context",
		protocol:       "mysql",
		file:           file,
		startedAt:      time.Now(),
		audit:          audit,
		auditSessionID: "session-context",
		onFatal: func(error) {
			fatalCalls++
			_ = client.Close()
			_ = upstream.Close()
		},
	}
	defer recorder.Close()

	record, decision := recorder.StartQuery("SELECT 1", nil)
	if !decision.Allowed {
		t.Fatalf("StartQuery() decision = %#v, want allowed", decision)
	}
	recorder.FinishQuery(record, queryFinish{Status: queryStatusSuccess})
	recorder.reportFatal(errors.New("duplicate fatal signal"))

	contexts := audit.contextSnapshot()
	if len(contexts) != 1 || contexts[0].Value(recorderContextKey{}) != "connection-value" {
		t.Fatalf("CreateAuditDBQuery contexts = %#v, want connection context", contexts)
	}
	if fatalCalls != 1 {
		t.Fatalf("fatal callback calls = %d, want 1", fatalCalls)
	}
	select {
	case <-client.closed:
	default:
		t.Fatal("query audit failure did not close client")
	}
	select {
	case <-upstream.closed:
	default:
		t.Fatal("query audit failure did not close upstream")
	}
}

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
				ctx:                   context.Background(),
				id:                    "connection-1",
				protocol:              "mysql",
				maxClientMessageBytes: defaultMaxClientMessageBytes,
				file:                  file,
				startedAt:             time.Now(),
				audit:                 audit,
				auditSessionID:        "session-1",
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
			if strings.Contains(text, "sql_sha256") {
				t.Fatalf("queries.jsonl exposed a reversible SQL fingerprint: %s", text)
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
			if queries[0].OriginalSQLBytes != int64(len(sql)) ||
				queries[0].SQLTruncated {
				t.Fatalf("database audit SQL metadata = %#v, want original length without truncation", queries[0])
			}
		})
	}
}

type queryCaptureAudit struct {
	mu            sync.Mutex
	queries       []model.AuditDBQuery
	queryContexts []context.Context
	queryErr      error
}

func (*queryCaptureAudit) CreateAuditSession(context.Context, *model.AuditSession) error {
	return nil
}
func (*queryCaptureAudit) EndAuditSession(context.Context, string) error { return nil }
func (a *queryCaptureAudit) CreateAuditDBQuery(ctx context.Context, query *model.AuditDBQuery) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.queries = append(a.queries, *query)
	a.queryContexts = append(a.queryContexts, ctx)
	return a.queryErr
}

func (a *queryCaptureAudit) snapshot() []model.AuditDBQuery {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]model.AuditDBQuery(nil), a.queries...)
}

func (a *queryCaptureAudit) contextSnapshot() []context.Context {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]context.Context(nil), a.queryContexts...)
}

type recorderContextKey struct{}

var _ auditWriter = (*queryCaptureAudit)(nil)
