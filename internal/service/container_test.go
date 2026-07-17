package service

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"golang.org/x/crypto/ssh"
	"net/http/httptest"
	"strings"
	"testing"

	"jianmen/internal/model"
)

func TestContainerServiceDockerAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_ping":
			_, _ = w.Write([]byte("OK"))
		case "/containers/json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"Id":"abc123","Names":["/api"],"Image":"nginx:latest","State":"running","Status":"Up 1 minute","Ports":[]}]`))
		case "/containers/abc123/logs":
			_, _ = w.Write([]byte("2026-07-17T00:00:00Z ready"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	svc := NewContainerService()
	endpoint := ContainerEndpointConfig{Runtime: model.ContainerRuntimeDocker, ConnectionMode: model.ContainerConnectionDockerAPI, Address: server.URL}
	result, err := svc.Test(context.Background(), endpoint)
	if err != nil || !result.OK {
		t.Fatalf("test connection = %#v, err=%v", result, err)
	}
	items, err := svc.List(context.Background(), endpoint)
	if err != nil {
		t.Fatalf("list containers: %v", err)
	}
	if len(items) != 1 || items[0].Name != "api" {
		t.Fatalf("containers = %#v", items)
	}
	logs, err := svc.Logs(context.Background(), endpoint, "abc123", 50)
	if err != nil || !strings.Contains(logs, "ready") {
		t.Fatalf("logs = %q, err=%v", logs, err)
	}
}

func TestContainerServiceRejectsUnsafeContainerID(t *testing.T) {
	svc := NewContainerService()
	_, err := svc.Logs(context.Background(), ContainerEndpointConfig{ConnectionMode: model.ContainerConnectionDockerAPI, Address: "http://127.0.0.1:2375"}, "bad/id", 20)
	if err == nil {
		t.Fatal("unsafe container id was accepted")
	}
}

func TestContainerServiceSSHHandshakeHonorsContextCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	accepted := make(chan struct{})
	defer close(accepted)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		<-accepted
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	svc := NewContainerService()
	_, err = svc.sshCommand(ctx, ContainerEndpointConfig{
		SSHAddress: listener.Addr().String(),
		SSHConfig:  &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey()},
	}, "true")
	if err == nil {
		t.Fatal("SSH handshake without a server was not canceled")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Fatalf("SSH cancellation error = %v", err)
	}
}
