package guacd

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
)

const maxBufferedLogBytes = 64 * 1024

type logWriter struct {
	logger *slog.Logger
	level  slog.Level
	stream string

	mu      sync.Mutex
	pending []byte
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.pending = append(w.pending, p...)
	for {
		index := bytes.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		w.logLine(w.pending[:index])
		w.pending = w.pending[index+1:]
	}
	if len(w.pending) > maxBufferedLogBytes {
		w.logLine(w.pending)
		w.pending = w.pending[:0]
	}
	return len(p), nil
}

func (w *logWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.pending) == 0 {
		return
	}
	w.logLine(w.pending)
	w.pending = nil
}

func (w *logWriter) logLine(line []byte) {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 {
		return
	}
	w.logger.Log(
		context.Background(),
		w.level,
		"guacd output",
		"stream", w.stream,
		"message", string(line),
	)
}
