package dbproxy

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

func validateResolvedAccountProtocol(resolved *resolvedDBAccount, listenerProtocol databaseProtocol) error {
	if resolved == nil || resolved.account == nil {
		return errors.New("resolved database account is missing")
	}

	expectedProtocol := string(listenerProtocol)
	if listenerProtocol == databaseProtocolPostgreSQL {
		expectedProtocol = "postgres"
	}
	instanceProtocol := strings.ToLower(strings.TrimSpace(resolved.account.Instance.Protocol))
	if instanceProtocol != expectedProtocol {
		return fmt.Errorf(
			"database account %q belongs to %q instance, not %q listener",
			resolved.account.ID,
			instanceProtocol,
			expectedProtocol,
		)
	}
	return nil
}

func writePostgresAccountProtocolError(connection net.Conn) error {
	payload := []byte("SFATAL\x00C28000\x00Mdatabase account protocol does not match this listener\x00\x00")
	return writePostgresMessage(connection, 'E', payload)
}

func writePostgresAuthenticationError(connection net.Conn) error {
	payload := []byte("SFATAL\x00C28000\x00Mdatabase gateway authentication failed\x00\x00")
	return writePostgresMessage(connection, 'E', payload)
}
