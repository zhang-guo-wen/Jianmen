package dbproxy

import (
	"bytes"
	"encoding/binary"
)

func validPostgresFrontendMessage(messageType byte, payload []byte) bool {
	switch messageType {
	case 'Q', 'f':
		return isSinglePostgresCString(payload)
	case 'P':
		return validPostgresParse(payload)
	case 'B':
		return validPostgresBind(payload)
	case 'E':
		return validPostgresExecute(payload)
	case 'C', 'D':
		return validPostgresTargetName(payload)
	case 'H', 'S', 'X', 'c':
		return len(payload) == 0
	case 'd':
		return true
	default:
		return false
	}
}

func validPostgresBackendMessage(messageType byte, payload []byte) bool {
	switch messageType {
	case '1', '2', '3', 'I', 'n', 's', 'c':
		return len(payload) == 0
	case 'A':
		return validPostgresNotification(payload)
	case 'C':
		return isSinglePostgresCString(payload)
	case 'D':
		return validPostgresDataRow(payload)
	case 'E', 'N':
		return validPostgresErrorFields(payload)
	case 'G', 'H', 'W':
		return validPostgresCopyResponse(payload)
	case 'K':
		return len(payload) == 8
	case 'S':
		return validPostgresParameterStatus(payload)
	case 'T':
		return validPostgresRowDescription(payload)
	case 'Z':
		return len(payload) == 1 && (payload[0] == 'I' || payload[0] == 'T' || payload[0] == 'E')
	case 'd':
		return true
	case 't':
		return validPostgresParameterDescription(payload)
	default:
		return false
	}
}

func validPostgresParse(payload []byte) bool {
	rest, ok := consumePostgresCString(payload)
	if !ok {
		return false
	}
	rest, ok = consumePostgresCString(rest)
	if !ok {
		return false
	}
	count, rest, ok := consumePostgresUint16(rest)
	return ok && len(rest) == int(count)*4
}

func validPostgresBind(payload []byte) bool {
	rest, ok := consumePostgresCString(payload)
	if !ok {
		return false
	}
	rest, ok = consumePostgresCString(rest)
	if !ok {
		return false
	}
	formatCount, rest, ok := consumePostgresUint16(rest)
	if !ok || len(rest) < int(formatCount)*2 {
		return false
	}
	if !validPostgresFormatCodes(rest[:int(formatCount)*2]) {
		return false
	}
	rest = rest[int(formatCount)*2:]
	parameterCount, rest, ok := consumePostgresUint16(rest)
	if !ok || (formatCount != 0 && formatCount != 1 && formatCount != parameterCount) {
		return false
	}
	for index := 0; index < int(parameterCount); index++ {
		if len(rest) < 4 {
			return false
		}
		length := int32(binary.BigEndian.Uint32(rest[:4]))
		rest = rest[4:]
		if length == -1 {
			continue
		}
		if length < 0 || int64(length) > int64(len(rest)) {
			return false
		}
		rest = rest[int(length):]
	}
	resultCount, rest, ok := consumePostgresUint16(rest)
	return ok &&
		len(rest) == int(resultCount)*2 &&
		validPostgresFormatCodes(rest)
}

func validPostgresExecute(payload []byte) bool {
	rest, ok := consumePostgresCString(payload)
	if !ok || len(rest) != 4 {
		return false
	}
	return int32(binary.BigEndian.Uint32(rest)) >= 0
}

func validPostgresTargetName(payload []byte) bool {
	if len(payload) < 2 || (payload[0] != 'S' && payload[0] != 'P') {
		return false
	}
	return isSinglePostgresCString(payload[1:])
}

func validPostgresNotification(payload []byte) bool {
	if len(payload) < 6 {
		return false
	}
	rest, ok := consumePostgresCString(payload[4:])
	if !ok {
		return false
	}
	rest, ok = consumePostgresCString(rest)
	return ok && len(rest) == 0
}

func validPostgresParameterStatus(payload []byte) bool {
	rest, ok := consumePostgresCString(payload)
	if !ok {
		return false
	}
	rest, ok = consumePostgresCString(rest)
	return ok && len(rest) == 0
}

func validPostgresErrorFields(payload []byte) bool {
	for len(payload) > 0 {
		fieldType := payload[0]
		payload = payload[1:]
		if fieldType == 0 {
			return len(payload) == 0
		}
		var ok bool
		payload, ok = consumePostgresCString(payload)
		if !ok {
			return false
		}
	}
	return false
}

func validPostgresDataRow(payload []byte) bool {
	count, rest, ok := consumePostgresUint16(payload)
	if !ok {
		return false
	}
	for index := 0; index < int(count); index++ {
		if len(rest) < 4 {
			return false
		}
		length := int32(binary.BigEndian.Uint32(rest[:4]))
		rest = rest[4:]
		if length == -1 {
			continue
		}
		if length < 0 || int64(length) > int64(len(rest)) {
			return false
		}
		rest = rest[int(length):]
	}
	return len(rest) == 0
}

func validPostgresRowDescription(payload []byte) bool {
	count, rest, ok := consumePostgresUint16(payload)
	if !ok {
		return false
	}
	for index := 0; index < int(count); index++ {
		rest, ok = consumePostgresCString(rest)
		if !ok || len(rest) < 18 {
			return false
		}
		if binary.BigEndian.Uint16(rest[16:18]) > 1 {
			return false
		}
		rest = rest[18:]
	}
	return len(rest) == 0
}

func validPostgresParameterDescription(payload []byte) bool {
	count, rest, ok := consumePostgresUint16(payload)
	return ok && len(rest) == int(count)*4
}

func validPostgresCopyResponse(payload []byte) bool {
	if len(payload) < 3 || (payload[0] != 0 && payload[0] != 1) {
		return false
	}
	count := int(binary.BigEndian.Uint16(payload[1:3]))
	if len(payload) != 3+count*2 {
		return false
	}
	for offset := 3; offset < len(payload); offset += 2 {
		format := binary.BigEndian.Uint16(payload[offset : offset+2])
		if format > 1 {
			return false
		}
	}
	return true
}

func consumePostgresCString(payload []byte) ([]byte, bool) {
	index := bytes.IndexByte(payload, 0)
	if index < 0 {
		return nil, false
	}
	return payload[index+1:], true
}

func isSinglePostgresCString(payload []byte) bool {
	index := bytes.IndexByte(payload, 0)
	return index >= 0 && index == len(payload)-1
}

func consumePostgresUint16(payload []byte) (uint16, []byte, bool) {
	if len(payload) < 2 {
		return 0, nil, false
	}
	return binary.BigEndian.Uint16(payload[:2]), payload[2:], true
}

func validPostgresFormatCodes(payload []byte) bool {
	if len(payload)%2 != 0 {
		return false
	}
	for len(payload) > 0 {
		if binary.BigEndian.Uint16(payload[:2]) > 1 {
			return false
		}
		payload = payload[2:]
	}
	return true
}
