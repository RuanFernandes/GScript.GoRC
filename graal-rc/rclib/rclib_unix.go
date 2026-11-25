//go:build !windows

package rclib

import (
	"fmt"
	"runtime"

	"github.com/ebitengine/purego"
)

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
		// 386 passes every argument on the stack by position, so only the
		// register conventions need the typed binding.
		if runtime.GOARCH != "386" {
			if native, ok := floatABINative(name, func(fptr any) { purego.RegisterFunc(fptr, addr) }); ok {
				return &proc{name: name, native: native}, nil
			}
		}
		// purego.SyscallN invokes the symbol with the platform's native C ABI
		// (SysV AMD64 on Linux, etc.) — NOT the stdlib syscall.Syscall family,
		// which on non-Windows hosts executes the kernel `syscall` instruction.
		return &proc{name: name, native: func(a []uintptr) uintptr {
			r1, _, _ := purego.SyscallN(addr, a...)
			return r1
		}}, nil
	})
}
