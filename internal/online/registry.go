package online

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var ErrSessionNotFound = errors.New("online session not found")

type Session struct {
	ID              string    `json:"id"`
	AuditSessionID  string    `json:"audit_session_id"`
	UserSessionID   string    `json:"user_session_id,omitempty"`
	SessionID       string    `json:"session_id,omitempty"`
	ResourceType    string    `json:"resource_type"`
	ResourceID      string    `json:"resource_id"`
	AccountID       string    `json:"account_id,omitempty"`
	Instance        string    `json:"instance"`
	Protocol        string    `json:"protocol"`
	ProtocolSubtype string    `json:"protocol_subtype,omitempty"`
	Account         string    `json:"account"`
	Operator        string    `json:"operator"`
	StartedAt       time.Time `json:"started_at"`
	HasReplay       bool      `json:"has_replay"`
}

type onlineSessionEntry struct {
	session    Session
	disconnect func()
}

type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*onlineSessionEntry
}

func NewRegistry() *Registry {
	return &Registry{sessions: make(map[string]*onlineSessionEntry)}
}

func (r *Registry) Register(session Session, disconnect func()) func() {
	if r == nil || session.ID == "" || disconnect == nil {
		return func() {}
	}
	entry := &onlineSessionEntry{session: session, disconnect: disconnect}
	r.mu.Lock()
	r.sessions[session.ID] = entry
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		if r.sessions[session.ID] == entry {
			delete(r.sessions, session.ID)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) List() []Session {
	if r == nil {
		return []Session{}
	}
	r.mu.RLock()
	items := make([]Session, 0, len(r.sessions))
	for _, entry := range r.sessions {
		items = append(items, entry.session)
	}
	r.mu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	return items
}

func (r *Registry) Disconnect(id string) error {
	if r == nil {
		return ErrSessionNotFound
	}
	r.mu.Lock()
	entry := r.sessions[id]
	if entry != nil {
		delete(r.sessions, id)
	}
	r.mu.Unlock()
	if entry == nil {
		return ErrSessionNotFound
	}
	entry.disconnect()
	return nil
}

func (r *Registry) UpdateProtocolSubtype(id, subtype string) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	if entry := r.sessions[id]; entry != nil {
		entry.session.ProtocolSubtype = subtype
	}
	r.mu.Unlock()
}
