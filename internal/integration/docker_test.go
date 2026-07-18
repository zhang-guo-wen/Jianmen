//go:build integration

package integration

import (
	"context"
	"errors"
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
	id := dockerContainerID(t, dockerOutput(t, 5*time.Minute, runArgs...))
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

func dockerBindPath(t *testing.T, path string) string {
	t.Helper()
	cli, err := findDockerCLI()
	if err != nil || cli.name != "wsl.exe" {
		return path
	}
	output, err := exec.Command("wsl.exe", "-e", "wslpath", "-a", path).Output()
	if err != nil {
		t.Fatalf("convert Docker bind path %q for WSL: %v", path, err)
	}
	return strings.TrimSpace(string(output))
}

func dockerBindHost(t *testing.T) string {
	t.Helper()
	cli, err := findDockerCLI()
	if err != nil {
		t.Fatalf("find Docker CLI: %v", err)
	}
	if cli.name != "wsl.exe" {
		return "127.0.0.1"
	}

	modeOutput, err := exec.Command("wsl.exe", "-e", "wslinfo", "--networking-mode").Output()
	if err != nil || strings.TrimSpace(string(modeOutput)) != "nat" {
		return "127.0.0.1"
	}

	routeOutput, err := exec.Command("wsl.exe", "-e", "ip", "route", "show", "default").Output()
	if err != nil {
		t.Fatalf("resolve WSL default route: %v", err)
	}
	iface, err := parseDefaultRouteInterface(string(routeOutput))
	if err != nil {
		t.Fatal(err)
	}
	addrOutput, err := exec.Command("wsl.exe", "-e", "ip", "-4", "-o", "addr", "show", "dev", iface, "scope", "global").Output()
	if err != nil {
		t.Fatalf("resolve WSL NAT interface address: %v", err)
	}
	address, err := parsePrivateInterfaceAddress(string(addrOutput), iface)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func parseDefaultRouteInterface(output string) (string, error) {
	var iface string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "default" {
			continue
		}
		candidate := ""
		for index := 1; index+1 < len(fields); index++ {
			if fields[index] == "dev" {
				candidate = fields[index+1]
				break
			}
		}
		if candidate == "" {
			return "", fmt.Errorf("WSL default route has no interface")
		}
		if iface != "" && iface != candidate {
			return "", fmt.Errorf("WSL default route has ambiguous interfaces")
		}
		iface = candidate
	}
	if iface == "" {
		return "", fmt.Errorf("WSL default route is missing")
	}
	return iface, nil
}

func parsePrivateInterfaceAddress(output, iface string) (string, error) {
	var address string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for index := 0; index+1 < len(fields); index++ {
			if fields[index] != "inet" {
				continue
			}
			ip, _, err := net.ParseCIDR(fields[index+1])
			if err != nil || ip.To4() == nil || !ip.IsPrivate() {
				return "", fmt.Errorf("WSL NAT interface %s has an invalid private IPv4 address", iface)
			}
			if address != "" {
				return "", fmt.Errorf("WSL NAT interface %s has ambiguous private IPv4 addresses", iface)
			}
			address = ip.To4().String()
		}
	}
	if address == "" {
		return "", fmt.Errorf("WSL NAT interface %s has no private IPv4 address", iface)
	}
	return address, nil
}

func dockerContainerID(t *testing.T, output string) string {
	t.Helper()
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if isDockerContainerID(line) {
			return line
		}
	}
	t.Fatalf("docker run output did not contain a container id:\n%s", output)
	return ""
}

func isDockerContainerID(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
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

func waitMySQLContainerInitialized(t *testing.T, containerID string) {
	t.Helper()
	waitFor(t, 2*time.Minute, time.Second, func() error {
		logs := dockerOutput(t, 30*time.Second, "logs", containerID)
		if strings.Contains(logs, "MySQL init process done. Ready for start up.") {
			return nil
		}
		return errors.New("MySQL container initialization is not complete")
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
