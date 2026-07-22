//go:build windows

package rclib

import (
	"fmt"
	"syscall"
)

// proc wraps a syscall.Proc (Windows). Call delegates to syscall.Proc.Call so
// every call site is unchanged from the pre-cross-platform implementation.
type proc struct {
	name string
	win  *syscall.Proc
}

// Call invokes the DLL export. The third return is the syscall error (nil if
// the call did not set last_error); call sites ignore it.
func (p *proc) Call(a ...uintptr) (uintptr, uintptr, error) {
	return p.win.Call(a...)
}

// createNPCCall is the Windows entry for CreateNPC (see rclib.go). syscall.Proc
// handles arbitrary arity here, so it goes through the normal proc.
func createNPCCall(h, name, id, npcType, scripter, level, x, y uintptr) uintptr {
	r1, _, _ := procCreateNPCOnServer.Call(h, name, id, npcType, scripter, level, x, y)
	return r1
}

// newCallback wraps a Go function as a C-callable callback (syscall.NewCallback
// on Windows — correct __stdcall thunk for both amd64 and 386 DLLs).
func newCallback(fn any) uintptr { return syscall.NewCallback(fn) }

// loadProcs opens the native library via syscall.LoadLibrary and resolves every
// grclib proc through registerAll. Windows path.
func loadProcs(path string) error {
	h, err := syscall.LoadLibrary(path)
	if err != nil {
		return fmt.Errorf("LoadLibrary(%s): %w", path, err)
	}
	dll := &syscall.DLL{Handle: h}
	return registerAll(func(name string) (*proc, error) {
		p, e := dll.FindProc(name)
		if e != nil {
			return nil, fmt.Errorf("FindProc(%s): %w", name, e)
		}
		return &proc{name: name, win: p}, nil
	})
}
