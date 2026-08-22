package dbproxy

import (
	"bytes"
	"errors"
)

const (
	databaseSQLRedactNormal uint8 = iota
	databaseSQLRedactDash
	databaseSQLRedactSlash
	databaseSQLRedactQuoted
	databaseSQLRedactQuotePending
	databaseSQLRedactLineComment
	databaseSQLRedactBlockComment
	databaseSQLRedactBlockStar
	databaseSQLRedactDollarCandidate
	databaseSQLRedactDollarBody
	databaseSQLRedactNumeric
	databaseSQLRedactIdentifier
	databaseSQLRedactSensitiveSeparator
	databaseSQLRedactSensitiveB
	databaseSQLRedactSensitiveValueStart
	databaseSQLRedactSensitiveValue
)

// databaseSQLStreamRedactor removes literal values without retaining the full SQL.
// It deliberately treats both quote styles as sensitive to preserve existing behavior.
type databaseSQLStreamRedactor struct {
	write       func([]byte) error
	state       uint8
	quote       byte
	escaped     bool
	previous    byte
	dollarTag   []byte
	dollarMatch int
	identifier  []byte
}

func newDatabaseSQLStreamRedactor(write func([]byte) error) *databaseSQLStreamRedactor {
	return &databaseSQLStreamRedactor{write: write}
}

func (r *databaseSQLStreamRedactor) Write(data []byte) error {
	if r == nil || len(data) == 0 {
		return nil
	}
	if r.write == nil {
		return errors.New("database SQL redactor output is unavailable")
	}
	var output bytes.Buffer
	output.Grow(len(data))
	for index := 0; index < len(data); {
		value := data[index]
		reprocess := false
		switch r.state {
		case databaseSQLRedactNormal:
			switch {
			case value == '\'' || value == '"':
				r.quote = value
				r.escaped = false
				output.WriteByte(value)
				output.WriteString(sqlAuditRedacted)
				r.state = databaseSQLRedactQuoted
			case value == '-':
				r.state = databaseSQLRedactDash
			case value == '/':
				r.state = databaseSQLRedactSlash
			case value == '#':
				output.WriteString("# ")
				output.WriteString(sqlAuditRedacted)
				r.state = databaseSQLRedactLineComment
			case isSQLIdentifierStart(value):
				r.identifier = append(r.identifier[:0], value)
				r.state = databaseSQLRedactIdentifier
			case value == '$':
				r.dollarTag = append(r.dollarTag[:0], '$')
				r.state = databaseSQLRedactDollarCandidate
			case startsDatabaseSQLStreamNumber(r.previous, value):
				output.WriteString(sqlAuditRedacted)
				r.state = databaseSQLRedactNumeric
				reprocess = true
			default:
				output.WriteByte(value)
				r.previous = value
			}
		case databaseSQLRedactDash:
			if value == '-' {
				output.WriteString("-- ")
				output.WriteString(sqlAuditRedacted)
				r.state = databaseSQLRedactLineComment
			} else {
				output.WriteByte('-')
				r.previous = '-'
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		case databaseSQLRedactSlash:
			if value == '*' {
				output.WriteString("/* ")
				output.WriteString(sqlAuditRedacted)
				output.WriteString(" */")
				r.state = databaseSQLRedactBlockComment
			} else {
				output.WriteByte('/')
				r.previous = '/'
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		case databaseSQLRedactQuoted:
			if r.escaped {
				r.escaped = false
			} else if value == '\\' {
				r.escaped = true
			} else if value == r.quote {
				r.state = databaseSQLRedactQuotePending
			}
		case databaseSQLRedactQuotePending:
			if value == r.quote {
				r.state = databaseSQLRedactQuoted
			} else {
				output.WriteByte(r.quote)
				r.previous = r.quote
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		case databaseSQLRedactLineComment:
			if value == '\n' {
				output.WriteByte('\n')
				r.previous = '\n'
				r.state = databaseSQLRedactNormal
			}
		case databaseSQLRedactBlockComment:
			if value == '*' {
				r.state = databaseSQLRedactBlockStar
			}
		case databaseSQLRedactBlockStar:
			if value == '/' {
				r.previous = '/'
				r.state = databaseSQLRedactNormal
			} else if value != '*' {
				r.state = databaseSQLRedactBlockComment
			}
		case databaseSQLRedactDollarCandidate:
			switch {
			case value == '$':
				r.dollarTag = append(r.dollarTag, '$')
				output.Write(r.dollarTag)
				output.WriteString(sqlAuditRedacted)
				r.dollarMatch = 0
				r.state = databaseSQLRedactDollarBody
			case len(r.dollarTag) == 1 && isSQLIdentifierStart(value),
				len(r.dollarTag) > 1 && isSQLIdentifierPart(value):
				if len(r.dollarTag) >= postgresStreamNameBytes {
					output.Write(r.dollarTag)
					r.previous = r.dollarTag[len(r.dollarTag)-1]
					r.dollarTag = r.dollarTag[:0]
					r.state = databaseSQLRedactNormal
					reprocess = true
				} else {
					r.dollarTag = append(r.dollarTag, value)
				}
			default:
				output.Write(r.dollarTag)
				r.previous = r.dollarTag[len(r.dollarTag)-1]
				r.dollarTag = r.dollarTag[:0]
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		case databaseSQLRedactDollarBody:
			if value == r.dollarTag[r.dollarMatch] {
				r.dollarMatch++
				if r.dollarMatch == len(r.dollarTag) {
					output.Write(r.dollarTag)
					r.previous = '$'
					r.dollarMatch = 0
					r.dollarTag = r.dollarTag[:0]
					r.state = databaseSQLRedactNormal
				}
			} else if value == '$' {
				r.dollarMatch = 1
			} else {
				r.dollarMatch = 0
			}
		case databaseSQLRedactNumeric:
			if !isDatabaseSQLStreamNumberPart(value) {
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		case databaseSQLRedactIdentifier:
			if isSQLIdentifierPart(value) && len(r.identifier) < 256 {
				r.identifier = append(r.identifier, value)
			} else {
				sensitive := isDatabaseSQLSensitiveAssignmentKey(r.identifier)
				output.Write(r.identifier)
				r.previous = r.identifier[len(r.identifier)-1]
				r.identifier = r.identifier[:0]
				if sensitive {
					r.state = databaseSQLRedactSensitiveSeparator
				} else {
					r.state = databaseSQLRedactNormal
				}
				reprocess = true
			}
		case databaseSQLRedactSensitiveSeparator:
			switch value {
			case ' ', '\t', '\r', '\n':
				output.WriteByte(value)
			case '=', ':':
				output.WriteByte(value)
				r.state = databaseSQLRedactSensitiveValueStart
			case 'b', 'B':
				r.state = databaseSQLRedactSensitiveB
			default:
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		case databaseSQLRedactSensitiveB:
			if value == 'y' || value == 'Y' {
				output.WriteString("BY")
				r.state = databaseSQLRedactSensitiveValueStart
			} else {
				output.WriteByte('B')
				r.previous = 'B'
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		case databaseSQLRedactSensitiveValueStart:
			if value == ' ' || value == '\t' || value == '\r' || value == '\n' {
				output.WriteByte(value)
			} else if isDatabaseSQLSensitiveValuePart(value) {
				output.WriteString(sqlAuditRedacted)
				r.state = databaseSQLRedactSensitiveValue
			} else {
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		case databaseSQLRedactSensitiveValue:
			if !isDatabaseSQLSensitiveValuePart(value) {
				r.state = databaseSQLRedactNormal
				reprocess = true
			}
		}
		if !reprocess {
			index++
		}
	}
	if output.Len() == 0 {
		return nil
	}
	return r.write(output.Bytes())
}

func (r *databaseSQLStreamRedactor) Flush() error {
	if r == nil || r.write == nil {
		return nil
	}
	var output bytes.Buffer
	switch r.state {
	case databaseSQLRedactDash:
		output.WriteByte('-')
	case databaseSQLRedactSlash:
		output.WriteByte('/')
	case databaseSQLRedactQuotePending:
		output.WriteByte(r.quote)
	case databaseSQLRedactDollarCandidate:
		output.Write(r.dollarTag)
	case databaseSQLRedactIdentifier:
		output.Write(r.identifier)
	case databaseSQLRedactSensitiveB:
		output.WriteByte('B')
	}
	r.state = databaseSQLRedactNormal
	r.dollarTag = nil
	r.identifier = nil
	r.dollarMatch = 0
	if output.Len() == 0 {
		return nil
	}
	return r.write(output.Bytes())
}

func isDatabaseSQLSensitiveAssignmentKey(identifier []byte) bool {
	for _, key := range [][]byte{
		[]byte("password"),
		[]byte("passwd"),
		[]byte("pwd"),
		[]byte("secret"),
		[]byte("token"),
		[]byte("authorization"),
		[]byte("api_key"),
		[]byte("access_token"),
		[]byte("refresh_token"),
		[]byte("client_secret"),
	} {
		if bytes.EqualFold(identifier, key) {
			return true
		}
	}
	return false
}

func isDatabaseSQLSensitiveValuePart(value byte) bool {
	return isSQLIdentifierPart(value) || value == '.' || value == '/' ||
		value == '+' || value == '-'
}

func startsDatabaseSQLStreamNumber(previous, value byte) bool {
	if value < '0' || value > '9' {
		return false
	}
	return !isSQLIdentifierPart(previous) && previous != '$'
}

func isDatabaseSQLStreamNumberPart(value byte) bool {
	return value >= '0' && value <= '9' ||
		value >= 'a' && value <= 'f' ||
		value >= 'A' && value <= 'F' ||
		value == '.' || value == 'x' || value == 'X' ||
		value == 'b' || value == 'B' || value == 'e' || value == 'E' ||
		value == '+' || value == '-'
}
