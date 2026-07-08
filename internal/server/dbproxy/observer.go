package dbproxy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type queryObserver interface {
	ObserveClientBytes(data []byte) *queryDecision
	ObserveServerBytes(data []byte)
	ErrorResponse(decision queryDecision) []byte
}

type querySink interface {
	StartQuery(sql string, detail map[string]any) (queryRecord, queryDecision)
	FinishQuery(record queryRecord, finish queryFinish)
}

func newQueryObserver(protocol string, sink querySink) queryObserver {
	switch protocol {
	case "mysql":
		return &mysqlObserver{sink: sink}
	case "postgres":
		return &postgresObserver{sink: sink, startupDone: true}
	case "redis":
		return &redisObserver{sink: sink}
	default:
		return noopObserver{}
	}
}

type noopObserver struct{}

func (noopObserver) ObserveClientBytes(_ []byte) *queryDecision { return nil }
func (noopObserver) ObserveServerBytes(_ []byte)                {}
func (noopObserver) ErrorResponse(_ queryDecision) []byte       { return nil }

type mysqlObserver struct {
	sink      querySink
	clientBuf []byte
	serverBuf []byte
	pending   []queryRecord
}

func (o *mysqlObserver) ObserveClientBytes(data []byte) *queryDecision {
	if len(data) == 0 {
		return nil
	}
	o.clientBuf = append(o.clientBuf, data...)
	for {
		if len(o.clientBuf) < 4 {
			return nil
		}
		payloadLen := int(o.clientBuf[0]) | int(o.clientBuf[1])<<8 | int(o.clientBuf[2])<<16
		total := 4 + payloadLen
		if payloadLen < 0 || total < 4 {
			o.clientBuf = nil
			return nil
		}
		if len(o.clientBuf) < total {
			return nil
		}
		seq := o.clientBuf[3]
		payload := o.clientBuf[4:total]
		if decision := o.handleClientPacket(seq, payload); decision != nil && !decision.Allowed {
			o.clientBuf = nil
			return decision
		}
		o.clientBuf = o.clientBuf[total:]
	}
}

func (o *mysqlObserver) ObserveServerBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	o.serverBuf = append(o.serverBuf, data...)
	for {
		if len(o.serverBuf) < 4 {
			return
		}
		payloadLen := int(o.serverBuf[0]) | int(o.serverBuf[1])<<8 | int(o.serverBuf[2])<<16
		total := 4 + payloadLen
		if payloadLen < 0 || total < 4 {
			o.serverBuf = nil
			return
		}
		if len(o.serverBuf) < total {
			return
		}
		payload := o.serverBuf[4:total]
		o.handleServerPacket(payload)
		o.serverBuf = o.serverBuf[total:]
	}
}

func (o *mysqlObserver) ErrorResponse(decision queryDecision) []byte {
	message := decision.ErrorMessage
	if message == "" {
		message = "query denied by database proxy policy"
	}
	payload := make([]byte, 0, 9+len(message))
	payload = append(payload, 0xff)
	payload = append(payload, 0x84, 0x04)
	payload = append(payload, '#')
	payload = append(payload, []byte("HY000")...)
	payload = append(payload, []byte(message)...)
	return mysqlPacketWithSeq(1, payload)
}

func (o *mysqlObserver) handleClientPacket(seq byte, payload []byte) (decision *queryDecision) {
	defer func() {
		if r := recover(); r != nil {
			decision = nil // 防止 panic 导致连接卡死
		}
	}()
	if len(payload) == 0 || o.sink == nil {
		return nil
	}
	cmd := payload[0]
	switch cmd {
	case 0x03: // COM_QUERY
		record, decision := o.sink.StartQuery(string(payload[1:]), map[string]any{
			"protocol": "mysql",
			"command":  "COM_QUERY",
			"seq":      seq,
		})
		if !decision.Allowed {
			return &decision
		}
		o.pending = append(o.pending, record)
	case 0x16: // COM_STMT_PREPARE
		record, decision := o.sink.StartQuery(string(payload[1:]), map[string]any{
			"protocol": "mysql",
			"command":  "COM_STMT_PREPARE",
			"seq":      seq,
		})
		if !decision.Allowed {
			return &decision
		}
		o.pending = append(o.pending, record)
	case 0x17: // COM_STMT_EXECUTE
		stmtID := 0
		if len(payload) >= 5 {
			stmtID = int(binary.LittleEndian.Uint32(payload[1:5]))
		}
		record, decision := o.sink.StartQuery(fmt.Sprintf("EXECUTE stmt_id=%d", stmtID), map[string]any{
			"protocol": "mysql",
			"command":  "COM_STMT_EXECUTE",
			"stmt_id":  stmtID,
			"seq":      seq,
		})
		if !decision.Allowed {
			return &decision
		}
		o.pending = append(o.pending, record)
	}
	return nil
}

func (o *mysqlObserver) handleServerPacket(payload []byte) {
	defer func() { recover() }()
	if len(payload) == 0 || len(o.pending) == 0 || o.sink == nil {
		return
	}
	record := o.pending[0]
	o.pending = o.pending[1:]

	finish := queryFinish{Status: queryStatusSuccess}
	switch payload[0] {
	case 0xff:
		finish.Status = queryStatusError
		if len(payload) >= 3 {
			finish.ErrorCode = fmt.Sprintf("%d", binary.LittleEndian.Uint16(payload[1:3]))
		}
		finish.ErrorMessage = ParseMySQLErrorMessage(payload)
	case 0x00:
		if rows, _, ok := readLengthEncodedInt(payload[1:]); ok {
			value := int64(rows)
			finish.RowsAffected = &value
		}
	default:
		finish.Detail = map[string]any{
			"result": "result_set_started",
		}
	}
	o.sink.FinishQuery(record, finish)
}

type postgresObserver struct {
	sink        querySink
	clientBuf   []byte
	serverBuf   []byte
	startupDone bool
	disabled    bool
	pending     []queryRecord
}

func (o *postgresObserver) ObserveClientBytes(data []byte) *queryDecision {
	if len(data) == 0 || o.disabled {
		return nil
	}
	o.clientBuf = append(o.clientBuf, data...)
	for {
		if !o.startupDone {
			if !o.consumeStartup() {
				return nil
			}
			continue
		}
		if len(o.clientBuf) < 5 {
			return nil
		}
		typ := o.clientBuf[0]
		msgLen := int(binary.BigEndian.Uint32(o.clientBuf[1:5]))
		if msgLen < 4 || msgLen > 128*1024*1024 {
			o.disabled = true
			o.clientBuf = nil
			return nil
		}
		total := 1 + msgLen
		if len(o.clientBuf) < total {
			return nil
		}
		payload := o.clientBuf[5:total]
		if decision := o.handleClientMessage(typ, payload); decision != nil && !decision.Allowed {
			o.clientBuf = nil
			return decision
		}
		o.clientBuf = o.clientBuf[total:]
	}
}

func (o *postgresObserver) ObserveServerBytes(data []byte) {
	if len(data) == 0 || o.disabled {
		return
	}
	o.serverBuf = append(o.serverBuf, data...)
	for {
		if len(o.serverBuf) < 5 {
			return
		}
		typ := o.serverBuf[0]
		msgLen := int(binary.BigEndian.Uint32(o.serverBuf[1:5]))
		if msgLen < 4 || msgLen > 128*1024*1024 {
			o.disabled = true
			o.serverBuf = nil
			return
		}
		total := 1 + msgLen
		if len(o.serverBuf) < total {
			return
		}
		payload := o.serverBuf[5:total]
		o.handleServerMessage(typ, payload)
		o.serverBuf = o.serverBuf[total:]
	}
}

func (o *postgresObserver) ErrorResponse(decision queryDecision) []byte {
	message := decision.ErrorMessage
	if message == "" {
		message = "query denied by database proxy policy"
	}
	payload := []byte{'S'}
	payload = append(payload, []byte("ERROR")...)
	payload = append(payload, 0, 'C')
	payload = append(payload, []byte("42501")...)
	payload = append(payload, 0, 'M')
	payload = append(payload, []byte(message)...)
	payload = append(payload, 0, 0)
	return append(postgresMessageWithType('E', payload), postgresReadyForQuery()...)
}

func (o *postgresObserver) consumeStartup() bool {
	if len(o.clientBuf) < 4 {
		return false
	}
	msgLen := int(binary.BigEndian.Uint32(o.clientBuf[:4]))
	if msgLen < 8 || msgLen > 128*1024*1024 {
		o.disabled = true
		o.clientBuf = nil
		return false
	}
	if len(o.clientBuf) < msgLen {
		return false
	}
	code := binary.BigEndian.Uint32(o.clientBuf[4:8])
	o.clientBuf = o.clientBuf[msgLen:]
	switch code {
	case 80877103, 80877104:
		return true
	default:
		o.startupDone = true
		return true
	}
}

func (o *postgresObserver) handleClientMessage(typ byte, payload []byte) *queryDecision {
	if o.sink == nil {
		return nil
	}
	switch typ {
	case 'Q':
		record, decision := o.sink.StartQuery(trimCString(payload), map[string]any{
			"protocol": "postgres",
			"message":  "Query",
		})
		if !decision.Allowed {
			return &decision
		}
		o.pending = append(o.pending, record)
	case 'P':
		name, rest := splitCString(payload)
		sql := trimCString(rest)
		if sql != "" {
			record, decision := o.sink.StartQuery(sql, map[string]any{
				"protocol":       "postgres",
				"message":        "Parse",
				"statement_name": name,
			})
			if !decision.Allowed {
				return &decision
			}
			o.pending = append(o.pending, record)
		}
	}
	return nil
}

func (o *postgresObserver) handleServerMessage(typ byte, payload []byte) {
	if len(o.pending) == 0 || o.sink == nil {
		return
	}
	switch typ {
	case '1':
		o.finishNext(queryFinish{Status: queryStatusSuccess})
	case 'C':
		finish := queryFinish{Status: queryStatusSuccess}
		tag := trimCString(payload)
		finish.Detail = map[string]any{"command_tag": tag}
		if rows, ok := parsePostgresRowsFromCommandTag(tag); ok {
			finish.RowsAffected = &rows
		}
		o.finishNext(finish)
	case 'E':
		code, message := parsePostgresError(payload)
		o.finishNext(queryFinish{
			Status:       queryStatusError,
			ErrorCode:    code,
			ErrorMessage: message,
		})
	case 'Z':
		if len(o.pending) > 0 {
			o.finishNext(queryFinish{Status: queryStatusUnknown})
		}
	}
}

func (o *postgresObserver) finishNext(finish queryFinish) {
	record := o.pending[0]
	o.pending = o.pending[1:]
	o.sink.FinishQuery(record, finish)
}

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

func ParseMySQLErrorMessage(payload []byte) string {
	if len(payload) <= 3 {
		return ""
	}
	if len(payload) > 9 && payload[3] == '#' {
		return strings.TrimSpace(string(payload[9:]))
	}
	return strings.TrimSpace(string(payload[3:]))
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
		value = value*10 + int64(r-'0')
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

// --- Symbols moved from policy.go ---

type queryDecision struct {
	Allowed      bool
	Status       string
	ErrorCode    string
	ErrorMessage string
	Detail       map[string]any
}

func allowQuery() queryDecision {
	return queryDecision{Allowed: true}
}

func classifyQueryKind(sql string) string {
	sql = stripLeadingSQLTrivia(sql)
	if sql == "" {
		return "unknown"
	}
	for i, r := range sql {
		if !(unicode.IsLetter(r) || r == '_') {
			if i == 0 {
				return "unknown"
			}
			return strings.ToLower(sql[:i])
		}
	}
	return strings.ToLower(sql)
}

func stripLeadingSQLTrivia(sql string) string {
	for {
		sql = strings.TrimSpace(sql)
		switch {
		case strings.HasPrefix(sql, "--"):
			if index := strings.IndexByte(sql, '\n'); index >= 0 {
				sql = sql[index+1:]
				continue
			}
			return ""
		case strings.HasPrefix(sql, "#"):
			if index := strings.IndexByte(sql, '\n'); index >= 0 {
				sql = sql[index+1:]
				continue
			}
			return ""
		case strings.HasPrefix(sql, "/*"):
			if index := strings.Index(sql, "*/"); index >= 0 {
				sql = sql[index+2:]
				continue
			}
			return ""
		default:
			return sql
		}
	}
}

func mergeDetails(values ...map[string]any) map[string]any {
	var out map[string]any
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]any)
		}
		for k, v := range value {
			out[k] = v
		}
	}
	return out
}

type redisObserver struct {
	sink    querySink
	buf     []byte
	pending []queryRecord
}

func (o *redisObserver) ObserveClientBytes(data []byte) *queryDecision {
	if len(data) == 0 || o.sink == nil {
		return nil
	}
	o.buf = append(o.buf, data...)
	for {
		if len(o.buf) == 0 {
			return nil
		}
		// RESP 命令必须以 '*' 开头
		if o.buf[0] != '*' {
			// 非 RESP 命令，有可能在 AUTH 之后，但 RESP 都是 * 开头
			// 跳过无法识别的字节
			o.buf = nil
			return nil
		}
		// 找到第一个 \r\n 获取参数数量
		crlfIdx := indexCRLF(o.buf)
		if crlfIdx < 0 {
			// 等待更多数据
			if len(o.buf) > 64*1024 {
				o.buf = nil
			}
			return nil
		}
		count, err := strconv.Atoi(string(o.buf[1:crlfIdx]))
		if err != nil || count <= 0 || count > 256 {
			o.buf = nil
			return nil
		}
		pos := crlfIdx + 2
		args := make([]string, 0, count)
		valid := true
		for i := 0; i < count; i++ {
			if pos >= len(o.buf) || o.buf[pos] != '$' {
				valid = false
				break
			}
			crlf := indexCRLF(o.buf[pos:])
			if crlf < 0 {
				if len(o.buf) > 64*1024 {
					o.buf = nil
				}
				return nil
			}
			argLen, err := strconv.Atoi(string(o.buf[pos+1 : pos+crlf]))
			if err != nil || argLen < 0 || argLen > 512*1024*1024 {
				valid = false
				break
			}
			pos += crlf + 2
			dataEnd := pos + argLen + 2 // arg + \r\n
			if dataEnd > len(o.buf) {
				// 等待更多数据
				return nil
			}
			args = append(args, string(o.buf[pos:pos+argLen]))
			pos = dataEnd
		}
		if !valid {
			o.buf = nil
			return nil
		}

		// 记录命令（跳过 AUTH、PING、QUIT 等非数据命令）
		cmd := ""
		if len(args) > 0 {
			cmd = strings.ToUpper(args[0])
		}
		if shouldRecordRedisCommand(cmd) {
			sql := strings.Join(args, " ")
			record, decision := o.sink.StartQuery(sql, map[string]any{
				"protocol": "redis",
				"command":  cmd,
			})
			if !decision.Allowed {
				return &decision
			}
			o.pending = append(o.pending, record)
		}
		o.buf = o.buf[pos:]
	}
}

func (o *redisObserver) ObserveServerBytes(data []byte) {
	if len(data) == 0 || len(o.pending) == 0 || o.sink == nil {
		return
	}
	// Match RESP response prefixes
	if len(data) > 0 {
		var finish queryFinish
		switch data[0] {
		case '+':
			finish = queryFinish{Status: queryStatusSuccess}
		case '-':
			finish = queryFinish{Status: queryStatusError}
			errMsg := strings.TrimSpace(string(data[1:]))
			if idx := strings.IndexAny(errMsg, "\r\n"); idx >= 0 {
				errMsg = errMsg[:idx]
			}
			// Parse Redis error: "-ERR message" or "-WRONGTYPE ..."
			if strings.HasPrefix(errMsg, "ERR ") {
				errMsg = errMsg[4:]
			}
			finish.ErrorMessage = errMsg
		case ':':
			finish = queryFinish{Status: queryStatusSuccess}
		case '$':
			finish = queryFinish{Status: queryStatusSuccess}
		case '*':
			finish = queryFinish{Status: queryStatusSuccess}
		default:
			return // not a complete response header we recognize
		}
		record := o.pending[0]
		o.pending = o.pending[1:]
		o.sink.FinishQuery(record, finish)
	}
}

func (o *redisObserver) ErrorResponse(decision queryDecision) []byte {
	message := decision.ErrorMessage
	if message == "" {
		message = "command denied by database proxy policy"
	}
	return []byte(fmt.Sprintf("-ERR %s\r\n", message))
}

// shouldRecordRedisCommand returns true for data commands (skips AUTH, PING, QUIT, etc.)
func shouldRecordRedisCommand(cmd string) bool {
	switch cmd {
	case "AUTH", "PING", "QUIT", "ECHO", "SELECT", "HELLO", "COMMAND":
		return false
	default:
		return true
	}
}

// indexCRLF returns the index of first \r\n in b, or -1
func indexCRLF(b []byte) int {
	for i := 0; i < len(b)-1; i++ {
		if b[i] == '\r' && b[i+1] == '\n' {
			return i
		}
	}
	return -1
}
