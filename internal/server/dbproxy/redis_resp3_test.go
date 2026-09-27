package dbproxy

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestRedisRESP3FrameLengthSupportsAllStableTypes(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{name: "null", frame: "_\r\n"},
		{name: "true", frame: "#t\r\n"},
		{name: "false", frame: "#f\r\n"},
		{name: "double", frame: ",1.25e2\r\n"},
		{name: "infinity", frame: ",inf\r\n"},
		{name: "big number", frame: "(3492890328409238509324850943850943825024385\r\n"},
		{name: "positive big number", frame: "(+3492890328409238509324850943850943825024385\r\n"},
		{name: "blob error", frame: "!21\r\nSYNTAX invalid syntax\r\n"},
		{name: "verbatim", frame: "=15\r\ntxt:hello world\r\n"},
		{name: "map", frame: "%2\r\n+server\r\n+redis\r\n+proto\r\n:3\r\n"},
		{name: "set", frame: "~2\r\n$3\r\none\r\n$3\r\ntwo\r\n"},
		{name: "push", frame: ">3\r\n+message\r\n$4\r\nnews\r\n$5\r\nhello\r\n"},
		{
			name:  "attribute followed by value",
			frame: "|1\r\n+ttl\r\n:10\r\n$5\r\nvalue\r\n",
		},
		{
			name:  "attribute nested in aggregate",
			frame: "*1\r\n|1\r\n+encoding\r\n+utf-8\r\n$5\r\nvalue\r\n",
		},
		{
			name: "nested",
			frame: "%2\r\n+set\r\n~2\r\n:1\r\n:2\r\n+values\r\n" +
				"*3\r\n_\r\n#t\r\n,2.5\r\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frame := []byte(test.frame)
			length, status := redisRESPFrameLength(frame)
			if status != redisRESPComplete || length != len(frame) {
				t.Fatalf("frame status = %v length = %d, want complete length %d", status, length, len(frame))
			}
			for end := 0; end < len(frame); end++ {
				if _, prefixStatus := redisRESPFrameLength(frame[:end]); prefixStatus != redisRESPIncomplete {
					t.Fatalf("prefix %d status = %v, want incomplete", end, prefixStatus)
				}
			}
		})
	}
}

func TestRedisRESP3FrameLengthRejectsMalformedTypes(t *testing.T) {
	tests := []string{
		"_x\r\n",
		"#yes\r\n",
		",not-a-double\r\n",
		"(12x\r\n",
		"!-1\r\n",
		"=3\r\nabc\nX",
		"=3\r\nx:y\r\n",
		"=4\r\ntext\r\n",
		"%-1\r\n",
		"|1\r\n+k\r\n+v\r\n",
		">x\r\n",
	}
	for _, frame := range tests {
		if _, status := redisRESPFrameLength([]byte(frame)); status != redisRESPMalformed && status != redisRESPIncomplete {
			t.Fatalf("frame %q status = %v, want malformed or incomplete", frame, status)
		}
	}
}

func TestRedisRESP3BlobErrorAndAttributedBlobErrorFinishAsErrors(t *testing.T) {
	frames := [][]byte{
		[]byte("!21\r\nSYNTAX invalid syntax\r\n"),
		[]byte("|1\r\n+source\r\n+cache\r\n!21\r\nSYNTAX invalid syntax\r\n"),
	}
	for _, frame := range frames {
		finish := redisResponseFinish(frame)
		if finish.Status != queryStatusError || finish.ErrorCode != "REDIS_ERROR" {
			t.Fatalf("finish = %#v, want sanitized RESP3 blob error", finish)
		}
	}
}

func TestRedisRESP3BlobErrorCompletesMatchingResponseSlot(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	if decision := observer.ObserveClientBytes(redisObserverTestCommand("GET", "key")); decision != nil {
		t.Fatalf("GET denied: %#v", decision)
	}
	frame := []byte("!21\r\nSYNTAX invalid syntax\r\n")
	if forward, decision := observer.ObserveServerRelayBytes(frame); decision != nil || !bytes.Equal(forward, frame) {
		t.Fatalf("blob error forward=%q decision=%#v", forward, decision)
	}
	if len(observer.slots) != 0 || len(sink.finished) != 1 ||
		sink.finished[0].finish.Status != queryStatusError ||
		sink.finished[0].finish.ErrorCode != "REDIS_ERROR" {
		t.Fatalf("blob error slots=%d finishes=%#v", len(observer.slots), sink.finished)
	}
}

func TestRedisRESP3PushDoesNotConsumeOrdinaryResponseSlot(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	if decision := observer.ObserveClientBytes(redisObserverTestCommand("GET", "key")); decision != nil {
		t.Fatalf("GET denied: %#v", decision)
	}

	push := []byte(">2\r\n+invalidate\r\n*1\r\n$3\r\nkey\r\n")
	if forward, decision := observer.ObserveServerRelayBytes(push); decision != nil || !bytes.Equal(forward, push) {
		t.Fatalf("push forward = %q decision = %#v", forward, decision)
	}
	if len(observer.slots) != 1 || len(sink.finished) != 0 {
		t.Fatalf("push consumed ordinary slot: slots=%d finished=%d", len(observer.slots), len(sink.finished))
	}

	response := []byte("$5\r\nvalue\r\n")
	if forward, decision := observer.ObserveServerRelayBytes(response); decision != nil || !bytes.Equal(forward, response) {
		t.Fatalf("response forward = %q decision = %#v", forward, decision)
	}
	if len(observer.slots) != 0 || len(sink.finished) != 1 {
		t.Fatalf("response did not consume GET slot: slots=%d finished=%d", len(observer.slots), len(sink.finished))
	}

	if forward, decision := observer.ObserveServerRelayBytes(push); decision != nil || !bytes.Equal(forward, push) {
		t.Fatalf("trailing push forward = %q decision = %#v", forward, decision)
	}
}

func TestRedisRESP3AttributeConsumesOnlyItsAttachedValueSlot(t *testing.T) {
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	commands := bytes.Join([][]byte{
		redisObserverTestCommand("GET", "first"),
		redisObserverTestCommand("GET", "second"),
	}, nil)
	if decision := observer.ObserveClientBytes(commands); decision != nil {
		t.Fatalf("pipeline denied: %#v", decision)
	}

	attribute := []byte("|1\r\n+ttl\r\n:60\r\n")
	if _, decision := observer.ObserveServerRelayBytes(attribute); decision != nil {
		t.Fatalf("partial attribute denied: %#v", decision)
	}
	if len(sink.finished) != 0 {
		t.Fatal("attribute metadata completed a slot without its attached value")
	}

	tail := []byte("$3\r\none\r\n$3\r\ntwo\r\n")
	if _, decision := observer.ObserveServerRelayBytes(tail); decision != nil {
		t.Fatalf("attributed pipeline response denied: %#v", decision)
	}
	if len(sink.finished) != 2 || len(observer.slots) != 0 {
		t.Fatalf("finished=%d slots=%d, want two completed replies", len(sink.finished), len(observer.slots))
	}
}

func TestRedisRESP3StreamingAttributeAndPushSemantics(t *testing.T) {
	largeValue := strings.Repeat("x", maxRedisObserverBufferBytes+1024)
	attributed := []byte("|1\r\n+source\r\n+cache\r\n$" +
		strconv.Itoa(len(largeValue)) + "\r\n" + largeValue + "\r\n")
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	if decision := observer.ObserveClientBytes(redisObserverTestCommand("GET", "large")); decision != nil {
		t.Fatalf("GET denied: %#v", decision)
	}
	for offset := 0; offset < len(attributed); {
		end := offset + 4096
		if end > len(attributed) {
			end = len(attributed)
		}
		if _, decision := observer.ObserveServerRelayBytes(attributed[offset:end]); decision != nil {
			t.Fatalf("attributed stream denied at %d: %#v", offset, decision)
		}
		offset = end
	}
	if len(sink.finished) != 1 || len(observer.slots) != 0 {
		t.Fatalf("attributed stream finished=%d slots=%d", len(sink.finished), len(observer.slots))
	}

	largePush := []byte(">2\r\n+invalidate\r\n$" +
		strconv.Itoa(len(largeValue)) + "\r\n" + largeValue + "\r\n")
	observer = &redisObserver{}
	var forwarded int
	for offset := 0; offset < len(largePush); {
		end := offset + 4096
		if end > len(largePush) {
			end = len(largePush)
		}
		chunk, decision := observer.ObserveServerRelayBytes(largePush[offset:end])
		if decision != nil {
			t.Fatalf("large push denied at %d: %#v", offset, decision)
		}
		forwarded += len(chunk)
		offset = end
	}
	if forwarded != len(largePush) || len(observer.slots) != 0 {
		t.Fatalf("large push forwarded=%d want=%d slots=%d", forwarded, len(largePush), len(observer.slots))
	}
}

func TestRedisRESP3StreamingAttributedBlobErrorFinishesAsError(t *testing.T) {
	payload := "ERR " + strings.Repeat("x", maxRedisObserverBufferBytes+1024)
	frame := []byte("|1\r\n+source\r\n+upstream\r\n!" +
		strconv.Itoa(len(payload)) + "\r\n" + payload + "\r\n")
	sink := &redisSlotSink{}
	observer := &redisObserver{sink: sink}
	if decision := observer.ObserveClientBytes(redisObserverTestCommand("GET", "large-error")); decision != nil {
		t.Fatalf("GET denied: %#v", decision)
	}
	for offset := 0; offset < len(frame); {
		end := offset + 4096
		if end > len(frame) {
			end = len(frame)
		}
		if _, decision := observer.ObserveServerRelayBytes(frame[offset:end]); decision != nil {
			t.Fatalf("attributed blob error denied at %d: %#v", offset, decision)
		}
		offset = end
	}
	if len(observer.slots) != 0 || len(sink.finished) != 1 ||
		sink.finished[0].finish.Status != queryStatusError ||
		sink.finished[0].finish.ErrorCode != "REDIS_ERROR" {
		t.Fatalf("attributed blob error slots=%d finishes=%#v", len(observer.slots), sink.finished)
	}
}
