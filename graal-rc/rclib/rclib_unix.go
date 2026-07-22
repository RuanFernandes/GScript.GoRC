//go:build !windows

package rclib

import (
	"fmt"
	"syscall"

	"github.com/ebitengine/purego"
)

// proc wraps a dlsym'd symbol address (Linux/macOS). Call routes through the
// stdlib syscall.Syscall family (no cgo); the largest call site is 8 args
// (CreateNPC), so Syscall9 covers the ceiling.
type proc struct {
	name string
	addr uintptr
}

// Call invokes the native symbol via syscall.Syscall/Syscall6. Linux's syscall
// package has no nargs parameter (unlike Windows) and caps at 6 args, so the
// single 8-arg export (rc_create_npc_on_server) is routed through createNPCNative
// instead. The third return (syscall.Errno) satisfies the error-typed return the
// Windows sibling uses; call sites ignore it.
func (p *proc) Call(a ...uintptr) (uintptr, uintptr, error) {
	switch len(a) {
	case 0, 1, 2, 3:
		return syscall.Syscall(p.addr, arg(a, 0), arg(a, 1), arg(a, 2))
	case 4, 5, 6:
		return syscall.Syscall6(p.addr, arg(a, 0), arg(a, 1), arg(a, 2), arg(a, 3), arg(a, 4), arg(a, 5))
	default:
		// Should never happen — CreateNPC uses createNPCNative. Guard anyway.
		panic("rclib: proc.Call exceeds 6 args on linux; route through purego instead")
	}
}

// createNPCNative is the typed binding for rc_create_npc_on_server (8 args,
// beyond syscall.Syscall6's reach on Linux). Registered via purego.RegisterFunc
// in loadProcs.
var createNPCNative func(h, name, id, npcType, scripter, level, x, y uintptr) uintptr

// createNPCCall is the unix entry for CreateNPC (see rclib.go).
func createNPCCall(h, name, id, npcType, scripter, level, x, y uintptr) uintptr {
	return createNPCNative(h, name, id, npcType, scripter, level, x, y)
}

// newCallback wraps a Go function as a C-callable callback via purego (pure-Go
// trampoline, no cgo) on Linux/macOS. Mirrors syscall.NewCallback on Windows.
func newCallback(fn any) uintptr { return purego.NewCallback(fn) }

// loadProcs dlopen's the native .so and resolves every grclib symbol through
// registerAll. The resolved address is stored on the proc and later invoked via
// syscall.Syscall (see proc.Call) — keeping CGO_ENABLED=0.
func loadProcs(path string) error {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("Dlopen(%s): %w", path, err)
	}
	// 8-arg export can't go through syscall.Syscall6 (Linux caps at 6); bind it
	// as a typed func via purego instead.
	if addr, e := purego.Dlsym(h, "rc_create_npc_on_server"); e == nil {
		purego.RegisterFunc(&createNPCNative, addr)
	} else {
		return fmt.Errorf("Dlsym(rc_create_npc_on_server): %w", e)
	}
	return registerAll(func(name string) (*proc, error) {
		addr, e := purego.Dlsym(h, name)
		if e != nil {
			return nil, fmt.Errorf("Dlsym(%s): %w", name, e)
		}
		return &proc{name: name, addr: addr}, nil
	})
}
