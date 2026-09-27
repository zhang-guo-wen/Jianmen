package service

import (
	"context"
	"encoding/binary"
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

	"jianmen/internal/model"
)

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

// demuxDockerLogs 解析 Docker 多路复用日志流格式，移除二进制帧头，提取纯文本日志。
//
// Docker Engine API 的 /containers/{id}/logs 端点返回二进制流格式，每帧结构：
//
//	header: [stream_type:1byte][0x00 0x00 0x00][frame_size:4bytes 大端序]
//	payload: [size 字节的日志文本]
//
// 该函数逐帧读取头部、剥离控制字节，仅保留日志正文。
func demuxDockerLogs(data []byte) string {
	if len(data) < 8 {
		return ""
	}
	var out strings.Builder
	out.Grow(len(data))
	for len(data) >= 8 {
		size := binary.BigEndian.Uint32(data[4:8])
		data = data[8:]
		if int(size) > len(data) {
			out.Write(data)
			break
		}
		out.Write(data[:size])
		data = data[size:]
	}
	return out.String()
}
