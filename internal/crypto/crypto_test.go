package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecrypt(t *testing.T) {
	tmpDir := t.TempDir()
	generated, err := Init(tmpDir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !generated {
		t.Fatal("expected new key to be generated")
	}

	plaintext := []byte("my-secret-password-123")
	encrypted, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if encrypted == "" {
		t.Fatal("encrypted is empty")
	}
	if encrypted == string(plaintext) {
		t.Fatal("encrypted equals plaintext")
	}

	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("round-trip failed: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptSamePlaintextDifferentCiphertext(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := Init(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("password")
	c1, _ := Encrypt(plaintext)
	c2, _ := Encrypt(plaintext)
	if c1 == c2 {
		t.Fatal("same plaintext produced identical ciphertext — nonce reuse?")
	}
}

func TestDecryptInvalidBase64(t *testing.T) {
	tmpDir := t.TempDir()
	_, _ = Init(tmpDir)

	_, err := Decrypt("not-valid-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}

func TestDecryptTamperedData(t *testing.T) {
	tmpDir := t.TempDir()
	_, _ = Init(tmpDir)

	encrypted, _ := Encrypt([]byte("secret"))
	tampered := encrypted[:len(encrypted)-2] + "AA"
	_, err := Decrypt(tampered)
	if err == nil {
		t.Fatal("expected error for tampered ciphertext (GCM auth failure)")
	}
}

func TestInitLoadsExistingKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "encryption.key")

	if err := os.WriteFile(keyPath, []byte("0123456789abcdef0123456789abcdef"), 0600); err != nil {
		t.Fatal(err)
	}

	generated, err := Init(tmpDir)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if generated {
		t.Fatal("expected existing key to be loaded, not regenerated")
	}

	if len(GetKey()) != 32 {
		t.Fatalf("key length: got %d, want 32", len(GetKey()))
	}
}

func TestKeyExists(t *testing.T) {
	tmpDir := t.TempDir()
	if KeyExists(tmpDir) {
		t.Fatal("expected no key in empty dir")
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "encryption.key"), make([]byte, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if !KeyExists(tmpDir) {
		t.Fatal("expected key to exist")
	}
}

func TestEncryptDecryptEmptyPlaintext(t *testing.T) {
	tmpDir := t.TempDir()
	_, _ = Init(tmpDir)

	encrypted, err := Encrypt([]byte{})
	if err != nil {
		t.Fatalf("Encrypt empty: %v", err)
	}
	decrypted, err := Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt empty: %v", err)
	}
	if len(decrypted) != 0 {
		t.Fatalf("empty round-trip: got %d bytes", len(decrypted))
	}
}

func TestEncryptDecryptBinaryData(t *testing.T) {
	tmpDir := t.TempDir()
	_, _ = Init(tmpDir)

	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	encrypted, _ := Encrypt(data)
	decrypted, _ := Decrypt(encrypted)
	if string(decrypted) != string(data) {
		t.Fatal("binary round-trip failed")
	}
}
