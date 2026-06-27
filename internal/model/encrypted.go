package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"jianmen/internal/crypto"
)

// EncryptedField 自动加解密的字符串字段。
// 写入数据库时通过 driver.Valuer 自动 AES-256-GCM 加密，
// 从数据库读取时通过 sql.Scanner 自动解密。
// JSON 序列化不输出明文（MarshalJSON 返回空字符串）。
type EncryptedField struct {
	plaintext string
}

// NewEncryptedField 创建包含明文的 EncryptedField。
func NewEncryptedField(plaintext string) EncryptedField {
	return EncryptedField{plaintext: plaintext}
}

// GetPlaintext 返回明文字符串。
func (e EncryptedField) GetPlaintext() string {
	return e.plaintext
}

// SetPlaintext 设置明文字符串。
func (e *EncryptedField) SetPlaintext(s string) {
	e.plaintext = s
}

// Value 实现 driver.Valuer。写入数据库时自动加密。
// 空字段返回 nil（数据库 NULL 值）。
func (e EncryptedField) Value() (driver.Value, error) {
	if e.plaintext == "" {
		return nil, nil
	}
	return crypto.Encrypt([]byte(e.plaintext))
}

// Scan 实现 sql.Scanner。从数据库读取时自动解密。
func (e *EncryptedField) Scan(src interface{}) error {
	if src == nil {
		e.plaintext = ""
		return nil
	}

	var s string
	switch v := src.(type) {
	case string:
		s = v
	case []byte:
		s = string(v)
	default:
		return fmt.Errorf("EncryptedField.Scan: unsupported type %T", src)
	}

	if s == "" {
		e.plaintext = ""
		return nil
	}

	plaintext, err := crypto.Decrypt(s)
	if err != nil {
		return fmt.Errorf("EncryptedField.Scan: decrypt: %w", err)
	}
	e.plaintext = string(plaintext)
	return nil
}

// MarshalJSON 实现 json.Marshaler。不输出明文。
func (e EncryptedField) MarshalJSON() ([]byte, error) {
	return json.Marshal("")
}

// UnmarshalJSON 实现 json.Unmarshaler。接收明文字符串。
func (e *EncryptedField) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	e.plaintext = s
	return nil
}
