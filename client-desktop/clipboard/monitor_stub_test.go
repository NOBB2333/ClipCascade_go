package clipboard

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/clipcascade/pkg/constants"
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

func TestContentHashNormalizesEquivalentImages(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(20 * x), G: uint8(30 * y), B: 180, A: 255})
		}
	}

	var slow bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&slow, img); err != nil {
		t.Fatalf("encode slow png: %v", err)
	}

	var fast bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.NoCompression}).Encode(&fast, img); err != nil {
		t.Fatalf("encode fast png: %v", err)
	}

	if bytes.Equal(slow.Bytes(), fast.Bytes()) {
		t.Fatal("expected different PNG encodings for the same image")
	}

	hashA := contentHash(base64.StdEncoding.EncodeToString(slow.Bytes()), constants.TypeImage)
	hashB := contentHash(base64.StdEncoding.EncodeToString(fast.Bytes()), constants.TypeImage)
	if hashA != hashB {
		t.Fatalf("equivalent images should share content hash: %d != %d", hashA, hashB)
	}
}
