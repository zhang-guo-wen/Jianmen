package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWaitForRecordingWaitsUntilGuacdFileIsStable(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "recording.guac")
	if err := os.WriteFile(filename, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendDone := make(chan error, 1)
	go func() {
		time.Sleep(75 * time.Millisecond)
		file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			_, err = file.WriteString("-complete")
		}
		if file != nil {
			err = errors.Join(err, file.Close())
		}
		appendDone <- err
	}()

	info, err := waitForRecording(context.Background(), filename, 2*time.Second)
	if err != nil {
		t.Fatalf("waitForRecording() error = %v", err)
	}
	if err := <-appendDone; err != nil {
		t.Fatalf("append recording: %v", err)
	}
	if info.Size() != int64(len("partial-complete")) {
		t.Fatalf("stable recording size = %d, want %d", info.Size(), len("partial-complete"))
	}
}
