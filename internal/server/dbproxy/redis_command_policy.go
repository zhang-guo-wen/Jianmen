package dbproxy

import "strings"

var allowedRedisPostAuthCommands = map[string]struct{}{
	"APPEND": {}, "ASKING": {}, "BITCOUNT": {}, "BITFIELD": {}, "BITFIELD_RO": {},
	"BITOP": {}, "BITPOS": {}, "BLMOVE": {}, "BLMPOP": {}, "BLPOP": {},
	"BRPOP": {}, "BRPOPLPUSH": {}, "BZPOPMAX": {}, "BZPOPMIN": {}, "COPY": {},
	"DBSIZE": {}, "DECR": {}, "DECRBY": {}, "DEL": {}, "DISCARD": {},
	"DUMP": {}, "ECHO": {}, "EVAL": {}, "EVALSHA": {}, "EXEC": {},
	"EXISTS": {}, "EXPIRE": {}, "EXPIREAT": {}, "EXPIRETIME": {}, "FCALL": {},
	"FCALL_RO": {}, "GEOADD": {}, "GEODIST": {}, "GEOHASH": {}, "GEOPOS": {},
	"GEOSEARCH": {}, "GEOSEARCHSTORE": {}, "GEORADIUS": {}, "GEORADIUSBYMEMBER": {},
	"GET": {}, "GETBIT": {}, "GETDEL": {}, "GETEX": {}, "GETRANGE": {},
	"GETSET": {}, "HDEL": {}, "HEXISTS": {}, "HEXPIRE": {}, "HEXPIREAT": {},
	"HEXPIRETIME": {}, "HGET": {}, "HGETALL": {}, "HGETDEL": {}, "HGETEX": {},
	"HINCRBY": {}, "HINCRBYFLOAT": {}, "HKEYS": {}, "HLEN": {}, "HMGET": {},
	"HMSET": {}, "HPERSIST": {}, "HPEXPIRE": {}, "HPEXPIREAT": {}, "HPEXPIRETIME": {},
	"HPTTL": {}, "HRANDFIELD": {}, "HSCAN": {}, "HSET": {}, "HSETEX": {},
	"HSETNX": {}, "HSTRLEN": {}, "HTTL": {}, "HVALS": {}, "INCR": {},
	"INCRBY": {}, "INCRBYFLOAT": {}, "INFO": {}, "KEYS": {}, "LASTSAVE": {},
	"LCS": {}, "LINDEX": {}, "LINSERT": {}, "LLEN": {}, "LMOVE": {},
	"LMPOP": {}, "LPOP": {}, "LPOS": {}, "LPUSH": {}, "LPUSHX": {},
	"LRANGE": {}, "LREM": {}, "LSET": {}, "LTRIM": {}, "MGET": {},
	"MOVE": {}, "MSET": {}, "MSETNX": {}, "MULTI": {}, "OBJECT": {},
	"PERSIST": {}, "PEXPIRE": {}, "PEXPIREAT": {}, "PEXPIRETIME": {}, "PFADD": {},
	"PFCOUNT": {}, "PFMERGE": {}, "PING": {}, "PSETEX": {}, "PTTL": {},
	"PUBLISH": {}, "QUIT": {}, "RANDOMKEY": {}, "READONLY": {}, "READWRITE": {},
	"RENAME": {}, "RENAMENX": {}, "RESTORE": {}, "RPOP": {}, "RPOPLPUSH": {},
	"RPUSH": {}, "RPUSHX": {}, "SADD": {}, "SCAN": {}, "SCARD": {},
	"SDIFF": {}, "SDIFFSTORE": {}, "SET": {}, "SETBIT": {}, "SETEX": {},
	"SETNX": {}, "SETRANGE": {}, "SINTER": {}, "SINTERCARD": {}, "SINTERSTORE": {},
	"SISMEMBER": {}, "SMEMBERS": {}, "SMISMEMBER": {}, "SMOVE": {}, "SORT": {},
	"SORT_RO": {}, "SPOP": {}, "SRANDMEMBER": {}, "SREM": {}, "SSCAN": {},
	"STRLEN": {}, "SUNION": {}, "SUNIONSTORE": {}, "TIME": {}, "TOUCH": {},
	"TTL": {}, "TYPE": {}, "UNLINK": {}, "UNWATCH": {}, "WAIT": {},
	"WATCH": {}, "XACK": {}, "XADD": {}, "XAUTOCLAIM": {}, "XCLAIM": {},
	"XDEL": {}, "XGROUP": {}, "XINFO": {}, "XLEN": {}, "XPENDING": {},
	"XRANGE": {}, "XREAD": {}, "XREADGROUP": {}, "XREVRANGE": {}, "XSETID": {},
	"XTRIM": {}, "ZADD": {}, "ZCARD": {}, "ZCOUNT": {}, "ZDIFF": {},
	"ZDIFFSTORE": {}, "ZINCRBY": {}, "ZINTER": {}, "ZINTERCARD": {}, "ZINTERSTORE": {},
	"ZLEXCOUNT": {}, "ZMPOP": {}, "ZMSCORE": {}, "ZPOPMAX": {}, "ZPOPMIN": {},
	"ZRANDMEMBER": {}, "ZRANGE": {}, "ZRANGEBYLEX": {}, "ZRANGEBYSCORE": {},
	"ZRANGESTORE": {}, "ZRANK": {}, "ZREM": {}, "ZREMRANGEBYLEX": {},
	"ZREMRANGEBYRANK": {}, "ZREMRANGEBYSCORE": {}, "ZREVRANGE": {}, "ZREVRANK": {},
	"ZSCAN": {}, "ZSCORE": {}, "ZUNION": {}, "ZUNIONSTORE": {},
	"COMMAND": {}, "SELECT": {}, "HELLO": {}, "CLIENT": {},
	"SUBSCRIBE": {}, "PSUBSCRIBE": {}, "SSUBSCRIBE": {},
	"UNSUBSCRIBE": {}, "PUNSUBSCRIBE": {}, "SUNSUBSCRIBE": {},
}

func isAllowedRedisPostAuthCommand(command string) bool {
	_, allowed := allowedRedisPostAuthCommands[command]
	return allowed && redisCommandHasAuditSpec(command)
}

func isAllowedRedisPostAuthCommandArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	command := upperRedisCommand(args[0])
	if !isAllowedRedisPostAuthCommand(command) {
		return false
	}
	switch command {
	case "HELLO":
		if len(args) != 2 && len(args) != 4 {
			return false
		}
		if args[1] != "2" && args[1] != "3" {
			return false
		}
		return len(args) == 2 || (upperRedisCommand(args[2]) == "SETNAME" && args[3] != "")
	case "CLIENT":
		return isAllowedRedisClientCommand(args)
	case "SUBSCRIBE", "PSUBSCRIBE", "SSUBSCRIBE":
		return len(args) >= 2
	default:
		return true
	}
}

func isAllowedRedisClientCommand(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch upperRedisCommand(args[1]) {
	case "ID", "GETNAME", "INFO":
		return len(args) == 2
	case "SETNAME":
		return len(args) == 3 && args[2] != ""
	case "SETINFO":
		if len(args) != 4 {
			return false
		}
		option := strings.ToUpper(args[2])
		return option == "LIB-NAME" || option == "LIB-VER"
	default:
		return false
	}
}

func upperRedisCommand(value string) string {
	if !validRedisCommandName(value) {
		return ""
	}
	return strings.ToUpper(value)
}

func redisCommandHasAuditSpec(command string) bool {
	if redisAuditNoKey(command) || redisAuditHasFirstKey(command) {
		return true
	}
	switch command {
	case "BITOP", "BLMPOP", "EVAL", "EVALSHA", "FCALL", "FCALL_RO", "LMPOP",
		"OBJECT", "SINTERCARD", "XGROUP", "XINFO", "XREAD", "XREADGROUP", "ZDIFF", "ZDIFFSTORE", "ZINTER",
		"ZINTERCARD", "ZINTERSTORE", "ZMPOP", "ZUNION", "ZUNIONSTORE":
		return true
	default:
		return false
	}
}
