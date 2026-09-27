package dbproxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	postgresStreamAuditPreviewBytes = 64 * 1024
	// Reserve room for the preview plus transient copies made by redaction.
	postgresStreamAuditReservationBytes = 8 * postgresStreamAuditPreviewBytes
	postgresStreamNameBytes             = 4 * 1024
	postgresAuditStateBytes             = 2 * 1024 * 1024
)

func (o *postgresObserver) auditStateLimit() int {
	limit := normalizeMaxClientMessageBytes(o.maxClientMessageBytes)
	if limit > postgresAuditStateBytes {
		return postgresAuditStateBytes
	}
	return limit
}

type postgresClientFrameStream struct {
	messageType  byte
	remaining    int
	forward      bool
	reject       *queryDecision
	parser       *postgresStreamParser
	frameCapture databaseAuditCapture
	lease        *observerMemoryLease
}

func newPostgresClientFrameStream(
	messageType byte,
	payloadBytes int,
	forward bool,
	reject *queryDecision,
	budget *observerMemoryBudget,
	sink querySink,
) *postgresClientFrameStream {
	stream := &postgresClientFrameStream{
		messageType: messageType,
		remaining:   payloadBytes,
		forward:     forward,
		reject:      reject,
	}
	if !forward || (messageType != 'Q' && messageType != 'P' && messageType != 'B') {
		return stream
	}
	previewBytes, redact := databaseAuditPolicy(sink, postgresStreamAuditPreviewBytes)
	if budget != nil {
		reservation := int64(previewBytes) * 8
		stream.lease = budget.tryAcquire(reservation)
		if stream.lease == nil {
			previewBytes = 0
		}
	}
	var capture databaseAuditCapture
	if artifactSink, ok := sink.(databaseAuditArtifactSink); ok {
		kind := databaseAuditArtifactSQL
		if messageType == 'B' {
			kind = databaseAuditArtifactParams
		}
		var err error
		capture, err = artifactSink.BeginDatabaseAuditCapture(kind)
		if err != nil {
			stream.forward = false
			stream.reject = newObserverFatalDecision(
				observerErrorAuditFailure,
				"database full audit recording failed",
			)
			return stream
		}
	}
	stream.parser = newPostgresStreamParser(messageType, previewBytes, redact)
	if messageType == 'B' {
		stream.frameCapture = capture
	} else {
		stream.parser.capture = capture
	}
	return stream
}

func (s *postgresClientFrameStream) close() {
	if s == nil {
		return
	}
	if s.parser != nil {
		s.parser.abortCapture()
		s.parser = nil
	}
	if s.frameCapture != nil {
		s.frameCapture.Abort()
		s.frameCapture = nil
	}
	if s.lease != nil {
		s.lease.release()
		s.lease = nil
	}
}

func (o *postgresObserver) consumePostgresClientFrameStream(data []byte) (
	forward []byte,
	consumed int,
	decision *queryDecision,
) {
	stream := o.clientStream
	if stream == nil || len(data) == 0 {
		return nil, 0, nil
	}
	consumed = stream.remaining
	if consumed > len(data) {
		consumed = len(data)
	}
	chunk := data[:consumed]
	stream.remaining -= consumed
	if stream.frameCapture != nil {
		if err := stream.frameCapture.Write(chunk); err != nil {
			stream.frameCapture.Abort()
			stream.frameCapture = nil
			stream.forward = false
			stream.reject = newObserverFatalDecision(
				observerErrorAuditFailure,
				"database parameter audit recording failed",
			)
		}
	}
	if stream.parser != nil && stream.reject == nil {
		if err := stream.parser.consume(chunk, stream.remaining == 0); err != nil {
			stream.parser.abortCapture()
			stream.parser = nil
			if stream.frameCapture != nil {
				stream.frameCapture.Abort()
				stream.frameCapture = nil
			}
			stream.forward = false
			errorCode := observerErrorProtocol
			if errors.Is(err, errDatabaseAuditArtifactWrite) {
				errorCode = observerErrorAuditFailure
			}
			stream.reject = newObserverFatalDecision(errorCode, err.Error())
		}
	}
	if stream.remaining > 0 {
		if stream.forward {
			return chunk, consumed, nil
		}
		return nil, consumed, nil
	}

	o.clientStream = nil
	defer stream.close()
	if stream.reject != nil {
		return nil, consumed, o.failDecision(stream.reject)
	}
	if stream.frameCapture != nil {
		artifact, err := stream.frameCapture.Complete()
		stream.frameCapture = nil
		if err != nil {
			return nil, consumed, o.fail(
				observerErrorAuditFailure,
				"database parameter audit recording failed",
			)
		}
		if stream.parser != nil {
			stream.parser.parameterArtifact = artifact
		}
	}
	if stream.parser != nil {
		if decision := stream.parser.finish(o); decision != nil {
			return nil, consumed, decision
		}
	}
	if stream.forward {
		return chunk, consumed, nil
	}
	return nil, consumed, nil
}

type postgresStreamParser struct {
	messageType       byte
	phase             uint8
	name              []byte
	statement         []byte
	preview           []byte
	previewMax        int
	previewCaptureMax int
	sqlBytes          int64
	redact            bool
	capture           databaseAuditCapture
	artifact          databaseAuditArtifact
	parameterArtifact databaseAuditArtifact
	countBuf          [2]byte
	countBytes        int
	typeBytes         int
	bind              postgresBindStreamValidator
}

func newPostgresStreamParser(messageType byte, previewBytes int, redact bool) *postgresStreamParser {
	captureBytes := previewBytes
	if redact && captureBytes > 0 {
		captureBytes += 256
	}
	return &postgresStreamParser{
		messageType:       messageType,
		previewMax:        previewBytes,
		previewCaptureMax: captureBytes,
		redact:            redact,
	}
}

func (p *postgresStreamParser) consume(data []byte, final bool) error {
	switch p.messageType {
	case 'Q':
		return p.consumeQuery(data, final)
	case 'P':
		return p.consumeParse(data)
	case 'B':
		return p.consumeBind(data)
	default:
		return fmt.Errorf("unsupported streamed PostgreSQL message %q", p.messageType)
	}
}

func (p *postgresStreamParser) finish(o *postgresObserver) *queryDecision {
	switch p.messageType {
	case 'Q':
		if p.phase != 1 {
			return o.fail(observerErrorProtocol, "malformed streamed PostgreSQL Query")
		}
		if decision := p.finishSQLCapture(o); decision != nil {
			return decision
		}
		if o.sink == nil {
			return nil
		}
		return o.startPostgresSimpleQuery(p.audit())
	case 'P':
		if p.phase != 4 {
			return o.fail(observerErrorProtocol, "malformed streamed PostgreSQL Parse")
		}
		if decision := p.finishSQLCapture(o); decision != nil {
			return decision
		}
		if o.sink == nil {
			return nil
		}
		return o.observePostgresParseSummary(string(p.name), p.audit())
	case 'B':
		if p.phase != 2 || !p.bind.complete() {
			return o.fail(observerErrorProtocol, "malformed streamed PostgreSQL Bind")
		}
		if o.sink == nil {
			return nil
		}
		return o.observePostgresBindNames(
			string(p.name),
			string(p.statement),
			p.parameterArtifact,
		)
	default:
		return o.fail(observerErrorProtocol, "unsupported streamed PostgreSQL message")
	}
}

func (p *postgresStreamParser) finishSQLCapture(o *postgresObserver) *queryDecision {
	if p.capture == nil {
		return nil
	}
	artifact, err := p.capture.Complete()
	p.capture = nil
	if err != nil {
		return o.fail(observerErrorAuditFailure, "database full SQL audit recording failed")
	}
	p.artifact = artifact
	return nil
}

func (p *postgresStreamParser) abortCapture() {
	if p == nil || p.capture == nil {
		return
	}
	p.capture.Abort()
	p.capture = nil
}

func (p *postgresStreamParser) consumeQuery(data []byte, final bool) error {
	if p.phase != 0 {
		return fmt.Errorf("malformed streamed PostgreSQL Query")
	}
	if !final {
		if bytes.IndexByte(data, 0) >= 0 {
			return fmt.Errorf("malformed streamed PostgreSQL Query")
		}
		return p.appendSQL(data)
	}
	if len(data) == 0 || data[len(data)-1] != 0 || bytes.IndexByte(data[:len(data)-1], 0) >= 0 {
		return fmt.Errorf("malformed streamed PostgreSQL Query")
	}
	if err := p.appendSQL(data[:len(data)-1]); err != nil {
		return err
	}
	p.phase = 1
	return nil
}

func (p *postgresStreamParser) consumeParse(data []byte) error {
	for len(data) > 0 {
		switch p.phase {
		case 0:
			value, rest, complete, err := appendPostgresStreamCString(p.name, data)
			if err != nil {
				return err
			}
			p.name = value
			data = rest
			if !complete {
				return nil
			}
			p.phase = 1
		case 1:
			index := bytes.IndexByte(data, 0)
			if index < 0 {
				return p.appendSQL(data)
			}
			if err := p.appendSQL(data[:index]); err != nil {
				return err
			}
			data = data[index+1:]
			p.phase = 2
		case 2:
			needed := len(p.countBuf) - p.countBytes
			if needed > len(data) {
				needed = len(data)
			}
			copy(p.countBuf[p.countBytes:], data[:needed])
			p.countBytes += needed
			data = data[needed:]
			if p.countBytes < len(p.countBuf) {
				return nil
			}
			p.typeBytes = int(binary.BigEndian.Uint16(p.countBuf[:])) * 4
			if p.typeBytes == 0 {
				p.phase = 4
			} else {
				p.phase = 3
			}
		case 3:
			consume := p.typeBytes
			if consume > len(data) {
				consume = len(data)
			}
			p.typeBytes -= consume
			data = data[consume:]
			if p.typeBytes == 0 {
				p.phase = 4
			}
		case 4:
			return fmt.Errorf("malformed streamed PostgreSQL Parse")
		}
	}
	return nil
}

func (p *postgresStreamParser) consumeBind(data []byte) error {
	for len(data) > 0 {
		switch p.phase {
		case 0:
			value, rest, complete, err := appendPostgresStreamCString(p.name, data)
			if err != nil {
				return err
			}
			p.name = value
			data = rest
			if !complete {
				return nil
			}
			p.phase = 1
		case 1:
			value, rest, complete, err := appendPostgresStreamCString(p.statement, data)
			if err != nil {
				return err
			}
			p.statement = value
			data = rest
			if !complete {
				return nil
			}
			p.phase = 2
		case 2:
			if err := p.bind.consume(data); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

func appendPostgresStreamCString(current, data []byte) ([]byte, []byte, bool, error) {
	index := bytes.IndexByte(data, 0)
	part := data
	complete := index >= 0
	if complete {
		part = data[:index]
	}
	if len(part) > postgresStreamNameBytes-len(current) {
		return nil, nil, false, fmt.Errorf("PostgreSQL streamed object name exceeds %d bytes", postgresStreamNameBytes)
	}
	current = append(current, part...)
	if !complete {
		return current, nil, false, nil
	}
	return current, data[index+1:], true, nil
}

func (p *postgresStreamParser) appendSQL(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if p.capture != nil {
		if err := p.capture.Write(data); err != nil {
			return fmt.Errorf("%w: %v", errDatabaseAuditArtifactWrite, err)
		}
	}
	p.sqlBytes += int64(len(data))
	remaining := p.previewCaptureMax - len(p.preview)
	if remaining <= 0 {
		return nil
	}
	if len(data) > remaining {
		data = data[:remaining]
	}
	p.preview = append(p.preview, data...)
	return nil
}

func (p *postgresStreamParser) audit() databaseSQLAudit {
	text := string(p.preview)
	adjusted := false
	if p.redact {
		text, adjusted = redactDatabaseSQLWithLimit(text, p.previewMax)
	} else {
		text, adjusted = normalizeAuditSQLUTF8(text, p.previewMax)
	}
	if p.previewMax == 0 && p.sqlBytes > 0 {
		text = "/* SQL audit preview omitted: global memory budget exhausted */"
	}
	return databaseSQLAudit{
		text:          text,
		originalBytes: p.sqlBytes,
		truncated:     adjusted || p.sqlBytes > int64(len(p.preview)),
		artifact:      p.artifact,
	}
}

const (
	postgresBindFormatCount uint8 = iota
	postgresBindFormatCode
	postgresBindParameterCount
	postgresBindParameterLength
	postgresBindParameterValue
	postgresBindResultCount
	postgresBindResultCode
	postgresBindComplete
)

type postgresBindStreamValidator struct {
	phase               uint8
	scratch             [4]byte
	scratchBytes        int
	formatCount         int
	formatRemaining     int
	parameterRemaining  int
	parameterValueBytes int64
	resultRemaining     int
}

func (v *postgresBindStreamValidator) consume(data []byte) error {
	for len(data) > 0 {
		switch v.phase {
		case postgresBindFormatCount:
			value, rest, complete := v.readUint16(data)
			data = rest
			if !complete {
				return nil
			}
			v.formatCount = int(value)
			v.formatRemaining = int(value)
			if v.formatRemaining == 0 {
				v.phase = postgresBindParameterCount
			} else {
				v.phase = postgresBindFormatCode
			}
		case postgresBindFormatCode:
			value, rest, complete := v.readUint16(data)
			data = rest
			if !complete {
				return nil
			}
			if value > 1 {
				return fmt.Errorf("malformed streamed PostgreSQL Bind format code")
			}
			v.formatRemaining--
			if v.formatRemaining == 0 {
				v.phase = postgresBindParameterCount
			}
		case postgresBindParameterCount:
			value, rest, complete := v.readUint16(data)
			data = rest
			if !complete {
				return nil
			}
			v.parameterRemaining = int(value)
			if v.formatCount != 0 && v.formatCount != 1 && v.formatCount != v.parameterRemaining {
				return fmt.Errorf("malformed streamed PostgreSQL Bind format count")
			}
			if v.parameterRemaining == 0 {
				v.phase = postgresBindResultCount
			} else {
				v.phase = postgresBindParameterLength
			}
		case postgresBindParameterLength:
			value, rest, complete := v.readInt32(data)
			data = rest
			if !complete {
				return nil
			}
			if value < -1 {
				return fmt.Errorf("malformed streamed PostgreSQL Bind parameter length")
			}
			if value <= 0 {
				v.finishParameter()
				continue
			}
			v.parameterValueBytes = int64(value)
			v.phase = postgresBindParameterValue
		case postgresBindParameterValue:
			consume := int64(len(data))
			if consume > v.parameterValueBytes {
				consume = v.parameterValueBytes
			}
			v.parameterValueBytes -= consume
			data = data[int(consume):]
			if v.parameterValueBytes == 0 {
				v.finishParameter()
			}
		case postgresBindResultCount:
			value, rest, complete := v.readUint16(data)
			data = rest
			if !complete {
				return nil
			}
			v.resultRemaining = int(value)
			if v.resultRemaining == 0 {
				v.phase = postgresBindComplete
			} else {
				v.phase = postgresBindResultCode
			}
		case postgresBindResultCode:
			value, rest, complete := v.readUint16(data)
			data = rest
			if !complete {
				return nil
			}
			if value > 1 {
				return fmt.Errorf("malformed streamed PostgreSQL Bind result format code")
			}
			v.resultRemaining--
			if v.resultRemaining == 0 {
				v.phase = postgresBindComplete
			}
		case postgresBindComplete:
			return fmt.Errorf("malformed streamed PostgreSQL Bind trailing data")
		}
	}
	return nil
}

func (v *postgresBindStreamValidator) finishParameter() {
	v.parameterRemaining--
	if v.parameterRemaining == 0 {
		v.phase = postgresBindResultCount
	} else {
		v.phase = postgresBindParameterLength
	}
}

func (v *postgresBindStreamValidator) readUint16(data []byte) (uint16, []byte, bool) {
	needed := 2 - v.scratchBytes
	if needed > len(data) {
		needed = len(data)
	}
	copy(v.scratch[v.scratchBytes:], data[:needed])
	v.scratchBytes += needed
	data = data[needed:]
	if v.scratchBytes < 2 {
		return 0, data, false
	}
	value := binary.BigEndian.Uint16(v.scratch[:2])
	v.scratchBytes = 0
	return value, data, true
}

func (v *postgresBindStreamValidator) readInt32(data []byte) (int32, []byte, bool) {
	needed := 4 - v.scratchBytes
	if needed > len(data) {
		needed = len(data)
	}
	copy(v.scratch[v.scratchBytes:], data[:needed])
	v.scratchBytes += needed
	data = data[needed:]
	if v.scratchBytes < 4 {
		return 0, data, false
	}
	value := int32(binary.BigEndian.Uint32(v.scratch[:4]))
	v.scratchBytes = 0
	return value, data, true
}

func (v *postgresBindStreamValidator) complete() bool {
	return v.phase == postgresBindComplete && v.scratchBytes == 0
}
