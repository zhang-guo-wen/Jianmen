package dbproxy

import (
	"strconv"
	"strings"
)

func newRedisStreamPubSubCapture(
	rootType byte,
	maxTopicBytes int,
	slot *redisResponseSlot,
) *redisStreamPubSubCapture {
	capture := &redisStreamPubSubCapture{
		rootType:      rootType,
		maxTopicBytes: normalizeMaxClientMessageBytes(maxTopicBytes),
		bulkElement:   -1,
	}
	if slot == nil {
		return capture
	}
	capture.expectedCommand = slot.command
	if slot.unsubscribeAll {
		capture.allowNullTopic = len(slot.unsubscribeTopics) == 0
		capture.expectedTopics = make([]string, 0, len(slot.unsubscribeTopics))
		for topic := range slot.unsubscribeTopics {
			capture.expectedTopics = append(capture.expectedTopics, topic)
		}
		return capture
	}
	if len(slot.pubSubArgs) > 1 {
		capture.expectedTopics = append(capture.expectedTopics, slot.pubSubArgs[1:]...)
	}
	return capture
}

func (c *redisStreamPubSubCapture) consumeHeader(line []byte) *queryDecision {
	if c == nil || c.invalid || len(line) < 3 {
		return nil
	}
	if !c.rootSeen {
		if line[0] == c.rootType {
			c.rootSeen = true
		}
		return nil
	}
	element := c.nextElement
	c.nextElement++
	value := line[1 : len(line)-2]
	switch line[0] {
	case '+':
		if decision := c.setScalar(element, value, false); decision != nil {
			return decision
		}
	case '_':
		if decision := c.setScalar(element, nil, true); decision != nil {
			return decision
		}
	case '$', '=':
		length, ok := parseCanonicalRESPNullableNumber(value)
		if !ok {
			c.invalid = true
			return nil
		}
		if length == -1 {
			return c.setScalar(element, nil, true)
		}
		if (element == 0 && length > 32) ||
			(element == 1 && length > int64(c.maxTopicBytes)) {
			c.invalid = true
			if element == 1 {
				return newObserverFatalDecision(
					observerErrorBufferLimit,
					"Redis Pub/Sub acknowledgement topic exceeds the configured client message limit",
				)
			}
			return nil
		}
		c.bulkElement = element
		c.bulkValue = nil
		if element == 1 && c.matchesExpectedACK() {
			c.topicOffset = 0
			candidates := c.expectedTopics[:0]
			for _, topic := range c.expectedTopics {
				if int64(len(topic)) == length {
					candidates = append(candidates, topic)
				}
			}
			c.expectedTopics = candidates
			if len(c.expectedTopics) == 0 {
				c.invalid = true
				return redisPubSubTopicMismatchDecision()
			}
		}
	case ':':
		if element != 2 {
			c.invalid = true
			return nil
		}
		count, err := strconv.ParseInt(string(value), 10, 64)
		if err != nil {
			c.invalid = true
			return nil
		}
		c.event.count = count
		c.countReady = true
	default:
		c.invalid = true
	}
	return nil
}

func (c *redisStreamPubSubCapture) consumeBulk(data []byte) *queryDecision {
	if c == nil || c.invalid || c.bulkElement < 0 || c.bulkElement > 1 {
		return nil
	}
	if c.bulkElement == 1 {
		if !c.matchesExpectedACK() {
			return nil
		}
		end := c.topicOffset + len(data)
		candidates := c.expectedTopics[:0]
		for _, topic := range c.expectedTopics {
			if redisStringSegmentEqualsBytes(topic, c.topicOffset, end, data) {
				candidates = append(candidates, topic)
			}
		}
		c.expectedTopics = candidates
		c.topicOffset = end
		if len(c.expectedTopics) == 0 {
			c.invalid = true
			return redisPubSubTopicMismatchDecision()
		}
		return nil
	}
	c.bulkValue = append(c.bulkValue, data...)
	return nil
}

func redisStringSegmentEqualsBytes(value string, start, end int, data []byte) bool {
	if start < 0 || end < start || end > len(value) || end-start != len(data) {
		return false
	}
	for index := range data {
		if value[start+index] != data[index] {
			return false
		}
	}
	return true
}

func (c *redisStreamPubSubCapture) finishBulk() {
	if c == nil || c.invalid || c.bulkElement < 0 {
		return
	}
	element := c.bulkElement
	c.bulkElement = -1
	if element == 0 {
		c.setScalar(element, c.bulkValue, false)
	} else if c.matchesExpectedACK() {
		if len(c.expectedTopics) == 0 || c.topicOffset != len(c.expectedTopics[0]) {
			c.invalid = true
		} else {
			c.event.topic = c.expectedTopics[0]
			c.event.topicNull = false
			c.topicReady = true
		}
	} else {
		c.topicReady = true
	}
	c.bulkValue = nil
}

func (c *redisStreamPubSubCapture) setScalar(
	element int,
	value []byte,
	null bool,
) *queryDecision {
	switch element {
	case 0:
		if null {
			c.invalid = true
			return nil
		}
		c.event.kind = strings.ToLower(string(value))
		c.kindReady = true
	case 1:
		if c.matchesExpectedACK() {
			if null {
				if !c.allowNullTopic {
					c.invalid = true
					return redisPubSubTopicMismatchDecision()
				}
			} else {
				topic, ok := c.expectedTopicForBytes(value)
				if !ok {
					c.invalid = true
					return redisPubSubTopicMismatchDecision()
				}
				c.event.topic = topic
			}
		}
		c.event.topicNull = null
		c.topicReady = true
	}
	return nil
}

func (c *redisStreamPubSubCapture) matchesExpectedACK() bool {
	return c != nil &&
		c.kindReady &&
		redisPubSubAckMatchesCommand(c.event.kind, c.expectedCommand)
}

func (c *redisStreamPubSubCapture) expectedTopicForBytes(value []byte) (string, bool) {
	for _, topic := range c.expectedTopics {
		if redisStringSegmentEqualsBytes(topic, 0, len(topic), value) {
			return topic, true
		}
	}
	return "", false
}

func redisPubSubTopicMismatchDecision() *queryDecision {
	return newObserverFatalDecision(
		observerErrorProtocol,
		"Redis Pub/Sub acknowledgement topic does not match the pending command",
	)
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
