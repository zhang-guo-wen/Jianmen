package sshproxy

import (
	"context"
	"io"
	"log/slog"
	"sync"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/proxy/sftpproxy"
	"jianmen/internal/recording"
)

type Access struct {
	SSH  bool
	SFTP bool
}

func (a Access) allows(requestType, subsystem string) bool {
	switch requestType {
	case "pty-req", "window-change", "shell", "exec", "env", "signal":
		return a.SSH
	case "subsystem":
		if subsystem == "sftp" {
			return a.SFTP
		}
		return a.SSH
	default:
		return true
	}
}

type Session struct {
	targetClient *ssh.Client
	client       ssh.Channel
	requests     <-chan *ssh.Request
	recorder     *recording.SessionRecorder
	logger       *slog.Logger
	access       Access

	target       ssh.Channel
	targetReqs   <-chan *ssh.Request
	proxyStarted bool
	proxyDone    chan struct{}
	doneOnce     sync.Once
	closeOnce    sync.Once
}

func NewSession(targetClient *ssh.Client, client ssh.Channel, requests <-chan *ssh.Request, recorder *recording.SessionRecorder, access Access, logger *slog.Logger) *Session {
	if logger == nil {
		logger = slog.Default()
	}
	return &Session{
		targetClient: targetClient,
		client:       client,
		requests:     requests,
		recorder:     recorder,
		logger:       logger,
		access:       access,
		proxyDone:    make(chan struct{}),
	}
}

func (s *Session) Serve(ctx context.Context) {
	defer s.close()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.proxyDone:
			return
		case req, ok := <-s.requests:
			if !ok {
				return
			}
			if !s.handleRequest(ctx, req) {
				return
			}
		}
	}
}

func (s *Session) handleRequest(ctx context.Context, req *ssh.Request) bool {
	subsystem := ""
	if req.Type == "subsystem" {
		subsystem = parseSubsystemName(req.Payload)
	}
	if !s.access.allows(req.Type, subsystem) {
		s.logger.Warn("SSH channel request denied by protocol permission", "type", req.Type, "subsystem", subsystem)
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return true
	}

	switch req.Type {
	case "pty-req":
		ok := s.forwardRequest(req, true)
		if ok {
			if pty, parsed := parsePtyRequest(req.Payload); parsed && s.recorder != nil {
				s.recorder.RecordResize("", int(pty.Columns), int(pty.Rows))
			}
		}
		return true
	case "window-change":
		_ = s.forwardRequest(req, false)
		if win, parsed := parseWindowChange(req.Payload); parsed && s.recorder != nil {
			s.recorder.RecordResize("", int(win.Columns), int(win.Rows))
		}
		return true
	case "shell", "exec":
		ok := s.forwardRequest(req, true)
		if ok {
			s.startCopy()
		}
		if req.Type == "exec" {
			if execReq, parsed := parseExecRequest(req.Payload); parsed && s.recorder != nil {
				s.recorder.RecordCommand(execReq.Command)
			}
		}
		return true
	case "subsystem":
		name := parseSubsystemName(req.Payload)
		if name == "sftp" {
			if s.recorder != nil {
				s.recorder.SetProtocolSubtype("sftp")
			}
			if req.WantReply {
				_ = req.Reply(true, nil)
			}
			err := sftpproxy.Serve(ctx, sftpproxy.Options{
				Channel:  s.client,
				Target:   s.targetClient,
				Recorder: s.recorder,
			})
			if err != nil && ctx.Err() == nil {
				s.logger.Warn("sftp semantic proxy stopped with error", "error", err)
			}
			return false
		}
		ok := s.forwardRequest(req, true)
		if ok {
			s.startCopy()
		}
		return true
	case "env":
		_ = s.forwardRequest(req, req.WantReply)
		return true
	case "signal":
		_ = s.forwardRequest(req, false)
		return true
	default:
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return true
	}
}

func (s *Session) forwardRequest(req *ssh.Request, wantReply bool) bool {
	target, err := s.ensureTarget()
	if err != nil {
		s.logger.Warn("failed to open target channel", "error", err)
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return false
	}
	ok, err := target.SendRequest(req.Type, wantReply, req.Payload)
	if err != nil {
		s.logger.Warn("failed to forward channel request", "type", req.Type, "error", err)
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
		return false
	}
	if req.WantReply {
		_ = req.Reply(ok, nil)
	}
	return ok || !wantReply
}

func (s *Session) ensureTarget() (ssh.Channel, error) {
	if s.target != nil {
		return s.target, nil
	}
	channel, reqs, err := s.targetClient.OpenChannel("session", nil)
	if err != nil {
		return nil, err
	}
	s.target = channel
	s.targetReqs = reqs
	return channel, nil
}

func (s *Session) startCopy() {
	if s.proxyStarted || s.target == nil {
		return
	}
	s.proxyStarted = true

	dataDone := make(chan struct{})
	reqDone := make(chan struct{})

	go func() {
		s.copyClientToTarget()
	}()

	go func() {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.copyTargetToClient()
		}()
		go func() {
			defer wg.Done()
			s.copyTargetStderrToClient()
		}()
		wg.Wait()
		_ = s.client.CloseWrite()
		close(dataDone)
	}()

	go func() {
		s.forwardTargetRequests()
		close(reqDone)
	}()

	go func() {
		<-dataDone
		<-reqDone
		s.close()
		s.finishProxy()
	}()
}

func (s *Session) forwardTargetRequests() {
	if s.targetReqs == nil {
		return
	}
	for req := range s.targetReqs {
		ok, err := s.client.SendRequest(req.Type, req.WantReply, req.Payload)
		if err != nil {
			s.logger.Warn("failed to forward target channel request", "type", req.Type, "error", err)
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
			return
		}
		if req.WantReply {
			_ = req.Reply(ok, nil)
		}
	}
}

func (s *Session) copyClientToTarget() {
	buf := make([]byte, 32*1024)
	for {
		n, readErr := s.client.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if s.recorder != nil {
				s.recorder.RecordInput(data)
			}
			if _, err := s.target.Write(data); err != nil {
				return
			}
		}
		if readErr != nil {
			_ = s.target.CloseWrite()
			return
		}
	}
}

func (s *Session) copyTargetToClient() {
	buf := make([]byte, 32*1024)
	for {
		n, readErr := s.target.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if s.recorder != nil {
				s.recorder.RecordOutput(data)
			}
			if _, err := s.client.Write(data); err != nil {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}

func (s *Session) copyTargetStderrToClient() {
	buf := make([]byte, 32*1024)
	source := s.target.Stderr()
	dest := s.client.Stderr()
	for {
		n, readErr := source.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			if s.recorder != nil {
				s.recorder.RecordOutput(data)
			}
			if _, err := dest.Write(data); err != nil {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}

func (s *Session) finishProxy() {
	s.doneOnce.Do(func() {
		close(s.proxyDone)
	})
}

func (s *Session) close() {
	s.closeOnce.Do(func() {
		if s.target != nil {
			_ = s.target.Close()
		}
		if s.client != nil {
			_ = s.client.Close()
		}
	})
}

func pipe(dst io.Writer, src io.Reader) error {
	_, err := io.Copy(dst, src)
	return err
}
