package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sync/singleflight"

	"jianmen/internal/model"
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

func (s *ContainerService) dockerRequest(ctx context.Context, endpoint ContainerEndpointConfig, method, path string, body io.Reader) ([]byte, error) {
	base := strings.TrimSpace(endpoint.Address)
	if base == "" {
		return nil, errors.New("docker api address is required")
	}
	if strings.HasPrefix(base, "unix://") {
		socketPath := strings.TrimPrefix(base, "unix://")
		if socketPath == "" {
			return nil, errors.New("docker unix socket path is required")
		}
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
		}}
		client := &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: rejectDockerRedirect}
		return doHTTP(ctx, client, "http://docker"+path, method, body)
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return nil, fmt.Errorf("invalid docker api address: %q", endpoint.Address)
	}
	scheme := strings.ToLower(parsed.Scheme)
	switch scheme {
	case "http":
		if !isLoopbackHost(parsed.Hostname()) {
			return nil, errors.New("non-loopback Docker HTTP API is not allowed")
		}
	case "https":
		// HTTPS is allowed for remote Docker endpoints, but never with a
		// transport that disables certificate verification.
	default:
		return nil, fmt.Errorf("unsupported docker api scheme %q", parsed.Scheme)
	}
	if endpoint.Port > 0 && parsed.Port() == "" {
		parsed.Host = net.JoinHostPort(parsed.Hostname(), strconv.Itoa(endpoint.Port))
	}
	client, err := s.dockerHTTPClient(scheme == "https")
	if err != nil {
		return nil, err
	}
	return doHTTP(ctx, client, strings.TrimRight(parsed.String(), "/")+path, method, body)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *ContainerService) dockerHTTPClient(requireTLSValidation bool) (*http.Client, error) {
	base := s.HTTPClient
	if base == nil {
		base = http.DefaultClient
	}
	client := *base
	client.CheckRedirect = rejectDockerRedirect
	transport := base.Transport
	if transport == nil {
		return &client, nil
	}
	standard, ok := transport.(*http.Transport)
	if !ok {
		if requireTLSValidation {
			return nil, errors.New("docker api HTTPS requires a standard TLS-validating transport")
		}
		return &client, nil
	}
	clone := standard.Clone()
	if clone.TLSClientConfig != nil {
		if clone.TLSClientConfig.InsecureSkipVerify {
			return nil, errors.New("docker api HTTPS cannot use InsecureSkipVerify")
		}
		clone.TLSClientConfig = clone.TLSClientConfig.Clone()
		clone.TLSClientConfig.InsecureSkipVerify = false
	}
	client.Transport = clone
	return &client, nil
}

// Docker Engine endpoints do not need redirects. Refusing them avoids
// redirecting a loopback HTTP client to another host or downgrading HTTPS.
func rejectDockerRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func doHTTP(ctx context.Context, client *http.Client, endpoint, method string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("container api returned %s: %s", resp.Status, strings.TrimSpace(string(data)))
	}
	return data, nil
}

type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
	Ports  []struct {
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	Created int64 `json:"Created"`
}

func mapDockerContainers(rows []dockerContainer) []ContainerRecord {
	items := make([]ContainerRecord, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimPrefix(firstString(row.Names), "/")
		ports := make([]string, 0, len(row.Ports))
		for _, port := range row.Ports {
			if port.PublicPort > 0 {
				ports = append(ports, fmt.Sprintf("%d->%d/%s", port.PublicPort, port.PrivatePort, port.Type))
			} else {
				ports = append(ports, fmt.Sprintf("%d/%s", port.PrivatePort, port.Type))
			}
		}
		items = append(items, ContainerRecord{ID: row.ID, Name: name, Image: row.Image, State: row.State, Status: row.Status, Ports: strings.Join(ports, ", "), Created: time.Unix(row.Created, 0).Format(time.RFC3339)})
	}
	return items
}

func parseContainerOutput(runtime string, body []byte) ([]ContainerRecord, error) {
	if runtime == model.ContainerRuntimeContainerd {
		var payload struct {
			Containers []struct {
				ID       string `json:"id"`
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Image struct {
					Image string `json:"image"`
				} `json:"image"`
				State  string `json:"state"`
				Status string `json:"status"`
			} `json:"containers"`
		}
		if err := json.Unmarshal(body, &payload); err == nil && payload.Containers != nil {
			items := make([]ContainerRecord, 0, len(payload.Containers))
			for _, row := range payload.Containers {
				items = append(items, ContainerRecord{ID: row.ID, Name: row.Metadata.Name, Image: row.Image.Image, State: row.State, Status: row.Status})
			}
			return items, nil
		}
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	items := make([]ContainerRecord, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			State  string `json:"State"`
			Status string `json:"Status"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("decode container list: %w", err)
		}
		items = append(items, ContainerRecord{ID: row.ID, Name: strings.TrimPrefix(row.Names, "/"), Image: row.Image, State: row.State, Status: row.Status})
	}
	return items, nil
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

var containerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

func safeContainerID(value string) bool {
	return containerIDPattern.MatchString(strings.TrimSpace(value))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
