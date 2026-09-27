package dbproxy

import "testing"

func TestSerializeRelayObserverIsIdempotent(t *testing.T) {
	t.Parallel()

	raw := &redisObserver{}
	wrapped := serializeRelayObserver(raw)
	serialized, ok := wrapped.(*serializedQueryObserver)
	if !ok {
		t.Fatalf("relay observer type = %T, want *serializedQueryObserver", wrapped)
	}
	if serialized.observer != raw {
		t.Fatalf("wrapped observer = %T, want original redis observer", serialized.observer)
	}
	if got := serializeRelayObserver(wrapped); got != wrapped {
		t.Fatal("serialized relay observer was wrapped a second time")
	}
}
