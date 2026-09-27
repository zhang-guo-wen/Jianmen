package dbproxy

import (
	"strings"
	"testing"
)

func TestValidLargeSQLCanExpandBeyondMySQLMediumTextDuringRedaction(t *testing.T) {
	const sourceBytes = 4 * 1024 * 1024
	prefix := "SELECT "
	input := prefix + strings.Repeat("1,", (sourceBytes-len(prefix))/2)
	if len(input) > defaultMaxClientMessageBytes {
		t.Fatalf(
			"test SQL = %d bytes, want within %d-byte proxy limit",
			len(input),
			defaultMaxClientMessageBytes,
		)
	}

	redacted := redactDatabaseSQL(input)
	const mysqlMediumTextMaxBytes = 0xffffff
	if len(redacted) <= mysqlMediumTextMaxBytes {
		t.Fatalf(
			"redacted SQL = %d bytes, want above MySQL MEDIUMTEXT limit %d",
			len(redacted),
			mysqlMediumTextMaxBytes,
		)
	}
}
