package service

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"jianmen/internal/dbtls"
	"jianmen/internal/model"
	"jianmen/internal/proxy/mysqlwire"
)

type DBGrant struct {
	Database  string `json:"database"`
	Privilege string `json:"privilege"`
}

const (
	maxMySQLAuthPacketBytes     = 1 << 20
	maxMySQLProvisionPacketSize = mysqlwire.MaxPacketPayloadBytes
	mysqlProvisionTimeout       = 5 * time.Second
)

var errMySQLStatementRejected = errors.New("mysql statement rejected")

type mysqlTLSHandshakeError struct {
	cause error
}

func (e *mysqlTLSHandshakeError) Error() string { return "mysql TLS handshake failed" }
func (e *mysqlTLSHandshakeError) Unwrap() error { return e.cause }

type MySQLDatabaseProvisioner struct{}

func (MySQLDatabaseProvisioner) ListDatabases(
	ctx context.Context,
	instance model.DatabaseInstance,
	account model.DatabaseAccount,
) ([]string, error) {
	return ListMySQLDatabases(ctx, instance, account)
}

func (MySQLDatabaseProvisioner) CreateAccount(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	username, password, host string,
) (DatabaseAccountCreateResult, error) {
	return CreateMySQLAccount(ctx, instance, admin, username, password, host)
}

func (MySQLDatabaseProvisioner) GrantAccount(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	username, host string,
	grants []DBGrant,
) error {
	return GrantMySQLAccount(ctx, instance, admin, username, host, grants)
}

func (MySQLDatabaseProvisioner) DropAccount(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	username, host string,
) error {
	return DropMySQLAccount(ctx, instance, admin, username, host)
}

func databaseInstancePort(instance model.DatabaseInstance) int {
	if instance.Port > 0 {
		return instance.Port
	}
	return 3306
}

func mysqlConnect(
	ctx context.Context,
	instance model.DatabaseInstance,
	username, password string,
) (net.Conn, error) {
	if ctx == nil {
		return nil, errors.New("connect mysql upstream: nil context")
	}
	address := net.JoinHostPort(
		instance.Address,
		fmt.Sprintf("%d", databaseInstancePort(instance)),
	)
	conn, err := (&net.Dialer{Timeout: mysqlProvisionTimeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, errors.New("connect mysql upstream")
	}
	closeWithError := func(result error) (net.Conn, error) {
		_ = conn.Close()
		return nil, result
	}
	deadline := time.Now().Add(mysqlProvisionTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return closeWithError(errors.New("set mysql authentication deadline"))
	}
	greeting, err := mysqlwire.ReadPacket(ctx, conn, maxMySQLAuthPacketBytes)
	if err != nil {
		return closeWithError(errors.New("read mysql handshake"))
	}
	handshake, err := mysqlwire.ParseHandshake(greeting.Payload)
	if err != nil {
		return closeWithError(err)
	}
	mode, err := dbtls.NormalizeMode(instance.TLSMode)
	if err != nil {
		return closeWithError(errors.New("invalid mysql TLS policy"))
	}
	loginSequence := byte(1)
	if mode != dbtls.ModeDisable {
		if err := writeMySQLTLSRequest(ctx, conn, handshake); err != nil {
			return closeWithError(err)
		}
		secured, err := dbtls.HandshakeClient(
			ctx,
			conn,
			dbtls.Config{
				Mode: mode, ServerName: instance.TLSServerName, CAPEM: instance.TLSCAPEM,
			},
			address,
		)
		if err != nil {
			return closeWithError(&mysqlTLSHandshakeError{cause: err})
		}
		conn = secured
		loginSequence = 2
	}
	loginPacket, err := mysqlwire.BuildHandshakeResponse41(
		handshake,
		mysqlwire.LoginOptions{
			Username: username, Password: password, AuthPlugin: handshake.AuthPluginName,
			Sequence: loginSequence, TLS: mode != dbtls.ModeDisable,
		},
	)
	if err != nil {
		return closeWithError(err)
	}
	if err := writeMySQLRawPacket(ctx, conn, loginPacket); err != nil {
		return closeWithError(err)
	}
	if err := mysqlwire.ContinueAuthentication(
		ctx,
		conn,
		mysqlwire.AuthenticationOptions{
			Password: password, VerifiedTLS: dbtls.IsVerified(conn),
			MaxPacketBytes: maxMySQLAuthPacketBytes,
		},
	); err != nil {
		return closeWithError(err)
	}
	if err := mysqlwire.ClearDeadline(conn); err != nil {
		return closeWithError(err)
	}
	return conn, nil
}

func writeMySQLTLSRequest(
	ctx context.Context,
	conn net.Conn,
	handshake mysqlwire.Handshake,
) error {
	if handshake.CapabilityFlags&mysqlwire.ClientSSL == 0 {
		return errors.New("mysql upstream does not support TLS")
	}
	capabilities := mysqlwire.ClientProtocol41 | mysqlwire.ClientSSL
	if handshake.CapabilityFlags&mysqlwire.ClientSecureConnection != 0 {
		capabilities |= mysqlwire.ClientSecureConnection
	}
	if handshake.CapabilityFlags&mysqlwire.ClientPluginAuth != 0 {
		capabilities |= mysqlwire.ClientPluginAuth
	}
	payload := make([]byte, 32)
	binary.LittleEndian.PutUint32(payload[:4], capabilities)
	binary.LittleEndian.PutUint32(payload[4:8], 1<<24)
	payload[8] = handshake.CharacterSet
	if payload[8] == 0 {
		payload[8] = 45
	}
	return mysqlwire.WritePacket(ctx, conn, 1, payload)
}

func writeMySQLRawPacket(ctx context.Context, conn net.Conn, packet []byte) error {
	return mysqlwire.WriteRawPacket(ctx, conn, packet)
}

func mysqlQuery(ctx context.Context, conn net.Conn, query string) ([][]string, error) {
	return mysqlQueryWithTimeout(ctx, conn, query, mysqlProvisionTimeout)
}

func mysqlQueryWithTimeout(
	ctx context.Context,
	conn net.Conn,
	query string,
	timeout time.Duration,
) ([][]string, error) {
	if ctx == nil || timeout <= 0 {
		return nil, errors.New("invalid mysql query context")
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload := append([]byte{0x03}, query...)
	if err := mysqlwire.WritePacket(commandContext, conn, 0, payload); err != nil {
		return nil, errors.New("write mysql query")
	}
	response, err := mysqlwire.ReadPacket(
		commandContext,
		conn,
		maxMySQLProvisionPacketSize,
	)
	if err != nil {
		return nil, errors.New("read mysql query response")
	}
	if len(response.Payload) == 0 {
		return nil, errors.New("empty mysql query response")
	}
	if response.Payload[0] == 0xff {
		return nil, errors.New("mysql query rejected")
	}
	columnCount, _ := readLenEncInt(response.Payload)
	if columnCount == 0 {
		return nil, nil
	}
	for index := uint64(0); index < columnCount+1; index++ {
		if _, err := mysqlwire.ReadPacket(
			commandContext,
			conn,
			maxMySQLProvisionPacketSize,
		); err != nil {
			return nil, errors.New("read mysql result metadata")
		}
	}
	var rows [][]string
	for {
		packet, err := mysqlwire.ReadPacket(
			commandContext,
			conn,
			maxMySQLProvisionPacketSize,
		)
		if err != nil {
			return nil, errors.New("read mysql result row")
		}
		if len(packet.Payload) == 0 {
			return nil, errors.New("empty mysql result row")
		}
		if packet.Payload[0] == 0xfe && len(packet.Payload) < 9 {
			return rows, nil
		}
		if packet.Payload[0] == 0xff {
			return nil, errors.New("mysql query rejected")
		}
		rows = append(rows, parseMySQLTextRow(packet.Payload))
	}
}

func mysqlExec(ctx context.Context, conn net.Conn, statement string) error {
	return mysqlExecWithTimeout(ctx, conn, statement, mysqlProvisionTimeout)
}

func mysqlExecWithTimeout(
	ctx context.Context,
	conn net.Conn,
	statement string,
	timeout time.Duration,
) error {
	if ctx == nil || timeout <= 0 {
		return errMySQLStatementNotSent
	}
	commandContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload := append([]byte{0x03}, statement...)
	if err := mysqlwire.WritePacket(commandContext, conn, 0, payload); err != nil {
		if mysqlwire.WrittenBytes(err) == 0 {
			return errMySQLStatementNotSent
		}
		return errMySQLStatementOutcomeUncertain
	}
	response, err := mysqlwire.ReadPacket(
		commandContext,
		conn,
		maxMySQLProvisionPacketSize,
	)
	if err != nil {
		return errMySQLStatementOutcomeUncertain
	}
	if len(response.Payload) == 0 {
		return errMySQLStatementOutcomeUncertain
	}
	switch response.Payload[0] {
	case 0x00:
		return nil
	case 0xff:
		return errMySQLStatementRejected
	default:
		return errMySQLStatementOutcomeUncertain
	}
}

func ListMySQLDatabases(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
) ([]string, error) {
	password := admin.Password.GetPlaintext()
	if password == "" {
		return nil, errors.New("database provisioning credential is unavailable")
	}
	conn, err := mysqlConnect(ctx, instance, admin.Username, password)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	rows, err := mysqlQuery(ctx, conn, "SHOW DATABASES")
	if err != nil {
		return nil, err
	}
	databases := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 && row[0] != "" {
			databases = append(databases, row[0])
		}
	}
	return databases, nil
}

func CreateMySQLAccount(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	username, password, host string,
) (DatabaseAccountCreateResult, error) {
	conn, err := connectWithProvisioningAdmin(ctx, instance, admin)
	if err != nil {
		return DatabaseAccountCreateResult{
			Disposition: DatabaseAccountCreateNotSent,
		}, err
	}
	defer conn.Close()
	return runMySQLCreateStatements(
		func(statement string) error {
			return mysqlExec(ctx, conn, statement)
		},
		username,
		password,
		host,
	)
}

func GrantMySQLAccount(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	username, host string,
	grants []DBGrant,
) error {
	statements, err := buildMySQLGrantStatements(username, host, grants)
	if err != nil {
		return err
	}
	conn, err := connectWithProvisioningAdmin(ctx, instance, admin)
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, statement := range statements {
		if err := mysqlExec(ctx, conn, statement); err != nil {
			return errors.New("grant mysql account privileges")
		}
	}
	return nil
}

func ProvisionMySQLAccount(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	username, password, host string,
	grants []DBGrant,
) error {
	result, err := CreateMySQLAccount(ctx, instance, admin, username, password, host)
	if err != nil {
		return err
	}
	if result.Disposition != DatabaseAccountCreateApplied {
		return errors.New("create mysql account did not complete")
	}
	return GrantMySQLAccount(ctx, instance, admin, username, host, grants)
}

func DropMySQLAccount(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	username, host string,
) error {
	if err := validateMySQLAccountName(username); err != nil {
		return err
	}
	if err := validateMySQLAccountHost(host); err != nil {
		return err
	}
	conn, err := connectWithProvisioningAdmin(ctx, instance, admin)
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, statement := range []string{
		mysqlNoBackslashEscapesSQL,
		dropMySQLUserStatement(username, host),
	} {
		if err := mysqlExec(ctx, conn, statement); err != nil {
			return errors.New("drop mysql account")
		}
	}
	return nil
}

func connectWithProvisioningAdmin(
	ctx context.Context,
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
) (net.Conn, error) {
	password := admin.Password.GetPlaintext()
	if password == "" {
		return nil, errors.New("database provisioning credential is unavailable")
	}
	return mysqlConnect(ctx, instance, admin.Username, password)
}

func readLenEncInt(data []byte) (uint64, int) {
	if len(data) == 0 {
		return 0, 0
	}
	switch data[0] {
	case 0xfc:
		if len(data) >= 3 {
			return uint64(data[1]) | uint64(data[2])<<8, 3
		}
	case 0xfd:
		if len(data) >= 4 {
			return uint64(data[1]) | uint64(data[2])<<8 | uint64(data[3])<<16, 4
		}
	case 0xfe:
		if len(data) >= 9 {
			return binary.LittleEndian.Uint64(data[1:9]), 9
		}
	default:
		if data[0] < 0xfb {
			return uint64(data[0]), 1
		}
	}
	return 0, 0
}

func parseMySQLTextRow(payload []byte) []string {
	position := 0
	var columns []string
	for position < len(payload) {
		if payload[position] == 0xfb {
			columns = append(columns, "")
			position++
			continue
		}
		length, offset := readLenEncInt(payload[position:])
		if offset == 0 {
			return columns
		}
		position += offset
		end := position + int(length)
		if end > len(payload) {
			return columns
		}
		columns = append(columns, string(payload[position:end]))
		position = end
	}
	return columns
}
