package dbproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

var errRedisUpstreamAuthenticationDenied = errors.New("redis upstream authentication denied")

func redisUpstreamAuthenticationDeadline(ctx context.Context, now time.Time) time.Time {
	deadline := now.Add(protocolHandshakeTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		return contextDeadline
	}
	return deadline
}

// authenticateUpstreamRedis uses the absolute deadline computed before dialing
// so authentication never extends the shared five-second upstream handshake.
func authenticateUpstreamRedis(conn net.Conn, username, password string, deadline time.Time) (err error) {
	latestDeadline := time.Now().Add(protocolHandshakeTimeout)
	if deadline.IsZero() || deadline.After(latestDeadline) {
		deadline = latestDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set Redis upstream authentication deadline: %w", err)
	}
	defer func() {
		if clearErr := conn.SetDeadline(time.Time{}); clearErr != nil {
			err = errors.Join(err, fmt.Errorf("clear Redis upstream authentication deadline: %w", clearErr))
		}
	}()

	var authCommand string
	if username != "" {
		authCommand = fmt.Sprintf("*3\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n$%d\r\n%s\r\n",
			len(username), username, len(password), password)
	} else {
		authCommand = fmt.Sprintf("*2\r\n$4\r\nAUTH\r\n$%d\r\n%s\r\n",
			len(password), password)
	}

	if _, err := fmt.Fprint(conn, authCommand); err != nil {
		return fmt.Errorf("send Redis upstream authentication: %w", err)
	}

	line, err := readRESPLine(bufio.NewReader(conn))
	if err != nil {
		return fmt.Errorf("read Redis upstream authentication response: %w", err)
	}
	if len(line) == 0 {
		return errors.New("empty Redis upstream authentication response")
	}
	switch line[0] {
	case '+':
		return nil
	case '-':
		return errRedisUpstreamAuthenticationDenied
	default:
		return errors.New("unexpected Redis upstream authentication response")
	}
}
