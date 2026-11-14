//go:build darwin

package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// On a Mac the secret is encrypted with a key of this program's own, and
// that key is kept in the user's login keychain: the same promise as DPAPI
// makes on Windows, that only this user account on this Mac can read the
// secret back. The key is made the first time something is protected.
//
// macOS ties a keychain item to the program that made it. A copy of the app
// it does not recognise as the same one (a new build that is not signed by
// the same developer) is asked about once: "PartyLine wants to use your
// confidential information", which Always Allow settles.
var (
	keychainService = "app.partyline"
	keychainAccount = "stream key encryption"
	keychainLabel   = "PartyLine for VRCDN"
)

const keySize = 32 // AES-256

// OSStatus values from Security's SecBase.h.
const (
	errSecUserCanceled          = -128
	errSecAuthFailed            = -25293
	errSecDuplicateItem         = -25299
	errSecItemNotFound          = -25300
	errSecInteractionNotAllowed = -25308
)

const cfStringEncodingUTF8 = 0x08000100

var (
	libOnce sync.Once
	libErr  error

	cfStringCreateWithCString func(alloc uintptr, s string, encoding uint32) uintptr
	cfDataCreate              func(alloc uintptr, bytes *byte, length int) uintptr
	cfDataGetLength           func(data uintptr) int
	cfDataGetBytePtr          func(data uintptr) unsafe.Pointer
	cfDictionaryCreateMutable func(alloc uintptr, capacity int, keyCallBacks, valueCallBacks uintptr) uintptr
	cfDictionarySetValue      func(dict, key, value uintptr)
	cfRelease                 func(ref uintptr)

	secItemAdd          func(attributes uintptr, result *uintptr) int32
	secItemCopyMatching func(query uintptr, result *uintptr) int32
	secItemDelete       func(query uintptr) int32

	// Constants the frameworks export as variables.
	dictKeyCallBacks, dictValueCallBacks uintptr
	cfBooleanTrue                        uintptr
	secClass, secClassGenericPassword    uintptr
	secAttrService, secAttrAccount       uintptr
	secAttrLabel, secValueData           uintptr
	secReturnData                        uintptr
	secMatchLimit, secMatchLimitOne      uintptr
)

func loadLibraries() error {
	libOnce.Do(func() { libErr = openLibraries() })
	return libErr
}

func openLibraries() error {
	const flags = purego.RTLD_NOW | purego.RTLD_GLOBAL
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", flags)
	if err != nil {
		return err
	}
	sec, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", flags)
	if err != nil {
		return err
	}

	// address finds an exported variable; value reads the pointer it holds.
	var missing error
	address := func(lib uintptr, name string) uintptr {
		addr, err := purego.Dlsym(lib, name)
		if err != nil && missing == nil {
			missing = err
		}
		return addr
	}
	value := func(lib uintptr, name string) uintptr {
		addr := address(lib, name)
		if addr == 0 {
			return 0
		}
		return **(**uintptr)(unsafe.Pointer(&addr))
	}
	dictKeyCallBacks = address(cf, "kCFTypeDictionaryKeyCallBacks")
	dictValueCallBacks = address(cf, "kCFTypeDictionaryValueCallBacks")
	cfBooleanTrue = value(cf, "kCFBooleanTrue")
	secClass = value(sec, "kSecClass")
	secClassGenericPassword = value(sec, "kSecClassGenericPassword")
	secAttrService = value(sec, "kSecAttrService")
	secAttrAccount = value(sec, "kSecAttrAccount")
	secAttrLabel = value(sec, "kSecAttrLabel")
	secValueData = value(sec, "kSecValueData")
	secReturnData = value(sec, "kSecReturnData")
	secMatchLimit = value(sec, "kSecMatchLimit")
	secMatchLimitOne = value(sec, "kSecMatchLimitOne")
	if missing != nil {
		return missing
	}

	purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&cfDataCreate, cf, "CFDataCreate")
	purego.RegisterLibFunc(&cfDataGetLength, cf, "CFDataGetLength")
	purego.RegisterLibFunc(&cfDataGetBytePtr, cf, "CFDataGetBytePtr")
	purego.RegisterLibFunc(&cfDictionaryCreateMutable, cf, "CFDictionaryCreateMutable")
	purego.RegisterLibFunc(&cfDictionarySetValue, cf, "CFDictionarySetValue")
	purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	purego.RegisterLibFunc(&secItemAdd, sec, "SecItemAdd")
	purego.RegisterLibFunc(&secItemCopyMatching, sec, "SecItemCopyMatching")
	purego.RegisterLibFunc(&secItemDelete, sec, "SecItemDelete")
	return nil
}

// item starts a dictionary naming this program's keychain item. The caller
// releases it.
func item() uintptr {
	dict := cfDictionaryCreateMutable(0, 0, dictKeyCallBacks, dictValueCallBacks)
	cfDictionarySetValue(dict, secClass, secClassGenericPassword)
	setString(dict, secAttrService, keychainService)
	setString(dict, secAttrAccount, keychainAccount)
	return dict
}

// setString adds a string to a dictionary, which keeps its own reference.
func setString(dict, key uintptr, s string) {
	ref := cfStringCreateWithCString(0, s, cfStringEncodingUTF8)
	cfDictionarySetValue(dict, key, ref)
	cfRelease(ref)
}

// readKey fetches the key from the keychain.
func readKey() ([]byte, int32) {
	query := item()
	defer cfRelease(query)
	cfDictionarySetValue(query, secReturnData, cfBooleanTrue)
	cfDictionarySetValue(query, secMatchLimit, secMatchLimitOne)

	var data uintptr
	if st := secItemCopyMatching(query, &data); st != 0 {
		return nil, st
	}
	defer cfRelease(data)
	n := cfDataGetLength(data)
	if n == 0 {
		return nil, 0
	}
	return append([]byte(nil), unsafe.Slice((*byte)(cfDataGetBytePtr(data)), n)...), 0
}

// addKey stores a new key in the keychain.
func addKey(key []byte) int32 {
	attrs := item()
	defer cfRelease(attrs)
	setString(attrs, secAttrLabel, keychainLabel)
	data := cfDataCreate(0, &key[0], len(key))
	cfDictionarySetValue(attrs, secValueData, data)
	cfRelease(data)
	return secItemAdd(attrs, nil)
}

// removeKey deletes the key, and with it the way to read anything that was
// protected. Only tests do that.
func removeKey() int32 {
	query := item()
	defer cfRelease(query)
	return secItemDelete(query)
}

var (
	keyMu sync.Mutex
	key   []byte // once read; a failure is not remembered, so it can be retried
)

// masterKey returns this program's key, making one on first use.
func masterKey() ([]byte, error) {
	keyMu.Lock()
	defer keyMu.Unlock()
	if key != nil {
		return key, nil
	}
	if err := loadLibraries(); err != nil {
		return nil, fmt.Errorf("the keychain cannot be used: %w", err)
	}
	found, st := readKey()
	if st == errSecItemNotFound {
		fresh := make([]byte, keySize)
		if _, err := rand.Read(fresh); err != nil {
			return nil, err
		}
		switch st = addKey(fresh); st {
		case 0:
			found = fresh
		case errSecDuplicateItem:
			// Made by another copy of the program in the meantime.
			found, st = readKey()
		}
	}
	if st != 0 {
		return nil, keychainError(st)
	}
	if len(found) != keySize {
		return nil, errors.New("the key in the keychain is not one this program made")
	}
	key = found
	return key, nil
}

func keychainError(st int32) error {
	switch st {
	case errSecUserCanceled, errSecAuthFailed:
		return errors.New("access to the keychain was not allowed")
	case errSecInteractionNotAllowed:
		return errors.New("the keychain is locked")
	}
	return fmt.Errorf("the keychain could not be used (error %d)", st)
}

func sealer() (cipher.AEAD, error) {
	k, err := masterKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Protect encrypts plain with a key held in the user's login keychain, and
// returns it as text.
func Protect(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	gcm, err := sealer()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Reveal reverses Protect. It fails for another user or another Mac.
func Reveal(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return "", err
	}
	gcm, err := sealer()
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("this was not protected here")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("this was protected by another user or on another Mac")
	}
	return string(plain), nil
}

const supported = true
