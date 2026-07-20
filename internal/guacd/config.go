package guacd

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	defaultStartupTimeout  = 15 * time.Second
	defaultShutdownTimeout = 5 * time.Second
)

// Config describes an already-installed guacd command.
//
// Command and Args are passed to the operating system unchanged. In
// particular, a WSL installation can use wsl.exe as Command and provide the
// distribution name and Linux guacd path in Args.
type Config struct {
	Enabled         bool
	Command         string
	Args            []string
	Env             map[string]string
	WorkDir         string
	ReadyAddress    string
	StartupTimeout  time.Duration
	ShutdownTimeout time.Duration
}

func (c Config) normalized() (Config, error) {
	if !c.Enabled {
		return c, nil
	}
	if strings.TrimSpace(c.Command) == "" {
		return Config{}, fmt.Errorf("guacd command is required")
	}
	if err := validateReadyAddress(c.ReadyAddress); err != nil {
		return Config{}, err
	}
	if c.StartupTimeout < 0 {
		return Config{}, fmt.Errorf("guacd startup timeout must not be negative")
	}
	if c.ShutdownTimeout < 0 {
		return Config{}, fmt.Errorf("guacd shutdown timeout must not be negative")
	}
	if c.StartupTimeout == 0 {
		c.StartupTimeout = defaultStartupTimeout
	}
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	return c, nil
}

func validateReadyAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid guacd ready address %q: %w", address, err)
	}
	if host == "" {
		return fmt.Errorf("guacd ready address %q must specify a loopback host", address)
	}
	if !strings.EqualFold(host, "localhost") {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("guacd ready address %q must use a loopback host", address)
		}
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return fmt.Errorf("guacd ready address %q has an invalid port", address)
	}
	return nil
}
