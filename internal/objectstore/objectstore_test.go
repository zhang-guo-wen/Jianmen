package objectstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	tests := []struct {
		name string
		ctx  context.Context
		cfg  Config
	}{
		{name: "nil context", cfg: Config{Provider: ProviderFilesystem, LocalDir: t.TempDir()}},
		{name: "canceled context", ctx: canceled, cfg: Config{Provider: ProviderFilesystem, LocalDir: t.TempDir()}},
		{name: "missing provider", ctx: context.Background(), cfg: Config{}},
		{name: "unknown provider", ctx: context.Background(), cfg: Config{Provider: "azure"}},
		{name: "missing filesystem directory", ctx: context.Background(), cfg: Config{Provider: ProviderFilesystem}},
		{
			name: "unsafe prefix",
			ctx:  context.Background(),
			cfg:  Config{Provider: ProviderFilesystem, LocalDir: t.TempDir(), Prefix: "../escape"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := New(tt.ctx, tt.cfg); err == nil {
				t.Fatal("New() error = nil, want configuration error")
			}
		})
	}
}

func TestContextReaderStopsAfterCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	reader := contextReader{ctx: ctx, src: strings.NewReader("payload")}
	buffer := make([]byte, 3)
	if _, err := reader.Read(buffer); err != nil {
		t.Fatalf("first Read() error = %v", err)
	}
	cancel()
	if _, err := reader.Read(buffer); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() after cancellation error = %v, want context.Canceled", err)
	}
}

func TestNormalizeKeyRejectsUnsafeAndNonCanonicalKeys(t *testing.T) {
	t.Parallel()

	keys := []string{
		"",
		" ",
		".",
		"..",
		"../escape",
		"a/../../escape",
		"a/../escape",
		"/absolute",
		`a\b`,
		"a//b",
		"a/./b",
		"a/",
		"a\x00b",
	}
	for _, key := range keys {
		key := key
		t.Run(strings.ReplaceAll(key, "/", "_"), func(t *testing.T) {
			t.Parallel()
			if _, err := normalizeKey(key); !errors.Is(err, ErrInvalidKey) {
				t.Fatalf("normalizeKey(%q) error = %v, want ErrInvalidKey", key, err)
			}
		})
	}
}

func TestNormalizePrefixAndPrefixedKey(t *testing.T) {
	t.Parallel()

	prefix, err := normalizePrefix(" /tenant/rdp/ ")
	if err != nil {
		t.Fatalf("normalizePrefix() error = %v", err)
	}
	if prefix != "tenant/rdp" {
		t.Fatalf("normalizePrefix() = %q, want tenant/rdp", prefix)
	}
	if got := prefixedKey(prefix, "sessions/one.cast"); got != "tenant/rdp/sessions/one.cast" {
		t.Fatalf("prefixedKey() = %q", got)
	}
	if got := prefixedKey("", "sessions/one.cast"); got != "sessions/one.cast" {
		t.Fatalf("prefixedKey() without prefix = %q", got)
	}
}
