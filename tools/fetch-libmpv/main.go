// Command fetch-libmpv downloads libmpv-2.dll for Windows (shinchiro's mpv
// builds) and puts it in the two places the project uses it:
//
//	build/bin/libmpv-2.dll                   for `wails dev`, probe.exe and plain builds
//	internal/libmpv/bundled/libmpv-2.dll.gz  compiled into the exe by
//	internal/libmpv/bundled/manifest.json    builds with -tags bundled_libmpv
//
// Run it from the repository root, on any OS:
//
//	go run -C tools/fetch-libmpv .                 # latest release
//	go run -C tools/fetch-libmpv . -tag 20261111   # a specific release
//	go run -C tools/fetch-libmpv . -archive C:\Downloads\mpv-dev-x86_64-….7z
//
// It is its own Go module so the 7z reader it needs stays out of the
// player's dependencies.
package main

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/bodgit/sevenzip"
)

const (
	releasesAPI = "https://api.github.com/repos/shinchiro/mpv-winbuild-cmake/releases"
	dllName     = "libmpv-2.dll"
)

// "mpv-dev-x86_64-<date>…": the plain x86-64 build, not the -v3 (AVX2) one,
// which would not start on older CPUs.
var assetPattern = regexp.MustCompile(`^mpv-dev-x86_64-\d.*\.7z$`)

// manifest must stay in step with libmpv.Manifest.
type manifest struct {
	File    string `json:"file"`
	Release string `json:"release"`
	Asset   string `json:"asset"`
	Source  string `json:"source"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

func main() {
	tag := flag.String("tag", "", "release tag to fetch (default: the latest release)")
	archive := flag.String("archive", "", "use this already-downloaded mpv-dev .7z instead of downloading")
	root := flag.String("root", filepath.Join("..", ".."), "repository root")
	flag.Parse()

	if err := run(*root, *tag, *archive); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(root, tag, archive string) error {
	if _, err := os.Stat(filepath.Join(root, "wails.json")); err != nil {
		return fmt.Errorf("%s does not look like the repository root (no wails.json); pass -root", root)
	}

	m := manifest{File: dllName, Release: "local", Asset: filepath.Base(archive)}
	if archive == "" {
		rel, err := findAsset(tag)
		if err != nil {
			return err
		}
		m.Release, m.Asset, m.Source = rel.tag, rel.name, rel.url

		// A fresh folder per run, so nothing left by an earlier download
		// is ever picked up.
		work, err := os.MkdirTemp("", "fetch-libmpv-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)

		archive = filepath.Join(work, rel.name)
		fmt.Printf("Downloading %s (%d MB)...\n", rel.name, rel.size>>20)
		if err := download(rel.url, archive); err != nil {
			return err
		}
	}

	binDir := filepath.Join(root, "build", "bin")
	bundleDir := filepath.Join(root, "internal", "libmpv", "bundled")
	for _, d := range []string{binDir, bundleDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}

	fmt.Printf("Extracting %s...\n", dllName)
	gzPath := filepath.Join(bundleDir, dllName+".gz")
	if err := extract(archive, filepath.Join(binDir, dllName), gzPath, &m); err != nil {
		return err
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(bundleDir, "manifest.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}

	gz, err := os.Stat(gzPath)
	if err != nil {
		return err
	}
	fmt.Printf("Done: libmpv %s, %d MB (%d MB compressed for embedding)\n", m.Release, m.Size>>20, gz.Size()>>20)
	return nil
}

type asset struct {
	tag, name, url string
	size           int64
}

func findAsset(tag string) (asset, error) {
	url := releasesAPI + "/latest"
	if tag != "" {
		url = releasesAPI + "/tags/" + tag
	}
	resp, err := get(url)
	if err != nil {
		return asset{}, err
	}
	defer resp.Body.Close()

	var rel struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return asset{}, fmt.Errorf("reading the release list: %w", err)
	}
	for _, a := range rel.Assets {
		if assetPattern.MatchString(a.Name) {
			return asset{tag: rel.TagName, name: a.Name, url: a.URL, size: a.Size}, nil
		}
	}
	return asset{}, fmt.Errorf("release %s has no mpv-dev-x86_64 archive", rel.TagName)
}

func get(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fetch-libmpv") // GitHub's API rejects requests without one
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func download(url, to string) error {
	resp, err := get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("downloading %s: %w", url, err)
	}
	return f.Close()
}

// extract writes the DLL from the archive to dllPath and, gzip-compressed,
// to gzPath, filling in the manifest's hash and size. Only the one named
// entry is read, and only to the two paths given.
func extract(archive, dllPath, gzPath string, m *manifest) error {
	r, err := sevenzip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("opening %s: %w", archive, err)
	}
	defer r.Close()

	var entry *sevenzip.File
	for _, f := range r.File {
		if f.Name == dllName {
			entry = f
			break
		}
	}
	if entry == nil {
		return fmt.Errorf("%s is not in %s", dllName, archive)
	}
	src, err := entry.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	dll, err := os.Create(dllPath + ".tmp")
	if err != nil {
		return err
	}
	defer os.Remove(dllPath + ".tmp")
	gzFile, err := os.Create(gzPath + ".tmp")
	if err != nil {
		dll.Close()
		return err
	}
	defer os.Remove(gzPath + ".tmp")

	zw, _ := gzip.NewWriterLevel(gzFile, gzip.BestCompression)
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(dll, zw, hash), src)
	if err := errors.Join(copyErr, zw.Close(), gzFile.Close(), dll.Close()); err != nil {
		return fmt.Errorf("extracting %s: %w", dllName, err)
	}

	m.Size = n
	m.SHA256 = hex.EncodeToString(hash.Sum(nil))

	// The DLL goes first: it is the one that can be locked (by a running
	// player or probe), and failing here leaves the embed files untouched.
	if !hasHash(dllPath, m.SHA256) {
		if err := os.Rename(dllPath+".tmp", dllPath); err != nil {
			return fmt.Errorf("%w\n%s is probably in use: close the player and probe, then run this again", err, dllPath)
		}
	}
	return os.Rename(gzPath+".tmp", gzPath)
}

// hasHash reports whether the file at path exists with the given SHA-256.
func hasHash(path, want string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return false
	}
	return hex.EncodeToString(hash.Sum(nil)) == want
}
