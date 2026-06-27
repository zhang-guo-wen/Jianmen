package recording

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"jianmen/internal/model"
)

type SessionRecorder struct {
	mu             sync.Mutex
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
	closed     bool
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

func NewSessionRecorder(root string, session model.Session, recordInput, recordCommands bool, logger *slog.Logger) (*SessionRecorder, error) {
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
		"user":             session.User.Username,
		"target":           session.Target,
		"client_ip":        session.ClientIP,
		"started_at":       startedAt.Format(time.RFC3339Nano),
		"protocol":         protocol,
		"protocol_subtype": session.ProtocolSubtype,
	}
	if raw, err := json.MarshalIndent(meta, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0o644)
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
		commands:       NewCommandRecorder(commandsFile, startedAt),
		files:          make(map[string]*FileSummary),
	}
	return rec, nil
}

func (r *SessionRecorder) Dir() string {
	return r.dir
}

func (r *SessionRecorder) RecordOutput(data []byte) {
	if r == nil || len(data) == 0 {
		return
	}
	if err := r.terminal.WriteOutput(data); err != nil && r.logger != nil {
		r.logger.Warn("failed to write terminal output", "session", r.session.ID, "error", err)
	}
	if r.recordCommands {
		r.commands.ObserveOutput(data)
	}
}

func (r *SessionRecorder) RecordInput(data []byte) {
	if r == nil || len(data) == 0 {
		return
	}
	if r.recordInput {
		if err := r.terminal.WriteInput(data); err != nil && r.logger != nil {
			r.logger.Warn("failed to write terminal input", "session", r.session.ID, "error", err)
		}
	}
	if r.recordCommands {
		r.commands.ObserveInput(data)
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
		return
	}
	_, _ = r.eventsFile.Write(append(raw, '\n'))
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

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.filesFile == nil {
		return
	}
	r.fileSeq++
	event.Seq = r.fileSeq
	event.SessionID = r.session.ID
	r.updateFileSummaryLocked(event)

	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	if _, err := r.filesFile.Write(append(raw, '\n')); err != nil && r.logger != nil {
		r.logger.Warn("failed to write file event", "session", r.session.ID, "error", err)
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

	var firstErr error
	if r.commands != nil {
		if err := r.commands.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.terminal != nil {
		if err := r.terminal.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if r.eventsFile != nil {
		if err := r.eventsFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if err := r.writeFileSummary(); err != nil && firstErr == nil {
		firstErr = err
	}
	if r.filesFile != nil {
		if err := r.filesFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// Write ended_at to meta.json so the listing can calculate duration.
	r.writeEndedAt()

	return firstErr
}

func (r *SessionRecorder) writeEndedAt() {
	metaPath := filepath.Join(r.dir, "meta.json")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return
	}
	meta["ended_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	updated, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(metaPath, updated, 0o644)
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
