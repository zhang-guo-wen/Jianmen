package recording

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"jianmen/internal/model"
)

// AuditSink receives audit events during recording.
type AuditSink interface {
	WriteCommand(sessionID string, timestamp time.Time, command string) error
	WriteFileEvent(sessionID string, timestamp time.Time, action, path string, size int64, result string) error
	UpdateProtocol(sessionID string, protocol string) error
}

type AuditRedactor interface {
	Redact(kind, value string) string
}

type SessionRecorder struct {
	mu             sync.Mutex
	streamMu       sync.Mutex
	session        model.Session
	startedAt      time.Time
	recordInput    bool
	recordCommands bool
	logger         *slog.Logger

	dir        string
	terminal   *AsciinemaWriter
	eventsFile *os.File
	filesFile  *os.File
	commands   *CommandRecorder
	fileSeq    int64
	files      map[string]*FileSummary
	auditSink  AuditSink
	redactor   AuditRedactor
	output     *auditStreamRedactor
	input      *auditStreamRedactor
	onFatal    func(error)
	fatalOnce  sync.Once
	promptTail  string
	redactInput bool
	inputSeen   bool
	closed      bool
}

type ResizeEvent struct {
	Type      string  `json:"type"`
	Time      float64 `json:"time"`
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	ChannelID string  `json:"channel_id,omitempty"`
}

type FileEvent struct {
	SessionID string         `json:"session_id"`
	ChannelID string         `json:"channel_id,omitempty"`
	Seq       int64          `json:"seq"`
	Action    string         `json:"action"`
	Path      string         `json:"path"`
	Path2     string         `json:"path2,omitempty"`
	Handle    string         `json:"handle,omitempty"`
	Offset    int64          `json:"offset,omitempty"`
	Size      int64          `json:"size,omitempty"`
	Result    string         `json:"result"`
	ErrorCode uint32         `json:"error_code,omitempty"`
	Error     string         `json:"error,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
	StartedAt int64          `json:"started_at"`
	EndedAt   int64          `json:"ended_at,omitempty"`
}

type FileSummary struct {
	Path       string           `json:"path"`
	Actions    map[string]int64 `json:"actions"`
	ReadBytes  int64            `json:"read_bytes,omitempty"`
	WriteBytes int64            `json:"write_bytes,omitempty"`
	LastResult string           `json:"last_result,omitempty"`
	LastAt     int64            `json:"last_at,omitempty"`
}

func NewSessionRecorder(
	root string,
	session model.Session,
	recordInput bool,
	recordCommands bool,
	redactor AuditRedactor,
	onFatal func(error),
	logger *slog.Logger,
	sink AuditSink,
) (*SessionRecorder, error) {
	if redactor == nil {
		return nil, errors.New("audit redactor is required")
	}
	if onFatal == nil {
		return nil, errors.New("audit fatal error handler is required")
	}
	dir := filepath.Join(root, "ssh", session.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	startedAt := session.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	terminalFile, err := os.OpenFile(filepath.Join(dir, "terminal.cast"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	eventsFile, err := os.OpenFile(filepath.Join(dir, "terminal-events.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = terminalFile.Close()
		return nil, err
	}
	commandsFile, err := os.OpenFile(filepath.Join(dir, "commands.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = terminalFile.Close()
		_ = eventsFile.Close()
		return nil, err
	}
	filesFile, err := os.OpenFile(filepath.Join(dir, "files.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		_ = terminalFile.Close()
		_ = eventsFile.Close()
		_ = commandsFile.Close()
		return nil, err
	}

	protocol := session.Protocol
	if protocol == "" {
		protocol = "ssh"
	}
	meta := map[string]any{
		"session_id":       session.ID,
		"user_id":          session.UserID,
		"user":             session.User.Username,
		"target":           session.Target,
		"account_username": session.AccountUsername,
		"client_ip":        session.ClientIP,
		"started_at":       startedAt.Format(time.RFC3339Nano),
		"protocol":         protocol,
		"protocol_subtype": session.ProtocolSubtype,
	}
	raw, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		closeRecorderFiles(terminalFile, eventsFile, commandsFile, filesFile)
		return nil, fmt.Errorf("marshal audit session metadata: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644); err != nil {
		closeRecorderFiles(terminalFile, eventsFile, commandsFile, filesFile)
		return nil, fmt.Errorf("write audit session metadata: %w", err)
	}

	rec := &SessionRecorder{
		session:        session,
		startedAt:      startedAt,
		recordInput:    recordInput,
		recordCommands: recordCommands,
		logger:         logger,
		dir:            dir,
		terminal:       NewAsciinemaWriter(terminalFile, startedAt, 80, 24),
		eventsFile:     eventsFile,
		filesFile:      filesFile,
		files:          make(map[string]*FileSummary),
		auditSink:      sink,
		redactor:       redactor,
		output:         newAuditStreamRedactor("output", redactor),
		input:          newAuditStreamRedactor("input", redactor),
		onFatal:        onFatal,
	}
	rec.commands = NewCommandRecorder(commandsFile, startedAt, redactor, sink, session.ID, rec.reportFatal)
	return rec, nil
}

func closeRecorderFiles(files ...*os.File) {
	for _, file := range files {
		if file != nil {
			_ = file.Close()
		}
	}
}

func (r *SessionRecorder) Dir() string {
	return r.dir
}

// SetProtocolSubtype updates the protocol subtype (e.g. "sftp") in meta.json and in the database.
func (r *SessionRecorder) SetProtocolSubtype(subtype string) {
	if r == nil {
		return
	}
	r.session.ProtocolSubtype = subtype
	if err := r.writeMetaField("protocol_subtype", subtype); err != nil {
		r.reportFatal(err)
	}
	if r.auditSink != nil {
		if err := r.auditSink.UpdateProtocol(r.session.ID, subtype); err != nil {
			r.reportFatal(fmt.Errorf("update audit protocol: %w", err))
		}
	}
}

func (r *SessionRecorder) writeMetaField(key, value string) error {
	metaPath := filepath.Join(r.dir, "meta.json")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("read audit metadata: %w", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fmt.Errorf("decode audit metadata: %w", err)
	}
	meta[key] = value
	updated, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, updated, 0o644); err != nil {
		return fmt.Errorf("write audit metadata: %w", err)
	}
	return nil
}

func (r *SessionRecorder) RecordOutput(data []byte) {
	if r == nil || len(data) == 0 {
		return
	}
	r.streamMu.Lock()
	defer r.streamMu.Unlock()
	if r.isClosed() {
		return
	}
	r.observeSensitivePrompt(data)
	if r.recordCommands {
		r.commands.ObserveOutput(data)
	}
	redacted, err := r.output.Write(data)
	if err != nil {
		r.reportFatal(fmt.Errorf("redact terminal output: %w", err))
		return
	}
	if err := r.terminal.WriteOutput(redacted); err != nil {
		r.reportFatal(fmt.Errorf("write terminal output: %w", err))
	}
}

func (r *SessionRecorder) RecordInput(data []byte) {
	if r == nil || len(data) == 0 {
		return
	}
	r.streamMu.Lock()
	defer r.streamMu.Unlock()
	if r.isClosed() {
		return
	}
	if r.recordCommands {
		r.commands.ObserveInput(data)
	}
	if r.recordInput {
		redacted, err := r.redactInputFrame(data)
		if err != nil {
			r.reportFatal(fmt.Errorf("redact terminal input: %w", err))
			return
		}
		if err := r.terminal.WriteInput(redacted); err != nil {
			r.reportFatal(fmt.Errorf("write terminal input: %w", err))
		}
	}
}

// RecordCommand 直接记录一条完整命令，用于 SSH exec 通道等场景。
func (r *SessionRecorder) RecordCommand(command string) {
	if r == nil || !r.recordCommands || command == "" {
		return
	}
	r.streamMu.Lock()
	defer r.streamMu.Unlock()
	if r.isClosed() {
		return
	}
	if err := r.commands.RecordDirect(command); err != nil {
		r.reportFatal(fmt.Errorf("record exec command: %w", err))
	}
}

func (r *SessionRecorder) RecordResize(channelID string, width, height int) {
	if r == nil {
		return
	}
	// Update cast header dimensions before it's written on first output.
	if r.terminal != nil {
		r.terminal.UpdateSize(width, height)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.eventsFile == nil {
		return
	}
	event := ResizeEvent{
		Type:      "resize",
		Time:      time.Since(r.startedAt).Seconds(),
		Width:     width,
		Height:    height,
		ChannelID: channelID,
	}
	raw, err := json.Marshal(event)
	if err != nil {
		r.reportFatal(fmt.Errorf("encode terminal resize event: %w", err))
		return
	}
	if _, err := r.eventsFile.Write(append(raw, '\n')); err != nil {
		r.reportFatal(fmt.Errorf("write terminal resize event: %w", err))
	}
}

func (r *SessionRecorder) RecordFileEvent(event FileEvent) {
	if r == nil {
		return
	}
	now := time.Now().UTC().UnixMilli()
	if event.StartedAt == 0 {
		event.StartedAt = now
	}
	if event.EndedAt == 0 {
		event.EndedAt = now
	}
	if event.Result == "" {
		event.Result = "success"
	}
	event.Path = r.redactor.Redact("path", event.Path)
	event.Path2 = r.redactor.Redact("path", event.Path2)
	event.Error = r.redactor.Redact("error", event.Error)

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.filesFile == nil {
		return
	}
	r.fileSeq++
	event.Seq = r.fileSeq
	event.SessionID = r.session.ID
	r.updateFileSummaryLocked(event)
	if r.auditSink != nil {
		if err := r.auditSink.WriteFileEvent(
			r.session.ID,
			time.UnixMilli(event.StartedAt),
			event.Action,
			event.Path,
			event.Size,
			event.Result,
		); err != nil {
			r.reportFatal(fmt.Errorf("write database file audit event: %w", err))
		}
	}

	raw, err := json.Marshal(event)
	if err != nil {
		r.reportFatal(fmt.Errorf("encode file audit event: %w", err))
		return
	}
	if _, err := r.filesFile.Write(append(raw, '\n')); err != nil {
		r.reportFatal(fmt.Errorf("write file audit event: %w", err))
	}
}

func (r *SessionRecorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()

	r.streamMu.Lock()
	defer r.streamMu.Unlock()
	var firstErr error
	recordError := func(operation string, err error) {
		if err == nil {
			return
		}
		wrapped := fmt.Errorf("%s: %w", operation, err)
		r.reportFatal(wrapped)
		if firstErr == nil {
			firstErr = wrapped
		}
	}
	if output := r.output.Flush(); len(output) > 0 {
		recordError("flush terminal output", r.terminal.WriteOutput(output))
	}
	if input := r.flushInput(); len(input) > 0 {
		recordError("flush terminal input", r.terminal.WriteInput(input))
	}
	if r.commands != nil {
		recordError("close command recorder", r.commands.Close())
	}
	if r.terminal != nil {
		recordError("close terminal recorder", r.terminal.Close())
	}
	if r.eventsFile != nil {
		recordError("close terminal event recorder", r.eventsFile.Close())
	}
	recordError("write file audit summary", r.writeFileSummary())
	if r.filesFile != nil {
		recordError("close file event recorder", r.filesFile.Close())
	}

	// Write ended_at to meta.json so the listing can calculate duration.
	recordError("write audit end metadata", r.writeEndedAt())

	return firstErr
}

func (r *SessionRecorder) writeEndedAt() error {
	metaPath := filepath.Join(r.dir, "meta.json")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return err
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return err
	}
	meta["ended_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	updated, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, updated, 0o644)
}

func (r *SessionRecorder) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *SessionRecorder) observeSensitivePrompt(data []byte) {
	r.mu.Lock()
	combined := r.promptTail + strings.ToLower(string(data))
	sensitive := isSensitivePrompt([]byte(combined))
	if sensitive {
		r.redactInput = true
		r.inputSeen = false
		r.promptTail = ""
	} else {
		const promptTailBytes = 64
		if len(combined) > promptTailBytes {
			combined = combined[len(combined)-promptTailBytes:]
		}
		r.promptTail = combined
	}
	r.mu.Unlock()
	if sensitive && r.commands != nil {
		r.commands.MarkSensitivePrompt()
	}
}

func (r *SessionRecorder) redactInputFrame(data []byte) ([]byte, error) {
	r.mu.Lock()
	if !r.redactInput {
		r.mu.Unlock()
		return r.input.Write(data)
	}
	r.inputSeen = r.inputSeen || len(data) > 0
	lineEnd := bytes.IndexAny(data, "\r\n")
	if lineEnd < 0 {
		r.mu.Unlock()
		return nil, nil
	}
	r.redactInput = false
	r.inputSeen = false
	remainderStart := lineEnd + 1
	if data[lineEnd] == '\r' && remainderStart < len(data) && data[remainderStart] == '\n' {
		remainderStart++
	}
	r.mu.Unlock()

	output := []byte("[REDACTED]\n")
	if remainderStart < len(data) {
		remainder, err := r.input.Write(data[remainderStart:])
		if err != nil {
			return nil, err
		}
		output = append(output, remainder...)
	}
	return output, nil
}

func (r *SessionRecorder) flushInput() []byte {
	r.mu.Lock()
	sensitive := r.redactInput && r.inputSeen
	r.redactInput = false
	r.inputSeen = false
	r.mu.Unlock()
	output := r.input.Flush()
	if sensitive {
		return append([]byte("[REDACTED]"), output...)
	}
	return output
}

func (r *SessionRecorder) reportFatal(err error) {
	if r == nil || err == nil {
		return
	}
	wrapped := fmt.Errorf("audit recording failed for session %q: %w", r.session.ID, err)
	if r.logger != nil {
		r.logger.Error("audit recording failed; terminating session", "session", r.session.ID, "error", err)
	}
	r.fatalOnce.Do(func() {
		r.onFatal(wrapped)
	})
}

func (r *SessionRecorder) updateFileSummaryLocked(event FileEvent) {
	if event.Path == "" {
		return
	}
	summary := r.files[event.Path]
	if summary == nil {
		summary = &FileSummary{
			Path:    event.Path,
			Actions: make(map[string]int64),
		}
		r.files[event.Path] = summary
	}
	summary.Actions[event.Action]++
	if event.Result == "success" {
		switch event.Action {
		case "read":
			summary.ReadBytes += event.Size
		case "write":
			summary.WriteBytes += event.Size
		}
	}
	summary.LastResult = event.Result
	summary.LastAt = event.EndedAt
}

func (r *SessionRecorder) writeFileSummary() error {
	r.mu.Lock()
	if len(r.files) == 0 {
		r.mu.Unlock()
		return nil
	}
	summaries := make([]*FileSummary, 0, len(r.files))
	for _, summary := range r.files {
		copySummary := *summary
		copySummary.Actions = make(map[string]int64, len(summary.Actions))
		for action, count := range summary.Actions {
			copySummary.Actions[action] = count
		}
		summaries = append(summaries, &copySummary)
	}
	r.mu.Unlock()

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Path < summaries[j].Path
	})
	raw, err := json.MarshalIndent(summaries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.dir, "files-summary.json"), raw, 0o644)
}

func isSensitivePrompt(data []byte) bool {
	text := strings.ToLower(string(data))
	return strings.Contains(text, "password:") ||
		strings.Contains(text, "passphrase:") ||
		strings.Contains(text, "verification code:")
}
