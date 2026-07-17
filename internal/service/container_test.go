package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

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

func TestContainerServiceCancellationClosesSSHTransportFirst(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create host signer: %v", err)
	}
	serverConfig := &ssh.ServerConfig{NoClientAuth: true}
	serverConfig.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		sshConn, channels, requests, handshakeErr := ssh.NewServerConn(conn, serverConfig)
		if handshakeErr != nil {
			return
		}
		go ssh.DiscardRequests(requests)
		for newChannel := range channels {
			if newChannel.ChannelType() != "session" {
				_ = newChannel.Reject(ssh.UnknownChannelType, "session required")
				continue
			}
			channel, channelRequests, acceptErr := newChannel.Accept()
			if acceptErr != nil {
				continue
			}
			go func() {
				defer channel.Close()
				for request := range channelRequests {
					if request.Type == "exec" {
						_ = request.Reply(true, nil)
					}
				}
			}()
		}
		_ = sshConn.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	svc := NewContainerService()
	started := time.Now()
	_, err = svc.sshCommand(ctx, ContainerEndpointConfig{
		SSHAddress: listener.Addr().String(),
		SSHConfig:  &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey()},
	}, "sleep")
	if err == nil {
		t.Fatal("canceled SSH command returned nil error")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled SSH command took %s", elapsed)
	}
	select {
	case <-serverDone:
	case <-time.After(time.Second):
		t.Fatal("SSH server did not observe the closed transport")
	}
}

func TestContainerServiceReusesSSHClientForOneTarget(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create host signer: %v", err)
	}
	serverConfig := &ssh.ServerConfig{NoClientAuth: true}
	serverConfig.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	var connectionCount atomic.Int32
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			connectionCount.Add(1)
			go func() {
				defer conn.Close()
				sshConn, channels, requests, handshakeErr := ssh.NewServerConn(conn, serverConfig)
				if handshakeErr != nil {
					return
				}
				defer sshConn.Close()
				go ssh.DiscardRequests(requests)
				for newChannel := range channels {
					if newChannel.ChannelType() != "session" {
						_ = newChannel.Reject(ssh.UnknownChannelType, "session required")
						continue
					}
					channel, channelRequests, acceptErr := newChannel.Accept()
					if acceptErr != nil {
						continue
					}
					go func() {
						defer channel.Close()
						for request := range channelRequests {
							if request.Type != "exec" {
								_ = request.Reply(false, nil)
								continue
							}
							_ = request.Reply(true, nil)
							_, _ = channel.Write([]byte("ok\n"))
							_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{Status: 0}))
							return
						}
					}()
				}
			}()
		}
	}()

	svc := NewContainerService()
	defer svc.Close()
	endpoint := ContainerEndpointConfig{
		SSHAddress:  listener.Addr().String(),
		SSHCacheKey: "target-1@" + listener.Addr().String(),
		SSHConfig:   &ssh.ClientConfig{User: "test", HostKeyCallback: ssh.InsecureIgnoreHostKey()},
	}
	for i := 0; i < 3; i++ {
		output, err := svc.sshCommand(context.Background(), endpoint, "printf ok")
		if err != nil || !strings.Contains(string(output), "ok") {
			t.Fatalf("SSH command %d = %q, err=%v", i, output, err)
		}
	}
	if got := connectionCount.Load(); got != 1 {
		t.Fatalf("SSH connection count = %d, want 1", got)
	}
	listener.Close()
	select {
	case <-serverDone:
	case <-time.After(time.Second):
	}
}
