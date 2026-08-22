package auditartifact

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sync"
)

const DefaultBufferBytes = 256 * 1024

var (
	ErrClosed        = errors.New("audit artifact writer is closed")
	ErrCaptureActive = errors.New("audit artifact capture is already active")
	ErrCaptureClosed = errors.New("audit artifact capture is closed")
	ErrUnusable      = errors.New("audit artifact writer is unusable")
)

type Section struct{ Offset, Bytes int64 }

type Processor interface {
	Write(data []byte, emit func([]byte) error) error
	Flush(emit func([]byte) error) error
}

type Writer struct {
	mu       sync.Mutex
	file     *os.File
	buffer   *bufio.Writer
	offset   int64
	active   *Capture
	closed   bool
	fatalErr error
}

func NewWriter(file *os.File, bufferBytes int) (*Writer, error) {
	if file == nil {
		return nil, errors.New("audit artifact file is required")
	}
	if bufferBytes <= 0 {
		bufferBytes = DefaultBufferBytes
	}
	offset, err := file.Seek(0, 2)
	if err != nil {
		return nil, fmt.Errorf("seek audit artifact end: %w", err)
	}
	return &Writer{file: file, buffer: bufio.NewWriterSize(file, bufferBytes), offset: offset}, nil
}

func Open(path string, mode os.FileMode, bufferBytes int) (*Writer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, mode)
	if err != nil {
		return nil, err
	}
	writer, err := NewWriter(file, bufferBytes)
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return writer, nil
}

func (w *Writer) Begin(processor Processor) (*Capture, error) {
	if w == nil {
		return nil, ErrClosed
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fatalErr != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnusable, w.fatalErr)
	}
	if w.closed || w.file == nil || w.buffer == nil {
		return nil, ErrClosed
	}
	if w.active != nil {
		return nil, ErrCaptureActive
	}
	capture := &Capture{owner: w, start: w.offset, processor: processor}
	w.active = capture
	return capture, nil
}

func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return w.fatalErr
	}
	var abortErr error
	if w.active != nil {
		abortErr = w.abortLocked(w.active)
	}
	w.closed = true
	var flushErr error
	if w.buffer != nil {
		flushErr = w.buffer.Flush()
	}
	var closeErr error
	if w.file != nil {
		closeErr = w.file.Close()
	}
	w.buffer = nil
	w.file = nil
	return errors.Join(w.fatalErr, abortErr, flushErr, closeErr)
}

func (w *Writer) abortLocked(c *Capture) error {
	if c == nil || w.active != c {
		return nil
	}
	w.active = nil
	c.closed = true
	if w.buffer == nil || w.file == nil {
		err := errors.New("audit artifact rollback writer is unavailable")
		w.fatalErr = errors.Join(w.fatalErr, err)
		return err
	}
	w.buffer.Reset(w.file)
	truncateErr := w.file.Truncate(c.start)
	_, seekErr := w.file.Seek(c.start, 0)
	if rollbackErr := errors.Join(truncateErr, seekErr); rollbackErr != nil {
		w.fatalErr = errors.Join(w.fatalErr, rollbackErr)
		return fmt.Errorf("rollback audit artifact: %w", rollbackErr)
	}
	w.offset = c.start
	return nil
}

type Capture struct {
	owner        *Writer
	processor    Processor
	start, bytes int64
	closed       bool
}

func (c *Capture) Write(data []byte) error {
	if c == nil || len(data) == 0 {
		return nil
	}
	if c.owner == nil {
		return ErrCaptureClosed
	}
	c.owner.mu.Lock()
	defer c.owner.mu.Unlock()
	if c.closed || c.owner.active != c {
		return ErrCaptureClosed
	}
	if c.processor != nil {
		return c.processor.Write(data, c.writeStoredLocked)
	}
	return c.writeStoredLocked(data)
}

func (c *Capture) Complete() (Section, error) {
	if c == nil || c.owner == nil {
		return Section{}, ErrCaptureClosed
	}
	owner := c.owner
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if c.closed || owner.active != c {
		return Section{}, ErrCaptureClosed
	}
	if c.processor != nil {
		if err := c.processor.Flush(c.writeStoredLocked); err != nil {
			return Section{}, errors.Join(err, owner.abortLocked(c))
		}
	}
	if err := owner.buffer.Flush(); err != nil {
		return Section{}, errors.Join(err, owner.abortLocked(c))
	}
	section := Section{Offset: c.start, Bytes: c.bytes}
	owner.active = nil
	c.closed = true
	return section, nil
}

func (c *Capture) Abort() error {
	if c == nil || c.owner == nil {
		return nil
	}
	c.owner.mu.Lock()
	defer c.owner.mu.Unlock()
	return c.owner.abortLocked(c)
}

func (c *Capture) writeStoredLocked(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if c.owner == nil || c.owner.buffer == nil {
		return ErrClosed
	}
	n, err := c.owner.buffer.Write(data)
	c.bytes += int64(n)
	c.owner.offset += int64(n)
	if err != nil {
		return err
	}
	if n != len(data) {
		return errors.New("short audit artifact write")
	}
	return nil
}
