package rclib

import (
	"fmt"
	"syscall"
	"unsafe"
)

// The native library is shipped only for 64-bit desktop targets. These limits
// are deliberately conservative protocol/ABI guardrails: a malformed C count
// must not turn into an unbounded Go allocation or slice header.
const (
	maxNativeElements = 1 << 20
	maxNativeBytes    = 512 << 20
	minCInt           = -(1 << 31)
	maxCInt           = 1<<31 - 1
)

// cString makes an owned, NUL-terminated argument for the native API. Embedded
// NULs are rejected because the C ABI cannot represent the remainder of a Go
// string; callers must not silently truncate protocol data.
func cString(value string) (*byte, error) {
	pointer, err := syscall.BytePtrFromString(value)
	if err != nil {
		return nil, fmt.Errorf("C string contains NUL: %w", err)
	}
	return pointer, nil
}

// nativePointer converts a machine word returned by the C ABI into a pointer.
//
// proc.Call cannot expose a typed pointer because syscall.Proc.Call and
// purego.SyscallN both return ABI words. The word passed here must come from a
// native return value or callback argument, never from a Go pointer kept in a
// uintptr. The type-pun keeps this one unavoidable boundary in one place; the
// callers below immediately copy/consume the native memory and never retain the
// returned pointer after the native call's ownership window.
//
// The indirection is intentional. It avoids teaching individual call sites to
// convert an arbitrary uintptr into unsafe.Pointer, which is exactly the class
// of mistake go vet's unsafeptr check is meant to catch.
func nativePointer(word uintptr) unsafe.Pointer {
	if word == 0 {
		return nil
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&word))
}

func nativeBytePointer(word uintptr) *byte {
	return (*byte)(nativePointer(word))
}

// nativeCount decodes a C int returned in an ABI word and rejects negative or
// implausibly large values before a native slice is formed.
func nativeCount(word uintptr) (int, error) {
	count := int64(int32(word))
	if count < 0 {
		return 0, fmt.Errorf("native count is negative: %d", count)
	}
	if count > maxNativeElements {
		return 0, fmt.Errorf("native count %d exceeds limit %d", count, maxNativeElements)
	}
	return int(count), nil
}

// nativeInt validates a Go integer before it is passed to a C int parameter.
// Converting an out-of-range Go int directly to uintptr would silently
// truncate the protocol value on the native side.
func nativeInt(value int) (uintptr, error) {
	value64 := int64(value)
	if value64 < minCInt || value64 > maxCInt {
		return 0, fmt.Errorf("value %d is outside C int range", value)
	}
	return uintptr(int32(value)), nil
}

// nativeSlice creates a bounded view over a C-owned array. The returned slice
// is only a temporary view: callers must copy all fields they need before the
// native owner frees or replaces the array.
func nativeSlice[T any](word uintptr, count int) ([]T, error) {
	if count < 0 || count > maxNativeElements {
		return nil, fmt.Errorf("native slice count %d exceeds limit %d", count, maxNativeElements)
	}
	if count == 0 {
		return nil, nil
	}
	if word == 0 {
		return nil, fmt.Errorf("native slice has nil pointer for count %d", count)
	}
	return unsafe.Slice((*T)(nativePointer(word)), count), nil
}

// nativeBytes copies a callback-owned byte buffer before returning from the
// callback. The length is a C int in the grclib ABI, so sign-extension and an
// upper bound are checked before unsafe.Slice is used.
func nativeBytes(pointer unsafe.Pointer, wordLength uintptr) ([]byte, error) {
	length := int64(int32(wordLength))
	if length < 0 {
		return nil, fmt.Errorf("native byte length is negative: %d", length)
	}
	if length == 0 {
		return nil, nil
	}
	if length > maxNativeBytes {
		return nil, fmt.Errorf("native byte length %d exceeds limit %d", length, maxNativeBytes)
	}
	if pointer == nil {
		return nil, fmt.Errorf("native byte buffer is nil for length %d", length)
	}
	return append([]byte(nil), unsafe.Slice((*byte)(pointer), int(length))...), nil
}
