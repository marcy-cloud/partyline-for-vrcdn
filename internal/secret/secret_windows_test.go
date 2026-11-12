//go:build windows

package secret

import "testing"

func TestProtectAndReveal(t *testing.T) {
	const key = "vrcdn_example_key_0123456789"
	stored, err := Protect(key)
	if err != nil {
		t.Fatal(err)
	}
	if stored == key || len(stored) < len(key) {
		t.Fatalf("stored form %q is not protected", stored)
	}
	got, err := Reveal(stored)
	if err != nil || got != key {
		t.Fatalf("Reveal = %q, %v", got, err)
	}
	if _, err := Reveal("not base64 at all!"); err == nil {
		t.Error("garbage should not reveal anything")
	}
	if _, err := Reveal("aGVsbG8gd29ybGQ="); err == nil {
		t.Error("text that was never protected should not reveal anything")
	}
}
