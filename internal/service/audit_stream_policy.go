package service

import "strings"

const maxSensitiveStreamTriggerBytes = 128

var sensitiveStreamKeys = []string{
	"password",
	"passwd",
	"pwd",
	"secret",
	"token",
	"api_key",
	"access_token",
	"refresh_token",
	"client_secret",
	"passphrase",
	"private_key",
	"pgpassword",
	"mysql_pwd",
	"authorization",
	"secret_key",
	"secret_access_key",
	"aws_secret_access_key",
	"aws_session_token",
	"azure_client_secret",
	"azure_storage_account_key",
	"gcp_private_key",
	"google_private_key",
	"service_account_key",
	"access_key_secret",
	"cloud_secret_key",
}

// SafeStreamPrefix returns the number of leading bytes that can be persisted
// before the next network frame arrives. A suffix that could still become a
// sensitive assignment, bearer credential, flag, or PEM header remains
// buffered. Fully detected secrets remain buffered until a logical line
// boundary so a value split across frames cannot leak its tail.
func (p AuditPolicy) SafeStreamPrefix(kind, value string) int {
	if value == "" {
		return 0
	}
	if p.Redact(kind, value) != value {
		return 0
	}
	if start := pendingSensitiveSuffixStart(value); start >= 0 {
		return start
	}
	return len(value)
}

func pendingSensitiveSuffixStart(value string) int {
	lower := strings.ToLower(value)
	lineStart := strings.LastIndexByte(lower, '\n') + 1
	line := lower[lineStart:]
	const pemBegin = "-----begin "
	if strings.HasPrefix(pemBegin, line) || strings.HasPrefix(line, pemBegin) {
		return lineStart
	}

	start := len(lower) - maxSensitiveStreamTriggerBytes
	if start < 0 {
		start = 0
	}
	for i := start; i < len(lower); i++ {
		if i > 0 && isKeyChar(lower[i-1]) {
			continue
		}
		suffix := lower[i:]
		if suffixCouldBecomeSensitiveTrigger(suffix) {
			if i > 0 && value[i-1] == '"' {
				return i - 1
			}
			return i
		}
	}
	return -1
}

func suffixCouldBecomeSensitiveTrigger(suffix string) bool {
	if suffix == "" {
		return false
	}
	if strings.HasPrefix("bearer", suffix) {
		return true
	}
	if strings.HasPrefix(suffix, "bearer") {
		return onlyHorizontalSpace(suffix[len("bearer"):])
	}
	if strings.HasPrefix("--", suffix) {
		return true
	}
	if strings.HasPrefix(suffix, "--") {
		return keySuffixCouldBecomeTrigger(suffix[2:], true)
	}
	if suffix[0] == '"' {
		return quotedKeySuffixCouldBecomeTrigger(suffix[1:])
	}
	return keySuffixCouldBecomeTrigger(suffix, false)
}

func quotedKeySuffixCouldBecomeTrigger(suffix string) bool {
	for _, key := range sensitiveStreamKeys {
		for _, spelling := range sensitiveKeySpellings(key) {
			if strings.HasPrefix(spelling, suffix) {
				return true
			}
			if !strings.HasPrefix(suffix, spelling) {
				continue
			}
			remainder := suffix[len(spelling):]
			if remainder == "" {
				return true
			}
			if remainder[0] != '"' {
				continue
			}
			remainder = strings.TrimLeft(remainder[1:], " \t")
			return remainder == ""
		}
	}
	return false
}

func keySuffixCouldBecomeTrigger(suffix string, flag bool) bool {
	for _, key := range sensitiveStreamKeys {
		for _, spelling := range sensitiveKeySpellings(key) {
			if strings.HasPrefix(spelling, suffix) {
				return true
			}
			if !strings.HasPrefix(suffix, spelling) {
				continue
			}
			remainder := suffix[len(spelling):]
			if remainder == "" || onlyHorizontalSpace(remainder) {
				return true
			}
			if flag && (remainder[0] == '=' || isSpace(remainder[0])) {
				return true
			}
		}
	}
	return false
}

func sensitiveKeySpellings(key string) []string {
	if !strings.Contains(key, "_") {
		return []string{key}
	}
	return []string{key, strings.ReplaceAll(key, "_", "-")}
}

func onlyHorizontalSpace(value string) bool {
	return strings.Trim(value, " \t") == ""
}
