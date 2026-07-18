package dbproxy

import "math"

func parseCanonicalRESPNumber(raw []byte) (int64, bool) {
	return parseCanonicalRESPUnsigned(raw, math.MaxInt64)
}

func parseCanonicalRESPNullableNumber(raw []byte) (int64, bool) {
	return parseCanonicalRESPNullable(raw, math.MaxInt64)
}

func parseCanonicalRESPUnsigned(raw []byte, maximum int64) (int64, bool) {
	if len(raw) == 0 || (len(raw) > 1 && raw[0] == '0') {
		return 0, false
	}
	var value int64
	for _, digit := range raw {
		if digit < '0' || digit > '9' {
			return 0, false
		}
		number := int64(digit - '0')
		if value > (math.MaxInt64-number)/10 {
			return 0, false
		}
		value = value*10 + number
		if value > maximum {
			return 0, false
		}
	}
	return value, true
}

func parseCanonicalRESPNullable(raw []byte, maximum int64) (int64, bool) {
	if len(raw) == 2 && raw[0] == '-' && raw[1] == '1' {
		return -1, true
	}
	return parseCanonicalRESPUnsigned(raw, maximum)
}
