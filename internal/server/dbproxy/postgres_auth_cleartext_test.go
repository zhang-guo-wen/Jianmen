package dbproxy

import (
	"errors"
	"net"
	"testing"

	"jianmen/internal/dbtls"
)

func TestAuthenticatePostgresUpstreamAllowsCleartextPasswordWhenTLSDisabled(t *testing.T) {
	upstreamServer, upstreamGateway := net.Pipe()
	defer upstreamServer.Close()
	defer upstreamGateway.Close()
	clientGateway, clientPeer := net.Pipe()
	defer clientGateway.Close()
	defer clientPeer.Close()

	serverResult := make(chan error, 1)
	go func() {
		if err := writePostgresMessage(upstreamServer, 'R', []byte{0, 0, 0, 3}); err != nil {
			serverResult <- err
			return
		}
		passwordMessage, err := readPostgresMessage(upstreamServer, maxPostgresAuthMessageBytes)
		if err != nil {
			serverResult <- err
			return
		}
		if passwordMessage.kind != 'p' || string(passwordMessage.payload) != "secret\x00" {
			serverResult <- errors.New("unexpected PostgreSQL cleartext password response")
			return
		}
		if err := writePostgresMessage(upstreamServer, 'R', []byte{0, 0, 0, 0}); err != nil {
			serverResult <- err
			return
		}
		serverResult <- writePostgresMessage(upstreamServer, 'Z', []byte{'I'})
	}()

	type authenticationResult struct {
		startup postgresUpstreamStartup
		err     error
	}
	authResult := make(chan authenticationResult, 1)
	go func() {
		startup, err := authenticatePostgresUpstream(
			upstreamGateway,
			clientGateway,
			"probe",
			"secret",
			dbtls.ModeDisable,
			func(postgresCancelKey) func() { return func() {} },
		)
		authResult <- authenticationResult{startup: startup, err: err}
	}()

	for _, wantKind := range []byte{'R', 'Z'} {
		message, err := readPostgresMessage(clientPeer, maxPostgresAuthMessageBytes)
		if err != nil {
			t.Fatal(err)
		}
		if message.kind != wantKind {
			t.Fatalf("client startup message kind = %q, want %q", message.kind, wantKind)
		}
	}
	if result := <-authResult; result.err != nil {
		t.Fatalf("authenticatePostgresUpstream() error = %v", result.err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestAuthenticatePostgresUpstreamRequiresVerifiedTLSWhenEnabled(t *testing.T) {
	for _, mode := range []string{dbtls.ModeVerifyCA, dbtls.ModeVerifyFull} {
		t.Run(mode, func(t *testing.T) {
			upstreamServer, upstreamGateway := net.Pipe()
			defer upstreamServer.Close()
			defer upstreamGateway.Close()
			clientGateway, clientPeer := net.Pipe()
			defer clientGateway.Close()
			defer clientPeer.Close()

			serverResult := make(chan error, 1)
			go func() {
				serverResult <- writePostgresMessage(upstreamServer, 'R', []byte{0, 0, 0, 3})
			}()
			_, err := authenticatePostgresUpstream(
				upstreamGateway,
				clientGateway,
				"probe",
				"secret",
				mode,
				func(postgresCancelKey) func() { return func() {} },
			)
			if !errors.Is(err, errVerifiedTLSRequired) {
				t.Fatalf("authenticatePostgresUpstream() error = %v, want errVerifiedTLSRequired", err)
			}
			if err := <-serverResult; err != nil {
				t.Fatal(err)
			}
		})
	}
}
