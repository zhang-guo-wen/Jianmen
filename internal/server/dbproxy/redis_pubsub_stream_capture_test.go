package dbproxy

import (
	"strconv"
	"strings"
	"testing"
)

func TestRedisStreamPubSubCaptureDoesNotAllocateDeclaredTopicLength(t *testing.T) {
	const topicBytes = 1024 * 1024
	topic := strings.Repeat("t", topicBytes)
	slot := &redisResponseSlot{
		command:    "SUBSCRIBE",
		pubSubArgs: []string{"SUBSCRIBE", topic},
	}
	capture := newRedisStreamPubSubCapture('>', 2*topicBytes, slot)

	if decision := capture.consumeHeader([]byte(">3\r\n")); decision != nil {
		t.Fatalf("root header denied: %#v", decision)
	}
	if decision := capture.consumeHeader([]byte("$9\r\n")); decision != nil {
		t.Fatalf("kind header denied: %#v", decision)
	}
	if decision := capture.consumeBulk([]byte("subscribe")); decision != nil {
		t.Fatalf("kind body denied: %#v", decision)
	}
	capture.finishBulk()
	topicHeader := []byte("$" + strconv.Itoa(topicBytes) + "\r\n")
	if decision := capture.consumeHeader(topicHeader); decision != nil {
		t.Fatalf("topic header denied: %#v", decision)
	}

	if capture.bulkValue != nil || cap(capture.bulkValue) != 0 {
		t.Fatalf(
			"topic header allocated payload buffer len=%d cap=%d",
			len(capture.bulkValue),
			cap(capture.bulkValue),
		)
	}
	if len(capture.expectedTopics) != 1 || capture.expectedTopics[0] != topic {
		t.Fatalf("expected topic candidates = %d, want client topic reuse", len(capture.expectedTopics))
	}
}

func TestRedisConfiguredLargePubSubACKPreservesSubscriptionState(t *testing.T) {
	const configuredLimit = 128 * 1024
	topic := strings.Repeat("l", maxRedisObserverBufferBytes+1024)
	subscribe := redisObserverTestCommand("SUBSCRIBE", topic)
	if len(subscribe) >= configuredLimit {
		t.Fatalf("large SUBSCRIBE length = %d, want below configured limit %d", len(subscribe), configuredLimit)
	}

	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			sink := &redisSlotSink{}
			observer := &redisObserver{
				sink:                  sink,
				maxClientMessageBytes: configuredLimit,
			}
			commands := append(append([]byte(nil), subscribe...), redisObserverTestCommand("PING")...)
			if decision := observer.ObserveClientBytes(commands); decision != nil {
				t.Fatalf("large SUBSCRIBE pipeline denied: %#v", decision)
			}

			acknowledgement := redisPubSubAck(protocolVersion, "subscribe", topic, 1)
			if len(acknowledgement) <= maxRedisObserverBufferBytes {
				t.Fatalf("large acknowledgement length = %d, want streaming path", len(acknowledgement))
			}
			feedRedisPubSubStream(t, observer, acknowledgement)
			if len(observer.slots) != 1 {
				t.Fatalf("large acknowledgement consumed PING slot: slots=%d", len(observer.slots))
			}
			if _, subscribed := observer.subscriptions.channels[topic]; !subscribed {
				t.Fatal("large acknowledgement did not update subscription state")
			}

			pong := []byte("+PONG\r\n")
			if protocolVersion == 2 {
				pong = redisRESP2PubSubFrame("pong", "")
			}
			if _, decision := observer.ObserveServerRelayBytes(pong); decision != nil {
				t.Fatalf("PONG denied after large acknowledgement: %#v", decision)
			}
			if len(observer.slots) != 0 || len(sink.finished) != 1 {
				t.Fatalf("completion slots=%d audit finishes=%d", len(observer.slots), len(sink.finished))
			}

			if decision := observer.ObserveClientBytes(
				redisObserverTestCommand("UNSUBSCRIBE", topic),
			); decision != nil {
				t.Fatalf("large UNSUBSCRIBE denied: %#v", decision)
			}
			feedRedisPubSubStream(
				t,
				observer,
				redisPubSubAck(protocolVersion, "unsubscribe", topic, 0),
			)
			if observer.subscriptions.active() || len(observer.slots) != 0 || len(sink.finished) != 2 {
				t.Fatalf(
					"large unsubscribe subscriptions=%#v slots=%d audit finishes=%d",
					observer.subscriptions,
					len(observer.slots),
					len(sink.finished),
				)
			}
		})
	}
}

func TestRedisPubSubACKTopicMustMatchPendingCommand(t *testing.T) {
	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			observer := &redisObserver{sink: &redisSlotSink{}}
			if decision := observer.ObserveClientBytes(
				redisObserverTestCommand("SUBSCRIBE", "expected"),
			); decision != nil {
				t.Fatalf("SUBSCRIBE denied: %#v", decision)
			}

			forward, decision := observer.ObserveServerRelayBytes(
				redisPubSubAck(protocolVersion, "subscribe", "attacker", 1),
			)
			if len(forward) != 0 ||
				decision == nil ||
				decision.ErrorCode != observerErrorProtocol ||
				!strings.Contains(decision.ErrorMessage, "does not match") {
				t.Fatalf(
					"mismatched acknowledgement forward=%d decision=%#v",
					len(forward),
					decision,
				)
			}
			if observer.subscriptions.active() {
				t.Fatalf("mismatched acknowledgement changed subscriptions: %#v", observer.subscriptions)
			}
		})
	}
}

func TestRedisStreamedPubSubACKRejectsDivergentTopicEarly(t *testing.T) {
	const configuredLimit = 128 * 1024
	expected := strings.Repeat("a", maxRedisObserverBufferBytes+1024)
	hostile := "b" + expected[1:]

	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			observer := &redisObserver{
				sink:                  &redisSlotSink{},
				maxClientMessageBytes: configuredLimit,
			}
			if decision := observer.ObserveClientBytes(
				redisObserverTestCommand("SUBSCRIBE", expected),
			); decision != nil {
				t.Fatalf("SUBSCRIBE denied: %#v", decision)
			}

			acknowledgement := redisPubSubAck(protocolVersion, "subscribe", hostile, 1)
			var terminal *queryDecision
			var forwarded int
			for offset := 0; offset < len(acknowledgement) && terminal == nil; {
				end := offset + 4096
				if end > len(acknowledgement) {
					end = len(acknowledgement)
				}
				chunk, decision := observer.ObserveServerRelayBytes(acknowledgement[offset:end])
				forwarded += len(chunk)
				terminal = decision
				offset = end
			}
			if terminal == nil ||
				terminal.ErrorCode != observerErrorProtocol ||
				!strings.Contains(terminal.ErrorMessage, "does not match") {
				t.Fatalf("divergent acknowledgement decision = %#v", terminal)
			}
			if forwarded >= len(acknowledgement) {
				t.Fatalf("divergent acknowledgement forwarded in full: %d bytes", forwarded)
			}
			if observer.subscriptions.active() {
				t.Fatalf("divergent acknowledgement changed subscriptions: %#v", observer.subscriptions)
			}
		})
	}
}

func TestRedisPubSubACKTopicOverConfiguredLimitIsRejected(t *testing.T) {
	const configuredLimit = 128 * 1024
	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			observer := &redisObserver{
				sink:                  &redisSlotSink{},
				maxClientMessageBytes: configuredLimit,
			}
			if decision := observer.ObserveClientBytes(
				redisObserverTestCommand("SUBSCRIBE", "expected"),
			); decision != nil {
				t.Fatalf("SUBSCRIBE denied: %#v", decision)
			}

			acknowledgement := redisPubSubAck(
				protocolVersion,
				"subscribe",
				strings.Repeat("x", configuredLimit+1),
				1,
			)
			var terminal *queryDecision
			var forwarded int
			for offset := 0; offset < len(acknowledgement) && terminal == nil; {
				end := offset + 4096
				if end > len(acknowledgement) {
					end = len(acknowledgement)
				}
				chunk, decision := observer.ObserveServerRelayBytes(acknowledgement[offset:end])
				forwarded += len(chunk)
				terminal = decision
				offset = end
			}
			if terminal == nil ||
				terminal.ErrorCode != observerErrorBufferLimit ||
				!strings.Contains(terminal.ErrorMessage, "configured client message limit") {
				t.Fatalf("over-limit acknowledgement decision = %#v", terminal)
			}
			if forwarded >= len(acknowledgement) {
				t.Fatalf("over-limit acknowledgement forwarded in full: %d bytes", forwarded)
			}
		})
	}
}
