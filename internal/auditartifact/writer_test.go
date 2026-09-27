package auditartifact

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type prefixProcessor struct{ flushed bool }

func (p *prefixProcessor) Write(data []byte, emit func([]byte) error) error {
	if err := emit([]byte("<")); err != nil {
		return err
	}
	return emit(data)
}
func (p *prefixProcessor) Flush(emit func([]byte) error) error {
	p.flushed = true
	return emit([]byte(">"))
}

func TestWriterCapturesSectionsAndProcessorOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	w, err := Open(path, 0o600, 8)
	if err != nil {
		t.Fatal(err)
	}
	first, err := w.Begin(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = first.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	s1, err := first.Complete()
	if err != nil {
		t.Fatal(err)
	}
	processor := &prefixProcessor{}
	second, err := w.Begin(processor)
	if err != nil {
		t.Fatal(err)
	}
	if err = second.Write([]byte("xy")); err != nil {
		t.Fatal(err)
	}
	s2, err := second.Complete()
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abc<xy>" || s1 != (Section{Offset: 0, Bytes: 3}) || s2 != (Section{Offset: 3, Bytes: 4}) || !processor.flushed {
		t.Fatalf("data=%q s1=%+v s2=%+v", data, s1, s2)
	}
}

func TestWriterAbortRollsBackAndReusesSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	w, err := Open(path, 0o600, 8)
	if err != nil {
		t.Fatal(err)
	}
	capture, _ := w.Begin(nil)
	_ = capture.Write([]byte("discard"))
	if err = capture.Abort(); err != nil {
		t.Fatal(err)
	}
	capture, _ = w.Begin(nil)
	_ = capture.Write([]byte("kept"))
	section, err := capture.Complete()
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "kept" || section != (Section{Offset: 0, Bytes: 4}) {
		t.Fatalf("data=%q section=%+v", data, section)
	}
}

func TestWriterPoisonsAfterRollbackFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	w, err := NewWriter(file, 8)
	if err != nil {
		t.Fatal(err)
	}
	capture, _ := w.Begin(nil)
	_ = capture.Write([]byte("unsafe"))
	_ = file.Close()
	if _, err = capture.Complete(); err == nil {
		t.Fatal("complete succeeded")
	}
	if _, err = w.Begin(nil); !errors.Is(err, ErrUnusable) {
		t.Fatalf("begin error=%v", err)
	}
}

func TestOpenSectionValidatesRange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, reader, err := OpenSection(path, Section{Offset: 2, Bytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(reader)
	_ = file.Close()
	if err != nil || string(data) != "cde" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	for _, section := range []Section{{Offset: -1, Bytes: 1}, {Offset: 0, Bytes: -1}, {Offset: 5, Bytes: 2}, {Offset: 1 << 62, Bytes: 1 << 62}} {
		if f, _, err := OpenSection(path, section); err == nil {
			_ = f.Close()
			t.Fatalf("section %+v accepted", section)
		}
	}
}
