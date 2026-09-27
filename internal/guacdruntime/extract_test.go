package guacdruntime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGzipPreservesRuntimeEntries(t *testing.T) {
	archive := makeTestArchive(t, []testArchiveEntry{
		{name: "opt/guacamole/sbin/", kind: tar.TypeDir, mode: 0o755},
		{name: "opt/guacamole/sbin/guacd", body: "runtime", mode: 0o755},
		{name: "opt/guacamole/sbin/guacd-link", kind: tar.TypeSymlink, link: "guacd", mode: 0o777},
	})
	root := t.TempDir()
	if err := extractTarGzip(root, archive); err != nil {
		t.Fatalf("extractTarGzip() error = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "opt", "guacamole", "sbin", "guacd"))
	if err != nil {
		t.Fatalf("read extracted file: %v", err)
	}
	if string(data) != "runtime" {
		t.Fatalf("extracted data = %q", data)
	}
	link, err := os.Readlink(filepath.Join(root, "opt", "guacamole", "sbin", "guacd-link"))
	if err != nil {
		t.Fatalf("read extracted link: %v", err)
	}
	if link != "guacd" {
		t.Fatalf("link = %q", link)
	}
}

func TestExtractTarGzipRejectsEscapingPaths(t *testing.T) {
	archive := makeTestArchive(t, []testArchiveEntry{
		{name: "../outside", body: "unsafe", mode: 0o600},
	})
	err := extractTarGzip(t.TempDir(), archive)
	if err == nil {
		t.Fatal("extractTarGzip() error = nil")
	}
}

type testArchiveEntry struct {
	name string
	body string
	kind byte
	mode int64
	link string
}

func makeTestArchive(t *testing.T, entries []testArchiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		header := &tar.Header{
			Name:     entry.name,
			Typeflag: kind,
			Mode:     entry.mode,
			Linkname: entry.link,
			Size:     int64(len(entry.body)),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if entry.body != "" {
			if _, err := tarWriter.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write tar body: %v", err)
			}
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return output.Bytes()
}
