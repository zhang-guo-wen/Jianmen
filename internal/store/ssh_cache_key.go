package store

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func targetSSHCacheKey(target TargetConfig) string {
	identity := strings.TrimSpace(target.HostKeyFingerprint) + "\x00" + strings.TrimSpace(target.KnownHosts)
	digest := sha256.Sum256([]byte(identity))
	return target.ID + "@" + target.Addr() + "#" + hex.EncodeToString(digest[:])
}
