package util

import (
	"crypto/sha1"
	"testing"
)

func TestVerifyMySQLNativePasswordResponse(t *testing.T) {
	const password = "correct horse battery staple"
	salt := []byte("12345678901234567890")
	response := mysqlNativeTestResponse(password, salt)

	hash := MySQLNativePasswordHash(password)
	if !VerifyMySQLNativePasswordResponse(hash, salt, response) {
		t.Fatal("correct password response was rejected")
	}
	if VerifyMySQLNativePasswordResponse(MySQLNativePasswordHash("wrong"), salt, response) {
		t.Fatal("wrong password verifier was accepted")
	}
}

func mysqlNativeTestResponse(password string, salt []byte) []byte {
	stage1 := sha1.Sum([]byte(password))
	stage2 := sha1.Sum(stage1[:])
	input := append(append(make([]byte, 0, len(salt)+len(stage2)), salt...), stage2[:]...)
	scramble := sha1.Sum(input)
	response := make([]byte, sha1.Size)
	for index := range response {
		response[index] = stage1[index] ^ scramble[index]
	}
	return response
}
