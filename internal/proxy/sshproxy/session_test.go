package sshproxy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/model"
	"jianmen/internal/recording"
)

type testExecRequest struct {
	Command string
}

type testExitStatusMsg struct {
	Status uint32
}

type testExitSignalMsg struct {
	Signal     string
	CoreDumped bool
	Error      string
	Lang       string
}

func TestAccessSeparatesSSHAndXFTPRequests(t *testing.T) {
	sshOnly := Access{SSH: true}
	if !sshOnly.allows("exec", "") || sshOnly.allows("subsystem", "sftp") {
		t.Fatal("SSH-only access did not separate exec and SFTP")
	}

	sftpOnly := Access{SFTP: true}
	if !sftpOnly.allows("subsystem", "sftp") || sftpOnly.allows("shell", "") {
		t.Fatal("XFTP-only access did not separate SFTP and shell")
	}
}

func TestSessionForwardsExecExitStatusAndStderr(t *testing.T) {
	target := newTargetClient(t, func(t *testing.T, ch ssh.Channel, command string) {
		t.Helper()
		if command != "status" {
			t.Fatalf("target received command %q, want status", command)
		}
		if _, err := ch.Write([]byte("stdout\n")); err != nil {
			t.Errorf("write stdout: %v", err)
		}
		if _, err := ch.Stderr().Write([]byte("stderr\n")); err != nil {
			t.Errorf("write stderr: %v", err)
		}
		if _, err := ch.SendRequest("eow", false, nil); err != nil {
			t.Errorf("send eow: %v", err)
		}
		if _, err := ch.SendRequest("exit-status", false, ssh.Marshal(&testExitStatusMsg{Status: 7})); err != nil {
			t.Errorf("send exit-status: %v", err)
		}
	})
	proxy := newProxyClient(t, target)

	session, err := proxy.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Setenv("FOO", "bar"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	err = session.Run("status")
	var exitErr *ssh.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run error = %T %v, want *ssh.ExitError", err, err)
	}
	if got := exitErr.ExitStatus(); got != 7 {
		t.Fatalf("exit status = %d, want 7", got)
	}
	if got := stdout.String(); got != "stdout\n" {
		t.Fatalf("stdout = %q, want stdout\\n", got)
	}
	if got := stderr.String(); got != "stderr\n" {
		t.Fatalf("stderr = %q, want stderr\\n", got)
	}
}

func TestSessionForwardsExitSignalAndEOW(t *testing.T) {
	target := newTargetClient(t, func(t *testing.T, ch ssh.Channel, command string) {
		t.Helper()
		if command != "signal" {
			t.Fatalf("target received command %q, want signal", command)
		}
		if _, err := ch.SendRequest("eow", false, nil); err != nil {
			t.Errorf("send eow: %v", err)
		}
		if _, err := ch.SendRequest("exit-signal", false, ssh.Marshal(&testExitSignalMsg{
			Signal: "TERM",
			Error:  "terminated",
			Lang:   "en",
		})); err != nil {
			t.Errorf("send exit-signal: %v", err)
		}
	})
	proxy := newProxyClient(t, target)

	ch, reqs, err := proxy.OpenChannel("session", nil)
	if err != nil {
		t.Fatalf("open session channel: %v", err)
	}
	defer ch.Close()

	ok, err := ch.SendRequest("exec", true, ssh.Marshal(&testExecRequest{Command: "signal"}))
	if err != nil {
		t.Fatalf("send exec: %v", err)
	}
	if !ok {
		t.Fatal("exec request was rejected")
	}

	var gotEOW bool
	var gotSignal bool
	deadline := time.After(2 * time.Second)
	for !gotEOW || !gotSignal {
		select {
		case req, ok := <-reqs:
			if !ok {
				t.Fatalf("client request stream closed before eow=%v signal=%v", gotEOW, gotSignal)
			}
			switch req.Type {
			case "eow":
				gotEOW = true
			case "exit-signal":
				var msg testExitSignalMsg
				if err := ssh.Unmarshal(req.Payload, &msg); err != nil {
					t.Fatalf("unmarshal exit-signal: %v", err)
				}
				if msg.Signal != "TERM" || msg.Error != "terminated" || msg.Lang != "en" {
					t.Fatalf("exit-signal = %+v, want TERM/terminated/en", msg)
				}
				gotSignal = true
			}
		case <-deadline:
			t.Fatalf("timed out waiting for eow=%v signal=%v", gotEOW, gotSignal)
		}
	}
}

func TestSessionRecordsExecCommandToRecorder(t *testing.T) {
	root := t.TempDir()

	// 记录 exec 命令的测试审计 sink
	type recordedCommand struct {
		sessionID string
		command   string
	}
	var commands []recordedCommand
	var mu sync.Mutex
	sink := &testRecorderAuditSink{onCommand: func(sessionID, command string) {
		mu.Lock()
		defer mu.Unlock()
		commands = append(commands, recordedCommand{sessionID, command})
	}}

	target := newTargetClient(t, func(t *testing.T, ch ssh.Channel, command string) {
		t.Helper()
		if _, err := ch.Write([]byte("output\n")); err != nil {
			t.Errorf("write stdout: %v", err)
		}
		if _, err := ch.SendRequest("exit-status", false, ssh.Marshal(&testExitStatusMsg{Status: 0})); err != nil {
			t.Errorf("send exit-status: %v", err)
		}
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	proxy := newTestSSHClient(t, func(newChannel ssh.NewChannel) {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			return
		}
		ch, reqs, err := newChannel.Accept()
		if err != nil {
			t.Errorf("accept proxy channel: %v", err)
			return
		}
		sessionModel := model.Session{
			ID:              "sess-exec-test",
			User:            model.User{Username: "test-user"},
			Target:          "root@127.0.0.1:22",
			AccountUsername: "root",
			ClientIP:        "127.0.0.1",
			Protocol:        "ssh",
			StartedAt:       time.Now().UTC(),
		}
		recorder, err := recording.NewSessionRecorder(
			root, sessionModel, false, true,
			&testPassthroughRedactor{}, func(error) {}, nil, sink,
		)
		if err != nil {
			t.Fatalf("new session recorder: %v", err)
		}
		defer recorder.Close()

		NewSession(target, ch, reqs, recorder, Access{SSH: true, SFTP: true}, logger).Serve(context.Background())
	})

	ch, reqs, err := proxy.OpenChannel("session", nil)
	if err != nil {
		t.Fatalf("open session channel: %v", err)
	}
	defer ch.Close()

	ok, err := ch.SendRequest("exec", true, ssh.Marshal(&testExecRequest{Command: "df -h"}))
	if err != nil {
		t.Fatalf("send exec: %v", err)
	}
	if !ok {
		t.Fatal("exec request was rejected")
	}

	// 读取 exec 输出并等待 channel 关闭
	var buf bytes.Buffer
	for {
		data := make([]byte, 1024)
		n, readErr := ch.Read(data)
		if n > 0 {
			buf.Write(data[:n])
		}
		if readErr != nil {
			break
		}
	}
	// 等待并消费来自 target 的扩展请求
	for req := range reqs {
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(commands) == 0 {
		t.Fatal("exec command was not recorded to audit sink")
	}
	if commands[0].command != "df -h" {
		t.Fatalf("recorded command = %q, want %q", commands[0].command, "df -h")
	}
}

// testRecorderAuditSink 是 recording.AuditSink 的测试桩
type testRecorderAuditSink struct {
	onCommand func(sessionID, command string)
}

func (s *testRecorderAuditSink) WriteCommand(sessionID string, _ time.Time, command string) error {
	if s.onCommand != nil {
		s.onCommand(sessionID, command)
	}
	return nil
}

func (s *testRecorderAuditSink) WriteFileEvent(_ string, _ time.Time, _, _ string, _ int64, _ string) error {
	return nil
}

func (s *testRecorderAuditSink) UpdateProtocol(_ string, _ string) error { return nil }

// testPassthroughRedactor 是 recording.AuditRedactor 的测试桩
type testPassthroughRedactor struct{}

func (testPassthroughRedactor) Redact(_ string, value string) string { return value }

func newTargetClient(t *testing.T, run func(*testing.T, ssh.Channel, string)) *ssh.Client {
	t.Helper()
	return newTestSSHClient(t, func(newChannel ssh.NewChannel) {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			return
		}
		ch, reqs, err := newChannel.Accept()
		if err != nil {
			t.Errorf("accept target channel: %v", err)
			return
		}
		defer ch.Close()

		for req := range reqs {
			switch req.Type {
			case "exec":
				var execReq testExecRequest
				if err := ssh.Unmarshal(req.Payload, &execReq); err != nil {
					t.Errorf("unmarshal exec request: %v", err)
					if req.WantReply {
						_ = req.Reply(false, nil)
					}
					return
				}
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
				run(t, ch, execReq.Command)
				return
			case "env", "pty-req", "shell", "window-change":
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
			default:
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
			}
		}
	})
}

func newProxyClient(t *testing.T, target *ssh.Client) *ssh.Client {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newTestSSHClient(t, func(newChannel ssh.NewChannel) {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			return
		}
		ch, reqs, err := newChannel.Accept()
		if err != nil {
			t.Errorf("accept proxy channel: %v", err)
			return
		}
		NewSession(target, ch, reqs, nil, Access{SSH: true, SFTP: true}, logger).Serve(context.Background())
	})
}

func newTestSSHClient(t *testing.T, handle func(ssh.NewChannel)) *ssh.Client {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen test SSH server: %v", err)
	}
	serverConfig := newTestServerConfig(t)
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		defer listener.Close()

		rawConn, err := listener.Accept()
		if err != nil {
			return
		}
		defer rawConn.Close()

		conn, chans, reqs, err := ssh.NewServerConn(rawConn, serverConfig)
		if err != nil {
			return
		}
		defer conn.Close()
		go ssh.DiscardRequests(reqs)

		var wg sync.WaitGroup
		for newChannel := range chans {
			wg.Add(1)
			go func(newChannel ssh.NewChannel) {
				defer wg.Done()
				handle(newChannel)
			}(newChannel)
		}
		wg.Wait()
	}()

	clientConfig := &ssh.ClientConfig{
		User:            "test",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         time.Second,
	}
	clientConn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		_ = listener.Close()
		t.Fatalf("dial test SSH server: %v", err)
	}
	conn, chans, reqs, err := ssh.NewClientConn(clientConn, "test", clientConfig)
	if err != nil {
		_ = clientConn.Close()
		t.Fatalf("new ssh client conn: %v", err)
	}
	client := ssh.NewClient(conn, chans, reqs)

	t.Cleanup(func() {
		_ = client.Close()
		_ = clientConn.Close()
		_ = listener.Close()
		select {
		case <-serverDone:
		case <-time.After(time.Second):
			t.Log("timed out waiting for test SSH server shutdown")
		}
	})
	return client
}

func newTestServerConfig(t *testing.T) *ssh.ServerConfig {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	config := &ssh.ServerConfig{NoClientAuth: true}
	config.AddHostKey(signer)
	return config
}
