package rdpproxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestConnectorPerformsGuacdHandshakeAndPreservesBufferedData(t *testing.T) {
	args := []string{
		"hostname", "port", "username", "password", "domain", "security",
		"ignore-cert", "cert-fingerprint", "width", "height", "dpi",
		"client-name", "timezone",
		"disable-copy", "disable-paste", "disable-upload", "disable-download",
		"enable-drive", "drive-path", "drive-name", "create-drive-path",
		"recording-path", "recording-name", "create-recording-path",
		"recording-include-keys", "HOSTNAME", "future-option",
	}
	request := ConnectRequest{
		Hostname:               "windows.internal",
		Port:                   3390,
		Username:               "administrator",
		Password:               "sensitive-secret",
		Domain:                 "EXAMPLE",
		Security:               "nla",
		IgnoreCertificate:      true,
		CertificateFingerprint: "AA:BB:CC",
		Width:                  1600,
		Height:                 900,
		DPI:                    120,
		ClientName:             "Jianmen test",
		Timezone:               "Asia/Shanghai",
		AudioMIMETypes:         []string{"audio/L8"},
		VideoMIMETypes:         []string{"video/test"},
		ImageMIMETypes:         []string{"image/png"},
		ChannelPolicy: ChannelPolicy{
			ClipboardRead:  true,
			ClipboardWrite: false,
			FileUpload:     true,
			FileDownload:   false,
			DriveMapping:   true,
		},
		DrivePath:       "/spool/drive",
		DriveName:       "Jianmen",
		CreateDrivePath: true,
		Recording: Recording{
			Path:        "/spool/recordings",
			Name:        "session-1",
			CreatePath:  true,
			IncludeKeys: false,
		},
	}

	listener, serverResult := startHandshakeServer(t, args, func(instructions []Instruction) error {
		if got, want := instructionOpcodes(instructions), []string{"size", "audio", "video", "image", "timezone", "name", "connect"}; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("handshake opcodes = %v, want %v", got, want)
		}
		if got, want := instructions[0].Args, []string{"1600", "900", "120"}; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("size args = %v, want %v", got, want)
		}
		if got, want := instructions[1].Args, request.AudioMIMETypes; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("audio args = %v, want %v", got, want)
		}
		if got, want := instructions[2].Args, request.VideoMIMETypes; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("video args = %v, want %v", got, want)
		}
		if got, want := instructions[3].Args, request.ImageMIMETypes; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("image args = %v, want %v", got, want)
		}
		if got, want := instructions[4].Args, []string{"Asia/Shanghai"}; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("timezone args = %v, want %v", got, want)
		}
		if got, want := instructions[5].Args, []string{"Jianmen test"}; !reflect.DeepEqual(got, want) {
			return fmt.Errorf("name args = %v, want %v", got, want)
		}

		values := valuesByArg(args, instructions[6].Args)
		expected := map[string]string{
			"hostname":               "windows.internal",
			"port":                   "3390",
			"username":               "administrator",
			"password":               "sensitive-secret",
			"domain":                 "EXAMPLE",
			"security":               "nla",
			"ignore-cert":            "true",
			"cert-fingerprint":       "AA:BB:CC",
			"width":                  "1600",
			"height":                 "900",
			"dpi":                    "120",
			"client-name":            "Jianmen test",
			"timezone":               "Asia/Shanghai",
			"disable-copy":           "false",
			"disable-paste":          "true",
			"disable-upload":         "false",
			"disable-download":       "true",
			"enable-drive":           "true",
			"drive-path":             "/spool/drive",
			"drive-name":             "Jianmen",
			"create-drive-path":      "true",
			"recording-path":         "/spool/recordings",
			"recording-name":         "session-1",
			"create-recording-path":  "true",
			"recording-include-keys": "false",
			"HOSTNAME":               "",
			"future-option":          "",
		}
		if !reflect.DeepEqual(values, expected) {
			return fmt.Errorf("connect values = %#v, want %#v", values, expected)
		}
		return nil
	})
	defer listener.Close()

	connector := NewConnector(listener.Addr().String())
	session, readyID, err := connector.Connect(context.Background(), request)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()
	if readyID != "ready-session-1" || session.ReadyID() != readyID {
		t.Fatalf("ready IDs = %q / %q, want ready-session-1", readyID, session.ReadyID())
	}

	const bufferedInstruction = "4.sync,2.42;"
	raw := make([]byte, len(bufferedInstruction))
	if _, err := io.ReadFull(session, raw); err != nil {
		t.Fatalf("read buffered instruction: %v", err)
	}
	if string(raw) != bufferedInstruction {
		t.Fatalf("buffered instruction = %q, want %q", raw, bufferedInstruction)
	}

	var nop bytes.Buffer
	if err := NewEncoder(&nop).Encode(Instruction{Opcode: "nop"}); err != nil {
		t.Fatalf("encode nop: %v", err)
	}
	if _, err := session.Write(nop.Bytes()); err != nil {
		t.Fatalf("write session instruction: %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestConnectorOptionalChannelsDefaultToDenied(t *testing.T) {
	args := []string{
		"disable-copy", "disable-paste", "disable-upload", "disable-download",
		"enable-drive", "recording-include-keys",
	}
	listener, serverResult := startHandshakeServer(t, args, func(instructions []Instruction) error {
		values := valuesByArg(args, instructions[len(instructions)-1].Args)
		want := map[string]string{
			"disable-copy":           "true",
			"disable-paste":          "true",
			"disable-upload":         "true",
			"disable-download":       "true",
			"enable-drive":           "false",
			"recording-include-keys": "false",
		}
		if !reflect.DeepEqual(values, want) {
			return fmt.Errorf("default policy values = %#v, want %#v", values, want)
		}
		return nil
	})
	defer listener.Close()

	session, _, err := NewConnector(listener.Addr().String()).Connect(context.Background(), ConnectRequest{
		Hostname: "windows.internal",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	const bufferedInstruction = "4.sync,2.42;"
	if _, err := io.CopyN(io.Discard, session, int64(len(bufferedInstruction))); err != nil {
		t.Fatalf("drain buffered instruction: %v", err)
	}
	if _, err := session.Write([]byte("3.nop;")); err != nil {
		t.Fatalf("write session instruction: %v", err)
	}
	if err := <-serverResult; err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeConnectRequestSecurityMode(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty defaults to any", want: "any"},
		{name: "any", input: "any", want: "any"},
		{name: "nla", input: "nla", want: "nla"},
		{name: "nla ext", input: "nla-ext", want: "nla-ext"},
		{name: "tls", input: "tls", want: "tls"},
		{name: "vmconnect", input: "vmconnect", want: "vmconnect"},
		{name: "rdp", input: "rdp", want: "rdp"},
		{name: "normalizes case and whitespace", input: "  NlA-ExT  ", want: "nla-ext"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request, err := normalizeConnectRequest(ConnectRequest{
				Hostname: "windows.internal",
				Security: test.input,
			})
			if err != nil {
				t.Fatalf("normalize connect request: %v", err)
			}
			if request.Security != test.want {
				t.Fatalf("security = %q, want %q", request.Security, test.want)
			}
		})
	}
}

func TestConnectorRejectsInvalidSecurityModeBeforeDial(t *testing.T) {
	dialer := &rejectUnexpectedDialer{}
	connector := NewConnector("127.0.0.1:4822")
	connector.Dialer = dialer

	_, _, err := connector.Connect(context.Background(), ConnectRequest{
		Hostname: "windows.internal",
		Security: "credssp-or-anything",
	})
	if err == nil || !strings.Contains(err.Error(), "rdp security mode") {
		t.Fatalf("connect error = %v, want invalid RDP security mode", err)
	}
	if dialer.called {
		t.Fatal("connector dialed guacd for an invalid security mode")
	}
}

func TestConnectorNeverIncludesPasswordInErrors(t *testing.T) {
	const password = "never-print-this-password"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	serverDone := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverDone <- acceptErr
			return
		}
		defer conn.Close()
		decoder := NewDecoder(conn)
		if _, readErr := decoder.Decode(); readErr != nil {
			serverDone <- readErr
			return
		}
		if writeErr := NewEncoder(conn).Encode(Instruction{Opcode: "args", Args: []string{"password"}}); writeErr != nil {
			serverDone <- writeErr
			return
		}
		for range 7 {
			if _, readErr := decoder.Decode(); readErr != nil {
				serverDone <- readErr
				return
			}
		}
		serverDone <- NewEncoder(conn).Encode(Instruction{Opcode: "error", Args: []string{password}})
	}()

	_, _, connectErr := NewConnector(listener.Addr().String()).Connect(context.Background(), ConnectRequest{
		Hostname: "windows.internal",
		Password: password,
	})
	if connectErr == nil {
		t.Fatal("connect unexpectedly succeeded")
	}
	if strings.Contains(connectErr.Error(), password) {
		t.Fatalf("connect error leaked password: %v", connectErr)
	}
	if err := <-serverDone; err != nil {
		t.Fatalf("fake guacd: %v", err)
	}
}

func TestConnectorRejectsOversizedGuacdInstruction(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = NewDecoder(conn).Decode()
		_, _ = io.WriteString(conn, "100.args;")
	}()

	connector := NewConnector(listener.Addr().String())
	connector.Limits = Limits{MaxElementLength: 16, MaxInstructionLength: 128, MaxElements: 8}
	_, _, connectErr := connector.Connect(context.Background(), ConnectRequest{Hostname: "windows.internal"})
	if !errors.Is(connectErr, ErrElementTooLarge) {
		t.Fatalf("connect error = %v, want ErrElementTooLarge", connectErr)
	}
}

func TestConnectorHonorsContextCancellationAndTimeout(t *testing.T) {
	t.Run("context cancellation", func(t *testing.T) {
		listener, accepted, serverDone := startStalledServer(t)
		defer listener.Close()

		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, _, err := NewConnector(listener.Addr().String()).Connect(ctx, ConnectRequest{Hostname: "windows.internal"})
			result <- err
		}()
		<-accepted
		cancel()

		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("connect error = %v, want context.Canceled", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("connect did not stop after context cancellation")
		}
		if err := <-serverDone; err != nil {
			t.Fatalf("stalled server: %v", err)
		}
	})

	t.Run("connector timeout", func(t *testing.T) {
		listener, accepted, serverDone := startStalledServer(t)
		defer listener.Close()

		connector := NewConnector(listener.Addr().String())
		connector.Timeout = 40 * time.Millisecond
		result := make(chan error, 1)
		go func() {
			_, _, err := connector.Connect(context.Background(), ConnectRequest{Hostname: "windows.internal"})
			result <- err
		}()
		<-accepted

		select {
		case err := <-result:
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("connect error = %v, want context.DeadlineExceeded", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("connect did not stop after connector timeout")
		}
		if err := <-serverDone; err != nil {
			t.Fatalf("stalled server: %v", err)
		}
	})
}

func startHandshakeServer(
	t *testing.T,
	args []string,
	validate func([]Instruction) error,
) (net.Listener, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	result := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			result <- acceptErr
			return
		}
		defer conn.Close()
		decoder := NewDecoder(conn)
		encoder := NewEncoder(conn)

		selectInstruction, readErr := decoder.Decode()
		if readErr != nil {
			result <- readErr
			return
		}
		if !reflect.DeepEqual(selectInstruction, Instruction{Opcode: "select", Args: []string{"rdp"}}) {
			result <- fmt.Errorf("select instruction = %#v", selectInstruction)
			return
		}
		if writeErr := encoder.Encode(Instruction{Opcode: "args", Args: args}); writeErr != nil {
			result <- writeErr
			return
		}

		instructions := make([]Instruction, 0, 7)
		for range 7 {
			instruction, readErr := decoder.Decode()
			if readErr != nil {
				result <- readErr
				return
			}
			instructions = append(instructions, instruction)
		}
		if validateErr := validate(instructions); validateErr != nil {
			result <- validateErr
			return
		}

		var readyAndSync bytes.Buffer
		bufferedEncoder := NewEncoder(&readyAndSync)
		if writeErr := bufferedEncoder.Encode(Instruction{Opcode: "ready", Args: []string{"ready-session-1"}}); writeErr != nil {
			result <- writeErr
			return
		}
		if writeErr := bufferedEncoder.Encode(Instruction{Opcode: "sync", Args: []string{"42"}}); writeErr != nil {
			result <- writeErr
			return
		}
		if _, writeErr := conn.Write(readyAndSync.Bytes()); writeErr != nil {
			result <- writeErr
			return
		}

		nop, readErr := decoder.Decode()
		if readErr != nil {
			result <- readErr
			return
		}
		if nop.Opcode != "nop" {
			result <- fmt.Errorf("post-ready instruction = %#v, want nop", nop)
			return
		}
		result <- nil
	}()
	return listener, result
}

func startStalledServer(t *testing.T) (net.Listener, <-chan struct{}, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	accepted := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			done <- acceptErr
			return
		}
		close(accepted)
		defer conn.Close()
		_, copyErr := io.Copy(io.Discard, conn)
		done <- copyErr
	}()
	return listener, accepted, done
}

type rejectUnexpectedDialer struct {
	called bool
}

func (d *rejectUnexpectedDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	d.called = true
	return nil, errors.New("unexpected dial")
}

func instructionOpcodes(instructions []Instruction) []string {
	opcodes := make([]string, len(instructions))
	for i := range instructions {
		opcodes[i] = instructions[i].Opcode
	}
	return opcodes
}

func valuesByArg(args, values []string) map[string]string {
	result := make(map[string]string, len(args))
	for i, name := range args {
		if i < len(values) {
			result[name] = values[i]
		}
	}
	return result
}
