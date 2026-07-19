package rdpproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultGuacdTimeout = 10 * time.Second
	defaultRDPPort      = 3389
	defaultWidth        = 1280
	defaultHeight       = 720
	defaultDPI          = 96
	defaultClientName   = "Jianmen Web RDP"
	defaultTimezone     = "UTC"
)

var ErrGuacdHandshake = errors.New("guacd handshake failed")

// ChannelPolicy controls optional RDP virtual-channel capabilities. Its zero
// value denies every optional capability.
type ChannelPolicy struct {
	// ClipboardRead permits copying clipboard data from the remote desktop.
	ClipboardRead bool
	// ClipboardWrite permits pasting clipboard data into the remote desktop.
	ClipboardWrite bool
	FileUpload     bool
	FileDownload   bool
	DriveMapping   bool
}

// Recording configures guacd's native Guacamole protocol recording.
// IncludeKeys deliberately defaults to false.
type Recording struct {
	Path        string
	Name        string
	CreatePath  bool
	IncludeKeys bool
}

// ConnectRequest contains the server-owned configuration used to establish an
// RDP connection. Password is only written to guacd and is never included in
// returned errors.
type ConnectRequest struct {
	Hostname               string
	Port                   int
	Username               string
	Password               string
	Domain                 string
	Security               string
	IgnoreCertificate      bool
	CertificateFingerprint string

	Width      int
	Height     int
	DPI        int
	ClientName string
	Timezone   string

	AudioMIMETypes []string
	VideoMIMETypes []string
	ImageMIMETypes []string

	ChannelPolicy   ChannelPolicy
	DrivePath       string
	DriveName       string
	CreateDrivePath bool
	Recording       Recording
}

// ContextDialer is the TCP dial boundary used by Connector.
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Connector establishes configured RDP sessions through guacd.
type Connector struct {
	Address string
	Timeout time.Duration
	Limits  Limits
	Dialer  ContextDialer
}

// Session preserves the bufio.Reader used during the guacd handshake. This is
// required because guacd may send display instructions in the same packet as
// the ready instruction.
type Session struct {
	conn    net.Conn
	reader  *bufio.Reader
	readyID string
	once    sync.Once
}

func NewConnector(address string) *Connector {
	return &Connector{Address: strings.TrimSpace(address), Timeout: defaultGuacdTimeout}
}

// Connect performs the complete guacd select/args/connect handshake. The
// returned Session implements io.ReadWriteCloser and retains any data buffered
// while reading the ready instruction.
func (c *Connector) Connect(ctx context.Context, request ConnectRequest) (*Session, string, error) {
	if ctx == nil {
		return nil, "", errors.New("connect context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if c == nil || strings.TrimSpace(c.Address) == "" {
		return nil, "", errors.New("guacd address is required")
	}

	normalized, err := normalizeConnectRequest(request)
	if err != nil {
		return nil, "", err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultGuacdTimeout
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: timeout}
	}

	conn, err := dialer.DialContext(ctx, "tcp", c.Address)
	if err != nil {
		return nil, "", normalizeConnectError(ctx, "connect to guacd", err)
	}
	success := false
	defer func() {
		if !success {
			_ = conn.Close()
		}
	}()

	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, "", fmt.Errorf("%w: configure handshake deadline", ErrGuacdHandshake)
	}
	stopCancellation := context.AfterFunc(ctx, func() {
		_ = conn.SetDeadline(time.Now())
	})

	reader := bufio.NewReader(conn)
	encoder := NewEncoderWithLimits(conn, c.Limits)
	decoder := NewDecoderWithLimits(reader, c.Limits)
	readyID, err := performHandshake(encoder, decoder, normalized)
	if err != nil {
		stopCancellation()
		return nil, "", normalizeConnectError(ctx, "perform guacd handshake", err)
	}
	if !stopCancellation() {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, "", fmt.Errorf("%w: clear handshake deadline", ErrGuacdHandshake)
	}

	session := &Session{conn: conn, reader: reader, readyID: readyID}
	success = true
	return session, readyID, nil
}

func (s *Session) Read(p []byte) (int, error) {
	if s == nil || s.reader == nil {
		return 0, net.ErrClosed
	}
	return s.reader.Read(p)
}

func (s *Session) Write(p []byte) (int, error) {
	if s == nil || s.conn == nil {
		return 0, net.ErrClosed
	}
	return s.conn.Write(p)
}

func (s *Session) Close() error {
	if s == nil || s.conn == nil {
		return nil
	}
	var err error
	s.once.Do(func() {
		err = s.conn.Close()
	})
	return err
}

func (s *Session) ReadyID() string {
	if s == nil {
		return ""
	}
	return s.readyID
}

var _ io.ReadWriteCloser = (*Session)(nil)

func performHandshake(encoder *Encoder, decoder *Decoder, request ConnectRequest) (string, error) {
	if err := encoder.Encode(Instruction{Opcode: "select", Args: []string{"rdp"}}); err != nil {
		return "", fmt.Errorf("%w: send select: %w", ErrGuacdHandshake, err)
	}

	argsInstruction, err := decoder.Decode()
	if err != nil {
		return "", fmt.Errorf("%w: receive args: %w", ErrGuacdHandshake, err)
	}
	if argsInstruction.Opcode != "args" {
		return "", fmt.Errorf("%w: expected args instruction", ErrGuacdHandshake)
	}

	handshakeInstructions := []Instruction{
		{Opcode: "size", Args: []string{
			strconv.Itoa(request.Width),
			strconv.Itoa(request.Height),
			strconv.Itoa(request.DPI),
		}},
		{Opcode: "audio", Args: append([]string(nil), request.AudioMIMETypes...)},
		{Opcode: "video", Args: append([]string(nil), request.VideoMIMETypes...)},
		{Opcode: "image", Args: append([]string(nil), request.ImageMIMETypes...)},
		{Opcode: "timezone", Args: []string{request.Timezone}},
		{Opcode: "name", Args: []string{request.ClientName}},
	}
	for _, instruction := range handshakeInstructions {
		if err := encoder.Encode(instruction); err != nil {
			return "", fmt.Errorf("%w: send client capabilities: %w", ErrGuacdHandshake, err)
		}
	}

	connectValues := make([]string, len(argsInstruction.Args))
	for i, name := range argsInstruction.Args {
		connectValues[i] = connectionValueForArg(name, request)
	}
	if err := encoder.Encode(Instruction{Opcode: "connect", Args: connectValues}); err != nil {
		return "", fmt.Errorf("%w: send connect: %w", ErrGuacdHandshake, err)
	}

	readyInstruction, err := decoder.Decode()
	if err != nil {
		return "", fmt.Errorf("%w: receive ready: %w", ErrGuacdHandshake, err)
	}
	if readyInstruction.Opcode != "ready" || len(readyInstruction.Args) != 1 || readyInstruction.Args[0] == "" {
		return "", fmt.Errorf("%w: expected ready instruction", ErrGuacdHandshake)
	}
	return readyInstruction.Args[0], nil
}

func connectionValueForArg(name string, request ConnectRequest) string {
	switch name {
	case "hostname":
		return request.Hostname
	case "port":
		return strconv.Itoa(request.Port)
	case "username":
		return request.Username
	case "password":
		return request.Password
	case "domain":
		return request.Domain
	case "security":
		return request.Security
	case "ignore-cert":
		return guacamoleBool(request.IgnoreCertificate)
	case "cert-fingerprint", "cert-fingerprints", "certificate-fingerprint", "server-cert-fingerprint":
		return request.CertificateFingerprint
	case "width":
		return strconv.Itoa(request.Width)
	case "height":
		return strconv.Itoa(request.Height)
	case "dpi":
		return strconv.Itoa(request.DPI)
	case "client-name":
		return request.ClientName
	case "timezone":
		return request.Timezone
	case "disable-copy":
		return guacamoleBool(!request.ChannelPolicy.ClipboardRead)
	case "disable-paste":
		return guacamoleBool(!request.ChannelPolicy.ClipboardWrite)
	case "disable-upload":
		return guacamoleBool(!request.ChannelPolicy.FileUpload)
	case "disable-download":
		return guacamoleBool(!request.ChannelPolicy.FileDownload)
	case "enable-drive":
		return guacamoleBool(request.ChannelPolicy.DriveMapping)
	case "drive-path":
		return request.DrivePath
	case "drive-name":
		return request.DriveName
	case "create-drive-path":
		return guacamoleBool(request.CreateDrivePath)
	case "recording-path":
		return request.Recording.Path
	case "recording-name":
		return request.Recording.Name
	case "create-recording-path":
		return guacamoleBool(request.Recording.CreatePath)
	case "recording-include-keys":
		return guacamoleBool(request.Recording.IncludeKeys)
	default:
		return ""
	}
}

func normalizeConnectRequest(request ConnectRequest) (ConnectRequest, error) {
	request.Hostname = strings.TrimSpace(request.Hostname)
	if request.Hostname == "" {
		return ConnectRequest{}, errors.New("rdp hostname is required")
	}
	security, err := normalizeSecurityMode(request.Security)
	if err != nil {
		return ConnectRequest{}, err
	}
	request.Security = security
	if request.Port == 0 {
		request.Port = defaultRDPPort
	}
	if request.Port < 1 || request.Port > 65535 {
		return ConnectRequest{}, errors.New("rdp port is invalid")
	}
	if request.Width <= 0 {
		request.Width = defaultWidth
	}
	if request.Height <= 0 {
		request.Height = defaultHeight
	}
	if request.DPI <= 0 {
		request.DPI = defaultDPI
	}
	if request.ClientName == "" {
		request.ClientName = defaultClientName
	}
	if request.Timezone == "" {
		request.Timezone = defaultTimezone
	}
	if len(request.ImageMIMETypes) == 0 {
		request.ImageMIMETypes = []string{"image/png", "image/jpeg"}
	}
	return request, nil
}

func normalizeSecurityMode(value string) (string, error) {
	security := strings.ToLower(strings.TrimSpace(value))
	if security == "" {
		return "any", nil
	}
	switch security {
	case "any", "nla", "nla-ext", "tls", "vmconnect", "rdp":
		return security, nil
	default:
		return "", fmt.Errorf("rdp security mode %q is invalid", security)
	}
}

func guacamoleBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func normalizeConnectError(ctx context.Context, operation string, err error) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%s: %w", operation, context.DeadlineExceeded)
	}
	return fmt.Errorf("%s: %w", operation, err)
}
