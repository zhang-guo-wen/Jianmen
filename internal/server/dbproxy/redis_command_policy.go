package dbproxy

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
	"COMMAND": {}, "SELECT": {},
}

func isAllowedRedisPostAuthCommand(command string) bool {
	_, allowed := allowedRedisPostAuthCommands[command]
	return allowed && redisCommandHasAuditSpec(command)
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
