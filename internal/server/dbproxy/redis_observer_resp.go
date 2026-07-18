package dbproxy

import (
	"strconv"
	"strings"
)

const (
	maxRedisObserverArrayElements = 4096
	maxRedisObserverNestingDepth  = 32
	maxRedisObserverLineBytes     = 1024
)

type redisRESPStatus uint8

const (
	redisRESPComplete redisRESPStatus = iota
	redisRESPIncomplete
	redisRESPMalformed
	redisRESPLimitExceeded
)

func redisRESPFrameLength(data []byte) (int, redisRESPStatus) {
	length, status := redisRESPValueLength(data, 0)
	if status == redisRESPComplete && length > maxRedisObserverBufferBytes {
		return 0, redisRESPLimitExceeded
	}
	return length, status
}

func redisRESPValueLength(data []byte, depth int) (int, redisRESPStatus) {
	if len(data) == 0 {
		return 0, redisRESPIncomplete
	}
	if depth > maxRedisObserverNestingDepth {
		return 0, redisRESPLimitExceeded
	}

	switch data[0] {
	case '+', '-':
		lineEnd, status := redisRESPLineEnd(data)
		if status != redisRESPComplete {
			return 0, status
		}
		return lineEnd + 2, redisRESPComplete
	case ':':
		lineEnd, status := redisRESPLineEnd(data)
		if status != redisRESPComplete {
			return 0, status
		}
		if _, err := strconv.ParseInt(string(data[1:lineEnd]), 10, 64); err != nil {
			return 0, redisRESPMalformed
		}
		return lineEnd + 2, redisRESPComplete
	case '$':
		return redisRESPBulkLength(data)
	case '*':
		return redisRESPArrayLength(data, depth)
	default:
		return 0, redisRESPMalformed
	}
}

func redisRESPBulkLength(data []byte) (int, redisRESPStatus) {
	lineEnd, status := redisRESPLineEnd(data)
	if status != redisRESPComplete {
		return 0, status
	}
	valueLength, ok := parseCanonicalRESPNullableNumber(data[1:lineEnd])
	if !ok {
		return 0, redisRESPMalformed
	}
	headerLength := lineEnd + 2
	if valueLength == -1 {
		return headerLength, redisRESPComplete
	}
	if valueLength > int64(maxRedisObserverBufferBytes-headerLength-2) {
		return 0, redisRESPLimitExceeded
	}
	total := headerLength + int(valueLength) + 2
	if len(data) < total {
		return 0, redisRESPIncomplete
	}
	if data[total-2] != '\r' || data[total-1] != '\n' {
		return 0, redisRESPMalformed
	}
	return total, redisRESPComplete
}

func redisRESPArrayLength(data []byte, depth int) (int, redisRESPStatus) {
	lineEnd, status := redisRESPLineEnd(data)
	if status != redisRESPComplete {
		return 0, status
	}
	count, ok := parseCanonicalRESPNullableNumber(data[1:lineEnd])
	if !ok {
		return 0, redisRESPMalformed
	}
	position := lineEnd + 2
	if count == -1 || count == 0 {
		return position, redisRESPComplete
	}
	if count > maxRedisObserverArrayElements {
		return 0, redisRESPLimitExceeded
	}
	for i := int64(0); i < count; i++ {
		length, itemStatus := redisRESPValueLength(data[position:], depth+1)
		if itemStatus != redisRESPComplete {
			return 0, itemStatus
		}
		position += length
		if position > maxRedisObserverBufferBytes {
			return 0, redisRESPLimitExceeded
		}
	}
	return position, redisRESPComplete
}

func redisRESPLineEnd(data []byte) (int, redisRESPStatus) {
	for index, value := range data {
		switch value {
		case '\r':
			if index > maxRedisObserverLineBytes {
				return 0, redisRESPLimitExceeded
			}
			if index+1 >= len(data) {
				return 0, redisRESPIncomplete
			}
			if data[index+1] != '\n' {
				return 0, redisRESPMalformed
			}
			return index, redisRESPComplete
		case '\n':
			return 0, redisRESPMalformed
		default:
			if index >= maxRedisObserverLineBytes {
				return 0, redisRESPLimitExceeded
			}
		}
	}
	return 0, redisRESPIncomplete
}

func redisResponseFinish(frame []byte) queryFinish {
	if len(frame) == 0 || frame[0] != '-' {
		return queryFinish{Status: queryStatusSuccess}
	}
	lineEnd := indexCRLF(frame)
	code := "REDIS_ERROR"
	if lineEnd > 1 {
		token := strings.Fields(string(frame[1:lineEnd]))
		if len(token) > 0 && validRedisErrorCode(token[0]) {
			code = token[0]
		}
	}
	return queryFinish{
		Status:       queryStatusError,
		ErrorCode:    code,
		ErrorMessage: "redis upstream error",
	}
}

func validRedisErrorCode(code string) bool {
	if code == "" || len(code) > 32 {
		return false
	}
	for _, value := range []byte(code) {
		if (value < 'A' || value > 'Z') && (value < '0' || value > '9') && value != '_' && value != '-' {
			return false
		}
	}
	return true
}
