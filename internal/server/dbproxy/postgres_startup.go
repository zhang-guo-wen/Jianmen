package dbproxy

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"strings"

	"jianmen/internal/util"
)

const postgresSupportedProtocolMinor uint32 = 0

// BuildPostgresPasswordResponse returns the password payload PostgreSQL expects for the given auth type.
func BuildPostgresPasswordResponse(authType uint32, username, password string, salt []byte) string {
	if authType != 5 {
		return password
	}
	h1 := md5.Sum([]byte(password + username))
	h1Hex := hex.EncodeToString(h1[:])
	h2Input := make([]byte, len(h1Hex)+len(salt))
	copy(h2Input, h1Hex)
	copy(h2Input[len(h1Hex):], salt)
	h2 := md5.Sum(h2Input)
	return "md5" + hex.EncodeToString(h2[:])
}

func postgresUpstreamDatabase(clientDatabase string) string {
	if clientDatabase == "" {
		return "postgres"
	}
	if _, _, _, err := util.ParseCompactUsername(clientDatabase); err == nil {
		return "postgres"
	}
	return clientDatabase
}

func BuildPostgresUpstreamStartupMessage(username, database string) []byte {
	startup := postgresStartup{
		parameters: []postgresStartupParameter{
			{name: "user", value: username},
			{name: "database", value: database},
		},
		username: username,
		database: database,
	}
	return buildPostgresUpstreamStartupMessage(username, startup)
}

func buildPostgresUpstreamStartupMessage(username string, startup postgresStartup) []byte {
	var payload strings.Builder
	writePostgresStartupParameter(&payload, "user", username)
	writePostgresStartupParameter(&payload, "database", postgresUpstreamDatabase(startup.database))
	for _, parameter := range startup.parameters {
		if parameter.name == "user" || parameter.name == "database" ||
			strings.HasPrefix(parameter.name, "_pq_.") {
			continue
		}
		writePostgresStartupParameter(&payload, parameter.name, parameter.value)
	}
	payload.WriteByte(0)

	message := make([]byte, 8+payload.Len())
	binary.BigEndian.PutUint32(message[:4], uint32(len(message)))
	binary.BigEndian.PutUint32(message[4:8], postgresProtocolVersion30)
	copy(message[8:], payload.String())
	return message
}

func writePostgresStartupParameter(payload *strings.Builder, name, value string) {
	payload.WriteString(name)
	payload.WriteByte(0)
	payload.WriteString(value)
	payload.WriteByte(0)
}

func writePostgresProtocolNegotiation(clientMessage postgresStartup, connection interface {
	Write([]byte) (int, error)
}) error {
	if clientMessage.protocolMinor <= postgresSupportedProtocolMinor &&
		len(clientMessage.unsupportedOptions) == 0 {
		return nil
	}
	payloadLength := 8
	for _, option := range clientMessage.unsupportedOptions {
		payloadLength += len(option) + 1
	}
	payload := make([]byte, 8, payloadLength)
	binary.BigEndian.PutUint32(payload[:4], postgresSupportedProtocolMinor)
	binary.BigEndian.PutUint32(payload[4:8], uint32(len(clientMessage.unsupportedOptions)))
	for _, option := range clientMessage.unsupportedOptions {
		payload = append(payload, option...)
		payload = append(payload, 0)
	}
	return writePostgresMessageTo(connection, 'v', payload)
}

func writePostgresMessageTo(connection interface {
	Write([]byte) (int, error)
}, kind byte, payload []byte) error {
	message := postgresWireMessage{kind: kind, payload: payload}
	return writePostgresBytes(connection, message.raw())
}

func shouldForwardPostgresAuthMessage(msg []byte) bool {
	if len(msg) < 9 || msg[0] != 'R' {
		return true
	}
	return binary.BigEndian.Uint32(msg[5:9]) == 0
}
