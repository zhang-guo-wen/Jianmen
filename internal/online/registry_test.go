package online

import (
	"errors"
	"testing"
	"time"
)

func TestRegistryLifecycle(t *testing.T) {
	registry := NewRegistry()
	disconnected := false
	unregister := registry.Register(Session{
		ID:        "session-1",
		Instance:  "host-a",
		StartedAt: time.Now().UTC(),
	}, func() {
		disconnected = true
	})

	items := registry.List()
	if len(items) != 1 || items[0].ID != "session-1" {
		t.Fatalf("unexpected online sessions: %#v", items)
	}
	registry.UpdateProtocolSubtype("session-1", "sftp")
	if got := registry.List()[0].ProtocolSubtype; got != "sftp" {
		t.Fatalf("protocol subtype = %q, want sftp", got)
	}
	if err := registry.Disconnect("session-1"); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if !disconnected {
		t.Fatal("disconnect callback was not called")
	}
	if got := registry.List(); len(got) != 0 {
		t.Fatalf("sessions after disconnect: %#v", got)
	}
	unregister()
	if got := registry.List(); len(got) != 0 {
		t.Fatalf("sessions after unregister: %#v", got)
	}
	if err := registry.Disconnect("missing"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("missing disconnect error = %v", err)
	}
}
