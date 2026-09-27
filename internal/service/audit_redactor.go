package service

import (
	"encoding/json"
	"strconv"
	"strings"
)

func redactJSONSensitiveValues(value string) string {
	var out strings.Builder
	out.Grow(len(value))
	last := 0
	for i := 0; i < len(value); {
		if value[i] != '"' {
			i++
			continue
		}
		keyEnd := jsonStringEnd(value, i)
		if keyEnd >= len(value) {
			break
		}
		p := keyEnd + 1
		for p < len(value) && isSpace(value[p]) {
			p++
		}
		if p >= len(value) || value[p] != ':' {
			i = keyEnd + 1
			continue
		}
		key, err := strconv.Unquote(value[i : keyEnd+1])
		if err != nil || !isSensitiveKey(key) {
			i = keyEnd + 1
			continue
		}
		valueStart := p + 1
		for valueStart < len(value) && isSpace(value[valueStart]) {
			valueStart++
		}
		out.WriteString(value[last:valueStart])
		out.WriteString(`"[REDACTED]"`)
		valueEnd, complete := jsonValueEnd(value, valueStart)
		if !complete {
			return out.String()
		}
		last = valueEnd
		i = valueEnd
	}
	out.WriteString(value[last:])
	return out.String()
}

func jsonValueEnd(value string, start int) (int, bool) {
	if start >= len(value) {
		return len(value), false
	}
	switch value[start] {
	case '"':
		end := jsonStringEnd(value, start)
		return min(end+1, len(value)), end < len(value)
	case '[', '{':
		return jsonCompositeEnd(value, start)
	default:
		end := start
		for end < len(value) && value[end] != ',' && value[end] != ']' && value[end] != '}' && !isSpace(value[end]) {
			end++
		}
		if end == start || !json.Valid([]byte(value[start:end])) {
			return len(value), false
		}
		tail := end
		for tail < len(value) && isSpace(value[tail]) {
			tail++
		}
		if tail < len(value) && value[tail] != ',' && value[tail] != ']' && value[tail] != '}' {
			return len(value), false
		}
		return end, true
	}
}

func jsonCompositeEnd(value string, start int) (int, bool) {
	stack := []byte{value[start]}
	for i := start + 1; i < len(value); i++ {
		switch value[i] {
		case '"':
			end := jsonStringEnd(value, i)
			if end >= len(value) {
				return len(value), false
			}
			i = end
		case '[', '{':
			stack = append(stack, value[i])
		case ']', '}':
			top := stack[len(stack)-1]
			if top == '[' && value[i] != ']' || top == '{' && value[i] != '}' {
				return len(value), false
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return i + 1, true
			}
		}
	}
	return len(value), false
}

func jsonStringEnd(value string, start int) int {
	for i := start + 1; i < len(value); i++ {
		if value[i] == '\\' {
			i++
			continue
		}
		if value[i] == '"' {
			return i
		}
	}
	return len(value)
}
