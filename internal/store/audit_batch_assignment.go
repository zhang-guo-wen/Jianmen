package store

import (
	"net/netip"
	"strings"
	"sync"
	"time"

	"jianmen/internal/model"
)

const auditBatchIdleWindow = 30 * time.Minute

type auditBatchKey struct {
	user, ip, resourceType, resource, target, address, account, protocol, access string
}

type auditBatchWindow struct {
	id            string
	first, expiry time.Time
}

type auditBatchEntry struct {
	mu     sync.Mutex
	window auditBatchWindow
	users  int // 由跟踪器的锁保护，含等待同键写入的连接。
}

type auditBatchTracker struct {
	mu        sync.Mutex
	entries   map[auditBatchKey]*auditBatchEntry
	nextSweep time.Time
}

func auditBatchKeyFor(s *model.AuditSession) (auditBatchKey, bool) {
	if s == nil || s.StartedAt.IsZero() {
		return auditBatchKey{}, false
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(s.ClientIP))
	if err != nil {
		return auditBatchKey{}, false
	}
	key := auditBatchKey{
		user: strings.TrimSpace(s.UserID), ip: ip.Unmap().String(),
		resourceType: strings.TrimSpace(s.ResourceType), resource: strings.TrimSpace(s.ResourceID),
		address: strings.TrimSpace(s.TargetAddress), account: strings.TrimSpace(s.AccountID),
		protocol: strings.ToLower(strings.TrimSpace(s.Protocol)), access: strings.ToLower(strings.TrimSpace(s.ProtocolSubtype)),
	}
	switch key.protocol {
	case "ssh", "sftp":
		if key.resourceType != model.ResourceTypeHostAccount {
			return auditBatchKey{}, false
		}
		key.target = strings.TrimSpace(s.HostID)
	case "mysql", "postgres", "postgresql", "redis":
		if key.resourceType != model.ResourceTypeDatabaseAccount {
			return auditBatchKey{}, false
		}
		if key.protocol == "postgresql" {
			key.protocol = "postgres"
		}
		key.target = strings.TrimSpace(s.BatchTargetID)
	default:
		return auditBatchKey{}, false
	}
	return key, key.user != "" && key.resource != "" && key.target != "" && key.address != "" && key.account != ""
}

func (t *auditBatchTracker) persist(s *model.AuditSession, now time.Time, write func() error) error {
	key, ok := auditBatchKeyFor(s)
	if !ok {
		return write()
	}
	entry := t.acquire(key, now)
	defer t.release(key, entry)
	// 只串行同一归组键，数据库慢写不会挡住其他连接。
	entry.mu.Lock()
	defer entry.mu.Unlock()
	at := s.StartedAt.UTC()
	current := entry.window
	found := current.id != ""
	older := found && at.Before(current.first)
	next := current
	if !found || older || !at.Before(current.expiry) {
		next = auditBatchWindow{id: model.NewID(), first: at, expiry: at.Add(auditBatchIdleWindow)}
	} else if expiry := at.Add(auditBatchIdleWindow); expiry.After(current.expiry) {
		next.expiry = expiry
	}
	previous := s.BatchID
	s.BatchID = next.id
	if err := write(); err != nil {
		s.BatchID = previous
		return err
	}
	if !older {
		entry.window = next
	}
	return nil
}

func (t *auditBatchTracker) acquire(key auditBatchKey, now time.Time) *auditBatchEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !now.Before(t.nextSweep) {
		for k, entry := range t.entries {
			// users 为零时没有读写窗口的调用，释放跟踪器锁之前也不会新增。
			if k != key && entry.users == 0 && !now.Before(entry.window.expiry) {
				delete(t.entries, k)
			}
		}
		t.nextSweep = now.Add(time.Minute)
	}
	entry := t.entries[key]
	if entry == nil {
		if t.entries == nil {
			t.entries = make(map[auditBatchKey]*auditBatchEntry)
		}
		entry = &auditBatchEntry{}
		t.entries[key] = entry
	}
	entry.users++
	return entry
}

func (t *auditBatchTracker) release(key auditBatchKey, entry *auditBatchEntry) {
	t.mu.Lock()
	defer t.mu.Unlock()
	entry.users--
	if entry.users == 0 && entry.window.id == "" {
		delete(t.entries, key)
	}
}
