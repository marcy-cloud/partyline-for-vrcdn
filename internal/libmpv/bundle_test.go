package libmpv

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func bundleFixture(t *testing.T, content string) (Manifest, []byte) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	zw.Write([]byte(content))
	zw.Close()
	sum := sha256.Sum256([]byte(content))
	return Manifest{File: "libmpv-2.dll", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}, buf.Bytes()
}

func TestUnpack(t *testing.T) {
	root := t.TempDir()
	m, gz := bundleFixture(t, "not really a dll")

	path, err := unpack(root, m, gz)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "not really a dll" {
		t.Errorf("unpacked content = %q", got)
	}
	if filepath.Base(path) != "libmpv-2.dll" || filepath.Dir(filepath.Dir(path)) != root {
		t.Errorf("unexpected location %s", path)
	}

	// Already there: must not need the compressed data again.
	again, err := unpack(root, m, nil)
	if err != nil || again != path {
		t.Errorf("second unpack = %q, %v; want the same path without unpacking", again, err)
	}

	// A new version replaces the old copy.
	m2, gz2 := bundleFixture(t, "a newer build")
	path2, err := unpack(root, m2, gz2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("old version still present (%v)", err)
	}
	if _, err := os.Stat(path2); err != nil {
		t.Error(err)
	}
}

func TestUnpackRejectsBadData(t *testing.T) {
	m, gz := bundleFixture(t, "payload")

	t.Run("checksum", func(t *testing.T) {
		root := t.TempDir()
		wrong := m
		wrong.SHA256 = hex.EncodeToString(make([]byte, sha256.Size))
		if _, err := unpack(root, wrong, gz); err == nil {
			t.Fatal("want an error for a checksum mismatch")
		}
		// Nothing half-written may be left where a later run would trust it.
		files, _ := filepath.Glob(filepath.Join(root, "*", "*"))
		if len(files) != 0 {
			t.Errorf("left behind: %v", files)
		}
	})

	t.Run("not gzip", func(t *testing.T) {
		if _, err := unpack(t.TempDir(), m, []byte("plain")); err == nil {
			t.Fatal("want an error for corrupt data")
		}
	})

	t.Run("file name with a path", func(t *testing.T) {
		evil := m
		evil.File = "../libmpv-2.dll"
		if _, err := unpack(t.TempDir(), evil, gz); err == nil {
			t.Fatal("want an error for a manifest file name with a path in it")
		}
	})

	t.Run("truncated copy on disk", func(t *testing.T) {
		root := t.TempDir()
		path, err := unpack(root, m, gz)
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(path, []byte("pay"), 0o644)
		if _, err := unpack(root, m, gz); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(path); string(got) != "payload" {
			t.Errorf("truncated copy was not replaced: %q", got)
		}
	})
}
