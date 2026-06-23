package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	addr := flag.String("addr", "http://127.0.0.1:47100", "admin API base URL")
	token := flag.String("token", os.Getenv("BASTION_ADMIN_TOKEN"), "admin API bearer token")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: bastionctl [flags] <command>\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Commands:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  health\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  users\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  targets\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  target-add <id> <host> <username> <password> [port]\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  sessions\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  session <id> <meta|commands|files|file-summary>\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  db\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  db-connection <id> <meta|queries>\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	client := &apiClient{
		baseURL: strings.TrimRight(*addr, "/"),
		token:   *token,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}

	path, err := commandPath(flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		flag.Usage()
		os.Exit(2)
	}
	if path == "target-add" {
		if err := client.addTarget(flag.Args()[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if err := client.get(path, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type apiClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func (c *apiClient) get(path string, out io.Writer) error {
	return c.do(http.MethodGet, path, nil, out)
}

func (c *apiClient) addTarget(args []string, out io.Writer) error {
	if len(args) < 4 || len(args) > 5 {
		return fmt.Errorf("target-add requires <id> <host> <username> <password> [port]")
	}
	port := 22
	if len(args) == 5 {
		if _, err := fmt.Sscanf(args[4], "%d", &port); err != nil {
			return fmt.Errorf("invalid port %q", args[4])
		}
	}
	body := map[string]any{
		"id":                       args[0],
		"name":                     args[0],
		"host":                     args[1],
		"port":                     port,
		"username":                 args[2],
		"password":                 args[3],
		"insecure_ignore_host_key": true,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.do(http.MethodPost, "/api/targets", bytes.NewReader(raw), out)
}

func (c *apiClient) do(method, path string, body io.Reader, out io.Writer) error {
	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("request failed: %s: %s", res.Status, strings.TrimSpace(string(raw)))
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		_, _ = out.Write(raw)
		return nil
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func commandPath(args []string) (string, error) {
	switch args[0] {
	case "health":
		return "/api/health", nil
	case "users":
		return "/api/users", nil
	case "targets":
		return "/api/targets", nil
	case "target-add":
		return "target-add", nil
	case "sessions":
		return "/api/sessions", nil
	case "session":
		if len(args) != 3 {
			return "", fmt.Errorf("session requires <id> <artifact>")
		}
		return "/api/sessions/" + args[1] + "/" + args[2], nil
	case "db":
		return "/api/db/connections", nil
	case "db-connection":
		if len(args) != 3 {
			return "", fmt.Errorf("db-connection requires <id> <artifact>")
		}
		return "/api/db/connections/" + args[1] + "/" + args[2], nil
	default:
		return "", fmt.Errorf("unknown command %q", args[0])
	}
}
