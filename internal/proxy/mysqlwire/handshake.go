package mysqlwire

import (
	"bytes"
	"encoding/binary"
	"errors"
)

func ParseHandshake(payload []byte) (Handshake, error) {
	if len(payload) < 1 {
		return Handshake{}, errors.New("mysql handshake packet is too short")
	}
	position := 0
	handshake := Handshake{ProtocolVersion: payload[position]}
	position++
	serverVersionEnd := bytes.IndexByte(payload[position:], 0)
	if serverVersionEnd < 0 {
		return Handshake{}, errors.New("mysql handshake has no server version terminator")
	}
	handshake.ServerVersion = string(payload[position : position+serverVersionEnd])
	position += serverVersionEnd + 1
	if len(payload)-position < 4+8+1+2 {
		return Handshake{}, errors.New("mysql handshake fixed fields are truncated")
	}
	handshake.ConnectionID = binary.LittleEndian.Uint32(payload[position:])
	position += 4
	firstSalt := append([]byte(nil), payload[position:position+8]...)
	position += 8
	position++
	lowerCapabilities := binary.LittleEndian.Uint16(payload[position:])
	position += 2
	if position == len(payload) {
		handshake.CapabilityFlags = uint32(lowerCapabilities)
		handshake.AuthData = firstSalt
		handshake.AuthPluginName = "mysql_native_password"
		return handshake, nil
	}
	if len(payload)-position < 1+2+2+1+10 {
		return Handshake{}, errors.New("mysql handshake capability fields are truncated")
	}
	handshake.CharacterSet = payload[position]
	position++
	handshake.StatusFlags = binary.LittleEndian.Uint16(payload[position:])
	position += 2
	upperCapabilities := binary.LittleEndian.Uint16(payload[position:])
	position += 2
	handshake.CapabilityFlags = uint32(lowerCapabilities) | uint32(upperCapabilities)<<16
	authPluginDataLength := int(payload[position])
	position++
	position += 10

	secondSaltLength := 12
	if authPluginDataLength > 8 {
		secondSaltLength = authPluginDataLength - 8
	}
	if secondSaltLength > len(payload)-position {
		secondSaltLength = len(payload) - position
	}
	secondSalt := payload[position : position+secondSaltLength]
	position += secondSaltLength
	secondSalt = bytes.TrimSuffix(secondSalt, []byte{0})
	handshake.AuthData = append(firstSalt, secondSalt...)
	if len(handshake.AuthData) > 20 {
		handshake.AuthData = handshake.AuthData[:20]
	}
	handshake.AuthPluginName = "mysql_native_password"
	if handshake.CapabilityFlags&ClientPluginAuth != 0 && position < len(payload) {
		pluginEnd := bytes.IndexByte(payload[position:], 0)
		if pluginEnd < 0 {
			pluginEnd = len(payload) - position
		}
		if pluginEnd > 0 {
			handshake.AuthPluginName = string(payload[position : position+pluginEnd])
		}
	}
	return handshake, nil
}
