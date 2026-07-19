package dbproxy

import (
	"strconv"
	"strings"
)

type redisSubscriptionState struct {
	channels map[string]struct{}
	patterns map[string]struct{}
	sharded  map[string]struct{}
}

type redisPubSubEvent struct {
	kind      string
	topic     string
	topicNull bool
	count     int64
	push      bool
}

type redisStreamPubSubCapture struct {
	rootType    byte
	rootSeen    bool
	nextElement int
	bulkElement int
	bulkValue   []byte
	event       redisPubSubEvent
	kindReady   bool
	topicReady  bool
	countReady  bool
	invalid     bool
}

func (s *redisSubscriptionState) active() bool {
	return len(s.channels)+len(s.patterns)+len(s.sharded) > 0
}

func (s redisSubscriptionState) clone() redisSubscriptionState {
	return redisSubscriptionState{
		channels: cloneRedisSubscriptionTopics(s.channels),
		patterns: cloneRedisSubscriptionTopics(s.patterns),
		sharded:  cloneRedisSubscriptionTopics(s.sharded),
	}
}

func cloneRedisSubscriptionTopics(topics map[string]struct{}) map[string]struct{} {
	if len(topics) == 0 {
		return nil
	}
	cloned := make(map[string]struct{}, len(topics))
	for topic := range topics {
		cloned[topic] = struct{}{}
	}
	return cloned
}

func (s *redisSubscriptionState) count(command string) int {
	switch command {
	case "UNSUBSCRIBE":
		return len(s.channels)
	case "PUNSUBSCRIBE":
		return len(s.patterns)
	case "SUNSUBSCRIBE":
		return len(s.sharded)
	default:
		return 0
	}
}

func (s *redisSubscriptionState) category(command string) *map[string]struct{} {
	switch command {
	case "SUBSCRIBE", "UNSUBSCRIBE":
		return &s.channels
	case "PSUBSCRIBE", "PUNSUBSCRIBE":
		return &s.patterns
	case "SSUBSCRIBE", "SUNSUBSCRIBE":
		return &s.sharded
	default:
		return nil
	}
}

func (s *redisSubscriptionState) apply(event redisPubSubEvent) {
	var target *map[string]struct{}
	add := false
	switch event.kind {
	case "subscribe":
		target, add = &s.channels, true
	case "unsubscribe":
		target = &s.channels
	case "psubscribe":
		target, add = &s.patterns, true
	case "punsubscribe":
		target = &s.patterns
	case "ssubscribe":
		target, add = &s.sharded, true
	case "sunsubscribe":
		target = &s.sharded
	default:
		return
	}
	if *target == nil {
		*target = make(map[string]struct{})
	}
	if add {
		(*target)[event.topic] = struct{}{}
	} else {
		delete(*target, event.topic)
	}
}

func (o *redisObserver) planRedisPubSubResponse(
	command string,
	args []string,
) (remaining int, unsubscribeAll bool, unsubscribeTopics map[string]struct{}) {
	target := o.subscriptionIntent.category(command)
	if target == nil {
		return 1, false, nil
	}
	if *target == nil {
		*target = make(map[string]struct{})
	}
	switch command {
	case "SUBSCRIBE", "PSUBSCRIBE", "SSUBSCRIBE":
		for _, topic := range args[1:] {
			(*target)[topic] = struct{}{}
		}
		return len(args) - 1, false, nil
	case "UNSUBSCRIBE", "PUNSUBSCRIBE", "SUNSUBSCRIBE":
		if len(args) > 1 {
			for _, topic := range args[1:] {
				delete(*target, topic)
			}
			return len(args) - 1, false, nil
		}
		unsubscribeTopics = make(map[string]struct{}, len(*target))
		for topic := range *target {
			unsubscribeTopics[topic] = struct{}{}
		}
		clear(*target)
		return 0, true, unsubscribeTopics
	default:
		return 1, false, nil
	}
}

func (o *redisObserver) rebuildRedisSubscriptionIntent(start int) {
	o.subscriptionIntent = o.subscriptions.clone()
	for index := start; index < len(o.slots); index++ {
		slot := &o.slots[index]
		if !isRedisPubSubStateCommand(slot.command) || len(slot.pubSubArgs) == 0 {
			continue
		}
		slot.remaining, slot.unsubscribeAll, slot.unsubscribeTopics =
			o.planRedisPubSubResponse(slot.command, slot.pubSubArgs)
	}
}

func redisPubSubAckMatchesCommand(kind, command string) bool {
	switch command {
	case "SUBSCRIBE":
		return kind == "subscribe"
	case "UNSUBSCRIBE":
		return kind == "unsubscribe"
	case "PSUBSCRIBE":
		return kind == "psubscribe"
	case "PUNSUBSCRIBE":
		return kind == "punsubscribe"
	case "SSUBSCRIBE":
		return kind == "ssubscribe"
	case "SUNSUBSCRIBE":
		return kind == "sunsubscribe"
	default:
		return false
	}
}

func isRedisPubSubStateCommand(command string) bool {
	switch command {
	case "SUBSCRIBE", "UNSUBSCRIBE", "PSUBSCRIBE", "PUNSUBSCRIBE", "SSUBSCRIBE", "SUNSUBSCRIBE":
		return true
	default:
		return false
	}
}

func isRedisPubSubMessage(kind string) bool {
	return kind == "message" || kind == "pmessage" || kind == "smessage"
}

func isRedisRESP2PubSubMessagePrefix(frame []byte) bool {
	if len(frame) == 0 || frame[0] != '*' {
		return false
	}
	lineEnd, status := redisRESPLineEnd(frame)
	if status != redisRESPComplete {
		return false
	}
	count, ok := parseCanonicalRESPNumber(frame[1:lineEnd])
	if !ok {
		return false
	}
	position := lineEnd + 2
	if position >= len(frame) {
		return false
	}
	kind, _, _, ok := redisRESPScalar(frame[position:])
	if !ok {
		return false
	}
	switch strings.ToLower(kind) {
	case "message", "smessage":
		return count == 3
	case "pmessage":
		return count == 4
	default:
		return false
	}
}

func (o *redisObserver) observeRedisRESPVersion(frame []byte) {
	switch redisRESPPrimaryType(frame) {
	case '_', '#', ',', '(', '!', '=', '%', '~', '>', '|':
		o.protocolVersion = 3
	}
}

func (o *redisObserver) consumeRedisPubSubFrame(frame []byte) (bool, *queryDecision) {
	event, parsed := parseRedisPubSubEvent(frame)
	if !parsed {
		return redisRESPPrimaryType(frame) == '>', nil
	}
	return o.consumeRedisPubSubEvent(event)
}

func (o *redisObserver) consumeRedisPubSubEvent(event redisPubSubEvent) (bool, *queryDecision) {
	if event.push {
		o.protocolVersion = 3
	}
	if len(o.slots) > 0 && redisPubSubAckMatchesCommand(event.kind, o.slots[0].command) {
		slot := &o.slots[0]
		o.subscriptions.apply(event)
		complete := false
		if slot.unsubscribeAll {
			delete(slot.unsubscribeTopics, event.topic)
			complete = event.topicNull || o.subscriptions.count(slot.command) == 0
		} else {
			if slot.remaining <= 0 {
				slot.remaining = 1
			}
			slot.remaining--
			complete = slot.remaining == 0
		}
		if !complete {
			return true, nil
		}
		if !o.finishSlotWithResult(slot, queryFinish{Status: queryStatusSuccess}) {
			slot.finishFailed = true
			return true, o.failDecision(auditSinkFailureDecision())
		}
		o.slots = o.slots[1:]
		return true, nil
	}
	if event.push {
		return true, nil
	}
	if o.protocolVersion != 3 && o.subscriptions.active() && isRedisPubSubMessage(event.kind) {
		return true, nil
	}
	return false, nil
}

func newRedisStreamPubSubCapture(rootType byte) *redisStreamPubSubCapture {
	return &redisStreamPubSubCapture{rootType: rootType, bulkElement: -1}
}

func (c *redisStreamPubSubCapture) consumeHeader(line []byte) {
	if c == nil || c.invalid || len(line) < 3 {
		return
	}
	if !c.rootSeen {
		if line[0] == c.rootType {
			c.rootSeen = true
		}
		return
	}
	element := c.nextElement
	c.nextElement++
	value := line[1 : len(line)-2]
	switch line[0] {
	case '+':
		c.setScalar(element, string(value), false)
	case '_':
		c.setScalar(element, "", true)
	case '$', '=':
		length, ok := parseCanonicalRESPNullableNumber(value)
		if !ok {
			c.invalid = true
			return
		}
		if length == -1 {
			c.setScalar(element, "", true)
			return
		}
		if (element == 0 && length > 32) ||
			(element == 1 && length > maxRedisObserverBufferBytes) {
			c.invalid = true
			return
		}
		c.bulkElement = element
		if element <= 1 {
			c.bulkValue = make([]byte, 0, int(length))
		}
	case ':':
		if element != 2 {
			c.invalid = true
			return
		}
		count, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil {
			c.invalid = true
			return
		}
		c.event.count = count
		c.countReady = true
	default:
		c.invalid = true
	}
}

func (c *redisStreamPubSubCapture) consumeBulk(data []byte) {
	if c == nil || c.invalid || c.bulkElement < 0 || c.bulkElement > 1 {
		return
	}
	c.bulkValue = append(c.bulkValue, data...)
}

func (c *redisStreamPubSubCapture) finishBulk() {
	if c == nil || c.invalid || c.bulkElement < 0 {
		return
	}
	element := c.bulkElement
	c.bulkElement = -1
	if element <= 1 {
		c.setScalar(element, string(c.bulkValue), false)
	}
	c.bulkValue = nil
}

func (c *redisStreamPubSubCapture) setScalar(element int, value string, null bool) {
	switch element {
	case 0:
		if null {
			c.invalid = true
			return
		}
		c.event.kind = strings.ToLower(value)
		c.kindReady = true
	case 1:
		c.event.topic = value
		c.event.topicNull = null
		c.topicReady = true
	}
}

func (c *redisStreamPubSubCapture) capturedEvent() (redisPubSubEvent, bool) {
	if c == nil || c.invalid || !c.kindReady {
		return redisPubSubEvent{}, false
	}
	c.event.push = c.rootType == '>'
	if redisPubSubAckMatchesAny(c.event.kind) {
		return c.event, c.topicReady && c.countReady
	}
	if isRedisPubSubMessage(c.event.kind) {
		return c.event, c.topicReady
	}
	return c.event, true
}

func parseRedisPubSubEvent(frame []byte) (redisPubSubEvent, bool) {
	frame = redisRESPPrimaryFrame(frame)
	if len(frame) == 0 || (frame[0] != '*' && frame[0] != '>') {
		return redisPubSubEvent{}, false
	}
	lineEnd, status := redisRESPLineEnd(frame)
	if status != redisRESPComplete {
		return redisPubSubEvent{}, false
	}
	count, ok := parseCanonicalRESPNumber(frame[1:lineEnd])
	if !ok || count < 1 {
		return redisPubSubEvent{}, false
	}
	position := lineEnd + 2
	kind, _, length, ok := redisRESPScalar(frame[position:])
	if !ok {
		return redisPubSubEvent{}, false
	}
	position += length
	event := redisPubSubEvent{kind: strings.ToLower(kind), push: frame[0] == '>'}
	if count > 1 {
		event.topic, event.topicNull, length, ok = redisRESPScalar(frame[position:])
		if !ok {
			return redisPubSubEvent{}, false
		}
		position += length
	}
	if count > 2 {
		event.count, ok = redisRESPInteger(frame[position:])
		if !ok && redisPubSubAckMatchesAny(event.kind) {
			return redisPubSubEvent{}, false
		}
	}
	return event, true
}

func redisPubSubAckMatchesAny(kind string) bool {
	switch kind {
	case "subscribe", "unsubscribe", "psubscribe", "punsubscribe", "ssubscribe", "sunsubscribe":
		return true
	default:
		return false
	}
}

func redisRESPPrimaryFrame(frame []byte) []byte {
	for len(frame) > 0 && frame[0] == '|' {
		lineEnd, status := redisRESPLineEnd(frame)
		if status != redisRESPComplete {
			return nil
		}
		count, ok := parseCanonicalRESPNumber(frame[1:lineEnd])
		if !ok {
			return nil
		}
		position := lineEnd + 2
		for index := int64(0); index < count*2; index++ {
			length, itemStatus := redisRESPValueLength(frame[position:], 1)
			if itemStatus != redisRESPComplete {
				return nil
			}
			position += length
		}
		if position >= len(frame) {
			return nil
		}
		frame = frame[position:]
	}
	return frame
}

func redisRESPScalar(frame []byte) (value string, null bool, length int, ok bool) {
	length, status := redisRESPValueLength(frame, 0)
	if status != redisRESPComplete || length == 0 {
		return "", false, 0, false
	}
	lineEnd, lineStatus := redisRESPLineEnd(frame)
	if lineStatus != redisRESPComplete {
		return "", false, 0, false
	}
	switch frame[0] {
	case '+':
		return string(frame[1:lineEnd]), false, length, true
	case '_':
		if lineEnd != 1 {
			return "", false, 0, false
		}
		return "", true, length, true
	case '$', '=':
		size, valid := parseCanonicalRESPNullableNumber(frame[1:lineEnd])
		if !valid {
			return "", false, 0, false
		}
		if size == -1 {
			return "", true, length, true
		}
		start := lineEnd + 2
		return string(frame[start : start+int(size)]), false, length, true
	default:
		return "", false, 0, false
	}
}

func redisRESPInteger(frame []byte) (int64, bool) {
	if len(frame) == 0 || frame[0] != ':' {
		return 0, false
	}
	lineEnd, status := redisRESPLineEnd(frame)
	if status != redisRESPComplete {
		return 0, false
	}
	value, err := strconv.ParseInt(string(frame[1:lineEnd]), 10, 64)
	return value, err == nil
}
