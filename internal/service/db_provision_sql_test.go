package service

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildMySQLProvisionStatementsQuotesValuesAfterDisablingBackslashEscapes(t *testing.T) {
	statements, err := buildMySQLProvisionStatements(
		"o'hara",
		`pa\ss'word`,
		"10.0.0.8",
		[]DBGrant{{Database: "customer-data", Privilege: "readwrite"}},
	)
	if err != nil {
		t.Fatalf("build statements: %v", err)
	}
	want := []string{
		mysqlNoBackslashEscapesSQL,
		`CREATE USER 'o''hara'@'10.0.0.8' IDENTIFIED BY 'pa\ss''word'`,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON `customer-data`.* TO 'o''hara'@'10.0.0.8'",
	}
	if len(statements) != len(want) {
		t.Fatalf("statements = %#v, want %#v", statements, want)
	}
	for index := range want {
		if statements[index] != want[index] {
			t.Fatalf("statement %d = %q, want %q", index, statements[index], want[index])
		}
	}
}

func TestValidateMySQLProvisioningRequiresExactAccountHost(t *testing.T) {
	for _, host := range []string{"", "%", "_", "10.0.0.%", "gateway_internal"} {
		t.Run(host, func(t *testing.T) {
			err := ValidateMySQLProvisioning(
				"app",
				"server-generated-secret",
				host,
				[]DBGrant{{Database: "app", Privilege: "read"}},
			)
			if err == nil {
				t.Fatalf("accepted non-exact database account host %q", host)
			}
		})
	}
	for _, host := range []string{"10.0.0.8", "gateway.internal", "2001:db8::8"} {
		t.Run(host, func(t *testing.T) {
			err := ValidateMySQLProvisioning(
				"app",
				"server-generated-secret",
				host,
				[]DBGrant{{Database: "app", Privilege: "read"}},
			)
			if err != nil {
				t.Fatalf("rejected exact database account host %q: %v", host, err)
			}
		})
	}
}

func TestBuildMySQLProvisionStatementsRejectsInjectionShapedInput(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		host     string
		grant    DBGrant
	}{
		{
			name:     "username statement",
			username: "alice'; DROP USER root; --",
			password: "secret",
			host:     "%",
			grant:    DBGrant{Database: "app", Privilege: "read"},
		},
		{
			name:     "host statement",
			username: "alice",
			password: "secret",
			host:     "%'; GRANT ALL ON *.* TO attacker; --",
			grant:    DBGrant{Database: "app", Privilege: "read"},
		},
		{
			name:     "database statement",
			username: "alice",
			password: "secret",
			host:     "%",
			grant:    DBGrant{Database: "app`.* TO 'attacker'@'%' ; --", Privilege: "read"},
		},
		{
			name:     "unknown privilege",
			username: "alice",
			password: "secret",
			host:     "%",
			grant:    DBGrant{Database: "app", Privilege: "all"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := buildMySQLProvisionStatements(
				test.username,
				test.password,
				test.host,
				[]DBGrant{test.grant},
			); err == nil {
				t.Fatal("build statements accepted injection-shaped input")
			}
		})
	}
}

func TestRunMySQLCreateStatementsClassifiesLostResponseAsUncertain(t *testing.T) {
	result, err := runMySQLCreateStatements(
		func(statement string) error {
			if strings.HasPrefix(statement, "CREATE USER ") {
				return errMySQLStatementOutcomeUncertain
			}
			return nil
		},
		"alice",
		"server-generated-secret",
		"10.0.0.8",
	)
	if !errors.Is(err, errMySQLStatementOutcomeUncertain) ||
		result.Disposition != DatabaseAccountCreateMayBeApplied {
		t.Fatalf("create result = %#v, error = %v, want possibly-applied", result, err)
	}
}

func TestRunMySQLCreateStatementsKeepsServerRejectionDeterministic(t *testing.T) {
	result, err := runMySQLCreateStatements(
		func(statement string) error {
			if strings.HasPrefix(statement, "CREATE USER ") {
				return errMySQLStatementRejected
			}
			return nil
		},
		"alice",
		"server-generated-secret",
		"10.0.0.8",
	)
	if !errors.Is(err, errMySQLStatementRejected) {
		t.Fatalf("create error = %v, want server rejection", err)
	}
	if result.Disposition != DatabaseAccountCreateNotCreated {
		t.Fatalf("server rejection result = %#v, want explicitly-not-created", result)
	}
}
