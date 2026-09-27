//go:build embedded_guacd && linux

package guacdruntime

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestEmbeddedRuntimeRunsGuacdVersion(t *testing.T) {
	process, err := Prepare(t.TempDir())
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	args := append(append([]string{}, process.ArgsPrefix...), "-v")
	command := exec.Command(process.Command, args...)
	command.Dir = process.WorkDir
	command.Env = os.Environ()
	for name, value := range process.Env {
		command.Env = append(command.Env, name+"="+value)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("guacd -v error = %v, output = %s", err, output)
	}
	if !strings.Contains(string(output), "guacd") || !strings.Contains(string(output), Version) {
		t.Fatalf("guacd -v output = %q", output)
	}
}

func TestEmbeddedRuntimeResolvesRelativeBaseDirectory(t *testing.T) {
	workingDir := t.TempDir()
	previousDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(workingDir); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previousDir); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})

	process, err := Prepare(filepath.Join("data", "runtime"))
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	paths := []string{process.Command, process.WorkDir, process.ArgsPrefix[2]}
	paths = append(paths, strings.Split(process.ArgsPrefix[1], ":")...)
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			t.Fatalf("embedded runtime path = %q, want absolute path", path)
		}
	}
}

func TestEmbeddedRuntimeLoadsRDPPlugin(t *testing.T) {
	process, err := Prepare(t.TempDir())
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve guacd port: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatalf("release guacd port: %v", err)
	}

	args := append(append([]string{}, process.ArgsPrefix...),
		"-f", "-b", "127.0.0.1", "-l", strconv.Itoa(port), "-L", "info")
	command := exec.Command(process.Command, args...)
	command.Dir = process.WorkDir
	command.Env = os.Environ()
	for name, value := range process.Env {
		command.Env = append(command.Env, name+"="+value)
	}
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatalf("start embedded guacd: %v", err)
	}
	defer func() {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_, _ = command.Process.Wait()
	}()

	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	connection := waitForGuacdConnection(t, address, &output)
	defer connection.Close()
	if _, err := connection.Write([]byte("6.select,3.rdp;")); err != nil {
		t.Fatalf("write guacd select: %v", err)
	}
	if err := connection.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("set guacd read deadline: %v", err)
	}
	response := make([]byte, 8192)
	count, err := connection.Read(response)
	if err != nil {
		t.Fatalf("read guacd RDP args: %v; output=%s", err, output.String())
	}
	if !bytes.Contains(response[:count], []byte("args")) {
		t.Fatalf("guacd RDP response = %q; output=%s", response[:count], output.String())
	}
}

func waitForGuacdConnection(t *testing.T, address string, output *bytes.Buffer) net.Conn {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			return connection
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("embedded guacd did not listen at %s; output=%s", address, output.String())
	return nil
}
