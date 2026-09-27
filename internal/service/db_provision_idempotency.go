package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

func ValidateDatabaseProvisioningIdempotencyKey(value string) (string, error) {
	key := strings.TrimSpace(value)
	if len(key) < 16 || len(key) > 128 {
		return "", ErrInvalidDatabaseProvisioningRequest
	}
	for _, character := range key {
		if !((character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			strings.ContainsRune("-_.:", character)) {
			return "", ErrInvalidDatabaseProvisioningRequest
		}
	}
	return key, nil
}

func normalizeDatabaseProvisioningIdempotencyKey(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return ValidateDatabaseProvisioningIdempotencyKey(value)
}

func canonicalProvisioningRequestHash(request ProvisionDatabaseAccountRequest) (string, []DBGrant, error) {
	actorID := strings.TrimSpace(request.Actor.UserID)
	if actorID == "" {
		return "", nil, ErrInvalidDatabaseProvisioningRequest
	}
	grants := append([]DBGrant(nil), request.Grants...)
	for index := range grants {
		grants[index].Database = strings.TrimSpace(grants[index].Database)
		grants[index].Privilege = strings.TrimSpace(grants[index].Privilege)
	}
	sort.Slice(grants, func(left, right int) bool {
		if grants[left].Database == grants[right].Database {
			return grants[left].Privilege < grants[right].Privilege
		}
		return grants[left].Database < grants[right].Database
	})
	canonicalGrants := grants[:0]
	for _, grant := range grants {
		if len(canonicalGrants) > 0 {
			previous := canonicalGrants[len(canonicalGrants)-1]
			if previous.Database == grant.Database && previous.Privilege == grant.Privilege {
				continue
			}
		}
		canonicalGrants = append(canonicalGrants, grant)
	}
	var expiresAt *time.Time
	if request.ExpiresAt != nil {
		normalized := request.ExpiresAt.UTC()
		expiresAt = &normalized
	}
	type canonicalRequest struct {
		ActorID        string     `json:"actor_id"`
		InstanceID     string     `json:"instance_id"`
		AdminAccountID string     `json:"admin_account_id"`
		Grants         []DBGrant  `json:"grants"`
		Group          string     `json:"group"`
		Remark         string     `json:"remark"`
		ExpiresAt      *time.Time `json:"expires_at"`
	}
	encoded, err := json.Marshal(canonicalRequest{
		ActorID: actorID, InstanceID: strings.TrimSpace(request.InstanceID),
		AdminAccountID: strings.TrimSpace(request.AdminAccountID),
		Grants:         canonicalGrants, Group: strings.TrimSpace(request.Group), Remark: strings.TrimSpace(request.Remark),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", nil, errors.New("encode canonical database provisioning request")
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), canonicalGrants, nil
}
