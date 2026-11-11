//go:build !windows

package libmpv

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/ebitengine/purego"
)

func libNames() []string {
	if runtime.GOOS == "darwin" {
		return []string{"libmpv.2.dylib", "libmpv.dylib"}
	}
	return []string{"libmpv.so.2", "libmpv.so.1", "libmpv.so"}
}

// explicitCandidates lists the places a user can put a libmpv library to
// have it used: LIBMPV_PATH and next to the program.
func explicitCandidates() []string {
	var out []string
	if p := os.Getenv("LIBMPV_PATH"); p != "" {
		out = append(out, p)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, n := range libNames() {
			out = append(out, filepath.Join(dir, n), filepath.Join(dir, "lib", n))
		}
	}
	return out
}

// systemCandidates lists the installed copies: bare names go through the
// system loader's search path, then Homebrew's directories on macOS.
func systemCandidates() []string {
	names := libNames()
	out := append([]string{}, names...)
	if runtime.GOOS == "darwin" {
		for _, d := range []string{"/opt/homebrew/lib", "/usr/local/lib"} {
			for _, n := range names {
				out = append(out, filepath.Join(d, n))
			}
		}
	}
	return out
}

func openLibrary(name string) (uintptr, error) {
	return purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
}

var setlocale func(category int32, locale string) uintptr

// prepareProcess resolves setlocale. libmpv links libc, so the symbol is
// reachable through its handle.
func prepareProcess(handle uintptr) {
	purego.RegisterLibFunc(&setlocale, handle, "setlocale")
}

// resetNumericLocale forces LC_NUMERIC back to "C". GTK calls
// setlocale(LC_ALL, "") on startup, and mpv_create fails under any other
// numeric locale.
func resetNumericLocale() {
	if setlocale == nil {
		return
	}
	lcNumeric := int32(1) // glibc, musl
	if runtime.GOOS == "darwin" {
		lcNumeric = 4
	}
	setlocale(lcNumeric, "C")
}
