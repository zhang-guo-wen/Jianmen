package dbproxy

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestRedisRESP2PubSubAcknowledgementsAndMessagesPreserveSlots(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	if decision := observer.ObserveClientBytes(redisObserverTestCommand("SUBSCRIBE", "news", "alerts")); decision != nil {
		t.Fatalf("SUBSCRIBE denied: %#v", decision)
	}
	acknowledgements := bytes.Join([][]byte{
		redisRESP2PubSubAck("subscribe", "news", 1),
		redisRESP2PubSubAck("subscribe", "alerts", 2),
	}, nil)
	if _, decision := observer.ObserveServerRelayBytes(acknowledgements); decision != nil {
		t.Fatalf("SUBSCRIBE acknowledgements denied: %#v", decision)
	}
	if len(observer.slots) != 0 || len(sink.finished) != 1 {
		t.Fatalf("SUBSCRIBE slots=%d finished=%d, want one completed command", len(observer.slots), len(sink.finished))
	}

	message := redisRESP2PubSubFrame("message", "news", "payload")
	if forward, decision := observer.ObserveServerRelayBytes(message); decision != nil || !bytes.Equal(forward, message) {
		t.Fatalf("message forward=%q decision=%#v", forward, decision)
	}
	if len(sink.finished) != 1 {
		t.Fatal("unsolicited message consumed a response slot")
	}

	if decision := observer.ObserveClientBytes(redisObserverTestCommand("PING", "probe")); decision != nil {
		t.Fatalf("subscribed PING denied: %#v", decision)
	}
	pong := redisRESP2PubSubFrame("pong", "probe")
	responses := append(append([]byte(nil), message...), pong...)
	if _, decision := observer.ObserveServerRelayBytes(responses); decision != nil {
		t.Fatalf("message before PONG denied: %#v", decision)
	}
	if len(observer.slots) != 0 {
		t.Fatalf("message consumed PING slot or PONG was unmatched: slots=%d", len(observer.slots))
	}

	if decision := observer.ObserveClientBytes(redisObserverTestCommand("UNSUBSCRIBE")); decision != nil {
		t.Fatalf("UNSUBSCRIBE denied: %#v", decision)
	}
	unsubscribe := bytes.Join([][]byte{
		redisRESP2PubSubAck("unsubscribe", "news", 1),
		redisRESP2PubSubAck("unsubscribe", "alerts", 0),
	}, nil)
	if _, decision := observer.ObserveServerRelayBytes(unsubscribe); decision != nil {
		t.Fatalf("UNSUBSCRIBE acknowledgements denied: %#v", decision)
	}
	if observer.subscriptions.active() || len(observer.slots) != 0 {
		t.Fatalf("subscriptions active=%t slots=%d after unsubscribe", observer.subscriptions.active(), len(observer.slots))
	}
}

func TestRedisRESP3PubSubPushAcknowledgementDoesNotConsumeFollowingGET(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	commands := bytes.Join([][]byte{
		redisObserverTestCommand("SUBSCRIBE", "news"),
		redisObserverTestCommand("GET", "key"),
	}, nil)
	if decision := observer.ObserveClientBytes(commands); decision != nil {
		t.Fatalf("pipeline denied: %#v", decision)
	}

	subscribe := []byte(">3\r\n+subscribe\r\n$4\r\nnews\r\n:1\r\n")
	message := []byte(">3\r\n+message\r\n$4\r\nnews\r\n$7\r\npayload\r\n")
	get := []byte("$5\r\nvalue\r\n")
	responses := bytes.Join([][]byte{subscribe, message, get}, nil)
	if _, decision := observer.ObserveServerRelayBytes(responses); decision != nil {
		t.Fatalf("RESP3 pubsub pipeline denied: %#v", decision)
	}
	if observer.protocolVersion != 3 {
		t.Fatalf("protocol version = %d, want RESP3", observer.protocolVersion)
	}
	if len(observer.slots) != 0 || len(sink.finished) != 2 {
		t.Fatalf("slots=%d finished=%d, want SUBSCRIBE and GET completed", len(observer.slots), len(sink.finished))
	}
	if sink.finished[1].sql != "GET key" {
		t.Fatalf("second completed query = %q, want GET key", sink.finished[1].sql)
	}
}

func TestRedisPipelinedSubscribeAllUnsubscribePreservesFollowingPINGSlot(t *testing.T) {
	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			sink := &redisSlotSink{}
			observer := &redisObserver{sink: sink}
			commands := bytes.Join([][]byte{
				redisObserverTestCommand("SUBSCRIBE", "a", "b"),
				redisObserverTestCommand("UNSUBSCRIBE"),
				redisObserverTestCommand("PING"),
			}, nil)
			if decision := observer.ObserveClientBytes(commands); decision != nil {
				t.Fatalf("pipeline denied: %#v", decision)
			}
			if len(observer.slots) != 3 ||
				!observer.slots[1].unsubscribeAll ||
				len(observer.slots[1].unsubscribeTopics) != 2 {
				t.Fatalf("planned slots = %#v", observer.slots)
			}

			subscribe := bytes.Join([][]byte{
				redisPubSubAck(protocolVersion, "subscribe", "a", 1),
				redisPubSubAck(protocolVersion, "subscribe", "b", 2),
			}, nil)
			if _, decision := observer.ObserveServerRelayBytes(subscribe); decision != nil {
				t.Fatalf("subscribe acknowledgements denied: %#v", decision)
			}
			if len(observer.slots) != 2 || len(observer.subscriptions.channels) != 2 {
				t.Fatalf("after subscribe slots=%d channels=%v", len(observer.slots), observer.subscriptions.channels)
			}

			if _, decision := observer.ObserveServerRelayBytes(
				redisPubSubAck(protocolVersion, "unsubscribe", "a", 1),
			); decision != nil {
				t.Fatalf("first unsubscribe acknowledgement denied: %#v", decision)
			}
			if len(observer.slots) != 2 || len(observer.subscriptions.channels) != 1 {
				t.Fatalf("first unsubscribe consumed wrong slot: slots=%d channels=%v", len(observer.slots), observer.subscriptions.channels)
			}

			if _, decision := observer.ObserveServerRelayBytes(
				redisPubSubAck(protocolVersion, "unsubscribe", "b", 0),
			); decision != nil {
				t.Fatalf("second unsubscribe acknowledgement denied: %#v", decision)
			}
			if len(observer.slots) != 1 || observer.subscriptions.active() {
				t.Fatalf("unsubscribe completion slots=%d subscriptions=%#v", len(observer.slots), observer.subscriptions)
			}

			if _, decision := observer.ObserveServerRelayBytes([]byte("+PONG\r\n")); decision != nil {
				t.Fatalf("PONG denied: %#v", decision)
			}
			if len(observer.slots) != 0 || len(sink.finished) != 2 {
				t.Fatalf("PING completion slots=%d finished=%#v", len(observer.slots), sink.finished)
			}
		})
	}
}

func TestRedisPubSubACLErrorRebuildsIntentAndRecovers(t *testing.T) {
	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			sink := &redisSlotSink{}
			observer := &redisObserver{sink: sink}
			commands := bytes.Join([][]byte{
				redisObserverTestCommand("SUBSCRIBE", "forbidden"),
				redisObserverTestCommand("SUBSCRIBE", "allowed"),
				redisObserverTestCommand("UNSUBSCRIBE"),
				redisObserverTestCommand("PING"),
			}, nil)
			if decision := observer.ObserveClientBytes(commands); decision != nil {
				t.Fatalf("pipeline denied: %#v", decision)
			}
			if len(observer.slots) != 4 ||
				!observer.slots[2].unsubscribeAll ||
				len(observer.slots[2].unsubscribeTopics) != 2 {
				t.Fatalf("initial projected slots = %#v", observer.slots)
			}

			if _, decision := observer.ObserveServerRelayBytes(
				[]byte("-NOPERM this user has no permissions to run the command\r\n"),
			); decision != nil {
				t.Fatalf("ACL error response denied: %#v", decision)
			}
			if len(observer.slots) != 3 {
				t.Fatalf("slots after ACL error = %d, want 3", len(observer.slots))
			}
			unsubscribe := observer.slots[1]
			if !unsubscribe.unsubscribeAll ||
				len(unsubscribe.unsubscribeTopics) != 1 {
				t.Fatalf("rebuilt unsubscribe slot = %#v", unsubscribe)
			}
			if _, exists := unsubscribe.unsubscribeTopics["allowed"]; !exists {
				t.Fatalf("allowed topic missing from rebuilt snapshot: %v", unsubscribe.unsubscribeTopics)
			}
			if _, exists := unsubscribe.unsubscribeTopics["forbidden"]; exists {
				t.Fatalf("failed topic remained in rebuilt snapshot: %v", unsubscribe.unsubscribeTopics)
			}

			responses := bytes.Join([][]byte{
				redisPubSubAck(protocolVersion, "subscribe", "allowed", 1),
				redisPubSubAck(protocolVersion, "unsubscribe", "allowed", 0),
				[]byte("+PONG\r\n"),
			}, nil)
			if _, decision := observer.ObserveServerRelayBytes(responses); decision != nil {
				t.Fatalf("recovery responses denied: %#v", decision)
			}
			if len(observer.slots) != 0 ||
				observer.subscriptions.active() ||
				observer.subscriptionIntent.active() {
				t.Fatalf(
					"recovery slots=%d confirmed=%#v projected=%#v",
					len(observer.slots),
					observer.subscriptions,
					observer.subscriptionIntent,
				)
			}
			if len(sink.finished) != 3 ||
				sink.finished[0].finish.Status != queryStatusError ||
				sink.finished[1].finish.Status != queryStatusSuccess ||
				sink.finished[2].finish.Status != queryStatusSuccess {
				t.Fatalf("recovery audit finishes = %#v", sink.finished)
			}
		})
	}
}

func TestRedisRejectedSubscribeDoesNotAccumulateProjectedIntent(t *testing.T) {
	observer := &redisObserver{}
	for index := 0; index < 64; index++ {
		topic := "forbidden-" + strconv.Itoa(index)
		if decision := observer.ObserveClientBytes(
			redisObserverTestCommand("SUBSCRIBE", topic),
		); decision != nil {
			t.Fatalf("SUBSCRIBE %d denied by observer: %#v", index, decision)
		}
		if len(observer.subscriptionIntent.channels) != 1 {
			t.Fatalf("projected topics before error = %d, want 1", len(observer.subscriptionIntent.channels))
		}
		if _, decision := observer.ObserveServerRelayBytes(
			[]byte("-NOPERM this user has no permissions to run the command\r\n"),
		); decision != nil {
			t.Fatalf("ACL error %d denied: %#v", index, decision)
		}
		if len(observer.slots) != 0 || len(observer.subscriptionIntent.channels) != 0 {
			t.Fatalf(
				"ACL error %d left slots=%d projected=%v",
				index,
				len(observer.slots),
				observer.subscriptionIntent.channels,
			)
		}
	}
}

func TestRedisAllUnsubscribeCompletesWhilePatternSubscriptionRemains(t *testing.T) {
	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			observer := &redisObserver{sink: &redisSlotSink{}}
			commands := bytes.Join([][]byte{
				redisObserverTestCommand("SUBSCRIBE", "a", "b"),
				redisObserverTestCommand("PSUBSCRIBE", "events:*"),
				redisObserverTestCommand("UNSUBSCRIBE"),
				redisObserverTestCommand("PING"),
			}, nil)
			if decision := observer.ObserveClientBytes(commands); decision != nil {
				t.Fatalf("pipeline denied: %#v", decision)
			}
			responses := bytes.Join([][]byte{
				redisPubSubAck(protocolVersion, "subscribe", "a", 1),
				redisPubSubAck(protocolVersion, "subscribe", "b", 2),
				redisPubSubAck(protocolVersion, "psubscribe", "events:*", 3),
				redisPubSubAck(protocolVersion, "unsubscribe", "a", 2),
				redisPubSubAck(protocolVersion, "unsubscribe", "b", 1),
			}, nil)
			if _, decision := observer.ObserveServerRelayBytes(responses); decision != nil {
				t.Fatalf("acknowledgements denied: %#v", decision)
			}
			if len(observer.slots) != 1 ||
				len(observer.subscriptions.channels) != 0 ||
				len(observer.subscriptions.patterns) != 1 {
				t.Fatalf("category completion slots=%d subscriptions=%#v", len(observer.slots), observer.subscriptions)
			}
		})
	}
}

func TestRedisRESP2LargePubSubMessageStreamsWithoutConsumingSlot(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	if decision := observer.ObserveClientBytes(redisObserverTestCommand("SUBSCRIBE", "news")); decision != nil {
		t.Fatalf("SUBSCRIBE denied: %#v", decision)
	}
	if _, decision := observer.ObserveServerRelayBytes(
		redisRESP2PubSubAck("subscribe", "news", 1),
	); decision != nil {
		t.Fatalf("SUBSCRIBE acknowledgement denied: %#v", decision)
	}

	largePayload := strings.Repeat("x", maxRedisObserverBufferBytes+1024)
	message := redisRESP2PubSubFrame("message", "news", largePayload)
	var forwarded int
	for offset := 0; offset < len(message); {
		end := offset + 4096
		if end > len(message) {
			end = len(message)
		}
		chunk, decision := observer.ObserveServerRelayBytes(message[offset:end])
		if decision != nil {
			t.Fatalf("large RESP2 Pub/Sub message denied at %d: %#v", offset, decision)
		}
		forwarded += len(chunk)
		offset = end
	}
	if forwarded != len(message) || len(observer.slots) != 0 || len(sink.finished) != 1 {
		t.Fatalf(
			"large message forwarded=%d/%d slots=%d finished=%d",
			forwarded,
			len(message),
			len(observer.slots),
			len(sink.finished),
		)
	}

	if decision := observer.ObserveClientBytes(redisObserverTestCommand("PING", "probe")); decision != nil {
		t.Fatalf("subscribed PING denied: %#v", decision)
	}
	for offset := 0; offset < len(message); {
		end := offset + 4096
		if end > len(message) {
			end = len(message)
		}
		if _, decision := observer.ObserveServerRelayBytes(message[offset:end]); decision != nil {
			t.Fatalf("large message before PONG denied at %d: %#v", offset, decision)
		}
		offset = end
	}
	if len(observer.slots) != 1 {
		t.Fatalf("large unsolicited message consumed PING slot: slots=%d", len(observer.slots))
	}
	if _, decision := observer.ObserveServerRelayBytes(
		redisRESP2PubSubFrame("pong", "probe"),
	); decision != nil {
		t.Fatalf("subscribed PONG denied: %#v", decision)
	}
	if len(observer.slots) != 0 || len(sink.finished) != 1 {
		t.Fatalf("PING completion slots=%d finished=%d", len(observer.slots), len(sink.finished))
	}
}

func TestRedisBoundaryPubSubACKStreamsPreserveStateAndPINGSlot(t *testing.T) {
	topic := redisBoundaryObserverTopic(t)
	subscribeCommand := redisObserverTestCommand("SUBSCRIBE", topic)
	if len(subscribeCommand) != maxRedisObserverBufferBytes {
		t.Fatalf("boundary SUBSCRIBE length = %d, want %d", len(subscribeCommand), maxRedisObserverBufferBytes)
	}

	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			sink := &redisSlotSink{}
			observer := &redisObserver{sink: sink}
			commands := append(append([]byte(nil), subscribeCommand...), redisObserverTestCommand("PING")...)
			if decision := observer.ObserveClientBytes(commands); decision != nil {
				t.Fatalf("boundary pipeline denied: %#v", decision)
			}

			subscribeACK := redisPubSubAck(protocolVersion, "subscribe", topic, 1)
			if len(subscribeACK) <= maxRedisObserverBufferBytes {
				t.Fatalf("boundary acknowledgement length = %d, want streaming path", len(subscribeACK))
			}
			feedRedisPubSubStream(t, observer, subscribeACK)
			if len(observer.slots) != 1 {
				t.Fatalf("subscribe acknowledgement consumed PING slot: slots=%d", len(observer.slots))
			}
			if _, subscribed := observer.subscriptions.channels[topic]; !subscribed {
				t.Fatal("streamed subscribe acknowledgement did not update subscription state")
			}

			message := redisRESP2PubSubFrame("message", topic, "payload")
			if protocolVersion == 3 {
				message[0] = '>'
			}
			feedRedisPubSubStream(t, observer, message)
			if len(observer.slots) != 1 {
				t.Fatalf("streamed message consumed PING slot: slots=%d", len(observer.slots))
			}

			pong := []byte("+PONG\r\n")
			if protocolVersion == 2 {
				pong = redisRESP2PubSubFrame("pong", "")
			}
			if _, decision := observer.ObserveServerRelayBytes(pong); decision != nil {
				t.Fatalf("PONG denied: %#v", decision)
			}
			if len(observer.slots) != 0 {
				t.Fatalf("PING response slots=%d, want 0", len(observer.slots))
			}

			if decision := observer.ObserveClientBytes(
				redisObserverTestCommand("UNSUBSCRIBE"),
			); decision != nil {
				t.Fatalf("boundary UNSUBSCRIBE denied: %#v", decision)
			}
			feedRedisPubSubStream(
				t,
				observer,
				redisPubSubAck(protocolVersion, "unsubscribe", topic, 0),
			)
			if observer.subscriptions.active() || len(observer.slots) != 0 {
				t.Fatalf("streamed unsubscribe state=%#v slots=%d", observer.subscriptions, len(observer.slots))
			}
			if len(sink.finished) != 2 {
				t.Fatalf("streamed Pub/Sub audit finishes=%#v", sink.finished)
			}
		})
	}
}

func TestRedisBoundaryPubSubACKReleasesDeferredRejection(t *testing.T) {
	topic := redisBoundaryObserverTopic(t)
	subscribe := redisObserverTestCommand("SUBSCRIBE", topic)
	rejected := redisObserverTestCommand("AUTH", "replacement", "secret")
	for _, protocolVersion := range []int{2, 3} {
		t.Run("RESP"+strconv.Itoa(protocolVersion), func(t *testing.T) {
			observer := &redisObserver{sink: &redisSlotSink{}}
			forward, decision := observer.ObserveClientRelayBytes(
				append(append([]byte(nil), subscribe...), rejected...),
			)
			if decision != nil || !bytes.Equal(forward, subscribe) ||
				observer.deferred == nil || len(observer.slots) != 1 {
				t.Fatalf(
					"client pipeline forward=%d/%d decision=%#v deferred=%#v slots=%d",
					len(forward),
					len(subscribe),
					decision,
					observer.deferred,
					len(observer.slots),
				)
			}

			acknowledgement := redisPubSubAck(protocolVersion, "subscribe", topic, 1)
			var terminal *queryDecision
			for offset := 0; offset < len(acknowledgement); {
				end := offset + 4096
				if end > len(acknowledgement) {
					end = len(acknowledgement)
				}
				_, terminal = observer.ObserveServerRelayBytes(acknowledgement[offset:end])
				offset = end
			}
			if terminal == nil || terminal.Allowed ||
				observer.deferred != nil || len(observer.slots) != 0 {
				t.Fatalf(
					"terminal=%#v deferred=%#v slots=%d",
					terminal,
					observer.deferred,
					len(observer.slots),
				)
			}
		})
	}
}

func TestRedisTransactionPipelinePreservesCommandResponseOrdering(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	commands := bytes.Join([][]byte{
		redisObserverTestCommand("MULTI"),
		redisObserverTestCommand("SET", "key", "value"),
		redisObserverTestCommand("GET", "key"),
		redisObserverTestCommand("EXEC"),
	}, nil)
	if decision := observer.ObserveClientBytes(commands); decision != nil {
		t.Fatalf("transaction pipeline denied: %#v", decision)
	}
	responses := []byte("+OK\r\n+QUEUED\r\n+QUEUED\r\n*2\r\n+OK\r\n$5\r\nvalue\r\n")
	if _, decision := observer.ObserveServerRelayBytes(responses); decision != nil {
		t.Fatalf("transaction responses denied: %#v", decision)
	}
	if len(observer.slots) != 0 {
		t.Fatalf("transaction response slots = %d, want 0", len(observer.slots))
	}
	if len(sink.finished) != 4 ||
		sink.finished[0].sql != "MULTI" ||
		sink.finished[1].sql != "SET key [REDACTED]" ||
		sink.finished[2].sql != "GET key" ||
		sink.finished[3].sql != "EXEC" {
		t.Fatalf("transaction audit finishes = %#v", sink.finished)
	}
}

func redisRESP2PubSubFrame(parts ...string) []byte {
	return redisObserverTestCommand(parts...)
}

func redisRESP2PubSubAck(kind, topic string, count int) []byte {
	frame := redisObserverTestCommand(kind, topic)
	frame[1] = '3'
	return append(frame, []byte(":"+strconv.Itoa(count)+"\r\n")...)
}

func redisPubSubAck(protocolVersion int, kind, topic string, count int) []byte {
	frame := redisRESP2PubSubAck(kind, topic, count)
	if protocolVersion == 3 {
		frame[0] = '>'
	}
	return frame
}

func redisBoundaryObserverTopic(t *testing.T) string {
	t.Helper()
	length := maxRedisObserverBufferBytes
	for {
		topic := strings.Repeat("c", length)
		commandLength := len(redisObserverTestCommand("SUBSCRIBE", topic))
		if commandLength <= maxRedisObserverBufferBytes {
			return topic
		}
		length -= commandLength - maxRedisObserverBufferBytes
		if length <= 0 {
			t.Fatal("could not construct boundary Redis topic")
		}
	}
}

func feedRedisPubSubStream(t *testing.T, observer *redisObserver, frame []byte) {
	t.Helper()
	var forwarded int
	for offset := 0; offset < len(frame); {
		end := offset + 4096
		if end > len(frame) {
			end = len(frame)
		}
		chunk, decision := observer.ObserveServerRelayBytes(frame[offset:end])
		if decision != nil {
			t.Fatalf("stream denied at %d: %#v", offset, decision)
		}
		forwarded += len(chunk)
		offset = end
	}
	if forwarded != len(frame) {
		t.Fatalf("stream forwarded=%d, want %d", forwarded, len(frame))
	}
}
