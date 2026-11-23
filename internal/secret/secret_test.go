//go:build windows

package secret

import (
	"strings"
	"testing"
)

func TestProtectRoundTrip(t *testing.T) {
	for _, plaintext := range []string{"hunter2", "p@ss:word/with?chars", "пароль", strings.Repeat("x", 512)} {
		encoded, err := Protect(plaintext)
		if err != nil {
			t.Fatalf("%q: %v", plaintext, err)
		}
		if encoded == "" {
			t.Fatalf("%q produced an empty blob", plaintext)
		}
		if strings.Contains(encoded, plaintext) && len(plaintext) > 3 {
			t.Fatalf("the ciphertext contains the plaintext")
		}
		decoded, err := Unprotect(encoded)
		if err != nil {
			t.Fatalf("%q: %v", plaintext, err)
		}
		if decoded != plaintext {
			t.Fatalf("round trip gave %q, want %q", decoded, plaintext)
		}
	}
}

// DPAPI salts every call, so the same value encrypts differently each time.
func TestProtectIsNotDeterministic(t *testing.T) {
	first, err := Protect("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Protect("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("two encryptions of the same value are identical")
	}
}

func TestEmptyPlaintext(t *testing.T) {
	encoded, err := Protect("")
	if err != nil || encoded != "" {
		t.Fatalf("Protect(\"\") = %q, %v", encoded, err)
	}
	decoded, err := Unprotect("")
	if err != nil || decoded != "" {
		t.Fatalf("Unprotect(\"\") = %q, %v", decoded, err)
	}
}

func TestUnprotectRejectsGarbage(t *testing.T) {
	for _, blob := range []string{"not base64!!!", "AAAA", "bm90IGEgZHBhcGkgYmxvYg=="} {
		if decoded, err := Unprotect(blob); err == nil {
			t.Errorf("%q decrypted to %q, want an error", blob, decoded)
		}
	}
}

func TestUnprotectForeignBlob(t *testing.T) {
	// A well formed base64 blob that is not DPAPI output at all.
	encoded, err := Protect("secret")
	if err != nil {
		t.Fatal(err)
	}
	tampered := encoded[:len(encoded)-4] + "AAAA"
	if _, err := Unprotect(tampered); err == nil {
		t.Fatal("a tampered blob must not decrypt")
	}
}
