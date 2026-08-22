package recording

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"jianmen/internal/model"
)

type unredactedAuditPolicy struct{}

func (unredactedAuditPolicy) Redact(_ string, value string) string { return value }
func (unredactedAuditPolicy) AuditRedactionEnabled() bool          { return false }

func TestSessionRecorderFinalizesRedactedOutputBeforeNextCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	session := model.Session{ID: "ssh-redacted-boundary", StartedAt: time.Now().UTC()}
	recorder, err := NewSessionRecorder(root, session, true, true, maskingAuditRedactor{}, func(error) {}, nil, nil)
	if err != nil {
		t.Fatalf("NewSessionRecorder: %v", err)
	}
	recorder.RecordInput([]byte("first-command\n"))
	recorder.RecordOutput([]byte("secret-value"))
	recorder.RecordInput([]byte("second-command\n"))
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dir := filepath.Join(root, "ssh", session.ID)
	file, err := os.Open(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatalf("open commands: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatal("missing first command event")
	}
	var first commandEvent
	if err := json.Unmarshal(scanner.Bytes(), &first); err != nil {
		t.Fatalf("decode first command: %v", err)
	}
	if first.Command != "first-command" || first.OutputBytes != int64(len("[MASKED]")) {
		t.Fatalf("first command output association = %#v", first)
	}
	data, err := os.ReadFile(filepath.Join(dir, "commands-data.bin"))
	if err != nil {
		t.Fatalf("read command data: %v", err)
	}
	if string(data[first.OutputOffset:first.OutputOffset+first.OutputBytes]) != "[MASKED]" {
		t.Fatalf("redacted command output = %q", data)
	}
}

func TestSessionRecorderStoresCompleteUnredactedCommandOutput(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	session := model.Session{
		ID:        "ssh-full-output",
		StartedAt: time.Now().UTC(),
	}
	recorder, err := NewSessionRecorder(
		root,
		session,
		true,
		true,
		unredactedAuditPolicy{},
		func(error) {},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewSessionRecorder: %v", err)
	}

	recorder.RecordOutput([]byte("Password: "))
	recorder.RecordInput([]byte("plain-password\n"))
	recorder.RecordInput([]byte("echo raw-secret\n"))
	output := []byte("first line\nsecond line with raw-secret\n")
	recorder.RecordOutput(output)
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	dir := filepath.Join(root, "ssh", session.ID)
	terminal, err := os.ReadFile(filepath.Join(dir, "terminal.cast"))
	if err != nil {
		t.Fatalf("read terminal recording: %v", err)
	}
	if !bytes.Contains(terminal, []byte("plain-password")) {
		t.Fatal("default-off SSH redaction did not retain enabled terminal input")
	}
	file, err := os.Open(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatalf("open commands: %v", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		t.Fatalf("read command event: %v", scanner.Err())
	}
	var event commandEvent
	if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
		t.Fatalf("decode command event: %v", err)
	}
	if event.Command != "echo raw-secret" {
		t.Fatalf("command = %q, want shell command after prompt input", event.Command)
	}
	if event.AuditDataRedacted {
		t.Fatal("default-off SSH audit was marked redacted")
	}
	if event.OutputBytes != int64(len(output)) {
		t.Fatalf("output bytes = %d, want %d", event.OutputBytes, len(output))
	}
	complete, err := os.ReadFile(filepath.Join(dir, "commands-data.bin"))
	if err != nil {
		t.Fatalf("read complete output: %v", err)
	}
	if string(complete[event.OutputOffset:event.OutputOffset+event.OutputBytes]) != string(output) {
		t.Fatalf("complete output = %q, want %q", complete, output)
	}
}
