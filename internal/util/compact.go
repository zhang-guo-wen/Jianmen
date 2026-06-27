package util

import "fmt"

// 资源前缀常量
const (
	PrefixHost     = "H"
	PrefixDatabase = "D"
)

// ResourceIDFromSeq 从序号生成资源ID（前缀+4位62进制）
func ResourceIDFromSeq(prefix string, seq int) string {
	return prefix + EncodeBase62Padded(uint64(seq), 4)
}

// FullUsername 组装10位完整连接用户名
func FullUsername(prefix string, resourceSeq int, sessionSeq int) string {
	return prefix +
		EncodeBase62Padded(uint64(resourceSeq), 4) +
		EncodeBase62Padded(uint64(sessionSeq), 5)
}

// ParseCompactUsername 解析10位紧凑用户名
// 返回 prefix, resourceSeq, sessionSeq
func ParseCompactUsername(username string) (prefix string, resourceSeq uint64, sessionSeq uint64, err error) {
	if len(username) != 10 {
		return "", 0, 0, fmt.Errorf("compact username must be 10 characters, got %d", len(username))
	}
	prefix = string(username[0])
	rID, err := DecodeBase62(username[1:5])
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid resource id: %w", err)
	}
	sID, err := DecodeBase62(username[5:10])
	if err != nil {
		return "", 0, 0, fmt.Errorf("invalid session id: %w", err)
	}
	return prefix, rID, sID, nil
}
