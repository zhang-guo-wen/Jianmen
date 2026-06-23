package recording

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

type AsciinemaWriter struct {
	mu          sync.Mutex
	file        *os.File
	startedAt   time.Time
	width       int
	height      int
	wroteHeader bool
}

func NewAsciinemaWriter(file *os.File, startedAt time.Time, width, height int) *AsciinemaWriter {
	if width <= 0 {
		width = 120
	}
	if height <= 0 {
		height = 40
	}
	return &AsciinemaWriter{
		file:      file,
		startedAt: startedAt,
		width:     width,
		height:    height,
	}
}

func (w *AsciinemaWriter) WriteOutput(data []byte) error {
	return w.writeRow("o", data)
}

func (w *AsciinemaWriter) WriteInput(data []byte) error {
	return w.writeRow("i", data)
}

func (w *AsciinemaWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

func (w *AsciinemaWriter) writeRow(stream string, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.ensureHeaderLocked(); err != nil {
		return err
	}
	row := []any{time.Since(w.startedAt).Seconds(), stream, string(data)}
	raw, err := json.Marshal(row)
	if err != nil {
		return err
	}
	if _, err := w.file.Write(raw); err != nil {
		return err
	}
	_, err = w.file.Write([]byte("\n"))
	return err
}

func (w *AsciinemaWriter) ensureHeaderLocked() error {
	if w.wroteHeader {
		return nil
	}
	header := map[string]any{
		"version":   2,
		"width":     w.width,
		"height":    w.height,
		"timestamp": w.startedAt.Unix(),
	}
	raw, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err := w.file.Write(raw); err != nil {
		return err
	}
	if _, err := w.file.Write([]byte("\n")); err != nil {
		return err
	}
	w.wroteHeader = true
	return nil
}
