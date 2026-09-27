package dbproxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
)

const (
	postgresProtocolVersion30       = 196608
	maxPostgresStartupMessageBytes  = 16 * 1024
	maxPostgresAuthMessageBytes     = 64 * 1024
	minPostgresStartupMessageBytes  = 8
	postgresMessageLengthFieldBytes = 4
	postgresTypedMessageHeaderBytes = 5
)

type postgresWireMessage struct {
	kind    byte
	payload []byte
}

type postgresStartupParameter struct {
	name  string
	value string
}

type postgresStartup struct {
	protocolMinor      uint32
	parameters         []postgresStartupParameter
	unsupportedOptions []string
	username           string
	database           string
}

func (message postgresWireMessage) raw() []byte {
	raw := make([]byte, postgresTypedMessageHeaderBytes+len(message.payload))
	raw[0] = message.kind
	binary.BigEndian.PutUint32(raw[1:5], uint32(postgresMessageLengthFieldBytes+len(message.payload)))
	copy(raw[5:], message.payload)
	return raw
}

func readPostgresStartupMessage(conn net.Conn, firstByte byte) ([]byte, error) {
	header := make([]byte, postgresMessageLengthFieldBytes)
	header[0] = firstByte
	if _, err := io.ReadFull(conn, header[1:]); err != nil {
		return nil, fmt.Errorf("read PostgreSQL startup header: %w", err)
	}
	messageLength := int(binary.BigEndian.Uint32(header))
	if messageLength < minPostgresStartupMessageBytes || messageLength > maxPostgresStartupMessageBytes {
		return nil, fmt.Errorf("invalid PostgreSQL StartupMessage length %d", messageLength)
	}
	message := make([]byte, messageLength)
	copy(message, header)
	if _, err := io.ReadFull(conn, message[len(header):]); err != nil {
		return nil, fmt.Errorf("read PostgreSQL StartupMessage: %w", err)
	}
	return message, nil
}

func parsePostgresStartupMessage(message []byte) (string, string, error) {
	startup, err := parsePostgresStartup(message)
	if err != nil {
		return "", "", err
	}
	return startup.username, startup.database, nil
}

func parsePostgresStartup(message []byte) (postgresStartup, error) {
	if len(message) < minPostgresStartupMessageBytes || len(message) > maxPostgresStartupMessageBytes {
		return postgresStartup{}, fmt.Errorf("invalid PostgreSQL StartupMessage size %d", len(message))
	}
	if declared := int(binary.BigEndian.Uint32(message[:4])); declared != len(message) {
		return postgresStartup{}, fmt.Errorf(
			"PostgreSQL StartupMessage length mismatch: declared %d, read %d",
			declared,
			len(message),
		)
	}
	protocol := binary.BigEndian.Uint32(message[4:8])
	if protocol>>16 != postgresProtocolVersion30>>16 {
		return postgresStartup{}, fmt.Errorf("unsupported PostgreSQL protocol version %d", protocol)
	}
	startup := postgresStartup{protocolMinor: protocol & 0xffff}
	parameters := message[8:]
	if len(parameters) == 0 || parameters[len(parameters)-1] != 0 {
		return postgresStartup{}, errors.New("PostgreSQL StartupMessage is missing its final terminator")
	}

	sawFinalTerminator := false
	sawUser := false
	sawDatabase := false
	for position := 0; position < len(parameters); {
		if parameters[position] == 0 {
			if position != len(parameters)-1 {
				return postgresStartup{}, errors.New(
					"PostgreSQL StartupMessage contains data after its final terminator",
				)
			}
			sawFinalTerminator = true
			break
		}
		keyEnd := indexPostgresNUL(parameters, position)
		if keyEnd < 0 {
			return postgresStartup{}, errors.New("PostgreSQL StartupMessage key is not terminated")
		}
		valueStart := keyEnd + 1
		valueEnd := indexPostgresNUL(parameters, valueStart)
		if valueEnd < 0 {
			return postgresStartup{}, errors.New("PostgreSQL StartupMessage value is not terminated")
		}
		key := string(parameters[position:keyEnd])
		value := string(parameters[valueStart:valueEnd])
		if key == "" {
			return postgresStartup{}, errors.New("PostgreSQL StartupMessage parameter name is empty")
		}
		startup.parameters = append(startup.parameters, postgresStartupParameter{name: key, value: value})
		switch key {
		case "user":
			if sawUser {
				return postgresStartup{}, errors.New(
					"PostgreSQL StartupMessage contains duplicate user parameters",
				)
			}
			sawUser = true
			startup.username = value
		case "database":
			if sawDatabase {
				return postgresStartup{}, errors.New(
					"PostgreSQL StartupMessage contains duplicate database parameters",
				)
			}
			sawDatabase = true
			startup.database = value
		default:
			if strings.HasPrefix(key, "_pq_.") {
				startup.unsupportedOptions = append(startup.unsupportedOptions, key)
			}
		}
		position = valueEnd + 1
	}
	if !sawFinalTerminator {
		return postgresStartup{}, errors.New("PostgreSQL StartupMessage is missing its final terminator")
	}
	if startup.username == "" {
		return postgresStartup{}, errors.New("PostgreSQL StartupMessage user is required")
	}
	return startup, nil
}

func readPostgresPasswordMessage(conn net.Conn) (string, error) {
	message, err := readPostgresMessage(conn, maxPostgresAuthMessageBytes)
	if err != nil {
		return "", err
	}
	if message.kind != 'p' {
		return "", fmt.Errorf("unexpected PostgreSQL password message type %q", message.kind)
	}
	if len(message.payload) == 0 || message.payload[len(message.payload)-1] != 0 {
		return "", errors.New("PostgreSQL PasswordMessage is missing its terminator")
	}
	if indexPostgresNUL(message.payload, 0) != len(message.payload)-1 {
		return "", errors.New("PostgreSQL PasswordMessage contains an embedded terminator")
	}
	return string(message.payload[:len(message.payload)-1]), nil
}

func readPostgresMessage(conn net.Conn, maxMessageBytes int) (postgresWireMessage, error) {
	if maxMessageBytes < postgresMessageLengthFieldBytes {
		return postgresWireMessage{}, errors.New("invalid PostgreSQL message size limit")
	}
	var header [postgresTypedMessageHeaderBytes]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		return postgresWireMessage{}, fmt.Errorf("read PostgreSQL message header: %w", err)
	}
	messageLength := int(binary.BigEndian.Uint32(header[1:]))
	if messageLength < postgresMessageLengthFieldBytes || messageLength > maxMessageBytes {
		return postgresWireMessage{}, fmt.Errorf("invalid PostgreSQL message length %d", messageLength)
	}
	payload := make([]byte, messageLength-postgresMessageLengthFieldBytes)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return postgresWireMessage{}, fmt.Errorf("read PostgreSQL message payload: %w", err)
	}
	return postgresWireMessage{kind: header[0], payload: payload}, nil
}

func writePostgresMessage(conn net.Conn, kind byte, payload []byte) error {
	if len(payload) > maxPostgresAuthMessageBytes-postgresMessageLengthFieldBytes {
		return fmt.Errorf("PostgreSQL message payload is too large: %d", len(payload))
	}
	message := postgresWireMessage{kind: kind, payload: payload}
	if err := writePostgresBytes(conn, message.raw()); err != nil {
		return fmt.Errorf("write PostgreSQL message: %w", err)
	}
	return nil
}

func writePostgresBytes(writer interface {
	Write([]byte) (int, error)
}, data []byte) error {
	for len(data) > 0 {
		written, err := writer.Write(data)
		if err != nil {
			return err
		}
		if written <= 0 || written > len(data) {
			return io.ErrShortWrite
		}
		data = data[written:]
	}
	return nil
}

func indexPostgresNUL(value []byte, start int) int {
	for index := start; index < len(value); index++ {
		if value[index] == 0 {
			return index
		}
	}
	return -1
}
