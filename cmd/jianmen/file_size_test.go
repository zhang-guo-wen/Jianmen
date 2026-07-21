package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandProductionFilesStayWithinLineLimit(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list command files: %v", err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		handle, err := os.Open(file)
		if err != nil {
			t.Fatalf("open %s: %v", file, err)
		}
		lines := 0
		scanner := bufio.NewScanner(handle)
		for scanner.Scan() {
			lines++
		}
		closeErr := handle.Close()
		if err := scanner.Err(); err != nil {
			t.Fatalf("scan %s: %v", file, err)
		}
		if closeErr != nil {
			t.Fatalf("close %s: %v", file, closeErr)
		}
		if lines > 150 {
			t.Errorf("%s has %d lines, hard limit is 150", file, lines)
		}
	}
}
