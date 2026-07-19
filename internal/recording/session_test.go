package recording

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"jianmen/internal/model"
)

type passthroughAuditRedactor struct{}

func (passthroughAuditRedactor) Redact(_ string, value string) string {
	return value
}

type maskingAuditRedactor struct{}

func (maskingAuditRedactor) Redact(_ string, value string) string {
	return strings.ReplaceAll(value, "secret-value", "[MASKED]")
}

func TestSessionRecorderWritesAuditArtifacts(t *testing.T) {
	root := t.TempDir()
	startedAt := time.Now().UTC().Add(-time.Second)
	session := model.Session{
		ID:              "sess-audit",
		User:            model.User{Username: "alice"},
		Target:          "root@10.0.0.10:22",
		AccountUsername: "root",
		ClientIP:        "192.0.2.10",
		Protocol:        "ssh",
		StartedAt:       startedAt,
	}

	recorder, err := NewSessionRecorder(root, session, true, true, passthroughAuditRedactor{}, func(error) {}, nil, nil)
	if err != nil {
		t.Fatalf("NewSessionRecorder: %v", err)
	}
	recorder.RecordOutput([]byte("hello\n"))
	recorder.RecordInput([]byte("ls\n"))
	recorder.RecordResize("chan-1", 100, 40)
	recorder.RecordFileEvent(FileEvent{
		Action: "read",
		Path:   "/etc/hosts",
		Size:   64,
		Result: "success",
	})
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dir := filepath.Join(root, "ssh", session.ID)
	meta := readJSONMap(t, filepath.Join(dir, "meta.json"))
	if meta["session_id"] != session.ID || meta["user"] != "alice" || meta["protocol"] != "ssh" {
		t.Fatalf("unexpected meta: %#v", meta)
	}
	if meta["ended_at"] == "" {
		t.Fatalf("meta missing ended_at: %#v", meta)
	}

	castRaw, err := os.ReadFile(filepath.Join(dir, "terminal.cast"))
	if err != nil {
		t.Fatalf("read terminal cast: %v", err)
	}
	if !strings.Contains(string(castRaw), "hello") {
		t.Fatalf("terminal cast missing output: %s", string(castRaw))
	}

	eventsRaw, err := os.ReadFile(filepath.Join(dir, "terminal-events.jsonl"))
	if err != nil {
		t.Fatalf("read terminal events: %v", err)
	}
	if !strings.Contains(string(eventsRaw), `"resize"`) || !strings.Contains(string(eventsRaw), `"chan-1"`) {
		t.Fatalf("terminal events missing resize: %s", string(eventsRaw))
	}

	var summaries []FileSummary
	rawSummary, err := os.ReadFile(filepath.Join(dir, "files-summary.json"))
	if err != nil {
		t.Fatalf("read file summary: %v", err)
	}
	if err := json.Unmarshal(rawSummary, &summaries); err != nil {
		t.Fatalf("parse file summary: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Path != "/etc/hosts" || summaries[0].ReadBytes != 64 {
		t.Fatalf("unexpected file summaries: %#v", summaries)
	}
}

func TestSessionRecorderRedactsValuesBeforeEveryReplayWrite(t *testing.T) {
	root := t.TempDir()
	session := model.Session{
		ID:        "sess-redacted",
		User:      model.User{Username: "alice"},
		Target:    "root@example.test",
		Protocol:  "ssh",
		StartedAt: time.Now().UTC(),
	}
	recorder, err := NewSessionRecorder(root, session, true, true, maskingAuditRedactor{}, func(error) {}, nil, nil)
	if err != nil {
		t.Fatalf("NewSessionRecorder: %v", err)
	}
	recorder.RecordOutput([]byte("token=secret-"))
	recorder.RecordOutput([]byte("value\n"))
	recorder.RecordInput([]byte("password=secret-"))
	recorder.RecordInput([]byte("value\n"))
	recorder.RecordFileEvent(FileEvent{
		Action: "read",
		Path:   "/tmp/secret-value",
		Path2:  "/tmp/secret-value-copy",
		Error:  "secret-value denied",
		Result: "failed",
	})
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dir := filepath.Join(root, "ssh", session.ID)
	for _, name := range []string{"terminal.cast", "commands.jsonl", "files.jsonl", "files-summary.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(string(raw), "secret-value") {
			t.Fatalf("%s contains unredacted secret: %s", name, raw)
		}
		if !strings.Contains(string(raw), "[MASKED]") {
			t.Fatalf("%s does not contain redacted marker: %s", name, raw)
		}
	}
}

func TestSessionRecorderRedactsSensitiveInputAfterSplitPrompt(t *testing.T) {
	root := t.TempDir()
	session := model.Session{ID: "sess-sensitive", Protocol: "ssh", StartedAt: time.Now().UTC()}
	recorder, err := NewSessionRecorder(
		root,
		session,
		true,
		true,
		passthroughAuditRedactor{},
		func(error) {},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewSessionRecorder: %v", err)
	}
	recorder.RecordOutput([]byte("Pass"))
	recorder.RecordOutput([]byte("word: "))
	recorder.RecordInput([]byte("do-not-"))
	recorder.RecordInput([]byte("persist\n"))
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "ssh", session.ID, "terminal.cast"))
	if err != nil {
		t.Fatalf("read terminal cast: %v", err)
	}
	if strings.Contains(string(raw), "do-not-persist") || !strings.Contains(string(raw), "[REDACTED]") {
		t.Fatalf("terminal cast sensitive input = %s", raw)
	}
}

func TestSessionRecorderSignalsFatalWriteFailure(t *testing.T) {
	root := t.TempDir()
	session := model.Session{ID: "sess-fatal", Protocol: "ssh", StartedAt: time.Now().UTC()}
	fatal := make(chan error, 1)
	recorder, err := NewSessionRecorder(
		root,
		session,
		false,
		false,
		passthroughAuditRedactor{},
		func(err error) { fatal <- err },
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewSessionRecorder: %v", err)
	}
	if err := recorder.terminal.file.Close(); err != nil {
		t.Fatalf("close terminal file: %v", err)
	}
	recorder.RecordOutput([]byte("output\n"))
	select {
	case err := <-fatal:
		if !strings.Contains(err.Error(), "write terminal output") {
			t.Fatalf("fatal error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fatal recording failure was not reported")
	}
	_ = recorder.Close()
}

func TestSessionRecorderInitializationFailsWhenReplayRootIsNotWritableDirectory(t *testing.T) {
	rootFile := filepath.Join(t.TempDir(), "replay-file")
	if err := os.WriteFile(rootFile, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("write replay root file: %v", err)
	}
	_, err := NewSessionRecorder(
		rootFile,
		model.Session{ID: "session"},
		false,
		false,
		passthroughAuditRedactor{},
		func(error) {},
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("NewSessionRecorder accepted a non-directory replay root")
	}
}

func readJSONMap(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return out
}
