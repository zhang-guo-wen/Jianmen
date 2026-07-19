package dbproxy

import (
	"context"
	"encoding/binary"
	"net"
	"time"
)

// handlePG authenticates the bastion user, applies RBAC, authenticates the
// selected upstream account, and captures cancellation routing metadata.
func (g *Gateway) handlePG(ctx context.Context, client net.Conn, firstByte byte) *gatewayConn {
	startupMessage, err := readPostgresStartupMessage(client, firstByte)
	if err != nil {
		g.logger.Warn("db gateway invalid PostgreSQL startup request")
		return nil
	}
	if len(startupMessage) >= 8 &&
		binary.BigEndian.Uint32(startupMessage[4:8]) == postgresCancelRequestCode {
		if err := g.forwardPostgresCancel(ctx, startupMessage); err != nil {
			g.logger.Warn("db gateway rejected PostgreSQL CancelRequest")
		}
		return nil
	}
	startup, err := parsePostgresStartup(startupMessage)
	if err != nil {
		g.logger.Warn("db gateway failed to parse PostgreSQL StartupMessage")
		_ = writePostgresAuthenticationError(client)
		return nil
	}
	if err := writePostgresProtocolNegotiation(startup, client); err != nil {
		return nil
	}

	resolved, err := g.resolveAccount(ctx, startup.username)
	if err != nil {
		g.logger.Warn("db gateway account resolution failed")
		_ = writePostgresAuthenticationError(client)
		return nil
	}
	account := resolved.account
	if err := validateResolvedAccountProtocol(resolved, databaseProtocolPostgreSQL); err != nil {
		g.logger.Warn("PostgreSQL gateway rejected cross-protocol account")
		if writeErr := writePostgresAccountProtocolError(client); writeErr != nil {
			g.logger.Warn("PostgreSQL gateway failed to send protocol rejection")
		}
		return nil
	}

	if err := writePostgresMessage(client, 'R', []byte{0, 0, 0, 3}); err != nil {
		return nil
	}
	password, err := readPostgresPasswordMessage(client)
	if err != nil {
		g.logger.Warn("db gateway invalid PostgreSQL PasswordMessage")
		_ = writePostgresAuthenticationError(client)
		return nil
	}
	if err := g.authenticatePostgresConnection(ctx, resolved, password); err != nil {
		g.logger.Warn("db gateway authentication or authorization failed")
		_ = writePostgresAuthenticationError(client)
		return nil
	}
	if account.Status == "disabled" {
		g.logger.Warn("db gateway account disabled")
		_ = writePostgresAuthenticationError(client)
		return nil
	}
	if account.ExpiresAt != nil && time.Now().UTC().After(*account.ExpiresAt) {
		g.logger.Warn("db gateway account expired")
		_ = writePostgresAuthenticationError(client)
		return nil
	}
	if account.Instance.Status == "disabled" {
		g.logger.Warn("db gateway instance disabled")
		_ = writePostgresAuthenticationError(client)
		return nil
	}

	upstream, err := dialPostgresUpstream(ctx, account.Instance)
	if err != nil {
		g.logger.Warn("db gateway upstream connect failed")
		_ = writePostgresAuthenticationError(client)
		return nil
	}
	upstreamStartupMessage := buildPostgresUpstreamStartupMessage(account.Username, startup)
	if err := writePostgresBytes(upstream, upstreamStartupMessage); err != nil {
		_ = upstream.Close()
		return nil
	}
	upstreamStartup, err := authenticatePostgresUpstream(
		upstream,
		client,
		account.Username,
		account.Password.GetPlaintext(),
		func(key postgresCancelKey) func() {
			return g.postgresCancels.register(key, account.Instance)
		},
	)
	if err != nil {
		g.logger.Warn("db gateway PostgreSQL upstream authentication failed")
		_ = writePostgresAuthenticationError(client)
		_ = upstream.Close()
		return nil
	}

	connection := &gatewayConn{
		protocol: "postgres", accountID: account.ID, instanceID: account.InstanceID,
		accountName: resolved.rawName, upstream: upstream,
		upstreamAddr: upstreamAddress(account.Instance), userID: resolved.user.ID,
		accountUser: account.Username, instanceName: account.Instance.Name,
		userSessionID:         resolved.userSessionID,
		postgresCancelCleanup: upstreamStartup.cancelCleanup,
	}
	return connection
}
