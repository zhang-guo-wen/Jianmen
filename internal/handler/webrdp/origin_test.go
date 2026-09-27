package webrdp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSameOriginOrNoOrigin(t *testing.T) {
	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		{name: "non browser client", want: true},
		{name: "same HTTP origin", origin: "http://example.com", want: true},
		{name: "same HTTPS origin", origin: "https://example.com", want: true},
		{name: "same origin and port", origin: "https://example.com:8443", want: true},
		{name: "foreign host", origin: "https://evil.example", want: false},
		{name: "foreign port", origin: "https://example.com:9443", want: false},
		{name: "unsupported scheme", origin: "file://example.com", want: false},
		{name: "opaque origin", origin: "null", want: false},
		{name: "userinfo", origin: "https://user@example.com", want: false},
		{name: "path", origin: "https://example.com/path", want: false},
		{name: "query", origin: "https://example.com?query=1", want: false},
		{name: "fragment", origin: "https://example.com#fragment", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://example.com/api/web-rdp", nil)
			if test.name == "same origin and port" {
				request.Host = "example.com:8443"
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if got := sameOriginOrNoOrigin(request); got != test.want {
				t.Fatalf("sameOriginOrNoOrigin() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestConnectRejectsCrossOriginBeforeConsumingTicket(t *testing.T) {
	tickets := &ticketStub{consumeFound: true}
	handler := &Handler{
		config:  Config{Enabled: true},
		tickets: tickets,
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		Path+"?target_id=account-1&ticket=single-use-ticket",
		nil,
	)
	request.Header.Set("Origin", "https://evil.example")

	handler.Connect(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if tickets.consumeCalls != 0 {
		t.Fatalf("ticket consume calls = %d, want 0", tickets.consumeCalls)
	}
}
