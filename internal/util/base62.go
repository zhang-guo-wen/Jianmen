package util

import (
	"errors"
	"strings"
)

const Base62Chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

var charToVal [256]int8

func init() {
	for i := range charToVal {
		charToVal[i] = -1
	}
	for i, c := range []byte(Base62Chars) {
		charToVal[c] = int8(i)
	}
}

func EncodeBase62(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = Base62Chars[n%62]
		n /= 62
	}
	return string(buf[i:])
}

func DecodeBase62(s string) (uint64, error) {
	if s == "" {
		return 0, errors.New("empty string")
	}
	var n uint64
	for _, c := range []byte(s) {
		if c >= 128 || charToVal[c] < 0 {
			return 0, errors.New("invalid base62 character: " + string(c))
		}
		val := uint64(charToVal[c])
		if n > (^uint64(0)-val)/62 {
			return 0, errors.New("base62 value overflows uint64")
		}
		n = n*62 + val
	}
	return n, nil
}

func EncodeBase62Padded(n uint64, width int) string {
	s := EncodeBase62(n)
	if len(s) >= width {
		return s
	}
	return strings.Repeat("0", width-len(s)) + s
}
