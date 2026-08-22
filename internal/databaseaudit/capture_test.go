package databaseaudit

import (
	"bytes"
	"jianmen/internal/auditartifact"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCaptureSQLUsesSamePolicyForPreviewAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query-data.bin")
	writer, err := auditartifact.Open(path, 0o600, 8)
	if err != nil {
		t.Fatal(err)
	}
	sql := []byte("SELECT password=boundary-secret, 'quoted-secret', 9988")
	result, err := CaptureSQL(writer, Policy{PreviewBytes: 4096, RedactionEnabled: true}, sql)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"boundary-secret", "quoted-secret", "9988"} {
		if bytes.Contains(stored, []byte(secret)) || strings.Contains(result.Preview, secret) {
			t.Fatalf("secret %q leaked: preview=%q file=%q", secret, result.Preview, stored)
		}
	}
	if result.Section.Offset != 0 || result.Section.Bytes != int64(len(stored)) || !result.Redacted {
		t.Fatalf("result=%+v", result)
	}
}
func TestCaptureSQLRawPreviewIsBoundedButFileComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query-data.bin")
	writer, err := auditartifact.Open(path, 0o600, 8)
	if err != nil {
		t.Fatal(err)
	}
	sql := []byte("SELECT " + strings.Repeat("x", 100))
	result, err := CaptureSQL(writer, Policy{PreviewBytes: 16}, sql)
	if err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	stored, _ := os.ReadFile(path)
	if !bytes.Equal(stored, sql) || len(result.Preview) > 16 || !result.Truncated || result.OriginalBytes != int64(len(sql)) {
		t.Fatalf("result=%+v stored=%d", result, len(stored))
	}
}
