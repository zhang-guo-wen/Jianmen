package dbproxy

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
)

func splitCString(data []byte) (string, []byte) {
	index := bytes.IndexByte(data, 0)
	if index < 0 {
		return string(data), nil
	}
	return string(data[:index]), data[index+1:]
}

func trimCString(data []byte) string {
	if index := bytes.IndexByte(data, 0); index >= 0 {
		data = data[:index]
	}
	return strings.TrimSpace(string(data))
}

func mysqlPacketWithSeq(seq byte, payload []byte) []byte {
	packet := make([]byte, 4+len(payload))
	packet[0] = byte(len(payload))
	packet[1] = byte(len(payload) >> 8)
	packet[2] = byte(len(payload) >> 16)
	packet[3] = seq
	copy(packet[4:], payload)
	return packet
}

func postgresMessageWithType(typ byte, payload []byte) []byte {
	msg := make([]byte, 1+4+len(payload))
	msg[0] = typ
	binary.BigEndian.PutUint32(msg[1:5], uint32(4+len(payload)))
	copy(msg[5:], payload)
	return msg
}

func postgresReadyForQuery() []byte {
	return postgresMessageWithType('Z', []byte{'I'})
}

func parsePostgresRowsFromCommandTag(tag string) (int64, bool) {
	fields := strings.Fields(tag)
	if len(fields) == 0 {
		return 0, false
	}
	last := fields[len(fields)-1]
	var value int64
	for _, r := range last {
		if r < '0' || r > '9' {
			return 0, false
		}
		digit := int64(r - '0')
		if value > (math.MaxInt64-digit)/10 {
			return 0, false
		}
		value = value*10 + digit
	}
	return value, true
}

func parsePostgresError(payload []byte) (string, string) {
	var code, message string
	for len(payload) > 0 {
		fieldType := payload[0]
		if fieldType == 0 {
			break
		}
		value, rest := splitCString(payload[1:])
		switch fieldType {
		case 'C':
			code = value
		case 'M':
			message = value
		}
		payload = rest
	}
	return code, message
}
