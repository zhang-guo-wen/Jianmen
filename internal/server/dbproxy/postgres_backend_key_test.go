package dbproxy

import (
	"encoding/binary"
	"testing"
)

func TestPostgresBackendMessageAcceptsVariableCancellationSecrets(t *testing.T) {
	for _, secretLength := range []int{4, 32, 256} {
		payload := make([]byte, 4+secretLength)
		binary.BigEndian.PutUint32(payload[:4], 42)
		if !validPostgresBackendMessage('K', payload) {
			t.Fatalf("BackendKeyData with %d-byte secret was rejected", secretLength)
		}
	}
	for _, secretLength := range []int{3, 257} {
		payload := make([]byte, 4+secretLength)
		if validPostgresBackendMessage('K', payload) {
			t.Fatalf("BackendKeyData with %d-byte secret was accepted", secretLength)
		}
	}
}
