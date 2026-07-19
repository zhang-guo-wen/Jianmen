package dbproxy

import (
	"strings"
	"unicode"

	"jianmen/internal/config"
)

const (
	defaultMaxClientMessageBytes = config.DefaultDatabaseGatewayMaxClientMessageBytes

	// These fixed thresholds protect server-response, startup, and streaming
	// protocol paths. Only post-authentication client SQL/command frames use
	// the configurable maxClientMessageBytes value.
	maxMySQLObserverBufferBytes    = 256 * 1024
	maxPostgresObserverBufferBytes = 256 * 1024
	maxRedisObserverBufferBytes    = 64 * 1024
	maxObserverPendingQueries      = 32
	maxObserverPreparedObjects     = 128

	observerErrorBufferLimit  = "OBSERVER_BUFFER_LIMIT"
	observerErrorProtocol     = "OBSERVER_PROTOCOL_ERROR"
	observerErrorPendingLimit = "OBSERVER_PENDING_LIMIT"
	observerErrorRelay        = "OBSERVER_RELAY_TERMINATED"
	observerErrorDrainTimeout = "OBSERVER_DRAIN_TIMEOUT"
	observerErrorAuditFailure = "OBSERVER_AUDIT_FAILURE"
)

func observerPendingAuditWithinLimit(
	pending []queryRecord,
	next databaseSQLAudit,
	limit int,
) bool {
	limit = normalizeMaxClientMessageBytes(limit)
	total := len(next.text)
	if total > limit {
		return false
	}
	for _, record := range pending {
		if len(record.sql) > limit-total {
			return false
		}
		total += len(record.sql)
	}
	return true
}

func normalizeMaxClientMessageBytes(limit int) int {
	if limit <= 0 {
		return defaultMaxClientMessageBytes
	}
	return limit
}

func appendObserverBufferChunk(buffer *[]byte, data *[]byte, limit int) bool {
	if len(*data) == 0 {
		return true
	}
	remaining := limit - len(*buffer)
	if remaining <= 0 {
		return false
	}
	appendLength := len(*data)
	if appendLength > remaining {
		appendLength = remaining
	}
	nextLen := len(*buffer) + appendLength
	if cap(*buffer) >= nextLen && cap(*buffer) <= limit {
		*buffer = append(*buffer, (*data)[:appendLength]...)
		*data = (*data)[appendLength:]
		return true
	}

	next := make([]byte, nextLen)
	copy(next, *buffer)
	copy(next[len(*buffer):], (*data)[:appendLength])
	*buffer = next
	*data = (*data)[appendLength:]
	return true
}

func newObserverRelayDecision() *queryDecision {
	return &queryDecision{
		Allowed:      false,
		Status:       queryStatusError,
		ErrorCode:    observerErrorRelay,
		ErrorMessage: "database proxy relay terminated",
	}
}

type queryDecision struct {
	Allowed      bool
	Status       string
	ErrorCode    string
	ErrorMessage string
	Detail       map[string]any
}

func allowQuery() queryDecision {
	return queryDecision{Allowed: true}
}

func newObserverFatalDecision(code, message string) *queryDecision {
	return &queryDecision{
		Allowed:      false,
		Status:       queryStatusPolicyDenied,
		ErrorCode:    code,
		ErrorMessage: message,
	}
}

func classifyQueryKind(sql string) string {
	sql = stripLeadingSQLTrivia(sql)
	if sql == "" {
		return "unknown"
	}
	for index, character := range sql {
		if !(unicode.IsLetter(character) || character == '_') {
			if index == 0 {
				return "unknown"
			}
			return strings.ToLower(sql[:index])
		}
	}
	return strings.ToLower(sql)
}

func stripLeadingSQLTrivia(sql string) string {
	for {
		sql = strings.TrimSpace(sql)
		switch {
		case strings.HasPrefix(sql, "--"):
			if index := strings.IndexByte(sql, '\n'); index >= 0 {
				sql = sql[index+1:]
				continue
			}
			return ""
		case strings.HasPrefix(sql, "#"):
			if index := strings.IndexByte(sql, '\n'); index >= 0 {
				sql = sql[index+1:]
				continue
			}
			return ""
		case strings.HasPrefix(sql, "/*"):
			if index := strings.Index(sql, "*/"); index >= 0 {
				sql = sql[index+2:]
				continue
			}
			return ""
		default:
			return sql
		}
	}
}

func mergeDetails(values ...map[string]any) map[string]any {
	var result map[string]any
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		if result == nil {
			result = make(map[string]any)
		}
		for key, item := range value {
			result[key] = item
		}
	}
	return result
}
