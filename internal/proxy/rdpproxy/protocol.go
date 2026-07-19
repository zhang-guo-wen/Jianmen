package rdpproxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

const (
	defaultMaxElementLength     = 1 << 20
	defaultMaxInstructionLength = 8 << 20
	defaultMaxElements          = 256
)

var (
	ErrMalformedInstruction = errors.New("malformed guacamole instruction")
	ErrElementTooLarge      = errors.New("guacamole element exceeds limit")
	ErrInstructionTooLarge  = errors.New("guacamole instruction exceeds limit")
	ErrTooManyElements      = errors.New("guacamole instruction has too many elements")
)

// Limits bounds memory and CPU consumed while handling untrusted Guacamole
// instructions. Element lengths are measured in Unicode code points, as
// required by the Guacamole protocol. Instruction lengths are measured in
// bytes on the wire.
type Limits struct {
	MaxElementLength     int
	MaxInstructionLength int
	MaxElements          int
}

// Instruction is one Guacamole protocol instruction. Opcode is the first
// protocol element; Args contains the remaining elements.
type Instruction struct {
	Opcode string
	Args   []string
}

// Encoder writes length-prefixed Guacamole instructions.
type Encoder struct {
	writer io.Writer
	limits Limits
}

// Decoder reads length-prefixed Guacamole instructions.
type Decoder struct {
	reader *bufio.Reader
	limits Limits
}

func NewEncoder(writer io.Writer) *Encoder {
	return NewEncoderWithLimits(writer, Limits{})
}

func NewEncoderWithLimits(writer io.Writer, limits Limits) *Encoder {
	return &Encoder{writer: writer, limits: normalizeLimits(limits)}
}

func NewDecoder(reader io.Reader) *Decoder {
	return NewDecoderWithLimits(reader, Limits{})
}

func NewDecoderWithLimits(reader io.Reader, limits Limits) *Decoder {
	buffered, ok := reader.(*bufio.Reader)
	if !ok {
		buffered = bufio.NewReader(reader)
	}
	return &Decoder{reader: buffered, limits: normalizeLimits(limits)}
}

func (e *Encoder) Encode(instruction Instruction) error {
	if e == nil || e.writer == nil {
		return errors.New("guacamole encoder writer is required")
	}
	if instruction.Opcode == "" {
		return fmt.Errorf("%w: empty opcode", ErrMalformedInstruction)
	}

	elements := make([]string, 1, len(instruction.Args)+1)
	elements[0] = instruction.Opcode
	elements = append(elements, instruction.Args...)
	if len(elements) > e.limits.MaxElements {
		return ErrTooManyElements
	}

	var encoded bytes.Buffer
	for i, element := range elements {
		if !utf8.ValidString(element) {
			return fmt.Errorf("%w: invalid utf-8", ErrMalformedInstruction)
		}
		length := utf8.RuneCountInString(element)
		if length > e.limits.MaxElementLength {
			return ErrElementTooLarge
		}
		encoded.WriteString(strconv.Itoa(length))
		encoded.WriteByte('.')
		encoded.WriteString(element)
		if i == len(elements)-1 {
			encoded.WriteByte(';')
		} else {
			encoded.WriteByte(',')
		}
		if encoded.Len() > e.limits.MaxInstructionLength {
			return ErrInstructionTooLarge
		}
	}

	raw := encoded.Bytes()
	written, err := e.writer.Write(raw)
	if err != nil {
		return fmt.Errorf("write guacamole instruction: %w", err)
	}
	if written != len(raw) {
		return io.ErrShortWrite
	}
	return nil
}

func (d *Decoder) Decode() (Instruction, error) {
	if d == nil || d.reader == nil {
		return Instruction{}, errors.New("guacamole decoder reader is required")
	}

	elements := make([]string, 0, 8)
	wireBytes := 0
	for {
		if len(elements) >= d.limits.MaxElements {
			return Instruction{}, ErrTooManyElements
		}
		element, separator, consumed, err := d.readElement(wireBytes)
		if err != nil {
			if len(elements) == 0 && wireBytes == 0 && err == io.EOF {
				return Instruction{}, io.EOF
			}
			return Instruction{}, err
		}
		wireBytes += consumed
		if wireBytes > d.limits.MaxInstructionLength {
			return Instruction{}, ErrInstructionTooLarge
		}
		elements = append(elements, element)

		switch separator {
		case ',':
			continue
		case ';':
			if elements[0] == "" {
				return Instruction{}, fmt.Errorf("%w: empty opcode", ErrMalformedInstruction)
			}
			return Instruction{
				Opcode: elements[0],
				Args:   append([]string(nil), elements[1:]...),
			}, nil
		default:
			return Instruction{}, fmt.Errorf("%w: invalid separator", ErrMalformedInstruction)
		}
	}
}

func (d *Decoder) readElement(alreadyConsumed int) (string, byte, int, error) {
	length, prefixBytes, err := d.readElementLength()
	if err != nil {
		return "", 0, prefixBytes, err
	}
	if length > d.limits.MaxElementLength {
		return "", 0, prefixBytes, ErrElementTooLarge
	}
	if alreadyConsumed+prefixBytes > d.limits.MaxInstructionLength {
		return "", 0, prefixBytes, ErrInstructionTooLarge
	}

	var value bytes.Buffer
	if length > 0 {
		value.Grow(length)
	}
	consumed := prefixBytes
	for i := 0; i < length; i++ {
		r, size, readErr := d.reader.ReadRune()
		if readErr != nil {
			return "", 0, consumed, fmt.Errorf("%w: truncated element: %w", ErrMalformedInstruction, readErr)
		}
		consumed += size
		if r == utf8.RuneError && size == 1 {
			return "", 0, consumed, fmt.Errorf("%w: invalid utf-8", ErrMalformedInstruction)
		}
		if alreadyConsumed+consumed > d.limits.MaxInstructionLength {
			return "", 0, consumed, ErrInstructionTooLarge
		}
		value.WriteRune(r)
	}

	separator, readErr := d.reader.ReadByte()
	if readErr != nil {
		return "", 0, consumed, fmt.Errorf("%w: missing separator: %w", ErrMalformedInstruction, readErr)
	}
	consumed++
	if separator != ',' && separator != ';' {
		return "", 0, consumed, fmt.Errorf("%w: invalid separator", ErrMalformedInstruction)
	}
	return value.String(), separator, consumed, nil
}

func (d *Decoder) readElementLength() (int, int, error) {
	length := 0
	digits := 0
	consumed := 0
	for {
		next, err := d.reader.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) && consumed == 0 {
				return 0, 0, io.EOF
			}
			return 0, consumed, fmt.Errorf("%w: truncated length: %w", ErrMalformedInstruction, err)
		}
		consumed++
		if next == '.' {
			if digits == 0 {
				return 0, consumed, fmt.Errorf("%w: empty length", ErrMalformedInstruction)
			}
			return length, consumed, nil
		}
		if next < '0' || next > '9' {
			return 0, consumed, fmt.Errorf("%w: non-decimal length", ErrMalformedInstruction)
		}
		digits++
		if digits > 10 {
			return 0, consumed, ErrElementTooLarge
		}
		digit := int(next - '0')
		if length > (d.limits.MaxElementLength-digit)/10 {
			return 0, consumed, ErrElementTooLarge
		}
		length = length*10 + digit
	}
}

func normalizeLimits(limits Limits) Limits {
	if limits.MaxElementLength <= 0 {
		limits.MaxElementLength = defaultMaxElementLength
	}
	if limits.MaxInstructionLength <= 0 {
		limits.MaxInstructionLength = defaultMaxInstructionLength
	}
	if limits.MaxElements <= 0 {
		limits.MaxElements = defaultMaxElements
	}
	return limits
}
