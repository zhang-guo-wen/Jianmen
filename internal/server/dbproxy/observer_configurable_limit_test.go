package dbproxy

import (
	"bytes"
	"testing"
)

func TestQueryObserversHonorConfiguredClientMessageLimit(t *testing.T) {
	const configuredLimit = 512 * 1024

	tests := []struct {
		name        string
		protocol    string
		legacyLimit int
		build       func(*testing.T, int) []byte
	}{
		{
			name:        "mysql",
			protocol:    "mysql",
			legacyLimit: maxMySQLObserverBufferBytes,
			build:       buildMySQLQueryWithFrameSize,
		},
		{
			name:        "postgres",
			protocol:    "postgres",
			legacyLimit: maxPostgresObserverBufferBytes,
			build:       buildPostgresQueryWithFrameSize,
		},
		{
			name:        "redis",
			protocol:    "redis",
			legacyLimit: maxRedisObserverBufferBytes,
			build:       redisCommandWithExactLength,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			safe := test.build(t, configuredLimit)
			if len(safe) <= test.legacyLimit {
				t.Fatalf("safe request length = %d, want above legacy limit %d", len(safe), test.legacyLimit)
			}
			observer := newQueryObserverWithLimit(test.protocol, nil, configuredLimit)
			forward, decision := observer.(relayClientQueryObserver).ObserveClientRelayBytes(safe)
			if decision != nil {
				t.Fatalf("request at configured limit denied: %#v", decision)
			}
			if !bytes.Equal(forward, safe) {
				t.Fatalf("forwarded request length = %d, want %d", len(forward), len(safe))
			}

			oversized := test.build(t, configuredLimit+1)
			observer = newQueryObserverWithLimit(test.protocol, nil, configuredLimit)
			forward, decision = observer.(relayClientQueryObserver).ObserveClientRelayBytes(oversized)
			if len(forward) != 0 {
				t.Fatalf("oversized request forwarded %d bytes", len(forward))
			}
			if decision == nil || decision.Allowed || decision.ErrorCode != observerErrorBufferLimit {
				t.Fatalf("oversized request decision = %#v, want %s", decision, observerErrorBufferLimit)
			}
		})
	}
}

func TestQueryObserverDefaultClientMessageLimitIsTenMiB(t *testing.T) {
	if defaultMaxClientMessageBytes != 10*1024*1024 {
		t.Fatalf("default client message limit = %d, want 10 MiB", defaultMaxClientMessageBytes)
	}
	tests := []struct {
		protocol string
		build    func(*testing.T, int) []byte
	}{
		{protocol: "mysql", build: buildMySQLQueryWithFrameSize},
		{protocol: "postgres", build: buildPostgresQueryWithFrameSize},
		{protocol: "redis", build: redisCommandWithExactLength},
	}
	for _, test := range tests {
		t.Run(test.protocol, func(t *testing.T) {
			request := test.build(t, defaultMaxClientMessageBytes)
			observer := newQueryObserver(test.protocol, nil)
			forward, decision := observer.(relayClientQueryObserver).
				ObserveClientRelayBytes(request)
			if decision != nil {
				t.Fatalf("10 MiB request denied: %#v", decision)
			}
			if !bytes.Equal(forward, request) {
				t.Fatalf(
					"forwarded request length = %d, want %d",
					len(forward),
					len(request),
				)
			}
		})
	}
}

func TestPostgresCopyDataRemainsStreamedOutsideQueryLimit(t *testing.T) {
	const configuredLimit = 64 * 1024
	copyData := postgresMessage(
		'd',
		bytes.Repeat([]byte{'x'}, maxPostgresObserverBufferBytes+1024),
	)
	observer := newQueryObserverWithLimit("postgres", nil, configuredLimit)

	forward, decision := feedObserverChunks(
		observer.(relayClientQueryObserver).ObserveClientRelayBytes,
		copyData,
		32*1024,
	)
	if decision != nil {
		t.Fatalf("PostgreSQL CopyData denied by query limit: %#v", decision)
	}
	if !bytes.Equal(forward, copyData) {
		t.Fatalf("forwarded CopyData length = %d, want %d", len(forward), len(copyData))
	}
}

func buildMySQLQueryWithFrameSize(t *testing.T, total int) []byte {
	t.Helper()
	if total < 5 || total > 0xffffff+4 {
		t.Fatalf("invalid MySQL test frame size %d", total)
	}
	payload := bytes.Repeat([]byte{'x'}, total-4)
	payload[0] = 0x03
	return buildMySQLPacket(0, payload)
}

func buildPostgresQueryWithFrameSize(t *testing.T, total int) []byte {
	t.Helper()
	if total < 6 {
		t.Fatalf("invalid PostgreSQL test frame size %d", total)
	}
	payload := bytes.Repeat([]byte{'x'}, total-5)
	payload[len(payload)-1] = 0
	return postgresMessage('Q', payload)
}
