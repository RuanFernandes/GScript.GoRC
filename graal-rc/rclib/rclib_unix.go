//go:build !windows

package rclib

import (
	"fmt"

	"github.com/ebitengine/purego"
)

// proc wraps a dlsym'd symbol address (Linux/macOS). Call routes through
// purego.SyscallN, which invokes the symbol with the platform's native C ABI
// (SysV AMD64 on Linux, etc.) — NOT the stdlib syscall.Syscall family, which on
// non-Windows hosts executes the kernel `syscall` CPU instruction and treats the
// function address as a syscall number (wrong ABI, returns garbage like -1 and
// crashes when the caller dereferences it).
type proc struct {
	name string
	addr uintptr
}

// Call invokes the native symbol via purego.SyscallN under dllMu so no two
// goroutines re-enter grclib concurrently (mirrors the single-threaded reference
// client; see dllMu in rclib.go). Unlike stdlib syscall.Syscall it handles any
// arg count with the correct C calling convention, so the 8-arg
// rc_create_npc_on_server no longer needs special casing. The third return is
// forwarded for parity with the Windows sibling; call sites ignore it.
func (p *proc) Call(a ...uintptr) (uintptr, uintptr, error) {
	started := beginNativeCall(p.name)
	defer endNativeCall(started)
	r1, r2, _ := purego.SyscallN(p.addr, a...)
	return r1, r2, nil
}

// createNPCCall is the unix entry for CreateNPC (see rclib.go). purego.SyscallN
// handles the 8 args with the correct C ABI, so it goes through the normal proc.
func createNPCCall(h, name, id, npcType, scripter, level, x, y uintptr) uintptr {
	r1, _, _ := procCreateNPCOnServer.Call(h, name, id, npcType, scripter, level, x, y)
	return r1
}

// newCallback wraps a Go function as a C-callable callback via purego (pure-Go
// trampoline, no cgo) on Linux/macOS. Mirrors syscall.NewCallback on Windows.
func newCallback(fn any) uintptr { return purego.NewCallback(fn) }

// loadProcs dlopen's the native .so and resolves every grclib symbol through
// registerAll. The resolved address is stored on the proc and later invoked via
// purego.SyscallN (see proc.Call) — keeping CGO_ENABLED=0.
func loadProcs(path string) error {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("Dlopen(%s): %w", path, err)
	}
	return registerAll(func(name string) (*proc, error) {
		addr, e := purego.Dlsym(h, name)
		if e != nil {
			return nil, fmt.Errorf("Dlsym(%s): %w", name, e)
		}
		return &proc{name: name, addr: addr}, nil
	})
}
