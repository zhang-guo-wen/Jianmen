package dbproxy

import (
	"bytes"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseRedisInitialHELLOAuthentication(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		version    int
		clientName string
		errorKind  redisInitialAuthenticationError
		ok         bool
	}{
		{
			name:    "RESP2 AUTH",
			args:    []string{"HELLO", "2", "AUTH", "R000100001", "bastion-secret"},
			version: 2,
			ok:      true,
		},
		{
			name:       "RESP3 AUTH SETNAME",
			args:       []string{"HELLO", "3", "AUTH", "R000100001", "bastion-secret", "SETNAME", "client"},
			version:    3,
			clientName: "client",
			ok:         true,
		},
		{
			name:      "missing AUTH",
			args:      []string{"HELLO", "3"},
			errorKind: redisInitialAuthenticationRequired,
		},
		{
			name:      "unsupported protocol",
			args:      []string{"HELLO", "4", "AUTH", "R000100001", "bastion-secret"},
			errorKind: redisInitialAuthenticationUnsupportedProtocol,
		},
		{
			name: "duplicate AUTH",
			args: []string{
				"HELLO", "3",
				"AUTH", "R000100001", "bastion-secret",
				"AUTH", "R000100001", "bastion-secret",
			},
			errorKind: redisInitialAuthenticationInvalid,
		},
		{
			name:      "unknown option",
			args:      []string{"HELLO", "3", "AUTH", "R000100001", "bastion-secret", "OTHER"},
			errorKind: redisInitialAuthenticationInvalid,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, errorKind, ok := parseRedisInitialAuthentication("HELLO", test.args)
			if ok != test.ok || errorKind != test.errorKind {
				t.Fatalf("parse ok=%t error=%d, want ok=%t error=%d", ok, errorKind, test.ok, test.errorKind)
			}
			if !ok {
				return
			}
			if request.username != "R000100001" || request.password != "bastion-secret" ||
				request.helloVersion != test.version || request.clientName != test.clientName {
				t.Fatalf("request = %#v", request)
			}
		})
	}
}

func TestRedisHELLOUpstreamNegotiationKeepsGatewayCredentialsSeparated(t *testing.T) {
	const (
		gatewayUsername  = "R000100001"
		gatewayPassword  = "gateway-bastion-secret"
		upstreamUsername = "app"
		upstreamPassword = "upstream-redis-secret"
	)
	request, _, ok := parseRedisInitialAuthentication("HELLO", []string{
		"HELLO", "3", "AUTH", gatewayUsername, gatewayPassword, "SETNAME", "library-client",
	})
	if !ok {
		t.Fatal("parse HELLO AUTH failed")
	}

	proxy, server := net.Pipe()
	defer proxy.Close()
	defer server.Close()
	serverDone := make(chan error, 1)
	var observed [][]string
	go func() {
		command, args, _, err := readRESPCommand(server)
		if err != nil {
			serverDone <- err
			return
		}
		if command != "AUTH" {
			serverDone <- fmt.Errorf("first upstream command = %q", command)
			return
		}
		observed = append(observed, args)
		if _, err := server.Write([]byte("+OK\r\n")); err != nil {
			serverDone <- err
			return
		}
		command, args, _, err = readRESPCommand(server)
		if err != nil {
			serverDone <- err
			return
		}
		if command != "HELLO" {
			serverDone <- fmt.Errorf("second upstream command = %q", command)
			return
		}
		observed = append(observed, args)
		_, err = server.Write([]byte("%2\r\n+server\r\n+redis\r\n+proto\r\n:3\r\n"))
		serverDone <- err
	}()

	deadline := time.Now().Add(time.Second)
	if err := authenticateUpstreamRedis(proxy, upstreamUsername, upstreamPassword, deadline); err != nil {
		t.Fatalf("authenticate upstream: %v", err)
	}
	response, err := negotiateUpstreamRedisHello(proxy, request.helloVersion, request.clientName, deadline)
	if err != nil {
		t.Fatalf("negotiate HELLO: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("upstream script: %v", err)
	}
	if primary := redisRESPPrimaryType(response); primary != '%' {
		t.Fatalf("HELLO response primary type = %q, want map", primary)
	}

	wire := bytes.Join([][]byte{
		[]byte(strings.Join(observed[0], " ")),
		[]byte(strings.Join(observed[1], " ")),
	}, nil)
	if bytes.Contains(wire, []byte(gatewayUsername)) || bytes.Contains(wire, []byte(gatewayPassword)) {
		t.Fatalf("upstream commands exposed gateway credentials: %q", wire)
	}
	if !bytes.Contains(wire, []byte(upstreamUsername)) || !bytes.Contains(wire, []byte(upstreamPassword)) {
		t.Fatalf("upstream AUTH did not use stored account credentials: %q", wire)
	}
	if got := observed[1]; len(got) != 4 || got[0] != "HELLO" || got[1] != "3" ||
		got[2] != "SETNAME" || got[3] != "library-client" {
		t.Fatalf("upstream HELLO args = %v", got)
	}
}

func TestRedisPostAuthenticationHELLORejectsEmbeddedAUTH(t *testing.T) {
	observer := &redisObserver{sink: &captureSink{}}
	command := redisObserverTestCommand("HELLO", "3", "AUTH", "replacement", "secret")
	if forward, decision := observer.ObserveClientRelayBytes(command); decision == nil || decision.Allowed || len(forward) != 0 {
		t.Fatalf("HELLO AUTH forward=%q decision=%#v, want terminal rejection", forward, decision)
	}
}
