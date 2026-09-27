package store

import "testing"

func TestTargetSSHCacheKeyIncludesHostIdentity(t *testing.T) {
	target := TargetConfig{
		ID: "account-1", Host: "192.0.2.10", Port: 22,
		HostKeyFingerprint: "SHA256:first", KnownHosts: "first known_hosts",
	}
	first := targetSSHCacheKey(target)
	if first == "" || first != targetSSHCacheKey(target) {
		t.Fatalf("cache key is empty or unstable: %q", first)
	}

	target.HostKeyFingerprint = "SHA256:second"
	target.KnownHosts = "second known_hosts"
	if second := targetSSHCacheKey(target); second == first {
		t.Fatalf("cache key did not change after identity refresh: %q", second)
	}
}
