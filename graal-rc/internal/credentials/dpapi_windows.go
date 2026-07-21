// SPDX-License-Identifier: LGPL-2.0-only
//go:build windows

package credentials

import (
	"errors"
	"syscall"
	"unsafe"
)

// dataBlob mirrors the Windows DATA_BLOB used by the DPAPI functions.
type dataBlob struct {
	cbData uint32
	pbData *byte
}

var (
	crypt32       = syscall.NewLazyDLL("crypt32.dll")
	kernel32      = syscall.NewLazyDLL("kernel32.dll")
	procProtect   = crypt32.NewProc("CryptProtectData")
	procUnprotect = crypt32.NewProc("CryptUnprotectData")
	procLocalFree = kernel32.NewProc("LocalFree")
)

// errEmpty is returned when protecting/unprotecting a zero-length buffer; DPAPI
// rejects empty inputs, so handle them explicitly.
var errEmpty = errors.New("dpapi: empty data")

// localFree releases memory that DPAPI allocated via LocalAlloc.
func localFree(p *byte) {
	procLocalFree.Call(uintptr(unsafe.Pointer(p)))
}

// protect encrypts data with DPAPI (CryptProtectData). The key is implicit to
// the current Windows user and machine, so only this user on this machine can
// call unprotect successfully.
func protect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errEmpty
	}
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob
	r, _, e := procProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // description (optional)
		0, // optional entropy
		0, // reserved
		0, // prompt struct
		0, // flags
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, e
	}
	defer localFree(out.pbData)
	return copyBytes(out.pbData, out.cbData), nil
}

// unprotect decrypts data produced by protect (CryptUnprotectData).
func unprotect(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errEmpty
	}
	in := dataBlob{cbData: uint32(len(data)), pbData: &data[0]}
	var out dataBlob
	r, _, e := procUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // description out (optional)
		0, // optional entropy
		0, // reserved
		0, // prompt struct
		0, // flags
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, e
	}
	defer localFree(out.pbData)
	return copyBytes(out.pbData, out.cbData), nil
}

// copyBytes returns a fresh Go-owned copy of the DPAPI-allocated buffer so the
// caller's result outlives the LocalFree that releases the original.
func copyBytes(p *byte, n uint32) []byte {
	if p == nil || n == 0 {
		return nil
	}
	out := make([]byte, int(n))
	copy(out, unsafe.Slice(p, n))
	return out
}
