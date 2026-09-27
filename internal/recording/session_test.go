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

// testAuditSink 记录所有审计写入，用于测试验证
type testAuditSink struct {
	commands    []testAuditCommand
	fileEvents  []testAuditFileEvent
	protocols   []string
}

type testAuditCommand struct {
	sessionID string
	timestamp time.Time
	command   string
}

type testAuditFileEvent struct {
	sessionID string
	timestamp time.Time
	action    string
	path      string
	size      int64
	result    string
}

func (s *testAuditSink) WriteCommand(sessionID string, timestamp time.Time, command string) error {
	s.commands = append(s.commands, testAuditCommand{sessionID, timestamp, command})
	return nil
}

func (s *testAuditSink) WriteFileEvent(sessionID string, timestamp time.Time, action, path string, size int64, result string) error {
	s.fileEvents = append(s.fileEvents, testAuditFileEvent{sessionID, timestamp, action, path, size, result})
	return nil
}

func (s *testAuditSink) UpdateProtocol(sessionID string, protocol string) error {
	s.protocols = append(s.protocols, protocol)
	return nil
}

func TestCommandRecorderRecordDirectWritesToFileAndAuditSink(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatalf("create commands file: %v", err)
	}
	defer file.Close()

	sink := &testAuditSink{}
	startedAt := time.Now().UTC()
	fatal := make(chan error, 1)
	recorder := NewCommandRecorder(file, startedAt, passthroughAuditRedactor{}, sink, "sess-exec-1", func(err error) {
		fatal <- err
	})

	if err := recorder.RecordDirect("ls -la /tmp"); err != nil {
		t.Fatalf("RecordDirect: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// 验证文件内容
	raw, err := os.ReadFile(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatalf("read commands file: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "ls -la /tmp") {
		t.Fatalf("commands file missing expected command: %s", content)
	}
	if !strings.Contains(content, `"confidence":"exact"`) {
		t.Fatalf("commands file missing exact confidence: %s", content)
	}

	// 验证审计 sink
	if len(sink.commands) != 1 {
		t.Fatalf("expected 1 command in audit sink, got %d", len(sink.commands))
	}
	if sink.commands[0].command != "ls -la /tmp" {
		t.Fatalf("audit sink command = %q, want %q", sink.commands[0].command, "ls -la /tmp")
	}
	if sink.commands[0].sessionID != "sess-exec-1" {
		t.Fatalf("audit sink sessionID = %q, want sess-exec-1", sink.commands[0].sessionID)
	}

	// 验证无 fatal 错误
	select {
	case err := <-fatal:
		t.Fatalf("unexpected fatal error: %v", err)
	default:
	}
}

func TestCommandRecorderRecordDirectRedactsCommand(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatalf("create commands file: %v", err)
	}
	defer file.Close()

	sink := &testAuditSink{}
	recorder := NewCommandRecorder(file, time.Now().UTC(), maskingAuditRedactor{}, sink, "sess-redact-1", func(error) {})

	if err := recorder.RecordDirect("echo secret-value"); err != nil {
		t.Fatalf("RecordDirect: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatalf("read commands file: %v", err)
	}
	content := string(raw)
	if strings.Contains(content, "secret-value") {
		t.Fatalf("commands file contains unredacted secret: %s", content)
	}
	if !strings.Contains(content, "[MASKED]") {
		t.Fatalf("commands file missing redacted marker: %s", content)
	}
}

func TestCommandRecorderRecordDirectEmptyCommand(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatalf("create commands file: %v", err)
	}
	defer file.Close()

	sink := &testAuditSink{}
	recorder := NewCommandRecorder(file, time.Now().UTC(), passthroughAuditRedactor{}, sink, "sess-empty-1", func(error) {})

	// 空命令或仅空白应静默跳过
	if err := recorder.RecordDirect(""); err != nil {
		t.Fatalf("RecordDirect empty: %v", err)
	}
	if err := recorder.RecordDirect("   "); err != nil {
		t.Fatalf("RecordDirect whitespace: %v", err)
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(sink.commands) != 0 {
		t.Fatalf("expected 0 commands for empty input, got %d", len(sink.commands))
	}
}

func TestSessionRecorderRecordCommandCallsThroughToCommandRecorder(t *testing.T) {
	root := t.TempDir()
	startedAt := time.Now().UTC().Add(-time.Second)
	session := model.Session{
		ID:              "sess-rec-cmd",
		User:            model.User{Username: "alice"},
		Target:          "root@10.0.0.10:22",
		AccountUsername: "root",
		ClientIP:        "192.0.2.10",
		Protocol:        "ssh",
		StartedAt:       startedAt,
	}

	sink := &testAuditSink{}
	recorder, err := NewSessionRecorder(root, session, false, true, passthroughAuditRedactor{}, func(error) {}, nil, sink)
	if err != nil {
		t.Fatalf("NewSessionRecorder: %v", err)
	}

	recorder.RecordCommand("uptime")
	recorder.RecordCommand("free -m")
	recorder.RecordCommand("") // 空命令应被忽略
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if len(sink.commands) != 2 {
		t.Fatalf("expected 2 commands in audit sink, got %d", len(sink.commands))
	}
	if sink.commands[0].command != "uptime" {
		t.Fatalf("first command = %q, want uptime", sink.commands[0].command)
	}
	if sink.commands[1].command != "free -m" {
		t.Fatalf("second command = %q, want free -m", sink.commands[1].command)
	}

	// 验证 commands.jsonl 文件也包含命令
	raw, err := os.ReadFile(filepath.Join(root, "ssh", session.ID, "commands.jsonl"))
	if err != nil {
		t.Fatalf("read commands file: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "uptime") || !strings.Contains(content, "free -m") {
		t.Fatalf("commands file missing expected commands: %s", content)
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
