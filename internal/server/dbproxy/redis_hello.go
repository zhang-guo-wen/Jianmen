package dbproxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type redisInitialAuthentication struct {
	username     string
	password     string
	helloVersion int
	clientName   string
}

type redisInitialAuthenticationError uint8

const (
	redisInitialAuthenticationInvalid redisInitialAuthenticationError = iota
	redisInitialAuthenticationRequired
	redisInitialAuthenticationUnsupportedProtocol
)

func parseRedisInitialAuthentication(command string, args []string) (redisInitialAuthentication, redisInitialAuthenticationError, bool) {
	switch command {
	case "AUTH":
		if len(args) != 3 {
			return redisInitialAuthentication{}, redisInitialAuthenticationInvalid, false
		}
		return redisInitialAuthentication{username: args[1], password: args[2]}, 0, true
	case "HELLO":
		return parseRedisInitialHello(args)
	default:
		return redisInitialAuthentication{}, redisInitialAuthenticationRequired, false
	}
}

func parseRedisInitialHello(args []string) (redisInitialAuthentication, redisInitialAuthenticationError, bool) {
	if len(args) < 2 || (args[1] != "2" && args[1] != "3") {
		return redisInitialAuthentication{}, redisInitialAuthenticationUnsupportedProtocol, false
	}
	request := redisInitialAuthentication{helloVersion: int(args[1][0] - '0')}
	seenAuth := false
	seenSetName := false
	for index := 2; index < len(args); {
		switch strings.ToUpper(args[index]) {
		case "AUTH":
			if seenAuth || index+2 >= len(args) {
				return redisInitialAuthentication{}, redisInitialAuthenticationInvalid, false
			}
			request.username = args[index+1]
			request.password = args[index+2]
			seenAuth = true
			index += 3
		case "SETNAME":
			if seenSetName || index+1 >= len(args) || args[index+1] == "" {
				return redisInitialAuthentication{}, redisInitialAuthenticationInvalid, false
			}
			request.clientName = args[index+1]
			seenSetName = true
			index += 2
		default:
			return redisInitialAuthentication{}, redisInitialAuthenticationInvalid, false
		}
	}
	if !seenAuth {
		return redisInitialAuthentication{}, redisInitialAuthenticationRequired, false
	}
	return request, 0, true
}

func negotiateUpstreamRedisHello(conn net.Conn, version int, clientName string, deadline time.Time) (response []byte, err error) {
	latestDeadline := time.Now().Add(protocolHandshakeTimeout)
	if deadline.IsZero() || deadline.After(latestDeadline) {
		deadline = latestDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("set Redis upstream HELLO deadline: %w", err)
	}
	defer func() {
		if clearErr := conn.SetDeadline(time.Time{}); clearErr != nil {
			err = errors.Join(err, fmt.Errorf("clear Redis upstream HELLO deadline: %w", clearErr))
		}
	}()

	parts := []string{"HELLO", strconv.Itoa(version)}
	if clientName != "" {
		parts = append(parts, "SETNAME", clientName)
	}
	command := encodeRedisCommand(parts...)
	if _, err := io.Copy(conn, bytes.NewReader(command)); err != nil {
		return nil, fmt.Errorf("send Redis upstream HELLO: %w", err)
	}
	response, err = readRedisHandshakeFrame(conn)
	if err != nil {
		return nil, fmt.Errorf("read Redis upstream HELLO response: %w", err)
	}
	return response, nil
}

func readRedisHandshakeFrame(reader io.Reader) ([]byte, error) {
	frame := make([]byte, 0, 256)
	var next [1]byte
	for len(frame) <= maxRedisObserverBufferBytes {
		if _, err := io.ReadFull(reader, next[:]); err != nil {
			return nil, err
		}
		frame = append(frame, next[0])
		switch _, status := redisRESPFrameLength(frame); status {
		case redisRESPComplete:
			return frame, nil
		case redisRESPMalformed:
			return nil, errors.New("malformed Redis HELLO response")
		case redisRESPLimitExceeded:
			return nil, errors.New("Redis HELLO response exceeds limit")
		}
	}
	return nil, errors.New("Redis HELLO response exceeds limit")
}

func encodeRedisCommand(parts ...string) []byte {
	var command bytes.Buffer
	_, _ = fmt.Fprintf(&command, "*%d\r\n", len(parts))
	for _, part := range parts {
		_, _ = fmt.Fprintf(&command, "$%d\r\n", len(part))
		command.WriteString(part)
		command.WriteString("\r\n")
	}
	return command.Bytes()
}
