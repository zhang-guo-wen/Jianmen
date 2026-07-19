package dbproxy

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	sqlAuditRedacted          = "[REDACTED]"
	sqlAuditPreparedDetailKey = "_sql_audit_prepared"
)

type databaseSQLAudit struct {
	text          string
	originalBytes int64
	truncated     bool
}

var sensitiveSQLAssignment = regexp.MustCompile(
	`(?i)\b(password|passwd|pwd|secret|token|authorization|api_key|access_token|refresh_token|client_secret)\b(\s*(?:=|:|\bBY\b)\s*)([A-Za-z_][A-Za-z0-9_./+\-]*)`,
)

var sensitiveSQLAuthorization = regexp.MustCompile(
	`(?i)\b(authorization)\b(\s*(?:=|:)\s*)(?:basic|bearer)\s+[A-Za-z0-9._~+/\-=]+`,
)

// redactDatabaseSQL is deliberately stricter than a SQL formatter. The relay
// still forwards the original bytes; only this audit copy is transformed.
func redactDatabaseSQL(sql string) string {
	redacted, _ := redactDatabaseSQLWithLimit(sql, 0)
	return redacted
}

func prepareDatabaseSQLAudit(sql string, limit int) databaseSQLAudit {
	redacted, truncated := redactDatabaseSQLWithLimit(
		sql,
		normalizeMaxClientMessageBytes(limit),
	)
	return databaseSQLAudit{
		text:          redacted,
		originalBytes: int64(len(sql)),
		truncated:     truncated,
	}
}

func (a databaseSQLAudit) withDetail(detail map[string]any) map[string]any {
	return mergeDetails(detail, map[string]any{
		sqlAuditPreparedDetailKey: true,
		"sql_original_bytes":      a.originalBytes,
		"sql_truncated":           a.truncated,
		"sql_audit_bytes":         len(a.text),
	})
}

func normalizeDatabaseSQLAudit(
	sql string,
	detail map[string]any,
	limit int,
) (databaseSQLAudit, map[string]any) {
	audit, prepared := preparedDatabaseSQLAudit(sql, detail)
	if !prepared {
		audit = prepareDatabaseSQLAudit(sql, limit)
	}
	cleanDetail := make(map[string]any, len(detail)+4)
	for key, value := range detail {
		if key != sqlAuditPreparedDetailKey {
			cleanDetail[key] = value
		}
	}
	cleanDetail["sql_original_bytes"] = audit.originalBytes
	cleanDetail["sql_truncated"] = audit.truncated
	cleanDetail["sql_audit_bytes"] = len(audit.text)
	return audit, cleanDetail
}

func preparedDatabaseSQLAudit(sql string, detail map[string]any) (databaseSQLAudit, bool) {
	prepared, ok := detail[sqlAuditPreparedDetailKey].(bool)
	if !ok || !prepared {
		return databaseSQLAudit{}, false
	}
	originalBytes, ok := detail["sql_original_bytes"].(int64)
	if !ok || originalBytes < 0 {
		return databaseSQLAudit{}, false
	}
	truncated, ok := detail["sql_truncated"].(bool)
	if !ok {
		return databaseSQLAudit{}, false
	}
	auditBytes, ok := detail["sql_audit_bytes"].(int)
	if !ok || auditBytes != len(sql) {
		return databaseSQLAudit{}, false
	}
	return databaseSQLAudit{
		text:          sql,
		originalBytes: originalBytes,
		truncated:     truncated,
	}, true
}

func redactDatabaseSQLWithLimit(sql string, limit int) (string, bool) {
	out := newSQLRedactionBuilder(limit, len(sql))
	for index := 0; index < len(sql); {
		switch {
		case sql[index] == '-' && index+1 < len(sql) && sql[index+1] == '-':
			end := strings.IndexByte(sql[index:], '\n')
			if end < 0 {
				out.writeString("-- ")
				out.writeString(sqlAuditRedacted)
				index = len(sql)
				continue
			}
			out.writeString("-- ")
			out.writeString(sqlAuditRedacted)
			out.writeByte('\n')
			index += end + 1
		case sql[index] == '#':
			end := strings.IndexByte(sql[index:], '\n')
			if end < 0 {
				out.writeString("# ")
				out.writeString(sqlAuditRedacted)
				index = len(sql)
				continue
			}
			out.writeString("# ")
			out.writeString(sqlAuditRedacted)
			out.writeByte('\n')
			index += end + 1
		case sql[index] == '/' && index+1 < len(sql) && sql[index+1] == '*':
			end := strings.Index(sql[index+2:], "*/")
			out.writeString("/* ")
			out.writeString(sqlAuditRedacted)
			out.writeString(" */")
			if end < 0 {
				index = len(sql)
			} else {
				index += end + 4
			}
		case sql[index] == '\'' || sql[index] == '"':
			index = redactSQLQuotedLiteral(out, sql, index, sql[index])
		case sql[index] == '$':
			if next, ok := redactSQLDollarLiteral(out, sql, index); ok {
				index = next
			} else {
				out.writeByte(sql[index])
				index++
			}
		case startsSQLNumericLiteral(sql, index):
			out.writeString(sqlAuditRedacted)
			index = consumeSQLNumericLiteral(sql, index)
		default:
			out.writeByte(sql[index])
			index++
		}
	}
	redacted, authorizationTruncated := redactSensitiveSQLPattern(
		out.String(),
		sensitiveSQLAuthorization,
		limit,
	)
	redacted, assignmentTruncated := redactSensitiveSQLPattern(
		redacted,
		sensitiveSQLAssignment,
		limit,
	)
	redacted, utf8Adjusted := normalizeAuditSQLUTF8(redacted, limit)
	return redacted, out.truncated ||
		authorizationTruncated ||
		assignmentTruncated ||
		utf8Adjusted
}

type sqlRedactionBuilder struct {
	builder   strings.Builder
	limit     int
	truncated bool
}

func newSQLRedactionBuilder(limit, capacity int) *sqlRedactionBuilder {
	result := &sqlRedactionBuilder{limit: limit}
	if limit > 0 && capacity > limit {
		capacity = limit
	}
	if capacity > 0 {
		result.builder.Grow(capacity)
	}
	return result
}

func (b *sqlRedactionBuilder) writeString(value string) {
	if value == "" {
		return
	}
	if b.limit <= 0 {
		b.builder.WriteString(value)
		return
	}
	remaining := b.limit - b.builder.Len()
	if remaining <= 0 {
		b.truncated = true
		return
	}
	if len(value) > remaining {
		value = value[:remaining]
		b.truncated = true
	}
	b.builder.WriteString(value)
}

func (b *sqlRedactionBuilder) writeByte(value byte) {
	if b.limit > 0 && b.builder.Len() >= b.limit {
		b.truncated = true
		return
	}
	b.builder.WriteByte(value)
}

func (b *sqlRedactionBuilder) String() string {
	return b.builder.String()
}

func redactSensitiveSQLPattern(input string, pattern *regexp.Regexp, limit int) (string, bool) {
	out := newSQLRedactionBuilder(limit, len(input))
	cursor := 0
	for cursor < len(input) {
		match := pattern.FindStringSubmatchIndex(input[cursor:])
		if match == nil {
			break
		}
		out.writeString(input[cursor : cursor+match[0]])
		for group := 1; group <= 2; group++ {
			start := match[group*2]
			end := match[group*2+1]
			if start >= 0 && end >= start {
				out.writeString(input[cursor+start : cursor+end])
			}
		}
		out.writeString(sqlAuditRedacted)
		cursor += match[1]
		if limit > 0 && out.builder.Len() >= limit && cursor < len(input) {
			out.truncated = true
			return out.String(), true
		}
	}
	out.writeString(input[cursor:])
	return out.String(), out.truncated
}

func normalizeAuditSQLUTF8(value string, limit int) (string, bool) {
	if utf8.ValidString(value) && (limit <= 0 || len(value) <= limit) {
		return value, false
	}
	capacity := len(value)
	if limit > 0 && capacity > limit {
		capacity = limit
	}
	var out strings.Builder
	out.Grow(capacity)
	adjusted := false
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		next := value[:size]
		if r == utf8.RuneError && size == 1 {
			next = "\uFFFD"
			adjusted = true
		}
		if limit > 0 && len(next) > limit-out.Len() {
			adjusted = true
			break
		}
		out.WriteString(next)
		value = value[size:]
	}
	return out.String(), adjusted
}

func redactSQLQuotedLiteral(out *sqlRedactionBuilder, sql string, start int, quote byte) int {
	out.writeByte(quote)
	out.writeString(sqlAuditRedacted)
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
		out.writeByte(quote)
		return index + 1
	}
	return len(sql)
}

func redactSQLDollarLiteral(out *sqlRedactionBuilder, sql string, start int) (int, bool) {
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
	out.writeString(delimiter)
	out.writeString(sqlAuditRedacted)
	if closingOffset < 0 {
		return len(sql), true
	}
	out.writeString(delimiter)
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
