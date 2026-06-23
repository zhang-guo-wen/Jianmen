package dbproxy

import (
	"bytes"
	"encoding/binary"
	"strings"
)

type queryObserver interface {
	ObserveClientBytes(data []byte)
}

type querySink interface {
	RecordQuery(sql string, detail map[string]any)
}

func newQueryObserver(protocol string, sink querySink) queryObserver {
	switch protocol {
	case "mysql":
		return &mysqlObserver{sink: sink}
	case "postgres":
		return &postgresObserver{sink: sink}
	default:
		return noopObserver{}
	}
}

type noopObserver struct{}

func (noopObserver) ObserveClientBytes(_ []byte) {}

type mysqlObserver struct {
	sink querySink
	buf  []byte
}

func (o *mysqlObserver) ObserveClientBytes(data []byte) {
	if len(data) == 0 {
		return
	}
	o.buf = append(o.buf, data...)
	for {
		if len(o.buf) < 4 {
			return
		}
		payloadLen := int(o.buf[0]) | int(o.buf[1])<<8 | int(o.buf[2])<<16
		total := 4 + payloadLen
		if payloadLen < 0 || total < 4 {
			o.buf = nil
			return
		}
		if len(o.buf) < total {
			return
		}
		seq := o.buf[3]
		payload := o.buf[4:total]
		o.handlePacket(seq, payload)
		o.buf = o.buf[total:]
	}
}

func (o *mysqlObserver) handlePacket(seq byte, payload []byte) {
	if len(payload) == 0 || o.sink == nil {
		return
	}
	cmd := payload[0]
	switch cmd {
	case 0x03:
		o.sink.RecordQuery(string(payload[1:]), map[string]any{
			"protocol": "mysql",
			"command":  "COM_QUERY",
			"seq":      seq,
		})
	case 0x16:
		o.sink.RecordQuery(string(payload[1:]), map[string]any{
			"protocol": "mysql",
			"command":  "COM_STMT_PREPARE",
			"seq":      seq,
		})
	}
}

type postgresObserver struct {
	sink        querySink
	buf         []byte
	startupDone bool
	disabled    bool
}

func (o *postgresObserver) ObserveClientBytes(data []byte) {
	if len(data) == 0 || o.disabled {
		return
	}
	o.buf = append(o.buf, data...)
	for {
		if !o.startupDone {
			if !o.consumeStartup() {
				return
			}
			continue
		}
		if len(o.buf) < 5 {
			return
		}
		typ := o.buf[0]
		msgLen := int(binary.BigEndian.Uint32(o.buf[1:5]))
		if msgLen < 4 || msgLen > 128*1024*1024 {
			o.disabled = true
			o.buf = nil
			return
		}
		total := 1 + msgLen
		if len(o.buf) < total {
			return
		}
		payload := o.buf[5:total]
		o.handleMessage(typ, payload)
		o.buf = o.buf[total:]
	}
}

func (o *postgresObserver) consumeStartup() bool {
	if len(o.buf) < 4 {
		return false
	}
	msgLen := int(binary.BigEndian.Uint32(o.buf[:4]))
	if msgLen < 8 || msgLen > 128*1024*1024 {
		o.disabled = true
		o.buf = nil
		return false
	}
	if len(o.buf) < msgLen {
		return false
	}
	code := binary.BigEndian.Uint32(o.buf[4:8])
	o.buf = o.buf[msgLen:]
	switch code {
	case 80877103, 80877104:
		return true
	default:
		o.startupDone = true
		return true
	}
}

func (o *postgresObserver) handleMessage(typ byte, payload []byte) {
	if o.sink == nil {
		return
	}
	switch typ {
	case 'Q':
		o.sink.RecordQuery(trimCString(payload), map[string]any{
			"protocol": "postgres",
			"message":  "Query",
		})
	case 'P':
		name, rest := splitCString(payload)
		sql := trimCString(rest)
		if sql != "" {
			o.sink.RecordQuery(sql, map[string]any{
				"protocol":       "postgres",
				"message":        "Parse",
				"statement_name": name,
			})
		}
	}
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
