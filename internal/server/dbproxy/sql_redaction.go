package dbproxy

import (
	"regexp"
	"strings"
)

const sqlAuditRedacted = "[REDACTED]"

var sensitiveSQLAssignment = regexp.MustCompile(
	`(?i)\b(password|passwd|pwd|secret|token|authorization|api_key|access_token|refresh_token|client_secret)\b(\s*(?:=|:|\bBY\b)\s*)([A-Za-z_][A-Za-z0-9_./+\-]*)`,
)

var sensitiveSQLAuthorization = regexp.MustCompile(
	`(?i)\b(authorization)\b(\s*(?:=|:)\s*)(?:basic|bearer)\s+[A-Za-z0-9._~+/\-=]+`,
)

// redactDatabaseSQL is deliberately stricter than a SQL formatter. The relay
// still forwards the original bytes; only this audit copy is transformed.
func redactDatabaseSQL(sql string) string {
	var out strings.Builder
	out.Grow(len(sql))
	for index := 0; index < len(sql); {
		switch {
		case sql[index] == '-' && index+1 < len(sql) && sql[index+1] == '-':
			end := strings.IndexByte(sql[index:], '\n')
			if end < 0 {
				out.WriteString("-- ")
				out.WriteString(sqlAuditRedacted)
				index = len(sql)
				continue
			}
			out.WriteString("-- ")
			out.WriteString(sqlAuditRedacted)
			out.WriteByte('\n')
			index += end + 1
		case sql[index] == '#':
			end := strings.IndexByte(sql[index:], '\n')
			if end < 0 {
				out.WriteString("# ")
				out.WriteString(sqlAuditRedacted)
				index = len(sql)
				continue
			}
			out.WriteString("# ")
			out.WriteString(sqlAuditRedacted)
			out.WriteByte('\n')
			index += end + 1
		case sql[index] == '/' && index+1 < len(sql) && sql[index+1] == '*':
			end := strings.Index(sql[index+2:], "*/")
			out.WriteString("/* ")
			out.WriteString(sqlAuditRedacted)
			out.WriteString(" */")
			if end < 0 {
				index = len(sql)
			} else {
				index += end + 4
			}
		case sql[index] == '\'' || sql[index] == '"':
			index = redactSQLQuotedLiteral(&out, sql, index, sql[index])
		case sql[index] == '$':
			if next, ok := redactSQLDollarLiteral(&out, sql, index); ok {
				index = next
			} else {
				out.WriteByte(sql[index])
				index++
			}
		case startsSQLNumericLiteral(sql, index):
			out.WriteString(sqlAuditRedacted)
			index = consumeSQLNumericLiteral(sql, index)
		default:
			out.WriteByte(sql[index])
			index++
		}
	}
	redacted := sensitiveSQLAuthorization.ReplaceAllString(out.String(), `${1}${2}`+sqlAuditRedacted)
	return sensitiveSQLAssignment.ReplaceAllString(redacted, `${1}${2}`+sqlAuditRedacted)
}

func redactSQLQuotedLiteral(out *strings.Builder, sql string, start int, quote byte) int {
	out.WriteByte(quote)
	out.WriteString(sqlAuditRedacted)
	for index := start + 1; index < len(sql); index++ {
		if sql[index] == '\\' && index+1 < len(sql) {
			index++
			continue
		}
		if sql[index] != quote {
			continue
		}
		if index+1 < len(sql) && sql[index+1] == quote {
			index++
			continue
		}
		out.WriteByte(quote)
		return index + 1
	}
	return len(sql)
}

func redactSQLDollarLiteral(out *strings.Builder, sql string, start int) (int, bool) {
	delimiterEnd := start + 1
	for delimiterEnd < len(sql) && sql[delimiterEnd] != '$' {
		value := sql[delimiterEnd]
		if (delimiterEnd == start+1 && !isSQLIdentifierStart(value)) ||
			(delimiterEnd > start+1 && !isSQLIdentifierPart(value)) {
			return start, false
		}
		delimiterEnd++
	}
	if delimiterEnd >= len(sql) {
		return start, false
	}
	delimiter := sql[start : delimiterEnd+1]
	contentStart := delimiterEnd + 1
	closingOffset := strings.Index(sql[contentStart:], delimiter)
	out.WriteString(delimiter)
	out.WriteString(sqlAuditRedacted)
	if closingOffset < 0 {
		return len(sql), true
	}
	out.WriteString(delimiter)
	return contentStart + closingOffset + len(delimiter), true
}

func startsSQLNumericLiteral(sql string, index int) bool {
	if index >= len(sql) {
		return false
	}
	isDigit := sql[index] >= '0' && sql[index] <= '9'
	isLeadingDecimal := sql[index] == '.' && index+1 < len(sql) && sql[index+1] >= '0' && sql[index+1] <= '9'
	if !isDigit && !isLeadingDecimal {
		return false
	}
	if index == 0 {
		return true
	}
	previous := sql[index-1]
	return !isSQLIdentifierPart(previous) && previous != '$'
}

func consumeSQLNumericLiteral(sql string, index int) int {
	if index+2 <= len(sql) && sql[index] == '0' && index+1 < len(sql) &&
		(sql[index+1] == 'x' || sql[index+1] == 'X' || sql[index+1] == 'b' || sql[index+1] == 'B') {
		binary := sql[index+1] == 'b' || sql[index+1] == 'B'
		index += 2
		for index < len(sql) && ((!binary && isSQLHexDigit(sql[index])) || (binary && (sql[index] == '0' || sql[index] == '1'))) {
			index++
		}
		return index
	}
	seenExponent := false
	for index < len(sql) {
		value := sql[index]
		switch {
		case value >= '0' && value <= '9', value == '.':
			index++
		case (value == 'e' || value == 'E') && !seenExponent:
			seenExponent = true
			index++
			if index < len(sql) && (sql[index] == '+' || sql[index] == '-') {
				index++
			}
		default:
			return index
		}
	}
	return index
}

func isSQLIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}

func isSQLIdentifierPart(value byte) bool {
	return isSQLIdentifierStart(value) || value >= '0' && value <= '9'
}

func isSQLHexDigit(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}
