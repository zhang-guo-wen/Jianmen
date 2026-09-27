package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var masterKey []byte

const keyFilename = "encryption.key"

// Init 从 dataDir/encryption.key 加载密钥，如果不存在则生成新密钥。
// 返回 true 表示生成了新密钥（系统进入初始化模式）。
func Init(dataDir string) (newKeyGenerated bool, err error) {
	keyPath := filepath.Join(dataDir, keyFilename)

	// 先尝试读取已有密钥
	existing, readErr := os.ReadFile(keyPath)
	if readErr == nil {
		if len(existing) != 32 {
			return false, fmt.Errorf("encryption key must be 32 bytes, got %d", len(existing))
		}
		masterKey = make([]byte, 32)
		copy(masterKey, existing)
		return false, nil
	}

	if !errors.Is(readErr, os.ErrNotExist) {
		return false, fmt.Errorf("read encryption key: %w", readErr)
	}

	// 生成新密钥
	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		return false, fmt.Errorf("generate encryption key: %w", err)
	}

	// 确保 dataDir 存在
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return false, fmt.Errorf("create data dir: %w", err)
	}

	if err := os.WriteFile(keyPath, newKey, 0600); err != nil {
		return false, fmt.Errorf("write encryption key: %w", err)
	}

	masterKey = make([]byte, 32)
	copy(masterKey, newKey)
	return true, nil
}

// KeyExists 检查密钥文件是否存在
func KeyExists(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, keyFilename))
	return err == nil
}

// GetKey 返回当前主密钥（仅用于测试）
func GetKey() []byte {
	return masterKey
}

// Encrypt 使用 AES-256-GCM 加密明文，返回 Base64 编码的密文。
// 格式: Base64(Nonce(12字节) + Ciphertext + Tag(16字节))
func Encrypt(plaintext []byte) (string, error) {
	if len(masterKey) != 32 {
		return "", errors.New("crypto not initialized: master key is not set")
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	// Seal 会追加密文和认证标签到 nonce 后面
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt 解密 Base64 编码的密文，返回明文。
func Decrypt(encoded string) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("crypto not initialized: master key is not set")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode base64: %w", err)
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short: %d bytes, need at least %d", len(ciphertext), nonceSize)
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}
