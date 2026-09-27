package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"jianmen/internal/model"
)

// ErrDatabaseUpstreamUnavailable indicates the database upstream could not be
// reached or rejected the SHOW GRANTS query while inspecting account
// privileges. The wrapped cause is intentionally not exposed to clients.
var ErrDatabaseUpstreamUnavailable = errors.New("database upstream unavailable")

// DatabaseAccountGrant is the per-database privilege of a database account as
// surfaced by MySQL SHOW GRANTS. Privilege is one of the provisioning-level
// values ("read", "readwrite") or "" when the grant does not map to that model
// (for example a global or table-level grant, which is skipped).
type DatabaseAccountGrant struct {
	Database   string   `json:"database"`
	Privilege  string   `json:"privilege"`
	Privileges []string `json:"privileges"`
}

// DatabaseAccountPrivileges is the result of inspecting the upstream privileges
// of a database account.
type DatabaseAccountPrivileges struct {
	AccountID  string
	InstanceID string
	Username   string
	Protocol   string
	Grants     []DatabaseAccountGrant
	Warnings   []string
}

var (
	mysqlGrantStatementRE = regexp.MustCompile(`(?is)^GRANT\s+(.+?)\s+ON\s+(.+?)\s+TO\s+`)
	mysqlGrantIdentifiedBY = regexp.MustCompile(`(?is)\s+IDENTIFIED\s+BY(\s+PASSWORD)?\s+'(?:[^']|'')*'`)
)

// AccountPrivileges inspects the upstream privileges of the requested database
// account. It connects to the instance using the account's own stored
// credentials and runs SHOW GRANTS, so the returned privileges reflect the
// account's current effective grants.
func (s *DatabaseManagementService) AccountPrivileges(
	ctx context.Context,
	actorID string,
	accountID string,
) (DatabaseAccountPrivileges, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return DatabaseAccountPrivileges{}, fmt.Errorf("%w: account_id is required", ErrDatabaseManagementInvalid)
	}
	target, err := s.SavedAccountProbe(ctx, actorID, accountID)
	if err != nil {
		return DatabaseAccountPrivileges{}, err
	}
	if !strings.EqualFold(strings.TrimSpace(target.Instance.Protocol), "mysql") {
		return DatabaseAccountPrivileges{}, fmt.Errorf("%w: privilege inspection supports mysql instances", ErrDatabaseManagementInvalid)
	}
	instance := model.DatabaseInstance{
		ID: target.Instance.ID, Name: target.Instance.Name, Protocol: target.Instance.Protocol,
		Address: target.Instance.Address, Port: target.Instance.Port, TLSMode: target.Instance.TLSMode,
		TLSServerName: target.Instance.TLSServerName, TLSCAPEM: target.Instance.TLSCAPEM,
		GroupName: target.Instance.GroupName, Remark: target.Instance.Remark, Status: target.Instance.Status,
	}
	conn, err := mysqlConnect(ctx, instance, target.Username, target.Password)
	if err != nil {
		return DatabaseAccountPrivileges{}, fmt.Errorf("%w: connect database account upstream", ErrDatabaseUpstreamUnavailable)
	}
	defer conn.Close()
	rows, err := mysqlQuery(ctx, conn, "SHOW GRANTS")
	if err != nil {
		return DatabaseAccountPrivileges{}, fmt.Errorf("%w: query database account privileges", ErrDatabaseUpstreamUnavailable)
	}
	grants, warnings := parseMySQLGrantRows(rows)
	return DatabaseAccountPrivileges{
		AccountID:  accountID,
		InstanceID: target.Instance.ID,
		Username:   target.Username,
		Protocol:   target.Instance.Protocol,
		Grants:     grants,
		Warnings:   warnings,
	}, nil
}

// parseMySQLGrantRows converts SHOW GRANTS result rows into per-database grants.
// Non-database-level statements (global grants on *.*, or table-level grants)
// are skipped and reported through warnings so callers can surface them.
func parseMySQLGrantRows(rows [][]string) ([]DatabaseAccountGrant, []string) {
	byDatabase := map[string][]string{}
	var order []string
	var warnings []string
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		statement := sanitizeMySQLGrantText(row[0])
		if statement == "" {
			continue
		}
		matches := mysqlGrantStatementRE.FindStringSubmatch(statement)
		if len(matches) != 3 {
			warnings = append(warnings, "无法解析的授权语句："+truncateMySQLGrantText(statement))
			continue
		}
		privileges := splitMySQLGrantPrivileges(matches[1])
		if len(privileges) == 0 || containsMySQLGrantPrivilege(privileges, "USAGE") {
			continue
		}
		database, table, global := parseMySQLGrantOnClause(matches[2])
		if global {
			warnings = append(warnings, "存在全局（*.*）授权，未按数据库展示："+strings.Join(privileges, ", "))
			continue
		}
		if database == "" || table != "*" {
			warnings = append(warnings, "存在表级或无法归一化的授权，未按数据库展示："+statement)
			continue
		}
		if _, seen := byDatabase[database]; !seen {
			order = append(order, database)
		}
		byDatabase[database] = appendUniqueMySQLGrantPrivileges(byDatabase[database], privileges...)
	}
	grants := make([]DatabaseAccountGrant, 0, len(order))
	for _, database := range order {
		privileges := byDatabase[database]
		grants = append(grants, DatabaseAccountGrant{
			Database:   database,
			Privilege:  classifyMySQLGrantPrivileges(privileges),
			Privileges: privileges,
		})
	}
	return grants, warnings
}

func sanitizeMySQLGrantText(statement string) string {
	statement = strings.TrimSpace(statement)
	statement = mysqlGrantIdentifiedBY.ReplaceAllString(statement, "")
	statement = strings.TrimSpace(statement)
	statement = strings.TrimSuffix(statement, ";")
	return strings.TrimSpace(statement)
}

func truncateMySQLGrantText(statement string) string {
	const max = 80
	if len(statement) <= max {
		return statement
	}
	return statement[:max] + "…"
}

func splitMySQLGrantPrivileges(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	value = strings.ReplaceAll(value, "WITH GRANT OPTION", "GRANT OPTION")
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.ToUpper(strings.TrimSpace(part))
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func appendUniqueMySQLGrantPrivileges(existing []string, values ...string) []string {
	for _, value := range values {
		found := false
		for _, current := range existing {
			if current == value {
				found = true
				break
			}
		}
		if !found {
			existing = append(existing, value)
		}
	}
	return existing
}

func containsMySQLGrantPrivilege(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// parseMySQLGrantOnClause parses the ON <db>.<table> portion of a grant. It
// returns global=true for *.*, and the database/table names (backticks removed).
// A database-level grant has table == "*".
func parseMySQLGrantOnClause(on string) (database, table string, global bool) {
	on = strings.TrimSpace(on)
	if on == "*.*" {
		return "", "", true
	}
	index := strings.LastIndex(on, ".")
	if index < 0 {
		return "", "", false
	}
	database = stripMySQLGrantBackticks(on[:index])
	table = stripMySQLGrantBackticks(on[index+1:])
	return database, table, false
}

func stripMySQLGrantBackticks(value string) string {
	return strings.ReplaceAll(value, "`", "")
}

// classifyMySQLGrantPrivileges maps a set of MySQL privileges to the
// provisioning-level privilege model ("read" | "readwrite"). Write privileges
// beyond SELECT map to readwrite; SELECT alone maps to read.
func classifyMySQLGrantPrivileges(privileges []string) string {
	readwritePrivileges := map[string]bool{
		"INSERT": true, "UPDATE": true, "DELETE": true, "CREATE": true, "DROP": true,
		"ALTER": true, "INDEX": true, "REFERENCES": true, "EXECUTE": true,
		"CREATE ROUTINE": true, "ALTER ROUTINE": true, "CREATE VIEW": true,
		"SHOW VIEW": true, "TRIGGER": true, "LOCK TABLES": true, "EVENT": true,
		"GRANT OPTION": true, "ALL": true, "ALL PRIVILEGES": true,
	}
	hasRead := false
	for _, privilege := range privileges {
		if readwritePrivileges[privilege] {
			return "readwrite"
		}
		if privilege == "SELECT" {
			hasRead = true
		}
	}
	if hasRead {
		return "read"
	}
	return ""
}
