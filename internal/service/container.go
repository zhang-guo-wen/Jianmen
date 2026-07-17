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
	"time"

	"golang.org/x/crypto/ssh"

	"jianmen/internal/model"
)

type ContainerEndpointConfig struct {
	Runtime        string
	ConnectionMode string
	Address        string
	Port           int
	SSHAddress     string
	SSHConfig      *ssh.ClientConfig
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
type ContainerService struct {
	HTTPClient *http.Client
}

func NewContainerService() *ContainerService {
	return &ContainerService{HTTPClient: &http.Client{Timeout: 12 * time.Second}}
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
	config := endpoint.SSHConfig
	if config == nil {
		return nil, errors.New("ssh client config is required")
	}
	address := endpoint.SSHAddress
	if address == "" {
		return nil, errors.New("ssh target address is empty")
	}
	dialer := net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	cc, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	client := ssh.NewClient(cc, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	defer session.Close()
	var stdout, stderr strings.Builder
	session.Stdout = &stdout
	session.Stderr = &stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-done:
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

func (s *ContainerService) dockerRequest(ctx context.Context, endpoint ContainerEndpointConfig, method, path string, body io.Reader) ([]byte, error) {
	base := strings.TrimSpace(endpoint.Address)
	if base == "" {
		return nil, errors.New("docker api address is required")
	}
	if strings.HasPrefix(base, "unix://") {
		socketPath := strings.TrimPrefix(base, "unix://")
		transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", socketPath)
		}}
		client := &http.Client{Transport: transport, Timeout: 12 * time.Second}
		return doHTTP(ctx, client, "http://docker"+path, method, body)
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		base = "http://" + base
	}
	if endpoint.Port > 0 && !strings.Contains(strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://"), ":") {
		base = strings.TrimRight(base, "/") + ":" + strconv.Itoa(endpoint.Port)
	}
	return doHTTP(ctx, s.HTTPClient, strings.TrimRight(base, "/")+path, method, body)
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
