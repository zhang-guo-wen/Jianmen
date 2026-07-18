package dbproxy

import (
	"bytes"
	"testing"
)

type observerByteChunkCase struct {
	name     string
	setup    func(t *testing.T) (func([]byte) ([]byte, *queryDecision), func() *queryDecision)
	safe     []byte
	fatal    []byte
	deferred bool
}

func TestDatabaseObserverFramesRemainAtomicAtEveryByteBoundary(t *testing.T) {
	mysqlQuery := buildMySQLPacket(0, append([]byte{0x03}, []byte("select 1")...))
	mysqlOK := buildMySQLPacket(1, []byte{0x00, 0, 0, 0, 0, 0, 0})
	postgresQuery := postgresMessage('Q', []byte("select 1\x00"))
	postgresCopyData := postgresMessage('d', []byte("copy-data"))
	redisPing := []byte("*1\r\n$4\r\nPING\r\n")
	redisPong := []byte("+PONG\r\n")

	tests := []observerByteChunkCase{
		{
			name: "mysql client",
			setup: func(t *testing.T) (func([]byte) ([]byte, *queryDecision), func() *queryDecision) {
				observer := &mysqlObserver{}
				return observer.ObserveClientRelayBytes, nil
			},
			safe:  mysqlQuery,
			fatal: buildMySQLPacket(0, []byte{0x11}),
		},
		{
			name: "mysql server",
			setup: func(t *testing.T) (func([]byte) ([]byte, *queryDecision), func() *queryDecision) {
				observer := &mysqlObserver{}
				forward, decision := observer.ObserveClientRelayBytes(mysqlQuery)
				requireMySQLRelayForward(t, forward, decision, mysqlQuery)
				return observer.ObserveServerRelayBytes, nil
			},
			safe:  mysqlOK,
			fatal: buildMySQLPacket(2, []byte{0x00}),
		},
		{
			name: "postgres client",
			setup: func(t *testing.T) (func([]byte) ([]byte, *queryDecision), func() *queryDecision) {
				observer := &postgresObserver{startupDone: true}
				return observer.ObserveClientRelayBytes, nil
			},
			safe:  postgresQuery,
			fatal: []byte{'Q', 0, 0, 0, 3},
		},
		{
			name: "postgres server",
			setup: func(t *testing.T) (func([]byte) ([]byte, *queryDecision), func() *queryDecision) {
				observer := &postgresObserver{startupDone: true}
				return observer.ObserveServerRelayBytes, nil
			},
			safe:  postgresCopyData,
			fatal: []byte{'Z', 0, 0, 0, 3},
		},
		{
			name: "redis client",
			setup: func(t *testing.T) (func([]byte) ([]byte, *queryDecision), func() *queryDecision) {
				observer := &redisObserver{}
				return observer.ObserveClientRelayBytes, func() *queryDecision {
					_, decision := feedObserverOneByte(observer.ObserveServerRelayBytes, redisPong)
					return decision
				}
			},
			safe:     redisPing,
			fatal:    []byte("*1\r\n$7\r\nFLUSHDB\r\n"),
			deferred: true,
		},
		{
			name: "redis server",
			setup: func(t *testing.T) (func([]byte) ([]byte, *queryDecision), func() *queryDecision) {
				observer := &redisObserver{}
				forward, decision := observer.ObserveClientRelayBytes(redisPing)
				requireMySQLRelayForward(t, forward, decision, redisPing)
				return observer.ObserveServerRelayBytes, nil
			},
			safe:  redisPong,
			fatal: []byte("!\r\n"),
		},
	}

	for _, test := range tests {
		t.Run(test.name+"/safe", func(t *testing.T) {
			relay, _ := test.setup(t)
			forward, decision := feedObserverOneByte(relay, test.safe)
			if decision != nil || !bytes.Equal(forward, test.safe) {
				t.Fatalf("safe relay = (%x, %#v), want original frame", forward, decision)
			}
		})

		t.Run(test.name+"/fatal", func(t *testing.T) {
			relay, _ := test.setup(t)
			forward, decision := feedObserverOneByte(relay, test.fatal)
			if len(forward) != 0 {
				t.Fatalf("fatal frame leaked bytes: %x", forward)
			}
			requireObserverDenied(t, decision)
		})

		t.Run(test.name+"/safe_prefix_fatal", func(t *testing.T) {
			relay, finishDeferred := test.setup(t)
			forward, decision := feedObserverOneByte(relay, append(append([]byte(nil), test.safe...), test.fatal...))
			if !bytes.Equal(forward, test.safe) {
				t.Fatalf("safe prefix = %x, want %x", forward, test.safe)
			}
			if test.deferred && decision == nil {
				decision = finishDeferred()
			}
			requireObserverDenied(t, decision)
		})
	}
}

func feedObserverOneByte(
	relay func([]byte) ([]byte, *queryDecision),
	data []byte,
) ([]byte, *queryDecision) {
	var forward []byte
	for _, value := range data {
		chunk, decision := relay([]byte{value})
		forward = append(forward, chunk...)
		if decision != nil {
			return forward, decision
		}
	}
	return forward, nil
}
