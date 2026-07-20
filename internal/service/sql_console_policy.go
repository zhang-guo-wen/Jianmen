package service

import (
	"errors"
	"strings"
	"unicode"
)

var (
	ErrSQLConsoleInvalid            = errors.New("invalid SQL console request")
	ErrSQLConsoleMultipleStatements = errors.New("only one SQL statement is allowed")
	ErrSQLConsoleUnsupported        = errors.New("unsupported SQL statement")
	ErrSQLConsoleWriteConfirmation  = errors.New("write statement confirmation is required")
)

const maxSQLConsoleStatementBytes = 64 * 1024

type sqlStatementPolicy struct {
	SQL       string
	QueryKind string
	ReadOnly  bool
}

func inspectSQLStatement(value string) (sqlStatementPolicy, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxSQLConsoleStatementBytes {
		return sqlStatementPolicy{}, ErrSQLConsoleInvalid
	}
	statement, err := singleSQLStatement(value)
	if err != nil {
		return sqlStatementPolicy{}, err
	}
	keyword := firstSQLKeyword(statement)
	switch keyword {
	case "select":
		if containsUnsafeSelectClause(statement) {
			return sqlStatementPolicy{}, ErrSQLConsoleUnsupported
		}
		return sqlStatementPolicy{SQL: statement, QueryKind: keyword, ReadOnly: true}, nil
	case "show", "describe", "desc", "explain", "with", "values":
		return sqlStatementPolicy{SQL: statement, QueryKind: keyword, ReadOnly: true}, nil
	case "insert", "update", "delete", "replace", "create", "alter", "drop", "truncate":
		return sqlStatementPolicy{SQL: statement, QueryKind: keyword}, nil
	default:
		return sqlStatementPolicy{}, ErrSQLConsoleUnsupported
	}
}

func singleSQLStatement(value string) (string, error) {
	state := byte(0)
	for index := 0; index < len(value); index++ {
		current := value[index]
		if state != 0 {
			if current == '\\' && state != '`' {
				index++
				continue
			}
			if current == state {
				if index+1 < len(value) && value[index+1] == state {
					index++
					continue
				}
				state = 0
			}
			continue
		}
		switch current {
		case '\'', '"', '`':
			state = current
		case '-':
			if index+1 < len(value) && value[index+1] == '-' {
				index = skipSQLLine(value, index+2)
			}
		case '#':
			index = skipSQLLine(value, index+1)
		case '/':
			if index+1 < len(value) && value[index+1] == '*' {
				end := strings.Index(value[index+2:], "*/")
				if end < 0 {
					return "", ErrSQLConsoleInvalid
				}
				index += end + 3
			}
		case ';':
			if hasSQLContent(value[index+1:]) {
				return "", ErrSQLConsoleMultipleStatements
			}
			return strings.TrimSpace(value[:index]), nil
		}
	}
	if state != 0 {
		return "", ErrSQLConsoleInvalid
	}
	return strings.TrimSpace(value), nil
}

func skipSQLLine(value string, start int) int {
	if next := strings.IndexByte(value[start:], '\n'); next >= 0 {
		return start + next
	}
	return len(value)
}

func hasSQLContent(value string) bool {
	for {
		value = strings.TrimSpace(value)
		switch {
		case value == "":
			return false
		case strings.HasPrefix(value, "--"):
			value = value[minInt(len(value), skipSQLLine(value, 2)+1):]
		case strings.HasPrefix(value, "#"):
			value = value[minInt(len(value), skipSQLLine(value, 1)+1):]
		case strings.HasPrefix(value, "/*"):
			end := strings.Index(value[2:], "*/")
			if end < 0 {
				return true
			}
			value = value[end+4:]
		default:
			return true
		}
	}
}

func firstSQLKeyword(value string) string {
	value = stripLeadingSQLComments(value)
	end := 0
	for end < len(value) && unicode.IsLetter(rune(value[end])) {
		end++
	}
	return strings.ToLower(value[:end])
}

func stripLeadingSQLComments(value string) string {
	for {
		value = strings.TrimSpace(value)
		switch {
		case strings.HasPrefix(value, "--"):
			start := skipSQLLine(value, 2)
			if start >= len(value) {
				return ""
			}
			value = value[start+1:]
		case strings.HasPrefix(value, "#"):
			start := skipSQLLine(value, 1)
			if start >= len(value) {
				return ""
			}
			value = value[start+1:]
		case strings.HasPrefix(value, "/*"):
			end := strings.Index(value[2:], "*/")
			if end < 0 {
				return ""
			}
			value = value[end+4:]
		default:
			return value
		}
	}
}

func containsUnsafeSelectClause(value string) bool {
	normalized := strings.ToUpper(strings.Join(strings.Fields(value), " "))
	return strings.Contains(normalized, " INTO ") ||
		strings.Contains(normalized, " FOR UPDATE") ||
		strings.Contains(normalized, " LOCK IN SHARE MODE")
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
