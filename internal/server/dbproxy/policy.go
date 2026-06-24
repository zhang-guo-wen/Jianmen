package dbproxy

import (
	"fmt"
	"strings"
	"unicode"

	"jianmen/internal/config"
)

type queryDecision struct {
	Allowed      bool
	Status       string
	ErrorCode    string
	ErrorMessage string
	Detail       map[string]any
}

type sqlPolicy struct {
	readOnly          bool
	deniedQueryKinds  map[string]struct{}
	deniedSQLPatterns []string
	maxQueryBytes     int
}

func newSQLPolicy(cfg config.DatabaseQueryPolicyConfig) *sqlPolicy {
	policy := &sqlPolicy{
		readOnly:          cfg.ReadOnly,
		deniedQueryKinds:  make(map[string]struct{}, len(cfg.DeniedQueryKinds)),
		deniedSQLPatterns: make([]string, 0, len(cfg.DeniedSQLPatterns)),
		maxQueryBytes:     cfg.MaxQueryBytes,
	}
	for _, kind := range cfg.DeniedQueryKinds {
		kind = strings.ToLower(strings.TrimSpace(kind))
		if kind != "" {
			policy.deniedQueryKinds[kind] = struct{}{}
		}
	}
	for _, pattern := range cfg.DeniedSQLPatterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern != "" {
			policy.deniedSQLPatterns = append(policy.deniedSQLPatterns, pattern)
		}
	}
	return policy
}

func (p *sqlPolicy) Evaluate(sql string) queryDecision {
	sql = strings.TrimSpace(sql)
	if sql == "" {
		return allowQuery()
	}
	kind := classifyQueryKind(sql)
	if p == nil {
		return allowQuery()
	}
	if p.maxQueryBytes > 0 && len([]byte(sql)) > p.maxQueryBytes {
		return denyQuery("query_too_large", fmt.Sprintf("query exceeds max size of %d bytes", p.maxQueryBytes), map[string]any{
			"max_query_bytes": p.maxQueryBytes,
		})
	}
	if _, denied := p.deniedQueryKinds[kind]; denied {
		return denyQuery("query_kind_denied", fmt.Sprintf("query kind %q is denied by database proxy policy", kind), map[string]any{
			"query_kind": kind,
		})
	}
	if p.readOnly && !isReadOnlyQueryKind(kind) {
		return denyQuery("read_only_policy", fmt.Sprintf("query kind %q is denied by read-only database proxy policy", kind), map[string]any{
			"query_kind": kind,
		})
	}
	lowerSQL := strings.ToLower(sql)
	for _, pattern := range p.deniedSQLPatterns {
		if strings.Contains(lowerSQL, pattern) {
			return denyQuery("sql_pattern_denied", "query matches a denied SQL pattern", map[string]any{
				"pattern": pattern,
			})
		}
	}
	return allowQuery()
}

func allowQuery() queryDecision {
	return queryDecision{Allowed: true}
}

func denyQuery(code, message string, detail map[string]any) queryDecision {
	return queryDecision{
		Allowed:      false,
		Status:       queryStatusPolicyDenied,
		ErrorCode:    code,
		ErrorMessage: message,
		Detail:       detail,
	}
}

func classifyQueryKind(sql string) string {
	sql = stripLeadingSQLTrivia(sql)
	if sql == "" {
		return "unknown"
	}
	for i, r := range sql {
		if !(unicode.IsLetter(r) || r == '_') {
			if i == 0 {
				return "unknown"
			}
			return strings.ToLower(sql[:i])
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

func isReadOnlyQueryKind(kind string) bool {
	switch kind {
	case "select", "show", "describe", "desc", "explain", "use", "set", "begin", "start", "commit", "rollback":
		return true
	default:
		return false
	}
}

func mergeDetails(values ...map[string]any) map[string]any {
	var out map[string]any
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]any)
		}
		for k, v := range value {
			out[k] = v
		}
	}
	return out
}
