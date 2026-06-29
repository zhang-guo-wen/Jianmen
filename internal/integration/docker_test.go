//go:build integration

package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
)

type dockerCLI struct {
	name       string
	baseArgs   []string
	describeAs string
}

func requireDocker(t *testing.T) {
	t.Helper()
	cli, err := findDockerCLI()
	if err != nil {
		skipOrFailDocker(t, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := append(append([]string{}, cli.baseArgs...), "version", "--format", "{{.Server.Version}}")
	out, err := exec.CommandContext(ctx, cli.name, args...).CombinedOutput()
	if err == nil {
		return
	}
	skipOrFailDocker(t, fmt.Sprintf("%s daemon is not available: %v\n%s", cli.describeAs, err, string(out)))
}

func skipOrFailDocker(t *testing.T, message string) {
	t.Helper()
	if os.Getenv("JIANMEN_REQUIRE_DOCKER") == "1" {
		t.Fatal(message)
	}
	t.Skipf("%s; skipping integration test", message)
}

func findDockerCLI() (dockerCLI, error) {
	if _, err := exec.LookPath("docker"); err == nil {
		return dockerCLI{name: "docker", describeAs: "docker"}, nil
	}
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("wsl.exe"); err == nil {
			return dockerCLI{
				name:       "wsl.exe",
				baseArgs:   []string{"-e", "docker"},
				describeAs: "wsl docker",
			}, nil
		}
	}
	return dockerCLI{}, fmt.Errorf("docker CLI is not available")
}

func runContainer(t *testing.T, namePrefix string, args ...string) string {
	t.Helper()
	name := fmt.Sprintf("%s-%d", namePrefix, time.Now().UnixNano())
	runArgs := append([]string{"run", "--rm", "-d", "--name", name}, args...)
	id := strings.TrimSpace(dockerOutput(t, 5*time.Minute, runArgs...))
	if id == "" {
		t.Fatalf("docker run returned empty container id for %s", name)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cli, err := findDockerCLI()
		if err != nil {
			return
		}
		rmArgs := append(append([]string{}, cli.baseArgs...), "rm", "-f", id)
		_ = exec.CommandContext(ctx, cli.name, rmArgs...).Run()
	})
	return id
}

func dockerOutput(t *testing.T, timeout time.Duration, args ...string) string {
	t.Helper()
	cli, err := findDockerCLI()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmdArgs := append(append([]string{}, cli.baseArgs...), args...)
	cmd := exec.CommandContext(ctx, cli.name, cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", cli.describeAs, strings.Join(args, " "), err, string(out))
	}
	return string(out)
}

func containerAddress(t *testing.T, containerID, containerPort string) string {
	t.Helper()
	out := dockerOutput(t, 30*time.Second, "port", containerID, containerPort)
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		host, port, err := net.SplitHostPort(line)
		if err != nil {
			t.Fatalf("parse docker port %q: %v", line, err)
		}
		if host == "" || host == "0.0.0.0" || host == "::" {
			host = "127.0.0.1"
		}
		return net.JoinHostPort(host, port)
	}
	t.Fatalf("container %s has no published address for %s", containerID, containerPort)
	return ""
}

func waitFor(t *testing.T, timeout time.Duration, interval time.Duration, check func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := check(); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(interval)
	}
	t.Fatalf("condition not ready within %s: %v", timeout, lastErr)
}

func waitTCP(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	waitFor(t, timeout, 100*time.Millisecond, func() error {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			return err
		}
		return conn.Close()
	})
}

func freeTCPAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate tcp port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close tcp listener: %v", err)
	}
	return addr
}
