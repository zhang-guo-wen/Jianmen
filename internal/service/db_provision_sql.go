package service

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

const mysqlNoBackslashEscapesSQL = "SET SESSION sql_mode = CONCAT_WS(',', NULLIF(@@SESSION.sql_mode, ''), 'NO_BACKSLASH_ESCAPES')"

var (
	errMySQLStatementNotSent          = errors.New("mysql statement was not sent")
	errMySQLStatementOutcomeUncertain = errors.New("mysql statement outcome is uncertain")
)

func ValidateMySQLProvisioning(username, password, host string, grants []DBGrant) error {
	_, err := buildMySQLProvisionStatements(username, password, host, grants)
	return err
}

func buildMySQLProvisionStatements(username, password, host string, grants []DBGrant) ([]string, error) {
	createStatements, err := buildMySQLCreateStatements(username, password, host)
	if err != nil {
		return nil, err
	}
	grantStatements, err := buildMySQLGrantStatements(username, host, grants)
	if err != nil {
		return nil, err
	}
	return append(createStatements, grantStatements[1:]...), nil
}

func buildMySQLCreateStatements(username, password, host string) ([]string, error) {
	if err := validateMySQLAccountName(username); err != nil {
		return nil, err
	}
	if err := validateMySQLAccountHost(host); err != nil {
		return nil, err
	}
	if err := validateMySQLPassword(password); err != nil {
		return nil, err
	}
	return []string{
		mysqlNoBackslashEscapesSQL,
		fmt.Sprintf(
			"CREATE USER %s@%s IDENTIFIED BY %s",
			quoteMySQLString(username),
			quoteMySQLString(host),
			quoteMySQLString(password),
		),
	}, nil
}

func buildMySQLGrantStatements(username, host string, grants []DBGrant) ([]string, error) {
	if err := validateMySQLAccountName(username); err != nil {
		return nil, err
	}
	if err := validateMySQLAccountHost(host); err != nil {
		return nil, err
	}
	if len(grants) == 0 {
		return nil, errors.New("at least one database grant is required")
	}
	if len(grants) > 128 {
		return nil, errors.New("too many database grants")
	}

	quotedUser := quoteMySQLString(username)
	quotedHost := quoteMySQLString(host)
	statements := []string{mysqlNoBackslashEscapesSQL}
	for _, grant := range grants {
		database, err := quoteMySQLDatabaseIdentifier(grant.Database)
		if err != nil {
			return nil, err
		}
		var privileges string
		switch grant.Privilege {
		case "read":
			privileges = "SELECT"
		case "readwrite":
			privileges = "SELECT, INSERT, UPDATE, DELETE"
		default:
			return nil, fmt.Errorf("unsupported database privilege %q", grant.Privilege)
		}
		statements = append(
			statements,
			fmt.Sprintf("GRANT %s ON %s.* TO %s@%s", privileges, database, quotedUser, quotedHost),
		)
	}
	return statements, nil
}

func runMySQLCreateStatements(
	exec func(string) error,
	username, password, host string,
) (DatabaseAccountCreateResult, error) {
	statements, err := buildMySQLCreateStatements(username, password, host)
	if err != nil {
		return DatabaseAccountCreateResult{
			Disposition: DatabaseAccountCreateNotSent,
		}, err
	}
	for index, statement := range statements {
		if err := exec(statement); err != nil {
			if index == 0 {
				return DatabaseAccountCreateResult{
					Disposition: DatabaseAccountCreateNotSent,
				}, err
			}
			if errors.Is(err, errMySQLStatementOutcomeUncertain) {
				return DatabaseAccountCreateResult{
					Disposition: DatabaseAccountCreateMayBeApplied,
				}, err
			}
			if errors.Is(err, errMySQLStatementNotSent) {
				return DatabaseAccountCreateResult{
					Disposition: DatabaseAccountCreateNotSent,
				}, err
			}
			if errors.Is(err, errMySQLStatementRejected) {
				return DatabaseAccountCreateResult{
					Disposition: DatabaseAccountCreateNotCreated,
				}, err
			}
			return DatabaseAccountCreateResult{
				Disposition: DatabaseAccountCreateNotSent,
			}, err
		}
	}
	return DatabaseAccountCreateResult{Disposition: DatabaseAccountCreateApplied}, nil
}

func dropMySQLUserStatement(username, host string) string {
	return fmt.Sprintf(
		"DROP USER IF EXISTS %s@%s",
		quoteMySQLString(username),
		quoteMySQLString(host),
	)
}

func quoteMySQLString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func quoteMySQLDatabaseIdentifier(value string) (string, error) {
	if value == "" || len(value) > 64 {
		return "", errors.New("database name must contain 1 to 64 characters")
	}
	for _, character := range value {
		if !isMySQLIdentifierCharacter(character) {
			return "", errors.New("database name contains an unsupported character")
		}
	}
	return "`" + strings.ReplaceAll(value, "`", "``") + "`", nil
}

func validateMySQLAccountName(value string) error {
	if value == "" || len(value) > 32 {
		return errors.New("database username must contain 1 to 32 characters")
	}
	for _, character := range value {
		if !isMySQLUsernameCharacter(character) {
			return errors.New("database username contains an unsupported character")
		}
	}
	return nil
}

func validateMySQLAccountHost(value string) error {
	if value == "" || len(value) > 255 {
		return errors.New("database account host must contain 1 to 255 characters")
	}
	if strings.ContainsAny(value, "%_") {
		return errors.New("database account host must be an exact address")
	}
	if net.ParseIP(value) != nil {
		return nil
	}
	if len(value) > 253 {
		return errors.New("database account host is too long")
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 ||
			label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("database account host is not an exact hostname")
		}
		for _, character := range label {
			if !isASCIIAlphaNumeric(character) && character != '-' {
				return errors.New("database account host contains an unsupported character")
			}
		}
	}
	return nil
}

func validateMySQLPassword(value string) error {
	if value == "" || len(value) > 1024 {
		return errors.New("database password must contain 1 to 1024 characters")
	}
	if strings.ContainsAny(value, "\x00\r\n") {
		return errors.New("database password contains an unsupported control character")
	}
	return nil
}

func isMySQLIdentifierCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		character == '_' || character == '$' || character == '-'
}

func isMySQLUsernameCharacter(character rune) bool {
	return isMySQLIdentifierCharacter(character) ||
		character == '.' || character == '\''
}

func isASCIIAlphaNumeric(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9'
}
