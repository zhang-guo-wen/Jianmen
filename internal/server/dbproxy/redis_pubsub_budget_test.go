package dbproxy

import (
	"bytes"
	"strings"
	"testing"
)

func TestRedisObserverCapsPendingPubSubTopicBytes(t *testing.T) {
	const limit = 256
	observer := &redisObserver{
		sink:                  &redisSlotSink{},
		maxClientMessageBytes: limit,
	}
	topic := strings.Repeat("t", 100)
	command := redisObserverTestCommand("UNSUBSCRIBE", topic)
	for index := 0; index < 2; index++ {
		if forward, decision := observer.ObserveClientRelayBytes(command); decision != nil || !bytes.Equal(forward, command) {
			t.Fatalf("UNSUBSCRIBE %d relay = (%d bytes, %#v), want accepted", index+1, len(forward), decision)
		}
	}
	forward, decision := observer.ObserveClientRelayBytes(command)
	if len(forward) != 0 || decision == nil || decision.ErrorCode != observerErrorPendingLimit {
		t.Fatalf("third UNSUBSCRIBE relay = (%d bytes, %#v), want pending topic byte limit", len(forward), decision)
	}
}

func TestRedisObserverCapsRetainedSubscriptionTopicBytes(t *testing.T) {
	const limit = 256
	observer := &redisObserver{
		sink:                  &redisSlotSink{},
		maxClientMessageBytes: limit,
	}
	for index := 0; index < 2; index++ {
		topic := strings.Repeat(string(rune('a'+index)), 100)
		command := redisObserverTestCommand("SUBSCRIBE", topic)
		if forward, decision := observer.ObserveClientRelayBytes(command); decision != nil || !bytes.Equal(forward, command) {
			t.Fatalf("SUBSCRIBE %d relay = (%d bytes, %#v), want accepted", index+1, len(forward), decision)
		}
		ack := redisRESP2PubSubAck("subscribe", topic, index+1)
		if _, decision := observer.ObserveServerRelayBytes(ack); decision != nil {
			t.Fatalf("SUBSCRIBE %d acknowledgement denied: %#v", index+1, decision)
		}
	}

	topic := strings.Repeat("z", 100)
	command := redisObserverTestCommand("SUBSCRIBE", topic)
	forward, decision := observer.ObserveClientRelayBytes(command)
	if len(forward) != 0 || decision == nil || decision.ErrorCode != observerErrorPendingLimit {
		t.Fatalf("third SUBSCRIBE relay = (%d bytes, %#v), want retained topic byte limit", len(forward), decision)
	}
}
