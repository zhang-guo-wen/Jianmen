package service

import (
	"strings"
	"time"
)

const (
	defaultAuditRetentionDays = 30
	maxAuditRetentionDays     = 3650
	redactedValue             = "[REDACTED]"
)

// AuditPolicy contains the pure, explicit rules applied before audit data is stored.
// It is intentionally independent from configuration and persistence packages.
type AuditPolicy struct {
	retentionDays       int
	recordOriginalInput bool
}

// NewAuditPolicy creates an audit policy. Invalid retention periods fall back to the
// conservative default, and raw input recording is disabled unless explicitly enabled.
func NewAuditPolicy(retentionDays int, recordOriginalInput bool) AuditPolicy {
	if retentionDays <= 0 {
		retentionDays = defaultAuditRetentionDays
	} else if retentionDays > maxAuditRetentionDays {
		retentionDays = maxAuditRetentionDays
	}
	return AuditPolicy{retentionDays: retentionDays, recordOriginalInput: recordOriginalInput}
}

// RetentionCutoff returns the oldest timestamp that may be retained at now.
func (p AuditPolicy) RetentionCutoff(now time.Time) time.Time {
	return now.AddDate(0, 0, -p.retentionDays)
}

// Redact removes secrets from an audit value without needing protocol-specific state.
func (p AuditPolicy) Redact(kind, value string) string {
	if !p.recordOriginalInput && isInputKind(kind) {
		return redactedValue
	}
	if isSQLKind(kind) {
		value = redactSQLStrings(value)
	}
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "json":
		value = redactJSONSensitiveValues(value)
	case "url":
		value = redactURLUserinfo(value)
	}
	value = redactPrivateKeys(value)
	value = redactAssignments(value)
	return redactBearerAndFlags(value)
}

// RedactInput is the typo-resistant API for writers handling terminal or user input.
func (p AuditPolicy) RedactInput(value string) string {
	if !p.recordOriginalInput {
		return redactedValue
	}
	return p.Redact("input", value)
}

func isInputKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "input", "terminal_input", "stdin", "keystroke":
		return true
	default:
		return false
	}
}

func isSQLKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "sql", "query", "database_query":
		return true
	default:
		return false
	}
}

func redactURLUserinfo(value string) string {
	schemeEnd := strings.Index(value, "://")
	if schemeEnd < 0 {
		return value
	}
	authorityStart := schemeEnd + 3
	authorityEnd := authorityStart
	for authorityEnd < len(value) && !strings.ContainsRune("/?#", rune(value[authorityEnd])) && !isSpace(value[authorityEnd]) {
		authorityEnd++
	}
	at := strings.LastIndexByte(value[authorityStart:authorityEnd], '@')
	if at < 0 {
		return value
	}
	at += authorityStart
	colon := strings.IndexByte(value[authorityStart:at], ':')
	if colon < 0 {
		return value
	}
	colon += authorityStart
	return value[:colon+1] + redactedValue + value[at:]
}

func redactPrivateKeys(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	copyFrom := 0
	for lineStart := 0; lineStart < len(value); {
		lineEnd, nextLine := lineBoundary(value, lineStart)
		if !isPrivateKeyMarker(value[lineStart:lineEnd], "-----BEGIN ") {
			lineStart = nextLine
			continue
		}
		blockEnd := len(value)
		foundEnd := false
		for search := nextLine; search < len(value); {
			endLine, followingLine := lineBoundary(value, search)
			if isPrivateKeyMarker(value[search:endLine], "-----END ") {
				blockEnd = endLine
				foundEnd = true
				break
			}
			search = followingLine
		}
		out.WriteString(value[copyFrom:lineStart])
		out.WriteString("[REDACTED PRIVATE KEY]")
		copyFrom = blockEnd
		if !foundEnd {
			return out.String()
		}
		lineStart = blockEnd
	}
	out.WriteString(value[copyFrom:])
	return out.String()
}

func lineBoundary(value string, start int) (end, next int) {
	offset := strings.IndexByte(value[start:], '\n')
	if offset < 0 {
		end = len(value)
		next = len(value)
	} else {
		end = start + offset
		next = end + 1
	}
	if end > start && value[end-1] == '\r' {
		end--
	}
	return end, next
}

func isPrivateKeyMarker(line, prefix string) bool {
	const suffix = "PRIVATE KEY-----"
	return len(line) >= len(prefix)+len(suffix) &&
		strings.EqualFold(line[:len(prefix)], prefix) &&
		strings.EqualFold(line[len(line)-len(suffix):], suffix)
}

func redactAssignments(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); {
		keyStart := i
		if value[i] == '"' {
			keyStart++
		}
		keyEnd := keyStart
		for keyEnd < len(value) && isKeyChar(value[keyEnd]) {
			keyEnd++
		}
		if keyEnd == keyStart {
			out.WriteByte(value[i])
			i++
			continue
		}
		if !isSensitiveKey(value[keyStart:keyEnd]) || !validKeyBoundary(value, keyStart, keyEnd) {
			out.WriteString(value[i:keyEnd])
			i = keyEnd
			continue
		}
		p := keyEnd
		if keyStart > i && p < len(value) && value[p] == '"' {
			p++
		}
		for p < len(value) && isSpace(value[p]) {
			p++
		}
		if p >= len(value) || (value[p] != '=' && value[p] != ':') {
			out.WriteByte(value[i])
			i++
			continue
		}
		p++
		for p < len(value) && isSpace(value[p]) {
			p++
		}
		if credentialStart, ok := authorizationCredentialStart(value, p); ok {
			out.WriteString(value[i:credentialStart])
			i = writeRedactedSecret(&out, value, credentialStart)
			continue
		}
		end := secretEnd(value, p)
		if strings.EqualFold(value[keyStart:keyEnd], "authorization") && p < len(value) && value[p] != '\'' && value[p] != '"' {
			end = authorizationEnd(value, p)
		}
		if p < len(value) && (value[p] == '\'' || value[p] == '"') && end < len(value) && value[end] == value[p] {
			out.WriteString(value[i : p+1])
			out.WriteString(redactedValue)
			out.WriteByte(value[end])
			i = end + 1
			continue
		}
		out.WriteString(value[i:p])
		out.WriteString(redactedValue)
		i = end
	}
	return out.String()
}

func authorizationCredentialStart(value string, start int) (int, bool) {
	for _, scheme := range []string{"basic", "bearer"} {
		if !hasWordAt(value, start, scheme) {
			continue
		}
		credentialStart := start + len(scheme)
		if credentialStart >= len(value) || !isSpace(value[credentialStart]) {
			continue
		}
		for credentialStart < len(value) && isSpace(value[credentialStart]) {
			credentialStart++
		}
		return credentialStart, credentialStart < len(value)
	}
	return 0, false
}

func redactBearerAndFlags(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); {
		if hasWordAt(value, i, "bearer") {
			p := i + len("bearer")
			if p < len(value) && isSpace(value[p]) {
				for p < len(value) && isSpace(value[p]) {
					p++
				}
				out.WriteString(value[i:p])
				i = writeRedactedSecret(&out, value, p)
				continue
			}
		}
		if strings.HasPrefix(value[i:], "--") {
			start := i + 2
			end := start
			for end < len(value) && isKeyChar(value[end]) {
				end++
			}
			if end > start && isSensitiveKey(value[start:end]) {
				p := end
				for p < len(value) && isSpace(value[p]) {
					p++
				}
				if p < len(value) && value[p] == '=' {
					p++
					for p < len(value) && isSpace(value[p]) {
						p++
					}
				}
				if p > end && p < len(value) {
					out.WriteString(value[i:p])
					i = writeRedactedSecret(&out, value, p)
					continue
				}
			}
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String()
}

func writeRedactedSecret(out *strings.Builder, value string, start int) int {
	if start < len(value) && (value[start] == '\'' || value[start] == '"') {
		end := secretEnd(value, start)
		out.WriteByte(value[start])
		out.WriteString(redactedValue)
		if end < len(value) && value[end] == value[start] {
			out.WriteByte(value[end])
			return end + 1
		}
		return len(value)
	}
	out.WriteString(redactedValue)
	return secretEnd(value, start)
}

func redactSQLStrings(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	for i := 0; i < len(value); {
		if value[i] == '\'' {
			out.WriteByte('\'')
			out.WriteString(redactedValue)
			end := sqlSingleQuoteEnd(value, i)
			if end < len(value) {
				out.WriteByte('\'')
				i = end + 1
			} else {
				i = len(value)
			}
			continue
		}
		if value[i] == '$' {
			delimiterEnd := sqlDollarDelimiterEnd(value, i)
			if delimiterEnd > i {
				delimiter := value[i : delimiterEnd+1]
				contentStart := delimiterEnd + 1
				closingOffset := strings.Index(value[contentStart:], delimiter)
				if closingOffset >= 0 {
					out.WriteString(delimiter)
					out.WriteString(redactedValue)
					out.WriteString(delimiter)
					i = contentStart + closingOffset + len(delimiter)
					continue
				}
				out.WriteString(delimiter)
				out.WriteString(redactedValue)
				return out.String()
			}
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String()
}

func sqlSingleQuoteEnd(value string, start int) int {
	for i := start + 1; i < len(value); i++ {
		if value[i] == '\\' {
			if i+1 < len(value) {
				i++
			}
			continue
		}
		if value[i] != '\'' {
			continue
		}
		if i+1 < len(value) && value[i+1] == '\'' {
			i++
			continue
		}
		return i
	}
	return len(value)
}

func sqlDollarDelimiterEnd(value string, start int) int {
	for i := start + 1; i < len(value); i++ {
		if value[i] == '$' {
			return i
		}
		if i == start+1 {
			if !isASCIIAlpha(value[i]) && value[i] != '_' {
				return -1
			}
			continue
		}
		if !isASCIIAlpha(value[i]) && (value[i] < '0' || value[i] > '9') && value[i] != '_' {
			return -1
		}
	}
	return -1
}

func isASCIIAlpha(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isSensitiveKey(key string) bool {
	key = strings.ReplaceAll(strings.ToLower(key), "-", "_")
	switch key {
	case "password", "passwd", "pwd", "secret", "token", "api_key", "access_token", "refresh_token",
		"client_secret", "passphrase", "private_key", "pgpassword", "mysql_pwd", "authorization":
		return true
	case "secret_key", "secret_access_key", "aws_secret_access_key", "aws_session_token",
		"azure_client_secret", "azure_storage_account_key", "gcp_private_key", "google_private_key",
		"service_account_key", "access_key_secret", "cloud_secret_key":
		return true
	default:
		return false
	}
}

func hasWordAt(value string, start int, word string) bool {
	end := start + len(word)
	return end <= len(value) && strings.EqualFold(value[start:end], word) && validKeyBoundary(value, start, end)
}

func validKeyBoundary(value string, start, end int) bool {
	return (start == 0 || !isKeyChar(value[start-1])) && (end == len(value) || !isKeyChar(value[end]))
}

func secretEnd(value string, start int) int {
	if start >= len(value) {
		return start
	}
	if quote := value[start]; quote == '\'' || quote == '"' {
		for i := start + 1; i < len(value); i++ {
			if value[i] == '\\' {
				i++
				continue
			}
			if value[i] == quote {
				return i
			}
		}
		return len(value)
	}
	for i := start; i < len(value); i++ {
		if value[i] == '*' && i+1 < len(value) && value[i+1] == '/' {
			return i
		}
		if isSpace(value[i]) || strings.ContainsRune("&,;#}", rune(value[i])) {
			return i
		}
	}
	return len(value)
}

func authorizationEnd(value string, start int) int {
	for i := start; i < len(value); i++ {
		if value[i] == '*' && i+1 < len(value) && value[i+1] == '/' {
			return i
		}
		if value[i] == '\r' || value[i] == '\n' || value[i] == '&' || value[i] == '#' {
			return i
		}
	}
	return len(value)
}

func isKeyChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}
func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
