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

	recorder, err := NewSessionRecorder(root, session, true, true, nil)
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
