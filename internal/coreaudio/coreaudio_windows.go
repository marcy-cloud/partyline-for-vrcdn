//go:build windows

package coreaudio

import (
	"errors"
	"fmt"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

type proc = *windows.LazyProc

func newCallback(fn any) uintptr { return syscall.NewCallback(fn) }

// ErrNotInstalled means Apple's encoder is not on this PC.
var ErrNotInstalled = errors.New("Apple's AAC encoder is not installed (it comes with iTunes)")

// Find returns the path of CoreAudioToolbox.dll, or "" if it isn't installed.
func Find() string {
	// Apple Application Support records where it lives.
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Apple Inc.\Apple Application Support`, registry.QUERY_VALUE); err == nil {
		defer k.Close()
		if dir, _, err := k.GetStringValue("InstallDir"); err == nil {
			if p := filepath.Join(dir, "CoreAudioToolbox.dll"); exists(p) {
				return p
			}
		}
	}
	return ""
}

func exists(path string) bool {
	_, err := windows.GetFileAttributes(windows.StringToUTF16Ptr(path))
	return err == nil
}

func load() error {
	dllPath := Find()
	if dllPath == "" {
		return ErrNotInstalled
	}
	// The DLL leans on others in its own folder; this flag makes Windows
	// look there for them.
	h, err := windows.LoadLibraryEx(dllPath, 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		return fmt.Errorf("loading %s: %w", dllPath, err)
	}
	dll := &windows.LazyDLL{Name: dllPath}
	_ = h
	procNew = dll.NewProc("AudioConverterNew")
	procDispose = dll.NewProc("AudioConverterDispose")
	procSetProperty = dll.NewProc("AudioConverterSetProperty")
	procGetProperty = dll.NewProc("AudioConverterGetProperty")
	procFill = dll.NewProc("AudioConverterFillComplexBuffer")
	for _, p := range []*windows.LazyProc{procNew, procDispose, procSetProperty, procGetProperty, procFill} {
		if err := p.Find(); err != nil {
			return fmt.Errorf("%s is not usable: %w", dllPath, err)
		}
	}
	libPath = dllPath
	return nil
}
