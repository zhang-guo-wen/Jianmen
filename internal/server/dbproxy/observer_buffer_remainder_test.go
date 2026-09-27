package dbproxy

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestObserversConsumeSafeFrameBeforeApplyingRemainderLimit(t *testing.T) {
	t.Run("mysql client", func(t *testing.T) {
		observer := &mysqlObserver{}
		safe := buildMySQLPacket(0, append([]byte{0x03}, bytes.Repeat([]byte{'x'}, maxMySQLObserverBufferBytes-5)...))
		fatal := buildMySQLPacket(0, []byte{0x11})
		requireSafePrefixAfterNearLimitSplit(
			t,
			safe,
			fatal,
			observer.ObserveClientRelayBytes,
			false,
		)
	})

	t.Run("mysql server", func(t *testing.T) {
		observer := &mysqlObserver{}
		query := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
		forward, decision := observer.ObserveClientRelayBytes(query)
		requireMySQLRelayForward(t, forward, decision, query)
		okPayload := make([]byte, maxMySQLObserverBufferBytes-4)
		okPayload[0] = 0x00
		safe := buildMySQLPacket(1, okPayload)
		fatal := buildMySQLPacket(99, []byte{0x00})
		requireSafePrefixAfterNearLimitSplit(
			t,
			safe,
			fatal,
			observer.ObserveServerRelayBytes,
			false,
		)
	})

	t.Run("postgres client", func(t *testing.T) {
		observer := &postgresObserver{startupDone: true}
		payload := bytes.Repeat([]byte{'x'}, maxPostgresObserverBufferBytes-5)
		payload[len(payload)-1] = 0
		safe := postgresMessage('Q', payload)
		fatal := []byte{'Q', 0, 0, 0, 3}
		requireSafePrefixAfterNearLimitSplit(
			t,
			safe,
			fatal,
			observer.ObserveClientRelayBytes,
			false,
		)
	})

	t.Run("postgres server", func(t *testing.T) {
		observer := &postgresObserver{startupDone: true}
		safe := postgresMessage('d', bytes.Repeat([]byte{'x'}, maxPostgresObserverBufferBytes-5))
		fatal := []byte{'Z', 0, 0, 0, 3}
		requireSafePrefixAfterNearLimitSplit(
			t,
			safe,
			fatal,
			observer.ObserveServerRelayBytes,
			false,
		)
	})

	t.Run("redis client", func(t *testing.T) {
		observer := &redisObserver{}
		safe := redisCommandWithExactLength(t, maxRedisObserverBufferBytes)
		fatal := []byte("*1\r\n$7\r\nFLUSHDB\r\n")
		requireSafePrefixAfterNearLimitSplit(
			t,
			safe,
			fatal,
			observer.ObserveClientRelayBytes,
			true,
		)
		response := []byte("+OK\r\n")
		forward, decision := observer.ObserveServerRelayBytes(response)
		if !bytes.Equal(forward, response) {
			t.Fatalf("redis response safe prefix = %q, want %q", forward, response)
		}
		requireObserverDenied(t, decision)
	})

	t.Run("redis server", func(t *testing.T) {
		observer := &redisObserver{}
		command := []byte("*2\r\n$3\r\nGET\r\n$1\r\nk\r\n")
		forward, decision := observer.ObserveClientRelayBytes(command)
		requireMySQLRelayForward(t, forward, decision, command)
		safe := redisBulkWithExactLength(t, maxRedisObserverBufferBytes)
		fatal := []byte("!\r\n")
		requireSafePrefixAfterNearLimitSplit(
			t,
			safe,
			fatal,
			observer.ObserveServerRelayBytes,
			false,
		)
	})
}

func requireSafePrefixAfterNearLimitSplit(
	t *testing.T,
	safe []byte,
	fatal []byte,
	relay func([]byte) ([]byte, *queryDecision),
	deferred bool,
) {
	t.Helper()
	if len(safe) < 2 {
		t.Fatal("safe frame too short")
	}
	forward, decision := relay(safe[:len(safe)-1])
	if len(forward) != 0 || decision != nil {
		t.Fatalf("partial near-limit frame relay = (%x, %#v)", forward, decision)
	}
	forward, decision = relay(append(append([]byte(nil), safe[len(safe)-1:]...), fatal...))
	if !bytes.Equal(forward, safe) {
		t.Fatalf("safe prefix length = %d, want %d", len(forward), len(safe))
	}
	if deferred {
		if decision != nil {
			t.Fatalf("deferred fatal decision = %#v, want nil until response drain", decision)
		}
		return
	}
	requireObserverDenied(t, decision)
}

func redisCommandWithExactLength(t *testing.T, total int) []byte {
	t.Helper()
	for payloadLength := total; payloadLength >= 0; payloadLength-- {
		command := []byte(fmt.Sprintf(
			"*3\r\n$3\r\nSET\r\n$1\r\nk\r\n$%d\r\n%s\r\n",
			payloadLength,
			strings.Repeat("x", payloadLength),
		))
		if len(command) == total {
			return command
		}
	}
	t.Fatalf("cannot build Redis command of %d bytes", total)
	return nil
}

func redisBulkWithExactLength(t *testing.T, total int) []byte {
	t.Helper()
	for payloadLength := total; payloadLength >= 0; payloadLength-- {
		response := []byte(fmt.Sprintf("$%d\r\n%s\r\n", payloadLength, strings.Repeat("x", payloadLength)))
		if len(response) == total {
			return response
		}
	}
	t.Fatalf("cannot build Redis bulk response of %d bytes", total)
	return nil
}
