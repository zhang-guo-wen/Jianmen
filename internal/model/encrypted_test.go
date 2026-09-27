package model

import (
	"encoding/json"
	"os"
	"testing"

	"jianmen/internal/crypto"
)

// TestMain initializes the crypto package for all tests in this package.
func TestMain(m *testing.M) {
	tmpDir, err := os.MkdirTemp("", "encrypted-field-test-*")
	if err != nil {
		panic("failed to create temp dir for crypto init: " + err.Error())
	}
	defer os.RemoveAll(tmpDir)

	if _, err := crypto.Init(tmpDir); err != nil {
		panic("failed to init crypto: " + err.Error())
	}

	os.Exit(m.Run())
}

func TestEncryptedField_Value_Scan_RoundTrip(t *testing.T) {
	original := NewEncryptedField("my-secret-password")
	val, err := original.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if val == nil {
		t.Fatal("expected non-nil value for non-empty field")
	}

	var restored EncryptedField
	if err := restored.Scan(val); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if restored.GetPlaintext() != "my-secret-password" {
		t.Fatalf("round-trip: got %q", restored.GetPlaintext())
	}
}

func TestEncryptedField_EmptyValue_ReturnsNil(t *testing.T) {
	e := EncryptedField{}
	val, err := e.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if val != nil {
		t.Fatal("expected nil for empty field")
	}
}

func TestEncryptedField_Scan_Nil_SetsEmpty(t *testing.T) {
	var e EncryptedField
	if err := e.Scan(nil); err != nil {
		t.Fatalf("Scan nil: %v", err)
	}
	if e.GetPlaintext() != "" {
		t.Fatalf("expected empty after scanning nil, got %q", e.GetPlaintext())
	}
}

func TestEncryptedField_Scan_EmptyBytes_SetsEmpty(t *testing.T) {
	var e EncryptedField
	if err := e.Scan([]byte{}); err != nil {
		t.Fatalf("Scan empty bytes: %v", err)
	}
	if e.GetPlaintext() != "" {
		t.Fatal("expected empty after scanning empty bytes")
	}
}

func TestEncryptedField_Scan_EmptyString_SetsEmpty(t *testing.T) {
	var e EncryptedField
	if err := e.Scan(""); err != nil {
		t.Fatalf("Scan empty string: %v", err)
	}
	if e.GetPlaintext() != "" {
		t.Fatal("expected empty after scanning empty string")
	}
}

func TestEncryptedField_Scan_InvalidJSON(t *testing.T) {
	var e EncryptedField
	err := e.Scan(12345) // int — 不支持的类型
	if err == nil {
		t.Fatal("expected error for unsupported scan type")
	}
}

func TestEncryptedField_MarshalJSON_Empty(t *testing.T) {
	e := NewEncryptedField("secret")
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	// 序列化为空字符串，不泄露敏感信息
	if string(data) != `""` {
		t.Fatalf("expected empty JSON string, got %q", data)
	}
}

func TestEncryptedField_UnmarshalJSON(t *testing.T) {
	var e EncryptedField
	if err := json.Unmarshal([]byte(`"my-password"`), &e); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if e.GetPlaintext() != "my-password" {
		t.Fatalf("unmarshal: got %q", e.GetPlaintext())
	}
}

func TestEncryptedField_UnmarshalJSON_Empty(t *testing.T) {
	var e EncryptedField
	if err := json.Unmarshal([]byte(`""`), &e); err != nil {
		t.Fatalf("UnmarshalJSON empty: %v", err)
	}
	if e.GetPlaintext() != "" {
		t.Fatalf("expected empty after unmarshal, got %q", e.GetPlaintext())
	}
}

func TestEncryptedField_SetGetPlaintext(t *testing.T) {
	var e EncryptedField
	e.SetPlaintext("new-value")
	if e.GetPlaintext() != "new-value" {
		t.Fatal("SetPlaintext/GetPlaintext mismatch")
	}
}

func TestEncryptedField_DifferentInstancesEncryptDifferently(t *testing.T) {
	e1 := NewEncryptedField("password")
	e2 := NewEncryptedField("password")

	v1, _ := e1.Value()
	v2, _ := e2.Value()

	if v1.(string) == v2.(string) {
		t.Fatal("same plaintext should produce different ciphertext (unique nonce)")
	}
}
