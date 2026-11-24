//go:build windows

// Package secret encrypts small values with the Windows Data Protection API.
//
// The point is a saved proxy password: CryptProtectData ties the ciphertext to
// the current Windows account, so config.json is useless when copied to another
// machine or read by another Windows account. It does not defend against other
// programs running as the same user; see the entropy comment below.
package secret

import (
	"encoding/base64"
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// cryptProtectUIForbidden keeps DPAPI from showing a prompt: this program runs
// without a console and with a window that must not be blocked.
const cryptProtectUIForbidden = 0x1

var (
	crypt32  = syscall.NewLazyDLL("crypt32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	procLocalFree          = kernel32.NewProc("LocalFree")
)

// entropy is mixed into the key. It is a constant in the source, so it does not
// stop another program running as the same user: anyone who reads this file can
// call DPAPI on the same bytes and get the plaintext back. What DPAPI itself
// buys is the account binding — the blob cannot be decrypted from another
// Windows account or another machine, so a copied config.json is useless.
var entropy = []byte("mctunnel/config/v1")

// dataBlob mirrors DATA_BLOB.
type dataBlob struct {
	size uint32
	data *byte
}

// Protect encrypts plaintext and returns a base64 blob safe to store on disk.
func Protect(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	in, err := blobOf([]byte(plaintext))
	if err != nil {
		return "", err
	}

	extra, err := blobOf(entropy)
	if err != nil {
		return "", err
	}

	var out dataBlob
	ok, _, callErr := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // no description
		uintptr(unsafe.Pointer(&extra)),
		0, // reserved
		0, // no prompt
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return "", fmt.Errorf("DPAPI could not encrypt the value: %w", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	return base64.StdEncoding.EncodeToString(unsafe.Slice(out.data, out.size)), nil
}

// Unprotect reverses Protect. It fails when the blob belongs to another Windows
// account or another machine.
func Unprotect(encoded string) (string, error) {
	if encoded == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("stored secret is not valid base64: %w", err)
	}
	if len(raw) == 0 {
		return "", nil
	}
	in, err := blobOf(raw)
	if err != nil {
		return "", err
	}

	extra, err := blobOf(entropy)
	if err != nil {
		return "", err
	}

	var out dataBlob
	ok, _, callErr := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0,
		uintptr(unsafe.Pointer(&extra)),
		0,
		0,
		cryptProtectUIForbidden,
		uintptr(unsafe.Pointer(&out)),
	)
	if ok == 0 {
		return "", fmt.Errorf("DPAPI could not decrypt the value (was the file copied from another account?): %w", callErr)
	}
	defer procLocalFree.Call(uintptr(unsafe.Pointer(out.data)))
	return string(unsafe.Slice(out.data, out.size)), nil
}

var errEmpty = errors.New("secret: nothing to protect")

func blobOf(data []byte) (dataBlob, error) {
	if len(data) == 0 {
		return dataBlob{}, errEmpty
	}
	return dataBlob{size: uint32(len(data)), data: &data[0]}, nil
}
