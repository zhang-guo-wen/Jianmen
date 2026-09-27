package dbproxy

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jianmen/internal/config"
)

func TestDatabaseAuditArtifactRedactsAcrossWriteBoundaries(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "query-data.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	artifactFile := newDatabaseAuditArtifactFile(file, true)
	capture, err := artifactFile.BeginDatabaseAuditCapture(databaseAuditArtifactSQL)
	if err != nil {
		t.Fatalf("begin capture: %v", err)
	}
	chunks := []string{
		"ALTER ROLE demo PASS",
		"WORD = unquoted-secret; SELECT '",
		"quoted-secret', 9988, $$dollar-secret$$ -- comment-secret\n",
	}
	for _, chunk := range chunks {
		if err := capture.Write([]byte(chunk)); err != nil {
			t.Fatalf("write capture: %v", err)
		}
	}
	if _, err := capture.Complete(); err != nil {
		t.Fatalf("complete capture: %v", err)
	}
	if err := artifactFile.close(); err != nil {
		t.Fatalf("close artifact: %v", err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	for _, secret := range []string{
		"unquoted-secret", "quoted-secret", "9988", "dollar-secret", "comment-secret",
	} {
		if bytes.Contains(stored, []byte(secret)) {
			t.Fatalf("redacted artifact exposed %q: %s", secret, stored)
		}
	}
	if !bytes.Contains(stored, []byte(sqlAuditRedacted)) {
		t.Fatalf("redacted artifact has no marker: %s", stored)
	}
}

func TestDatabaseAuditArtifactPoisonsWriterWhenRollbackFails(t *testing.T) {
	t.Parallel()

	file, err := os.OpenFile(filepath.Join(t.TempDir(), "query-data.bin"), os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open artifact: %v", err)
	}
	artifactFile := newDatabaseAuditArtifactFile(file, false)
	capture, err := artifactFile.BeginDatabaseAuditCapture(databaseAuditArtifactSQL)
	if err != nil {
		t.Fatalf("begin capture: %v", err)
	}
	if err := capture.Write([]byte("SELECT unsafe-offset")); err != nil {
		t.Fatalf("buffer capture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close underlying file: %v", err)
	}
	if _, err := capture.Complete(); err == nil {
		t.Fatal("Complete succeeded with a closed artifact file")
	}
	if _, err := artifactFile.BeginDatabaseAuditCapture(databaseAuditArtifactSQL); err == nil {
		t.Fatal("artifact writer reused offsets after failed rollback")
	}
}

func TestPostgresStreamPreviewRedactsAssignmentAcrossLimit(t *testing.T) {
	t.Parallel()

	parser := newPostgresStreamParser('Q', 24, true)
	if err := parser.appendSQL([]byte("SELECT password=boundary-secret, 1")); err != nil {
		t.Fatalf("append SQL: %v", err)
	}
	audit := parser.audit()
	if strings.Contains(audit.text, "boundary-secret") || strings.Contains(audit.text, "boundary-") {
		t.Fatalf("preview exposed boundary secret: %q", audit.text)
	}
	if len(audit.text) > 24 {
		t.Fatalf("preview bytes = %d, want at most 24", len(audit.text))
	}
}

func TestDatabaseRecorderStoresRawPreviewAndCompleteArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	audit := &queryCaptureAudit{}
	gateway := &Gateway{
		cfg: config.DatabaseGatewayConfig{
			AuditPreviewBytes:     config.DefaultDatabaseAuditPreviewBytes,
			AuditRedactionEnabled: false,
		},
		replayDir: root,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		audit:     audit,
	}
	recorder, err := gateway.newRecorder(
		context.Background(),
		&gatewayConn{protocol: "postgres"},
		"audit-session",
		func(error) {},
	)
	if err != nil {
		t.Fatalf("newRecorder: %v", err)
	}

	secretPrefix := "SELECT 'raw-secret-prefix', "
	sql := secretPrefix + strings.Repeat("x", 96*1024)
	sqlAudit, err := prepareAndCaptureDatabaseSQLAudit(
		recorder,
		sql,
		postgresStreamAuditPreviewBytes,
	)
	if err != nil {
		t.Fatalf("capture SQL: %v", err)
	}
	bindPayload := []byte("portal\x00statement\x00\x00\x00\x00\x01\x00\x00\x00\x0draw-parameter\x00\x00")
	parameterArtifact, err := captureDatabaseAuditBytes(
		recorder,
		databaseAuditArtifactParams,
		bindPayload,
	)
	if err != nil {
		t.Fatalf("capture parameters: %v", err)
	}
	detail := sqlAudit.withDetail(map[string]any{
		"protocol":                "postgres",
		"message":                 "Execute",
		"_parameter_log_offset":   parameterArtifact.offset,
		"_parameter_log_bytes":    parameterArtifact.bytes,
		"_parameter_log_redacted": parameterArtifact.redacted,
	})
	record, decision := recorder.StartQuery(sqlAudit.text, detail)
	if !decision.Allowed {
		t.Fatalf("StartQuery rejected: %#v", decision)
	}
	recorder.FinishQuery(record, queryFinish{Status: queryStatusSuccess})
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	queryLog, err := os.ReadFile(filepath.Join(root, "db", "audit-session", "queries.jsonl"))
	if err != nil {
		t.Fatalf("read query log: %v", err)
	}
	if !bytes.Contains(queryLog, []byte(secretPrefix)) {
		t.Fatalf("query preview was unexpectedly redacted: %s", queryLog)
	}
	if bytes.Contains(queryLog, []byte(strings.Repeat("x", 70*1024))) {
		t.Fatal("query preview exceeded the configured 64 KiB prefix")
	}

	data, err := os.ReadFile(filepath.Join(root, "db", "audit-session", databaseAuditDataFileName))
	if err != nil {
		t.Fatalf("read complete audit data: %v", err)
	}
	if got := data[sqlAudit.artifact.offset : sqlAudit.artifact.offset+sqlAudit.artifact.bytes]; !bytes.Equal(got, []byte(sql)) {
		t.Fatalf("complete SQL mismatch: got %d bytes, want %d", len(got), len(sql))
	}
	if got := data[parameterArtifact.offset : parameterArtifact.offset+parameterArtifact.bytes]; !bytes.Equal(got, bindPayload) {
		t.Fatalf("complete Bind payload mismatch: %x", got)
	}

	queries := audit.snapshot()
	if len(queries) != 1 {
		t.Fatalf("persisted query count = %d, want 1", len(queries))
	}
	query := queries[0]
	if query.SQLLogBytes != int64(len(sql)) || query.ParameterLogBytes != int64(len(bindPayload)) {
		t.Fatalf("artifact lengths = SQL %d, parameters %d", query.SQLLogBytes, query.ParameterLogBytes)
	}
	if query.AuditDataRedacted {
		t.Fatal("default-off database audit was marked redacted")
	}
}
