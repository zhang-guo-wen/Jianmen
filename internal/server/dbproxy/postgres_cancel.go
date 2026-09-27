package dbproxy

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"

	"jianmen/internal/model"
)

const (
	postgresCancelRequestCode       = 80877102
	minPostgresCancelSecretBytes    = 4
	maxPostgresCancelSecretBytes    = 256
	minPostgresCancelRequestBytes   = 12 + minPostgresCancelSecretBytes
	maxPostgresCancelRequestBytes   = 12 + maxPostgresCancelSecretBytes
	postgresCancelRequestHeaderSize = 8
)

var errPostgresCancelRouteNotFound = errors.New("PostgreSQL cancel route was not found")

type postgresCancelKey struct {
	processID uint32
	secret    string
}

type postgresCancelRegistry struct {
	mu      sync.RWMutex
	nextID  uint64
	entries map[postgresCancelKey]map[uint64]model.DatabaseInstance
}

func (registry *postgresCancelRegistry) register(
	key postgresCancelKey,
	instance model.DatabaseInstance,
) func() {
	registry.mu.Lock()
	if registry.entries == nil {
		registry.entries = make(map[postgresCancelKey]map[uint64]model.DatabaseInstance)
	}
	registry.nextID++
	entryID := registry.nextID
	if registry.entries[key] == nil {
		registry.entries[key] = make(map[uint64]model.DatabaseInstance)
	}
	registry.entries[key][entryID] = instance
	registry.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			registry.mu.Lock()
			defer registry.mu.Unlock()
			delete(registry.entries[key], entryID)
			if len(registry.entries[key]) == 0 {
				delete(registry.entries, key)
			}
		})
	}
}

func (registry *postgresCancelRegistry) lookup(
	key postgresCancelKey,
) (model.DatabaseInstance, bool) {
	registry.mu.RLock()
	defer registry.mu.RUnlock()
	entries := registry.entries[key]
	if len(entries) != 1 {
		return model.DatabaseInstance{}, false
	}
	for _, instance := range entries {
		return instance, true
	}
	return model.DatabaseInstance{}, false
}

func (g *Gateway) forwardPostgresCancel(ctx context.Context, message []byte) error {
	key, err := parsePostgresCancelRequest(message)
	if err != nil {
		return err
	}
	instance, exists := g.postgresCancels.lookup(key)
	if !exists {
		return errPostgresCancelRouteNotFound
	}
	upstream, err := dialPostgresUpstream(ctx, instance)
	if err != nil {
		return fmt.Errorf("connect PostgreSQL cancel upstream: %w", err)
	}
	defer upstream.Close()
	if err := writePostgresBytes(upstream, message); err != nil {
		return fmt.Errorf("forward PostgreSQL CancelRequest: %w", err)
	}
	return nil
}

func isPostgresCancelRequestHeader(header []byte) bool {
	if len(header) != postgresCancelRequestHeaderSize {
		return false
	}
	length := binary.BigEndian.Uint32(header[:4])
	return length >= minPostgresCancelRequestBytes &&
		length <= maxPostgresCancelRequestBytes &&
		binary.BigEndian.Uint32(header[4:8]) == postgresCancelRequestCode
}

func readPostgresCancelRequest(connection net.Conn, header []byte) ([]byte, error) {
	if !isPostgresCancelRequestHeader(header) {
		return nil, errors.New("invalid PostgreSQL CancelRequest header")
	}
	messageLength := int(binary.BigEndian.Uint32(header[:4]))
	message := make([]byte, messageLength)
	copy(message, header)
	if _, err := readFull(connection, message[len(header):]); err != nil {
		return nil, fmt.Errorf("read PostgreSQL CancelRequest: %w", err)
	}
	if _, err := parsePostgresCancelRequest(message); err != nil {
		return nil, err
	}
	return message, nil
}

func parsePostgresCancelRequest(message []byte) (postgresCancelKey, error) {
	if len(message) < minPostgresCancelRequestBytes || len(message) > maxPostgresCancelRequestBytes {
		return postgresCancelKey{}, fmt.Errorf(
			"invalid PostgreSQL CancelRequest size %d",
			len(message),
		)
	}
	if declared := int(binary.BigEndian.Uint32(message[:4])); declared != len(message) {
		return postgresCancelKey{}, fmt.Errorf(
			"PostgreSQL CancelRequest length mismatch: declared %d, read %d",
			declared,
			len(message),
		)
	}
	if code := binary.BigEndian.Uint32(message[4:8]); code != postgresCancelRequestCode {
		return postgresCancelKey{}, fmt.Errorf("invalid PostgreSQL CancelRequest code %d", code)
	}
	return postgresCancelKey{
		processID: binary.BigEndian.Uint32(message[8:12]),
		secret:    string(message[12:]),
	}, nil
}

func parsePostgresBackendKey(payload []byte) (postgresCancelKey, error) {
	if len(payload) < 4+minPostgresCancelSecretBytes ||
		len(payload) > 4+maxPostgresCancelSecretBytes {
		return postgresCancelKey{}, fmt.Errorf(
			"invalid PostgreSQL BackendKeyData size %d",
			len(payload),
		)
	}
	return postgresCancelKey{
		processID: binary.BigEndian.Uint32(payload[:4]),
		secret:    string(payload[4:]),
	}, nil
}
