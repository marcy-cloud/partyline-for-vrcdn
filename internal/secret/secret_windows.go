//go:build windows

package secret

import (
	"encoding/base64"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Protect encrypts plain with Windows' data protection API (DPAPI), bound to
// the current user, and returns it as text.
func Protect(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	in, out := blob([]byte(plain)), windows.DataBlob{}
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(take(out)), nil
}

// Reveal reverses Protect. It fails for another user or another PC.
func Reveal(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", err
	}
	in, out := blob(raw), windows.DataBlob{}
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	return string(take(out)), nil
}

func blob(data []byte) windows.DataBlob {
	return windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
}

// take copies a blob Windows allocated and frees the original.
func take(b windows.DataBlob) []byte {
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(b.Data)))
	return append([]byte(nil), unsafe.Slice(b.Data, b.Size)...)
}

const supported = true
