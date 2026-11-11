package libmpv

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Manifest describes a libmpv build compiled into the program. It is written
// by tools/fetch-libmpv next to the compressed library.
type Manifest struct {
	File    string `json:"file"`    // file name to unpack as, e.g. libmpv-2.dll
	Release string `json:"release"` // upstream release it came from
	Asset   string `json:"asset"`   // upstream archive name
	Source  string `json:"source"`  // where the archive was downloaded from
	SHA256  string `json:"sha256"`  // of the unpacked file
	Size    int64  `json:"size"`    // of the unpacked file
}

// cacheDirName is this program's folder under the user cache directory.
const cacheDirName = "vrcdn-lowlatency-player"

// Bundled reports the libmpv build compiled into this program, if there is
// one (builds with -tags bundled_libmpv).
func Bundled() (Manifest, bool) {
	if len(bundledGz) == 0 {
		return Manifest{}, false
	}
	var m Manifest
	if err := json.Unmarshal(bundledManifest, &m); err != nil {
		return Manifest{}, false
	}
	return m, true
}

// unpackBundled makes the built-in library available as a file, which is the
// only form the OS can load, and returns its path. It returns "" when the
// program has no built-in library.
func unpackBundled() (string, error) {
	if len(bundledGz) == 0 {
		return "", nil
	}
	m, ok := Bundled()
	if !ok {
		return "", errors.New("the built-in libmpv has no readable manifest")
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return unpack(filepath.Join(cache, cacheDirName, "libmpv"), m, bundledGz)
}

// unpack writes the gzip-compressed library to root/<hash prefix>/<file>
// unless it is already there, and removes copies left by other versions.
func unpack(root string, m Manifest, gz []byte) (string, error) {
	if m.File == "" || filepath.Base(m.File) != m.File || len(m.SHA256) != sha256.Size*2 {
		return "", errors.New("the built-in libmpv has an invalid manifest")
	}
	version := m.SHA256[:16]
	dir := filepath.Join(root, version)
	target := filepath.Join(dir, m.File)

	present := func() bool {
		st, err := os.Stat(target)
		return err == nil && st.Mode().IsRegular() && st.Size() == m.Size
	}
	if present() {
		return target, nil
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return "", fmt.Errorf("the built-in libmpv is corrupt: %w", err)
	}
	// Written under another name and renamed, so a half-written file is
	// never mistaken for the library.
	tmp, err := os.CreateTemp(dir, m.File+".*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())

	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, hash), zr)
	if err := errors.Join(copyErr, tmp.Close()); err != nil {
		return "", fmt.Errorf("unpacking the built-in libmpv: %w", err)
	}
	if n != m.Size || hex.EncodeToString(hash.Sum(nil)) != m.SHA256 {
		return "", errors.New("the built-in libmpv is corrupt (checksum mismatch)")
	}

	if err := os.Rename(tmp.Name(), target); err != nil && !present() {
		// (If it is present, another instance unpacked it meanwhile.)
		return "", err
	}
	removeOtherVersions(root, version)
	return target, nil
}

// removeOtherVersions deletes the copies unpacked by other versions of the
// program, which would otherwise pile up at over 100 MB each. Failures are
// fine: a copy still in use by a running instance cannot be deleted.
func removeOtherVersions(root, keep string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep {
			_ = os.RemoveAll(filepath.Join(root, e.Name()))
		}
	}
}
