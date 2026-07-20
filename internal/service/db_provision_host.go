package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"jianmen/internal/model"
)

type mysqlAuthenticatedConnector func(
	context.Context,
	model.DatabaseInstance,
	string,
	string,
) (net.Conn, error)

func (MySQLDatabaseProvisioner) ResolveAccountHost(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
) (string, error) {
	return resolveMySQLAccountHost(ctx, instance, admin, mysqlConnect)
}

func resolveMySQLAccountHost(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	connect mysqlAuthenticatedConnector,
) (string, error) {
	if ctx == nil {
		return "", errors.New("resolve mysql account host: nil context")
	}
	if connect == nil {
		return "", errors.New("resolve mysql account host: connector is required")
	}
	password := admin.Password.GetPlaintext()
	if strings.TrimSpace(admin.Username) == "" || password == "" {
		return "", errors.New("resolve mysql account host: administrator credential is unavailable")
	}
	conn, err := connect(ctx, instance, admin.Username, password)
	if err != nil {
		return "", fmt.Errorf("resolve mysql account host: %w", err)
	}
	if conn == nil {
		return "", errors.New("resolve mysql account host: connection is unavailable")
	}
	defer conn.Close()

	address, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok || address == nil || address.IP == nil || address.IP.IsUnspecified() {
		return "", errors.New("resolve mysql account host: local address is unavailable")
	}
	host := address.IP.String()
	if net.ParseIP(host) == nil {
		return "", errors.New("resolve mysql account host: local address is not an IP")
	}
	if err := validateMySQLAccountHost(host); err != nil {
		return "", fmt.Errorf("resolve mysql account host: invalid local address: %w", err)
	}
	return host, nil
}
