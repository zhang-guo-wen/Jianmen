package service

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type ApplicationAddress struct {
	Address   string
	EntryPath string
	Scheme    string
	Host      string
	Port      int
}

func ParseApplicationAddress(raw string) (ApplicationAddress, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ApplicationAddress{}, fmt.Errorf("application address is required")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return ApplicationAddress{}, fmt.Errorf("parse application address: %w", err)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ApplicationAddress{}, fmt.Errorf("application address scheme must be http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return ApplicationAddress{}, fmt.Errorf("application address host is required")
	}
	if parsed.User != nil {
		return ApplicationAddress{}, fmt.Errorf("application address must not contain credentials")
	}

	port := 80
	if parsed.Scheme == "https" {
		port = 443
	}
	if rawPort := parsed.Port(); rawPort != "" {
		port, err = strconv.Atoi(rawPort)
		if err != nil || port <= 0 || port > 65535 {
			return ApplicationAddress{}, fmt.Errorf("application address port must be 1-65535")
		}
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}

	entryPath := parsed.EscapedPath()
	if entryPath == "" {
		entryPath = "/"
	}
	if parsed.RawQuery != "" {
		entryPath += "?" + parsed.RawQuery
	}
	if parsed.Fragment != "" {
		entryPath += "#" + parsed.EscapedFragment()
	}

	return ApplicationAddress{
		Address:   parsed.String(),
		EntryPath: entryPath,
		Scheme:    parsed.Scheme,
		Host:      parsed.Hostname(),
		Port:      port,
	}, nil
}
