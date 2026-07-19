package admin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"jianmen/internal/service"
)

const apiTokenBytes = 32

func hashPassword(password string) (string, error) {
	return service.HashAdminPassword(password)
}

func verifyPassword(hash, password string) bool {
	return service.VerifyAdminPassword(hash, password)
}

func newAPIToken() (token, tokenHash string, err error) {
	tokenBytes := make([]byte, apiTokenBytes)
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	token = hex.EncodeToString(tokenBytes)
	return token, hashToken(token), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
