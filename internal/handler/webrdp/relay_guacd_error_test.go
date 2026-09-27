package webrdp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"jianmen/internal/proxy/rdpproxy"
	"jianmen/internal/service"
)

func TestRelayGuacdToBrowserForwardsErrorThenFailsSession(t *testing.T) {
	instruction := rdpproxy.Instruction{
		Opcode: "error",
		Args:   []string{"authentication failed", "769"},
	}
	var source bytes.Buffer
	if err := rdpproxy.NewEncoder(&source).Encode(instruction); err != nil {
		t.Fatal(err)
	}

	relayResult := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		}).Upgrade(w, r, nil)
		if err != nil {
			relayResult <- err
			return
		}
		defer conn.Close()
		relayResult <- relayGuacdToBrowser(
			context.Background(),
			&source,
			conn,
			newChannelFilter(
				"audit-session-1",
				"remote_to_browser",
				service.WebRDPChannelPolicy{},
				&channelAuditStub{},
			),
		)
	}))
	defer server.Close()

	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client, _, err := websocket.DefaultDialer.Dial(websocketURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, payload, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("read forwarded guacd error: %v", err)
	}
	forwarded, err := rdpproxy.NewDecoder(bytes.NewReader(payload)).Decode()
	if err != nil {
		t.Fatalf("decode forwarded guacd error: %v", err)
	}
	if forwarded.Opcode != instruction.Opcode ||
		len(forwarded.Args) != 2 ||
		forwarded.Args[1] != "769" {
		t.Fatalf("forwarded instruction = %#v, want %#v", forwarded, instruction)
	}

	select {
	case err := <-relayResult:
		var protocolErr *guacdProtocolError
		if !errors.As(err, &protocolErr) || protocolErr.status != 769 {
			t.Fatalf("relay error = %v, want guacd status 769", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not return after guacd error instruction")
	}
}
