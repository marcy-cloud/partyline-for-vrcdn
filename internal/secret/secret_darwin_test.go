//go:build darwin

package secret

import (
	"fmt"
	"os"
	"testing"
)

// useTestKeychainItem points the package at a keychain item of the test's
// own, so the real one is never touched, and removes it afterwards.
func useTestKeychainItem(t *testing.T) {
	service := keychainService
	keychainService = fmt.Sprintf("app.partyline.test-%d", os.Getpid())
	key = nil
	t.Cleanup(func() {
		removeKey()
		keychainService = service
		key = nil
	})
	if _, err := masterKey(); err != nil {
		t.Skipf("no usable keychain here: %v", err)
	}
}

func TestProtectAndReveal(t *testing.T) {
	useTestKeychainItem(t)
	const streamKey = "vrcdn_example_key_0123456789"
	stored, err := Protect(streamKey)
	if err != nil {
		t.Fatal(err)
	}
	if stored == streamKey || len(stored) < len(streamKey) {
		t.Fatalf("stored form %q is not protected", stored)
	}
	got, err := Reveal(stored)
	if err != nil || got != streamKey {
		t.Fatalf("Reveal = %q, %v", got, err)
	}
	if again, _ := Protect(streamKey); again == stored {
		t.Error("protecting the same key twice gave the same text")
	}
	if _, err := Reveal("not base64 at all!"); err == nil {
		t.Error("garbage should not reveal anything")
	}
	if _, err := Reveal("aGVsbG8gd29ybGQ="); err == nil {
		t.Error("text that was never protected should not reveal anything")
	}
}

// The key is read back from the keychain by a later run, and without it
// nothing can be revealed.
func TestTheKeyLivesInTheKeychain(t *testing.T) {
	useTestKeychainItem(t)
	stored, err := Protect("a stream key")
	if err != nil {
		t.Fatal(err)
	}

	key = nil // as a new run of the program would start
	if got, err := Reveal(stored); err != nil || got != "a stream key" {
		t.Fatalf("after forgetting the key: Reveal = %q, %v", got, err)
	}

	if st := removeKey(); st != 0 {
		t.Fatalf("removing the key: error %d", st)
	}
	key = nil
	if got, err := Reveal(stored); err == nil {
		t.Errorf("with the key gone, Reveal = %q", got)
	}
}
