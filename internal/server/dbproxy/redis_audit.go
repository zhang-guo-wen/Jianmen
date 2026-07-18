package dbproxy

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	redisAuditRedacted    = "[REDACTED]"
	maxRedisAuditKeyBytes = 256
)

func validRedisCommandName(command string) bool {
	if command == "" || len(command) > 32 || !utf8.ValidString(command) {
		return false
	}
	for _, character := range []byte(command) {
		if (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
			return false
		}
	}
	return true
}

func redisAuditCommand(args []string) string {
	audit, _ := redisAuditCommandChecked(args)
	return audit
}

func redisAuditCommandChecked(args []string) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	command := strings.ToUpper(args[0])
	switch command {
	case "EVAL", "EVALSHA":
		return redisAuditEvalCommand(command, args)
	case "FCALL", "FCALL_RO":
		return redisAuditFunctionCommand(command, args)
	case "SINTERCARD", "ZINTER", "ZDIFF", "ZUNION", "ZINTERCARD":
		return redisAuditCountedKeyCommand(command, args)
	case "ZINTERSTORE", "ZDIFFSTORE", "ZUNIONSTORE":
		return redisAuditDestinationCountedKeyCommand(command, args)
	case "BITOP":
		return redisAuditBITOPCommand(args)
	case "BLMPOP":
		return redisAuditPrefixedCountedKeyCommand(command, args, 2, 3)
	case "LMPOP", "ZMPOP":
		return redisAuditPrefixedCountedKeyCommand(command, args, 1, 2)
	case "OBJECT":
		return redisAuditObjectCommand(args)
	case "XREAD", "XREADGROUP":
		return redisAuditXReadCommand(command, args)
	case "XGROUP":
		return redisAuditXGroupCommand(args)
	case "XINFO":
		return redisAuditXInfoCommand(args)
	}
	if redisAuditNoKey(command) {
		return redisAuditNoKeyCommand(command, args)
	}
	if !redisAuditHasFirstKey(command) || len(args) < 2 {
		return "", false
	}
	audit := command + " " + redisAuditKey(args[1])
	if len(args) > 2 {
		audit += " " + redisAuditRedacted
	}
	return audit, true
}

func redisAuditPrefixedCountedKeyCommand(command string, args []string, countIndex, keyIndex int) (string, bool) {
	if len(args) <= keyIndex {
		return "", false
	}
	keyCount, ok := parseCanonicalRESPNumber([]byte(args[countIndex]))
	if !ok || keyCount <= 0 || keyCount > int64(len(args)-keyIndex-1) {
		return "", false
	}
	directionIndex := keyIndex + int(keyCount)
	if directionIndex >= len(args) ||
		(!strings.EqualFold(args[directionIndex], "LEFT") && !strings.EqualFold(args[directionIndex], "RIGHT")) {
		return "", false
	}
	return command + " " + redisAuditKey(args[keyIndex]) + " " + redisAuditRedacted, true
}

func redisAuditObjectCommand(args []string) (string, bool) {
	if len(args) == 2 && strings.EqualFold(args[1], "HELP") {
		return "OBJECT " + redisAuditRedacted, true
	}
	if len(args) != 3 {
		return "", false
	}
	switch strings.ToUpper(args[1]) {
	case "ENCODING", "FREQ", "IDLETIME", "REFCOUNT":
		return "OBJECT " + redisAuditKey(args[2]) + " " + redisAuditRedacted, true
	default:
		return "", false
	}
}

func redisAuditXInfoCommand(args []string) (string, bool) {
	if len(args) == 2 && strings.EqualFold(args[1], "HELP") {
		return "XINFO " + redisAuditRedacted, true
	}
	if len(args) < 3 {
		return "", false
	}
	switch strings.ToUpper(args[1]) {
	case "CONSUMERS":
		if len(args) != 4 {
			return "", false
		}
	case "GROUPS":
		if len(args) != 3 {
			return "", false
		}
	case "STREAM":
		switch len(args) {
		case 3:
		case 4:
			if !strings.EqualFold(args[3], "FULL") {
				return "", false
			}
		case 6:
			count, ok := parseCanonicalRESPNumber([]byte(args[5]))
			if !strings.EqualFold(args[3], "FULL") ||
				!strings.EqualFold(args[4], "COUNT") ||
				!ok || count < 0 {
				return "", false
			}
		default:
			return "", false
		}
	default:
		return "", false
	}
	return "XINFO " + redisAuditKey(args[2]) + " " + redisAuditRedacted, true
}

func redisAuditCountedKeyCommand(command string, args []string) (string, bool) {
	if len(args) < 3 {
		return "", false
	}
	keyCount, ok := parseCanonicalRESPNumber([]byte(args[1]))
	if !ok || keyCount <= 0 || keyCount > int64(len(args)-2) {
		return "", false
	}
	return command + " " + redisAuditKey(args[2]) + " " + redisAuditRedacted, true
}

func redisAuditDestinationCountedKeyCommand(command string, args []string) (string, bool) {
	if len(args) < 4 {
		return "", false
	}
	keyCount, ok := parseCanonicalRESPNumber([]byte(args[2]))
	if !ok || keyCount <= 0 || keyCount > int64(len(args)-3) {
		return "", false
	}
	return command + " " + redisAuditKey(args[1]) + " " + redisAuditRedacted, true
}

func redisAuditEvalCommand(command string, args []string) (string, bool) {
	// EVAL script numkeys key [key ...] arg [arg ...]
	if len(args) < 3 {
		return "", false
	}
	keyCount, err := strconv.Atoi(args[2])
	if err != nil || keyCount < 0 || keyCount > len(args)-3 {
		return "", false
	}
	audit := command
	if keyCount > 0 {
		audit += " " + redisAuditKey(args[3])
	}
	return audit + " " + redisAuditRedacted, true
}

func redisAuditFunctionCommand(command string, args []string) (string, bool) {
	// FCALL function numkeys key [key ...] arg [arg ...]
	if len(args) < 3 {
		return "", false
	}
	keyCount, err := strconv.Atoi(args[2])
	if err != nil || keyCount < 0 || keyCount > len(args)-3 {
		return "", false
	}
	audit := command
	if keyCount > 0 {
		audit += " " + redisAuditKey(args[3])
	}
	return audit + " " + redisAuditRedacted, true
}

func redisAuditBITOPCommand(args []string) (string, bool) {
	if len(args) < 4 {
		return "", false
	}
	switch strings.ToUpper(args[1]) {
	case "AND", "OR", "XOR":
	case "NOT":
		if len(args) != 4 {
			return "", false
		}
	default:
		return "", false
	}
	return "BITOP " + redisAuditKey(args[2]) + " " + redisAuditRedacted, true
}

func redisAuditXReadCommand(command string, args []string) (string, bool) {
	streams := -1
	for index := 1; index < len(args); index++ {
		if strings.EqualFold(args[index], "STREAMS") {
			streams = index
			break
		}
	}
	if streams < 0 {
		return "", false
	}
	remaining := len(args) - streams - 1
	if remaining < 2 || remaining%2 != 0 {
		return "", false
	}
	if command == "XREADGROUP" {
		if len(args) < 7 || !strings.EqualFold(args[1], "GROUP") {
			return "", false
		}
	}
	return command + " " + redisAuditKey(args[streams+1]) + " " + redisAuditRedacted, true
}

func redisAuditXGroupCommand(args []string) (string, bool) {
	if len(args) < 4 {
		return "", false
	}
	switch strings.ToUpper(args[1]) {
	case "CREATE", "CREATECONSUMER", "DELCONSUMER", "DESTROY", "SETID":
		return "XGROUP " + redisAuditKey(args[2]) + " " + redisAuditRedacted, true
	default:
		return "", false
	}
}

func redisAuditNoKeyCommand(command string, args []string) (string, bool) {
	switch command {
	case "PING":
		if len(args) > 2 {
			return "", false
		}
	case "SELECT":
		if len(args) != 2 {
			return "", false
		}
	}
	if len(args) > 1 {
		return command + " " + redisAuditRedacted, true
	}
	return command, true
}

func redisAuditNoKey(command string) bool {
	switch command {
	case "ASKING", "COMMAND", "DBSIZE", "DISCARD", "ECHO", "EXEC", "INFO",
		"KEYS", "LASTSAVE", "MULTI", "PING", "QUIT", "RANDOMKEY", "READONLY",
		"READWRITE", "SCAN", "SELECT", "TIME", "UNWATCH", "WAIT":
		return true
	default:
		return false
	}
}

func redisAuditKey(key string) string {
	if key == "" || len(key) > maxRedisAuditKeyBytes || !utf8.ValidString(key) {
		return redisAuditRedacted
	}
	for _, character := range key {
		if unicode.IsSpace(character) || unicode.IsControl(character) || unicode.Is(unicode.Cf, character) {
			return redisAuditRedacted
		}
	}
	return key
}

func redisAuditHasFirstKey(command string) bool {
	switch command {
	case "APPEND", "DECR", "DECRBY", "DEL", "DUMP", "EXISTS",
		"BITCOUNT", "BITFIELD", "BITFIELD_RO", "BITPOS", "BLMOVE", "BLPOP",
		"BRPOP", "BRPOPLPUSH", "BZPOPMAX", "BZPOPMIN", "COPY",
		"EXPIRE", "EXPIREAT", "EXPIRETIME", "GET", "GETBIT", "GETDEL", "GETEX",
		"GETRANGE", "GETSET", "HDEL", "HEXISTS", "HGET", "HGETALL",
		"HEXPIRE", "HEXPIREAT", "HEXPIRETIME", "HGETDEL", "HGETEX",
		"HINCRBY", "HINCRBYFLOAT", "HKEYS", "HLEN", "HMGET", "HMSET", "HPERSIST",
		"HPEXPIRE", "HPEXPIREAT", "HPEXPIRETIME", "HPTTL", "HRANDFIELD", "HSCAN",
		"HSET", "HSETEX", "HSETNX", "HSTRLEN", "HTTL", "HVALS",
		"INCR", "INCRBY", "INCRBYFLOAT", "LINDEX", "LINSERT", "LLEN",
		"LCS", "LMOVE", "LPOP", "LPOS", "LPUSH", "LPUSHX", "LRANGE", "LREM",
		"LSET", "LTRIM", "MGET", "MOVE", "MSET", "MSETNX", "PERSIST", "PEXPIRE",
		"PEXPIREAT", "PEXPIRETIME", "PFADD", "PFCOUNT", "PFMERGE", "PUBLISH",
		"PSETEX", "PTTL", "RENAME", "RENAMENX", "RESTORE", "RPOP", "RPOPLPUSH", "RPUSH",
		"RPUSHX", "SADD", "SCARD", "SDIFF", "SDIFFSTORE", "SET", "SETEX",
		"SETBIT", "SETNX", "SETRANGE", "SINTER", "SINTERSTORE", "SISMEMBER",
		"SMEMBERS", "SMISMEMBER", "SMOVE", "SPOP", "SRANDMEMBER", "SREM",
		"SORT", "SORT_RO", "SSCAN", "STRLEN", "SUNION", "SUNIONSTORE", "TOUCH", "TTL", "TYPE",
		"UNLINK", "WATCH", "XACK", "XADD", "XAUTOCLAIM", "XCLAIM", "XDEL", "XLEN",
		"XPENDING", "XRANGE", "XREVRANGE", "XSETID", "XTRIM",
		"GEOADD", "GEODIST", "GEOHASH", "GEOPOS", "GEOSEARCH", "GEOSEARCHSTORE",
		"GEORADIUS", "GEORADIUSBYMEMBER", "ZADD", "ZCARD",
		"ZCOUNT", "ZINCRBY", "ZLEXCOUNT", "ZPOPMAX", "ZPOPMIN",
		"ZMSCORE", "ZRANDMEMBER", "ZRANGE", "ZRANGEBYLEX", "ZRANGEBYSCORE",
		"ZRANGESTORE", "ZRANK", "ZREM", "ZREMRANGEBYLEX",
		"ZREMRANGEBYRANK", "ZREMRANGEBYSCORE", "ZREVRANGE", "ZREVRANK",
		"ZSCAN", "ZSCORE":
		return true
	default:
		return false
	}
}
