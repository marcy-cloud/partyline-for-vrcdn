//go:build windows

package libmpv

import (
	"os"
	"path/filepath"
	"syscall"
)

var dllNames = []string{"libmpv-2.dll", "mpv-2.dll", "libmpv.dll", "mpv-1.dll"}

// explicitCandidates lists the places a user can put a libmpv DLL to have it
// used: LIBMPV_PATH and next to the program.
func explicitCandidates() []string {
	var out []string
	if p := os.Getenv("LIBMPV_PATH"); p != "" {
		out = append(out, p)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, n := range dllNames {
			out = append(out, filepath.Join(dir, n), filepath.Join(dir, "lib", n))
		}
	}
	return out
}

// systemCandidates falls back to the normal DLL search order (PATH etc.).
func systemCandidates() []string { return dllNames }

func openLibrary(name string) (uintptr, error) {
	h, err := syscall.LoadLibrary(name)
	return uintptr(h), err
}

// prepareProcess is a no-op on Windows: the CRT starts in the "C" locale and
// WebView2 does not change it.
func prepareProcess(uintptr) {}

func resetNumericLocale() {}
