package clipboard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildAndParseFileStubPayload(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "a.txt")
	f2 := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(f1, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write f1: %v", err)
	}
	if err := os.WriteFile(f2, []byte("123456789"), 0o644); err != nil {
		t.Fatalf("write f2: %v", err)
	}

	payload := buildFileStubPayload([]string{f1, f2})
	meta := parseFileStubPayload(payload)

	if meta.Count != 2 {
		t.Fatalf("count mismatch: got %d", meta.Count)
	}
	if meta.TotalBytes != int64(5+9) {
		t.Fatalf("total bytes mismatch: got %d", meta.TotalBytes)
	}
	if len(meta.Names) != 2 || meta.Names[0] != "a.txt" || meta.Names[1] != "b.bin" {
		t.Fatalf("unexpected names: %#v", meta.Names)
	}
}

func TestParseLegacyFileStubPayload(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "legacy.txt")
	if err := os.WriteFile(f1, []byte("abc"), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	meta := parseFileStubPayload(f1 + "\n")
	if meta.Count != 1 {
		t.Fatalf("count mismatch: got %d", meta.Count)
	}
	if meta.TotalBytes != 3 {
		t.Fatalf("total bytes mismatch: got %d", meta.TotalBytes)
	}
	if len(meta.Names) != 1 || meta.Names[0] != "legacy.txt" {
		t.Fatalf("unexpected names: %#v", meta.Names)
	}
}
