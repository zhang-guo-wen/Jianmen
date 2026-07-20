package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/singleflight"

	"jianmen/internal/model"
	"jianmen/internal/sshhost"
)

type ContainerEndpointConfig struct {
	Runtime        string
	ConnectionMode string
	Address        string
	Port           int
	SSHAddress     string
	SSHConfig      *ssh.ClientConfig
	SSHCacheKey    string
	Unavailable    bool
}

type ContainerRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image,omitempty"`
	State   string `json:"state,omitempty"`
	Status  string `json:"status,omitempty"`
	Ports   string `json:"ports,omitempty"`
	Created string `json:"created,omitempty"`
}

type ContainerTestResult struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
}

// ContainerService reads containers through the Docker Engine API or SSH tools.
type cachedSSHClient struct {
	client   *ssh.Client
	lastUsed time.Time
}

const sshClientIdleTTL = 10 * time.Minute

// ContainerService reads containers through Docker/containerd and reuses SSH
// transports so switching containers on one host does not repeat handshakes.
type ContainerService struct {
	HTTPClient *http.Client

	sshMu      sync.Mutex
	sshClients map[string]cachedSSHClient
	sshDial    singleflight.Group
}

func NewContainerService() *ContainerService {
	return &ContainerService{
		HTTPClient: &http.Client{Timeout: 12 * time.Second},
		sshClients: make(map[string]cachedSSHClient),
	}
}

// Close releases cached SSH transports owned by the service.
func (s *ContainerService) Close() {
	s.sshMu.Lock()
	clients := make([]*ssh.Client, 0, len(s.sshClients))
	for key, cached := range s.sshClients {
		clients = append(clients, cached.client)
		delete(s.sshClients, key)
	}
	s.sshMu.Unlock()
	for _, client := range clients {
		_ = client.Close()
	}
}

func (s *ContainerService) Test(ctx context.Context, endpoint ContainerEndpointConfig) (ContainerTestResult, error) {
	started := time.Now()
	var err error
	switch endpoint.ConnectionMode {
	case model.ContainerConnectionDockerAPI:
		_, err = s.dockerRequest(ctx, endpoint, "GET", "/_ping", nil)
	case model.ContainerConnectionSSH, model.ContainerConnectionContainerd:
		_, err = s.sshCommand(ctx, endpoint, s.listCommand(endpoint))
	default:
		err = errors.New("unsupported container connection mode")
	}
	result := ContainerTestResult{OK: err == nil, LatencyMS: time.Since(started).Milliseconds()}
	if err != nil {
		var changed *sshhost.KeyChangedError
		var unavailable *sshhost.IdentityUnavailableError
		if errors.As(err, &changed) || errors.As(err, &unavailable) {
			return ContainerTestResult{}, err
		}
		result.Message = err.Error()
	}
	return result, nil
}

func (s *ContainerService) List(ctx context.Context, endpoint ContainerEndpointConfig) ([]ContainerRecord, error) {
	if endpoint.ConnectionMode == model.ContainerConnectionDockerAPI {
		body, err := s.dockerRequest(ctx, endpoint, "GET", "/containers/json?all=1", nil)
		if err != nil {
			return nil, err
		}
		var rows []dockerContainer
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, fmt.Errorf("decode docker containers: %w", err)
		}
		return mapDockerContainers(rows), nil
	}
	body, err := s.sshCommand(ctx, endpoint, s.listCommand(endpoint))
	if err != nil {
		return nil, err
	}
	return parseContainerOutput(endpoint.Runtime, body)
}

func (s *ContainerService) Logs(ctx context.Context, endpoint ContainerEndpointConfig, id string, tail int) (string, error) {
	if !safeContainerID(id) {
		return "", errors.New("invalid container id")
	}
	if tail <= 0 || tail > 10000 {
		tail = 200
	}
	if endpoint.ConnectionMode == model.ContainerConnectionDockerAPI {
		path := "/containers/" + url.PathEscape(id) + "/logs?stdout=1&stderr=1&timestamps=1&tail=" + strconv.Itoa(tail)
		body, err := s.dockerRequest(ctx, endpoint, "GET", path, nil)
		return string(body), err
	}
	command := "docker logs --tail " + strconv.Itoa(tail) + " " + shellQuote(id)
	if endpoint.Runtime == model.ContainerRuntimeContainerd {
		command = "crictl logs --tail=" + strconv.Itoa(tail) + " " + shellQuote(id)
	}
	body, err := s.sshCommand(ctx, endpoint, command)
	return string(body), err
}

func (s *ContainerService) listCommand(endpoint ContainerEndpointConfig) string {
	if endpoint.Runtime == model.ContainerRuntimeContainerd {
		return "crictl ps -a -o json"
	}
	return "docker ps -a --format '{{json .}}'"
}

func (s *ContainerService) sshCommand(ctx context.Context, endpoint ContainerEndpointConfig, command string) ([]byte, error) {
	if endpoint.Unavailable {
		return nil, errors.New("host account is unavailable")
	}
	if endpoint.SSHConfig == nil {
		return nil, errors.New("ssh client config is required")
	}
	if endpoint.SSHAddress == "" {
		return nil, errors.New("ssh target address is empty")
	}

	client, err := s.acquireSSHClient(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	cached := endpoint.SSHCacheKey != ""
	output, runErr := s.runSSHCommand(ctx, endpoint.SSHCacheKey, client, command)
	if !cached {
		_ = client.Close()
	}
	if runErr != nil && ctx.Err() == nil && cached {
		var exitErr *ssh.ExitError
		if !errors.As(runErr, &exitErr) {
			// A dead cached transport should be replaced transparently on the
			// next operation, including the current read-only operation.
			s.invalidateSSHClient(endpoint.SSHCacheKey, client)
			if replacement, acquireErr := s.acquireSSHClient(ctx, endpoint); acquireErr == nil {
				output, runErr = s.runSSHCommand(ctx, endpoint.SSHCacheKey, replacement, command)
			}
		}
	}
	return output, runErr
}

func (s *ContainerService) acquireSSHClient(ctx context.Context, endpoint ContainerEndpointConfig) (*ssh.Client, error) {
	key := endpoint.SSHCacheKey
	if key == "" {
		return s.dialSSHClient(ctx, endpoint)
	}

	now := time.Now()
	s.sshMu.Lock()
	s.pruneSSHClientsLocked(now)
	if cached, ok := s.sshClients[key]; ok {
		cached.lastUsed = now
		s.sshClients[key] = cached
		s.sshMu.Unlock()
		return cached.client, nil
	}
	s.sshMu.Unlock()

	result := s.sshDial.DoChan(key, func() (any, error) {
		s.sshMu.Lock()
		if cached, ok := s.sshClients[key]; ok {
			cached.lastUsed = time.Now()
			s.sshClients[key] = cached
			s.sshMu.Unlock()
			return cached.client, nil
		}
		s.sshMu.Unlock()

		client, err := s.dialSSHClient(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		s.sshMu.Lock()
		s.sshClients[key] = cachedSSHClient{client: client, lastUsed: time.Now()}
		s.sshMu.Unlock()
		return client, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case call := <-result:
		if call.Err != nil {
			return nil, call.Err
		}
		return call.Val.(*ssh.Client), nil
	}
}

func (s *ContainerService) dialSSHClient(ctx context.Context, endpoint ContainerEndpointConfig) (*ssh.Client, error) {
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", endpoint.SSHAddress)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	cc, chans, reqs, err := ssh.NewClientConn(conn, endpoint.SSHAddress, endpoint.SSHConfig)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	return ssh.NewClient(cc, chans, reqs), nil
}

func (s *ContainerService) runSSHCommand(ctx context.Context, cacheKey string, client *ssh.Client, command string) ([]byte, error) {
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	var stdout, stderr strings.Builder
	session.Stdout = &stdout
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	closeSession := func() {
		if cacheKey == "" {
			_ = client.Close()
			_ = session.Close()
			return
		}
		closed := make(chan struct{})
		go func() {
			_ = session.Close()
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(250 * time.Millisecond):
			s.invalidateSSHClient(cacheKey, client)
		}
	}

	select {
	case <-ctx.Done():
		closeSession()
		return nil, ctx.Err()
	case err := <-done:
		_ = session.Close()
		if err != nil {
			message := strings.TrimSpace(stderr.String())
			if message != "" {
				return nil, fmt.Errorf("remote command failed: %s", message)
			}
			return nil, err
		}
	}
	return []byte(stdout.String()), nil
}

func (s *ContainerService) invalidateSSHClient(key string, client *ssh.Client) {
	if key == "" || client == nil {
		return
	}
	s.sshMu.Lock()
	cached, ok := s.sshClients[key]
	if ok && cached.client == client {
		delete(s.sshClients, key)
	}
	s.sshMu.Unlock()
	if ok && cached.client == client {
		_ = client.Close()
	}
}

func (s *ContainerService) pruneSSHClientsLocked(now time.Time) {
	for key, cached := range s.sshClients {
		if now.Sub(cached.lastUsed) > sshClientIdleTTL {
			delete(s.sshClients, key)
			_ = cached.client.Close()
		}
	}
}
