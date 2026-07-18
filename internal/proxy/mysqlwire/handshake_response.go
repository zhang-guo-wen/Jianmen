package mysqlwire

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
)

const (
	ClientConnectWithDB    uint32 = 1 << 3
	ClientProtocol41       uint32 = 1 << 9
	ClientSSL              uint32 = 1 << 11
	ClientSecureConnection uint32 = 1 << 15
	ClientPluginAuth       uint32 = 1 << 19
)

type Handshake struct {
	ProtocolVersion byte
	ServerVersion   string
	ConnectionID    uint32
	AuthData        []byte
	CapabilityFlags uint32
	CharacterSet    byte
	StatusFlags     uint16
	AuthPluginName  string
}

type LoginOptions struct {
	Username   string
	Password   string
	Database   string
	AuthPlugin string
	Sequence   byte
	TLS        bool
}

func BuildHandshakeResponse41(handshake Handshake, options LoginOptions) ([]byte, error) {
	if handshake.CapabilityFlags&ClientProtocol41 == 0 {
		return nil, errors.New("mysql server does not support protocol 4.1")
	}
	if options.TLS && handshake.CapabilityFlags&ClientSSL == 0 {
		return nil, errors.New("mysql server does not support TLS")
	}
	if options.Database != "" &&
		handshake.CapabilityFlags&ClientConnectWithDB == 0 {
		return nil, errors.New("mysql server does not support database in login response")
	}
	plugin := options.AuthPlugin
	if plugin == "" {
		plugin = handshake.AuthPluginName
	}
	authResponse, err := BuildAuthResponse(plugin, options.Password, handshake.AuthData)
	if err != nil {
		return nil, err
	}
	capabilities := ClientProtocol41
	if handshake.CapabilityFlags&ClientSecureConnection != 0 {
		capabilities |= ClientSecureConnection
	}
	if options.TLS {
		capabilities |= ClientSSL
	}
	if plugin != "" && handshake.CapabilityFlags&ClientPluginAuth != 0 {
		capabilities |= ClientPluginAuth
	}
	if options.Database != "" && handshake.CapabilityFlags&ClientConnectWithDB != 0 {
		capabilities |= ClientConnectWithDB
	}

	payload := make([]byte, 32)
	binary.LittleEndian.PutUint32(payload[:4], capabilities)
	binary.LittleEndian.PutUint32(payload[4:8], 1<<24)
	characterSet := handshake.CharacterSet
	if characterSet == 0 {
		characterSet = 45
	}
	payload[8] = characterSet
	payload = append(payload, options.Username...)
	payload = append(payload, 0)
	if capabilities&ClientSecureConnection == 0 {
		return nil, errors.New("mysql server does not support secure authentication response")
	}
	if len(authResponse) > 255 {
		return nil, errors.New("mysql authentication response is too large")
	}
	payload = append(payload, byte(len(authResponse)))
	payload = append(payload, authResponse...)
	if capabilities&ClientConnectWithDB != 0 {
		payload = append(payload, options.Database...)
		payload = append(payload, 0)
	}
	if capabilities&ClientPluginAuth != 0 {
		payload = append(payload, plugin...)
		payload = append(payload, 0)
	}
	return EncodePacket(options.Sequence, payload)
}

func BuildAuthResponse(plugin, password string, salt []byte) ([]byte, error) {
	if len(salt) > 20 {
		salt = salt[:20]
	}
	switch plugin {
	case "mysql_native_password":
		stage1 := sha1.Sum([]byte(password))
		stage2 := sha1.Sum(stage1[:])
		input := append(append(make([]byte, 0, len(salt)+len(stage2)), salt...), stage2[:]...)
		stage3 := sha1.Sum(input)
		response := make([]byte, len(stage1))
		for index := range response {
			response[index] = stage1[index] ^ stage3[index]
		}
		return response, nil
	case "caching_sha2_password":
		stage1 := sha256.Sum256([]byte(password))
		stage2 := sha256.Sum256(stage1[:])
		input := append(append(make([]byte, 0, len(salt)+len(stage2)), salt...), stage2[:]...)
		stage3 := sha256.Sum256(input)
		response := make([]byte, len(stage1))
		for index := range response {
			response[index] = stage1[index] ^ stage3[index]
		}
		return response, nil
	default:
		return nil, errors.New("unsupported mysql authentication plugin")
	}
}
