package model

import (
	"fmt"
	"strings"
	"time"
)

// AuditTime 统一审计时间类型，JSON 序列化为 "2006-01-02 15:04:05" 格式。
type AuditTime time.Time

const auditTimeLayout = "2006-01-02 15:04:05"

func (t AuditTime) MarshalJSON() ([]byte, error) {
	s := time.Time(t).Format(auditTimeLayout)
	return []byte(`"` + s + `"`), nil
}

func (t *AuditTime) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	parsed, err := time.ParseInLocation(auditTimeLayout, s, time.Local)
	if err != nil {
		return fmt.Errorf("parse audit time %q: %w", s, err)
	}
	*t = AuditTime(parsed)
	return nil
}

// IsZero 判断是否为零值。
func (t AuditTime) IsZero() bool {
	return time.Time(t).IsZero()
}

// String 返回格式化字符串。
func (t AuditTime) String() string {
	return time.Time(t).Format(auditTimeLayout)
}

// ToAuditTime 将 time.Time 转换为 AuditTime。
func ToAuditTime(t time.Time) AuditTime {
	return AuditTime(t)
}

// ToAuditTimePtr 将 *time.Time 转换为 *AuditTime（空时为 nil，omitempty 时不输出）。
func ToAuditTimePtr(t *time.Time) *AuditTime {
	if t == nil {
		return nil
	}
	v := AuditTime(*t)
	return &v
}
