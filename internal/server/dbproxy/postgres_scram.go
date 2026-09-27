package dbproxy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/xdg-go/stringprep"
)

const (
	postgresSCRAMSHA256        = "SCRAM-SHA-256"
	minPostgresSCRAMIterations = 4096
	maxPostgresSCRAMIterations = 1_000_000
	postgresSCRAMNonceBytes    = 18
	maxPostgresSCRAMSaltBytes  = 1024
)

func runPostgresSCRAM(conn net.Conn, username, password string, offeredMechanisms []byte) error {
	nonceBytes := make([]byte, postgresSCRAMNonceBytes)
	if _, err := rand.Read(nonceBytes); err != nil {
		return fmt.Errorf("generate PostgreSQL SCRAM nonce: %w", err)
	}
	return runPostgresSCRAMWithNonce(
		conn,
		username,
		password,
		offeredMechanisms,
		base64.RawStdEncoding.EncodeToString(nonceBytes),
	)
}

func runPostgresSCRAMWithNonce(conn net.Conn, username, password string, offeredMechanisms []byte, clientNonce string) error {
	if !postgresSCRAMMechanismOffered(offeredMechanisms, postgresSCRAMSHA256) {
		return errors.New("PostgreSQL upstream does not offer SCRAM-SHA-256")
	}
	if clientNonce == "" || strings.ContainsAny(clientNonce, ",\x00") {
		return errors.New("invalid PostgreSQL SCRAM client nonce")
	}

	clientFirstBare := "n=" + escapePostgresSCRAMUsername(username) + ",r=" + clientNonce
	clientFirst := "n,," + clientFirstBare
	initialPayload := make([]byte, 0, len(postgresSCRAMSHA256)+1+4+len(clientFirst))
	initialPayload = append(initialPayload, postgresSCRAMSHA256...)
	initialPayload = append(initialPayload, 0)
	var responseLength [4]byte
	binary.BigEndian.PutUint32(responseLength[:], uint32(len(clientFirst)))
	initialPayload = append(initialPayload, responseLength[:]...)
	initialPayload = append(initialPayload, clientFirst...)
	if err := writePostgresMessage(conn, 'p', initialPayload); err != nil {
		return fmt.Errorf("write PostgreSQL SCRAM initial response: %w", err)
	}

	continueMessage, err := readPostgresMessage(conn, maxPostgresAuthMessageBytes)
	if err != nil {
		return fmt.Errorf("read PostgreSQL SCRAM continue response: %w", err)
	}
	if continueMessage.kind != 'R' || len(continueMessage.payload) < 4 ||
		binary.BigEndian.Uint32(continueMessage.payload[:4]) != 11 {
		return errors.New("expected PostgreSQL SCRAM continue response")
	}
	serverFirst := string(continueMessage.payload[4:])
	serverAttributes, err := parsePostgresSCRAMAttributes(serverFirst)
	if err != nil {
		return fmt.Errorf("parse PostgreSQL SCRAM continue response: %w", err)
	}
	if _, unsupported := serverAttributes["m"]; unsupported {
		return errors.New("PostgreSQL SCRAM mandatory extension is unsupported")
	}
	combinedNonce := serverAttributes["r"]
	if !strings.HasPrefix(combinedNonce, clientNonce) || len(combinedNonce) <= len(clientNonce) {
		return errors.New("PostgreSQL SCRAM server nonce does not extend client nonce")
	}
	salt, err := base64.StdEncoding.DecodeString(serverAttributes["s"])
	if err != nil || len(salt) == 0 || len(salt) > maxPostgresSCRAMSaltBytes {
		return errors.New("invalid PostgreSQL SCRAM salt")
	}
	iterations, err := strconv.Atoi(serverAttributes["i"])
	if err != nil || iterations < minPostgresSCRAMIterations || iterations > maxPostgresSCRAMIterations {
		return errors.New("invalid PostgreSQL SCRAM iteration count")
	}

	clientFinalWithoutProof := "c=biws,r=" + combinedNonce
	authMessage := clientFirstBare + "," + serverFirst + "," + clientFinalWithoutProof
	saltedPassword := PBKDF2Key(normalizePostgresSCRAMPassword(password), salt, iterations, sha256.Size)
	clientKey := HMACSHA256(saltedPassword, []byte("Client Key"))
	storedKey := SHA256Hash(clientKey)
	clientSignature := HMACSHA256(storedKey, []byte(authMessage))
	clientProof := XORBytes(clientKey, clientSignature)
	serverKey := HMACSHA256(saltedPassword, []byte("Server Key"))
	expectedServerSignature := HMACSHA256(serverKey, []byte(authMessage))

	clientFinal := clientFinalWithoutProof + ",p=" + base64.StdEncoding.EncodeToString(clientProof)
	if err := writePostgresMessage(conn, 'p', []byte(clientFinal)); err != nil {
		return fmt.Errorf("write PostgreSQL SCRAM final response: %w", err)
	}

	finalMessage, err := readPostgresMessage(conn, maxPostgresAuthMessageBytes)
	if err != nil {
		return fmt.Errorf("read PostgreSQL SCRAM final response: %w", err)
	}
	if finalMessage.kind != 'R' || len(finalMessage.payload) < 4 ||
		binary.BigEndian.Uint32(finalMessage.payload[:4]) != 12 {
		return errors.New("expected PostgreSQL SCRAM final verifier")
	}
	finalAttributes, err := parsePostgresSCRAMAttributes(string(finalMessage.payload[4:]))
	if err != nil {
		return fmt.Errorf("parse PostgreSQL SCRAM final verifier: %w", err)
	}
	if _, rejected := finalAttributes["e"]; rejected {
		return errors.New("PostgreSQL SCRAM server rejected authentication")
	}
	encodedVerifier, exists := finalAttributes["v"]
	if !exists {
		return errors.New("PostgreSQL SCRAM final response is missing the server signature")
	}
	verifier, err := base64.StdEncoding.DecodeString(encodedVerifier)
	if err != nil || !hmac.Equal(verifier, expectedServerSignature) {
		return errors.New("PostgreSQL SCRAM server signature mismatch")
	}
	return nil
}

func normalizePostgresSCRAMPassword(password string) []byte {
	normalized, err := stringprep.SASLprep.Prepare(password)
	if err != nil {
		return []byte(password)
	}
	return []byte(normalized)
}

func postgresSCRAMMechanismOffered(payload []byte, wanted string) bool {
	if len(payload) < 2 || payload[len(payload)-1] != 0 {
		return false
	}
	found := false
	for position := 0; position < len(payload); {
		end := indexPostgresNUL(payload, position)
		if end < 0 {
			return false
		}
		if end == position {
			return position == len(payload)-1 && found
		}
		if string(payload[position:end]) == wanted {
			found = true
		}
		position = end + 1
	}
	return false
}

func parsePostgresSCRAMAttributes(message string) (map[string]string, error) {
	if message == "" {
		return nil, errors.New("empty SCRAM attribute list")
	}
	attributes := make(map[string]string)
	for _, item := range strings.Split(message, ",") {
		if len(item) < 3 || item[1] != '=' {
			return nil, errors.New("malformed SCRAM attribute")
		}
		key := item[:1]
		if _, duplicate := attributes[key]; duplicate {
			return nil, fmt.Errorf("duplicate SCRAM attribute %q", key)
		}
		attributes[key] = item[2:]
	}
	return attributes, nil
}

func escapePostgresSCRAMUsername(username string) string {
	username = strings.ReplaceAll(username, "=", "=3D")
	return strings.ReplaceAll(username, ",", "=2C")
}

func PBKDF2Key(password, salt []byte, iterations, keyLength int) []byte {
	if iterations <= 0 || keyLength <= 0 {
		return nil
	}
	const hashLength = sha256.Size
	blocks := (keyLength + hashLength - 1) / hashLength
	result := make([]byte, 0, blocks*hashLength)
	for block := 1; block <= blocks; block++ {
		input := make([]byte, len(salt)+4)
		copy(input, salt)
		binary.BigEndian.PutUint32(input[len(salt):], uint32(block))
		current := HMACSHA256(password, input)
		accumulator := append([]byte(nil), current...)
		for iteration := 2; iteration <= iterations; iteration++ {
			current = HMACSHA256(password, current)
			for index := range accumulator {
				accumulator[index] ^= current[index]
			}
		}
		result = append(result, accumulator...)
	}
	return result[:keyLength]
}

func HMACSHA256(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return mac.Sum(nil)
}

func SHA256Hash(data []byte) []byte {
	hash := sha256.Sum256(data)
	return hash[:]
}

func XORBytes(left, right []byte) []byte {
	length := len(left)
	if len(right) < length {
		length = len(right)
	}
	result := make([]byte, length)
	for index := 0; index < length; index++ {
		result[index] = left[index] ^ right[index]
	}
	return result
}
