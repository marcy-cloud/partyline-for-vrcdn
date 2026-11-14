//go:build darwin

package coreaudio

import (
	"errors"
	"fmt"
	"os"

	"github.com/ebitengine/purego"
)

// audioToolbox is where macOS keeps the encoder. The file itself only
// exists inside the system's shared cache on current versions, but the
// loader resolves the path all the same.
const audioToolbox = "/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox"

// ErrNotInstalled means Apple's encoder could not be loaded. It is part of
// macOS, so this is not expected.
var ErrNotInstalled = errors.New("Apple's AAC encoder (AudioToolbox) is not available")

// Find returns the path of AudioToolbox, or "" if this Mac has no such
// framework.
func Find() string {
	if _, err := os.Stat("/System/Library/Frameworks/AudioToolbox.framework"); err != nil {
		return ""
	}
	return audioToolbox
}

// proc is the address of a C function.
type proc uintptr

// Call calls the function. All of them return an OSStatus, a 32-bit value;
// the rest of the register is not part of it.
func (p proc) Call(args ...uintptr) (uintptr, uintptr, error) {
	r1, r2, _ := purego.SyscallN(uintptr(p), args...)
	return uintptr(uint32(r1)), r2, nil
}

func newCallback(fn any) uintptr { return purego.NewCallback(fn) }

func load() error {
	path := Find()
	if path == "" {
		return ErrNotInstalled
	}
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("loading %s: %w", path, err)
	}
	for _, p := range []struct {
		proc *proc
		name string
	}{
		{&procNew, "AudioConverterNew"},
		{&procDispose, "AudioConverterDispose"},
		{&procSetProperty, "AudioConverterSetProperty"},
		{&procGetProperty, "AudioConverterGetProperty"},
		{&procFill, "AudioConverterFillComplexBuffer"},
	} {
		addr, err := purego.Dlsym(h, p.name)
		if err != nil {
			return fmt.Errorf("%s is not usable: %w", path, err)
		}
		*p.proc = proc(addr)
	}
	libPath = path
	return nil
}
