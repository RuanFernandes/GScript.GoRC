package rclib

// This file is the only way from a Handle to the native connection.
//
// rc_disconnect ends in `delete conn`, so a call that reaches grclib with that
// pointer afterwards runs on freed memory. Callers cannot prevent it: they read
// a handle, and a logout or login on another goroutine may destroy it before
// their call gets the native lock. Liveness is therefore decided here, under the
// same dllMu hold as the native call.
//
// Exports whose first argument is the connection are declared as connectionProc,
// which has no Call method. Their native pointer exists only inside
// withConnection's callback.
//
// The same hold also covers copying results out of library-owned memory:
// rc_get_* free the previous result on their next call.

import (
	"errors"
	"fmt"
	"log"
	"unsafe"
)

// Handle identifies a live grclib connection. It is a process-unique id, never
// the native pointer: the allocator may give a deleted connection's address to
// the next one, so a pointer cannot tell a stale handle from a new session.
// Ids are never reused. Zero is never a live handle.
type Handle uintptr

// ErrInvalidHandle reports a call on a handle that was never opened or has been
// disconnected. The native library was not entered.
var ErrInvalidHandle = errors.New("invalid handle: the native connection is closed")

// Both are guarded by dllMu.
var (
	liveConnections = map[Handle]uintptr{}
	lastHandle      Handle
)

// connectionProc is an export whose first C argument is the connection.
type connectionProc struct{ export *proc }

// nativeConnection is a lease on a live connection. It is valid only inside the
// withConnection callback that received it, and must not be retained. Nothing
// running under a lease may call an exported rclib function or proc.Call:
// dllMu is not reentrant.
type nativeConnection struct{ pointer uintptr }

// withConnection runs use while holding dllMu, if h is live. first names the
// operation in native call diagnostics.
func withConnection(h Handle, first *connectionProc, use func(nativeConnection) error) error {
	if first == nil {
		// The library never loaded, so no connection can exist.
		return ErrInvalidHandle
	}
	started := beginNativeCall(first.export.name)
	defer endNativeCall(started)
	pointer, live := liveConnections[h]
	if !live {
		return ErrInvalidHandle
	}
	return use(nativeConnection{pointer: pointer})
}

// call invokes a connection export with the connection as its first argument.
//
//go:uintptrescapes
func (c nativeConnection) call(p *connectionProc, args ...uintptr) uintptr {
	return p.export.native(append([]uintptr{c.pointer}, args...))
}

// release hands a library-allocated result back to a handle-free export without
// leaving the current lock hold.
func (c nativeConnection) release(p *proc, args ...uintptr) {
	p.native(args)
}

// lastError copies the connection's error string into Go memory.
func (c nativeConnection) lastError() string {
	return bptrToString(nativeBytePointer(c.call(procLastError)))
}

// failure reports the library's reason for the call that just failed, read
// before another call can replace it.
func (c nativeConnection) failure(fallback string) error {
	if reason := c.lastError(); reason != "" {
		return errors.New(reason)
	}
	return errors.New(fallback)
}

// invokeStatus calls an export that returns int, where 0 means failure. An empty
// fallback names the export.
//
//go:uintptrescapes
func invokeStatus(h Handle, p *connectionProc, fallback string, args ...uintptr) error {
	return withConnection(h, p, func(c nativeConnection) error {
		if c.call(p, args...) != 0 {
			return nil
		}
		if fallback == "" {
			fallback = p.export.name + " failed"
		}
		return c.failure(fallback)
	})
}

// invokeValue calls an export that returns a plain value (never a pointer), and
// returns 0 for a handle that is not live.
//
//go:uintptrescapes
func invokeValue(h Handle, p *connectionProc, args ...uintptr) (value uintptr) {
	_ = withConnection(h, p, func(c nativeConnection) error {
		value = c.call(p, args...)
		return nil
	})
	return value
}

// listQuery describes an export shaped as int f(handle, T** out).
type listQuery struct {
	label string
	get   *connectionProc
	// free releases a result the caller owns; nil when the library keeps it.
	free *proc
	// logEmpty records last_error for an empty result, which otherwise hides a
	// silent failure (auth rejected, network).
	logEmpty bool
}

// copyList runs the query and converts every entry into Go-owned memory before
// releasing the lock. convert runs under dllMu: it must only copy.
func copyList[N, T any](h Handle, query listQuery, convert func(N) T) ([]T, error) {
	var out []T
	var emptyReason string
	err := withConnection(h, query.get, func(c nativeConnection) error {
		var first uintptr
		result := c.call(query.get, uintptr(unsafe.Pointer(&first)))
		count, err := nativeCount(result)
		if err != nil {
			return fmt.Errorf("%s: %w", query.label, err)
		}
		if count <= 0 || first == 0 {
			if query.logEmpty {
				emptyReason = c.lastError()
			}
			return nil
		}
		if query.free != nil {
			defer c.release(query.free, first, uintptr(count))
		}
		entries, err := nativeSlice[N](first, count)
		if err != nil {
			return fmt.Errorf("%s: %w", query.label, err)
		}
		out = make([]T, count)
		for i := range out {
			out[i] = convert(entries[i])
		}
		return nil
	})
	// Logged outside the lock so a slow log sink never stalls native calls.
	if emptyReason != "" {
		log.Printf("[rclib] %s empty list; last_error=%q", query.label, emptyReason)
	}
	return out, err
}

// openConnection calls rc_connect and publishes the result as a new Handle.
//
//go:uintptrescapes
func openConnection(args ...uintptr) (Handle, bool) {
	started := beginNativeCall(procConnect.name)
	defer endNativeCall(started)
	pointer := procConnect.native(args)
	if pointer == 0 {
		return 0, false
	}
	lastHandle++
	liveConnections[lastHandle] = pointer
	return lastHandle, true
}

// destroyConnection retires the handle and deletes the connection in one lock
// hold. The handle is retired first, so even a panicking rc_disconnect cannot
// leave it usable.
func destroyConnection(h Handle) {
	_ = withConnection(h, procDisconnect, func(c nativeConnection) error {
		delete(liveConnections, h)
		c.call(procDisconnect)
		return nil
	})
}
