//go:build !embedded_guacd

package guacdruntime

import (
	"errors"
	"testing"
)

func TestPrepareWithoutEmbeddedRuntimeFailsClearly(t *testing.T) {
	_, err := Prepare(t.TempDir())
	if !errors.Is(err, ErrNotIncluded) {
		t.Fatalf("Prepare() error = %v, want ErrNotIncluded", err)
	}
}
