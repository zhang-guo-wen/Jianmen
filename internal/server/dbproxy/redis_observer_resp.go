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
	case '_':
		if len(data) < 3 {
			return 0, redisRESPIncomplete
		}
		if data[1] != '\r' || data[2] != '\n' {
			return 0, redisRESPMalformed
		}
		return 3, redisRESPComplete
	case '#':
		if len(data) < 4 {
			return 0, redisRESPIncomplete
		}
		if (data[1] != 't' && data[1] != 'f') || data[2] != '\r' || data[3] != '\n' {
			return 0, redisRESPMalformed
		}
		return 4, redisRESPComplete
	case ',', '(':
		lineEnd, status := redisRESPLineEnd(data)
		if status != redisRESPComplete {
			return 0, status
		}
		if data[0] == ',' {
			if !validRedisRESPDouble(data[1:lineEnd]) {
				return 0, redisRESPMalformed
			}
		} else if !validRedisRESPBigNumber(data[1:lineEnd]) {
			return 0, redisRESPMalformed
		}
		return lineEnd + 2, redisRESPComplete
	case '$', '!', '=':
		return redisRESPBulkLength(data, data[0])
	case '*', '~', '>', '%', '|':
		return redisRESPAggregateLength(data, depth)
	default:
		return 0, redisRESPMalformed
	}
}

func redisRESPBulkLength(data []byte, prefix byte) (int, redisRESPStatus) {
	lineEnd, status := redisRESPLineEnd(data)
	if status != redisRESPComplete {
		return 0, status
	}
	var valueLength int64
	var ok bool
	if prefix == '$' {
		valueLength, ok = parseCanonicalRESPNullableNumber(data[1:lineEnd])
	} else {
		valueLength, ok = parseCanonicalRESPNumber(data[1:lineEnd])
	}
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
	if prefix == '=' && (valueLength < 4 || data[headerLength+3] != ':') {
		return 0, redisRESPMalformed
	}
	return total, redisRESPComplete
}

func redisRESPAggregateLength(data []byte, depth int) (int, redisRESPStatus) {
	lineEnd, status := redisRESPLineEnd(data)
	if status != redisRESPComplete {
		return 0, status
	}
	count, ok := parseCanonicalRESPNullableNumber(data[1:lineEnd])
	if !ok {
		return 0, redisRESPMalformed
	}
	position := lineEnd + 2
	if count == -1 {
		if data[0] != '*' {
			return 0, redisRESPMalformed
		}
		return position, redisRESPComplete
	}
	elementCount, ok := redisRESPAggregateElementCount(data[0], count)
	if !ok || elementCount > maxRedisObserverArrayElements {
		return 0, redisRESPLimitExceeded
	}
	if elementCount == 0 {
		return position, redisRESPComplete
	}
	for i := int64(0); i < elementCount; i++ {
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

func redisRESPAggregateElementCount(prefix byte, count int64) (int64, bool) {
	if count < 0 {
		return 0, false
	}
	switch prefix {
	case '*', '~', '>':
		return count, true
	case '%':
		if count > int64(maxRedisObserverArrayElements)/2 {
			return 0, false
		}
		return count * 2, true
	case '|':
		if count > (int64(maxRedisObserverArrayElements)-1)/2 {
			return 0, false
		}
		return count*2 + 1, true
	default:
		return 0, false
	}
}

func validRedisRESPDouble(value []byte) bool {
	switch strings.ToLower(string(value)) {
	case "inf", "+inf", "-inf", "nan":
		return true
	}
	_, err := strconv.ParseFloat(string(value), 64)
	return err == nil
}

func validRedisRESPBigNumber(value []byte) bool {
	if len(value) == 0 {
		return false
	}
	start := 0
	if value[0] == '-' || value[0] == '+' {
		start = 1
	}
	if start == len(value) {
		return false
	}
	for _, digit := range value[start:] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
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
	responseType := redisRESPPrimaryType(frame)
	if responseType != '-' && responseType != '!' {
		return queryFinish{Status: queryStatusSuccess}
	}
	lineEnd := indexCRLF(frame)
	code := "REDIS_ERROR"
	if responseType == '-' && lineEnd > 1 {
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

func redisRESPPrimaryType(frame []byte) byte {
	return redisRESPPrimaryPrefixType(frame)
}

func redisRESPPrimaryPrefixType(frame []byte) byte {
	for len(frame) > 0 && frame[0] == '|' {
		lineEnd, status := redisRESPLineEnd(frame)
		if status != redisRESPComplete {
			return 0
		}
		count, ok := parseCanonicalRESPNumber(frame[1:lineEnd])
		if !ok || count > int64(maxRedisObserverArrayElements)/2 {
			return 0
		}
		position := lineEnd + 2
		for index := int64(0); index < count*2; index++ {
			length, itemStatus := redisRESPValueLength(frame[position:], 1)
			if itemStatus != redisRESPComplete {
				return 0
			}
			position += length
		}
		if position >= len(frame) {
			return 0
		}
		frame = frame[position:]
	}
	if len(frame) == 0 {
		return 0
	}
	return frame[0]
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
