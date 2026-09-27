package util

import (
	"crypto/sha1"
	"crypto/subtle"
	"encoding/hex"
)

// MySQLNativePasswordHash returns the stage-2 verifier used by mysql_native_password.
func MySQLNativePasswordHash(password string) string {
	stage1 := sha1.Sum([]byte(password))
	stage2 := sha1.Sum(stage1[:])
	return hex.EncodeToString(stage2[:])
}

// VerifyMySQLNativePasswordResponse verifies a mysql_native_password challenge response.
func VerifyMySQLNativePasswordResponse(hash string, salt, response []byte) bool {
	if len(salt) == 0 || len(response) != sha1.Size {
		return false
	}
	stage2, err := hex.DecodeString(hash)
	if err != nil || len(stage2) != sha1.Size {
		return false
	}

	scrambleInput := make([]byte, 0, len(salt)+len(stage2))
	scrambleInput = append(scrambleInput, salt...)
	scrambleInput = append(scrambleInput, stage2...)
	scramble := sha1.Sum(scrambleInput)
	stage1 := make([]byte, sha1.Size)
	for index := range stage1 {
		stage1[index] = response[index] ^ scramble[index]
	}
	candidateStage2 := sha1.Sum(stage1)
	return subtle.ConstantTimeCompare(candidateStage2[:], stage2) == 1
}
