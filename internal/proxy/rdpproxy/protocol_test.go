package rdpproxy

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestInstructionCodecRoundTrip(t *testing.T) {
	want := Instruction{
		Opcode: "sync",
		Args:   []string{"你好", "a,b;c", ""},
	}

	var wire bytes.Buffer
	if err := NewEncoder(&wire).Encode(want); err != nil {
		t.Fatalf("encode instruction: %v", err)
	}
	if got, prefix := wire.String(), "4.sync,2.你好,"; len(got) < len(prefix) || got[:len(prefix)] != prefix {
		t.Fatalf("encoded instruction = %q, want prefix %q", got, prefix)
	}

	got, err := NewDecoder(&wire).Decode()
	if err != nil {
		t.Fatalf("decode instruction: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded instruction = %#v, want %#v", got, want)
	}
}

func TestDecoderHandlesFragmentedNetPipeInput(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()

	writeErr := make(chan error, 1)
	go func() {
		defer server.Close()
		for _, fragment := range []string{"4.sy", "nc,2.", "你好;"} {
			if _, err := io.WriteString(server, fragment); err != nil {
				writeErr <- err
				return
			}
		}
		writeErr <- nil
	}()

	got, err := NewDecoder(bufio.NewReader(client)).Decode()
	if err != nil {
		t.Fatalf("decode fragmented instruction: %v", err)
	}
	if want := (Instruction{Opcode: "sync", Args: []string{"你好"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded instruction = %#v, want %#v", got, want)
	}
	if err := <-writeErr; err != nil {
		t.Fatalf("write fragments: %v", err)
	}
}

func TestDecoderRejectsMalformedInstructions(t *testing.T) {
	tests := []struct {
		name string
		wire []byte
	}{
		{name: "empty length", wire: []byte(".sync;")},
		{name: "non decimal length", wire: []byte("x.sync;")},
		{name: "invalid separator", wire: []byte("4.sync!")},
		{name: "truncated element", wire: []byte("5.sync;")},
		{name: "invalid utf8", wire: []byte{'1', '.', 0xff, ';'}},
		{name: "empty opcode", wire: []byte("0.;")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewDecoder(bytes.NewReader(test.wire)).Decode()
			if !errors.Is(err, ErrMalformedInstruction) {
				t.Fatalf("decode error = %v, want ErrMalformedInstruction", err)
			}
		})
	}
}

func TestCodecEnforcesLimits(t *testing.T) {
	t.Run("element", func(t *testing.T) {
		limits := Limits{MaxElementLength: 3, MaxInstructionLength: 128, MaxElements: 8}
		_, err := NewDecoderWithLimits(bytes.NewBufferString("4.sync;"), limits).Decode()
		if !errors.Is(err, ErrElementTooLarge) {
			t.Fatalf("decode error = %v, want ErrElementTooLarge", err)
		}

		var wire bytes.Buffer
		err = NewEncoderWithLimits(&wire, limits).Encode(Instruction{Opcode: "sync"})
		if !errors.Is(err, ErrElementTooLarge) {
			t.Fatalf("encode error = %v, want ErrElementTooLarge", err)
		}
	})

	t.Run("instruction", func(t *testing.T) {
		limits := Limits{MaxElementLength: 32, MaxInstructionLength: 10, MaxElements: 8}
		_, err := NewDecoderWithLimits(bytes.NewBufferString("4.sync,4.1234;"), limits).Decode()
		if !errors.Is(err, ErrInstructionTooLarge) {
			t.Fatalf("decode error = %v, want ErrInstructionTooLarge", err)
		}

		var wire bytes.Buffer
		err = NewEncoderWithLimits(&wire, limits).Encode(Instruction{Opcode: "sync", Args: []string{"1234"}})
		if !errors.Is(err, ErrInstructionTooLarge) {
			t.Fatalf("encode error = %v, want ErrInstructionTooLarge", err)
		}
	})

	t.Run("elements", func(t *testing.T) {
		limits := Limits{MaxElementLength: 32, MaxInstructionLength: 128, MaxElements: 1}
		_, err := NewDecoderWithLimits(bytes.NewBufferString("4.sync,1.1;"), limits).Decode()
		if !errors.Is(err, ErrTooManyElements) {
			t.Fatalf("decode error = %v, want ErrTooManyElements", err)
		}

		var wire bytes.Buffer
		err = NewEncoderWithLimits(&wire, limits).Encode(Instruction{Opcode: "sync", Args: []string{"1"}})
		if !errors.Is(err, ErrTooManyElements) {
			t.Fatalf("encode error = %v, want ErrTooManyElements", err)
		}
	})
}

func TestDecoderReturnsEOFOnlyAtInstructionBoundary(t *testing.T) {
	decoder := NewDecoder(bytes.NewReader(nil))
	if _, err := decoder.Decode(); !errors.Is(err, io.EOF) {
		t.Fatalf("empty decode error = %v, want EOF", err)
	}

	server, client := net.Pipe()
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	go func() {
		_, _ = io.WriteString(server, "4.syn")
		_ = server.Close()
	}()
	if _, err := NewDecoder(client).Decode(); !errors.Is(err, ErrMalformedInstruction) {
		t.Fatalf("partial decode error = %v, want ErrMalformedInstruction", err)
	}
}
