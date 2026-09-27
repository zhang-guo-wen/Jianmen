package dbproxy

import (
	"net"
	"sync"
)

type activeConnectionSet struct {
	mu          sync.Mutex
	connections map[net.Conn]struct{}
	closed      bool
}

func newActiveConnectionSet() *activeConnectionSet {
	return &activeConnectionSet{connections: make(map[net.Conn]struct{})}
}

func (s *activeConnectionSet) add(connection net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.connections[connection] = struct{}{}
	return true
}

func (s *activeConnectionSet) remove(connection net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.connections, connection)
}

func (s *activeConnectionSet) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for connection := range s.connections {
		_ = connection.Close()
	}
}
