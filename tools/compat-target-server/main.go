package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type execRequest struct {
	Command string
}

type subsystemRequest struct {
	Name string
}

func main() {
	addr := flag.String("addr", "127.0.0.1:47222", "listen address")
	root := flag.String("root", "data/compat-test/target-root", "sftp root")
	hostKey := flag.String("host-key", "data/compat-test/target_host_key", "host key path")
	flag.Parse()

	if err := os.MkdirAll(*root, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(*root, "hello.txt"), []byte("hello from target\n"), 0o644); err != nil {
		log.Fatal(err)
	}

	signer, err := loadOrCreateSigner(*hostKey)
	if err != nil {
		log.Fatal(err)
	}

	cfg := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if conn.User() == "targetuser" && string(password) == "targetpass" {
				return nil, nil
			}
			return nil, fmt.Errorf("password rejected for %s", conn.User())
		},
	}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("compat target ssh server listening on %s root=%s", *addr, *root)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		go handleConn(conn, cfg, *root)
	}
}

func loadOrCreateSigner(path string) (ssh.Signer, error) {
	if raw, err := os.ReadFile(path); err == nil {
		return ssh.ParsePrivateKey(raw)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	raw := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(raw)
}

func handleConn(raw net.Conn, cfg *ssh.ServerConfig, root string) {
	conn, chans, reqs, err := ssh.NewServerConn(raw, cfg)
	if err != nil {
		log.Printf("handshake: %v", err)
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, reqs, err := newCh.Accept()
		if err != nil {
			log.Printf("accept channel: %v", err)
			continue
		}
		go handleSession(ch, reqs, root)
	}
}

func handleSession(ch ssh.Channel, reqs <-chan *ssh.Request, root string) {
	defer ch.Close()
	for req := range reqs {
		switch req.Type {
		case "pty-req", "env", "window-change":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
		case "shell":
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			runShell(ch, root)
			return
		case "exec":
			var msg execRequest
			ssh.Unmarshal(req.Payload, &msg)
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			runExec(ch, msg.Command, root)
			return
		case "subsystem":
			var msg subsystemRequest
			ssh.Unmarshal(req.Payload, &msg)
			if msg.Name != "sftp" {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				return
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			server, err := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
			if err != nil {
				log.Printf("sftp server: %v", err)
				return
			}
			if err := server.Serve(); err != nil && !errors.Is(err, io.EOF) {
				log.Printf("sftp serve: %v", err)
			}
			_ = server.Close()
			return
		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

func runExec(ch ssh.Channel, command, root string) {
	stdout, stderr, code := evalCommand(command, root)
	if stdout != "" {
		_, _ = io.WriteString(ch, stdout)
	}
	if stderr != "" {
		_, _ = ch.Stderr().Write([]byte(stderr))
	}
	_, _ = ch.SendRequest("exit-status", false, exitStatusPayload(uint32(code)))
}

func runShell(ch ssh.Channel, root string) {
	_, _ = io.WriteString(ch, "Jianmen compat target\r\n$ ")
	scanner := bufio.NewScanner(ch)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			_, _ = io.WriteString(ch, "$ ")
			continue
		}
		if line == "exit" || line == "logout" {
			_, _ = io.WriteString(ch, "logout\r\n")
			_, _ = ch.SendRequest("exit-status", false, exitStatusPayload(0))
			return
		}
		stdout, stderr, _ := evalCommand(line, root)
		if stdout != "" {
			_, _ = io.WriteString(ch, strings.ReplaceAll(stdout, "\n", "\r\n"))
		}
		if stderr != "" {
			_, _ = ch.Stderr().Write([]byte(strings.ReplaceAll(stderr, "\n", "\r\n")))
		}
		_, _ = io.WriteString(ch, "$ ")
	}
	_, _ = ch.SendRequest("exit-status", false, exitStatusPayload(0))
}

func evalCommand(command, root string) (string, string, int) {
	command = strings.TrimSpace(command)
	switch {
	case command == "whoami":
		return "targetuser\n", "", 0
	case command == "pwd":
		return root + "\n", "", 0
	case command == "hostname":
		return "compat-target\n", "", 0
	case command == "stty size":
		return "24 80\n", "", 0
	case strings.HasPrefix(command, "sleep "):
		time.Sleep(10 * time.Second)
		return "slept\n", "", 0
	case strings.HasPrefix(command, "echo "):
		return strings.TrimPrefix(command, "echo ") + "\n", "", 0
	case command == "false":
		return "", "", 1
	case strings.Contains(command, "stderr"):
		return "stdout\n", "stderr\n", 0
	default:
		return "ran: " + command + "\n", "", 0
	}
}

func exitStatusPayload(code uint32) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, code)
	return payload
}
