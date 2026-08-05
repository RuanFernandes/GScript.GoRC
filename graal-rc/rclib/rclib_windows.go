//go:build windows

package rclib

import (
	"fmt"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

// procSetDllDirectoryW adds a directory to the DLL search path so grclib64.dll's
// MinGW runtime dependencies (libgcc_s_seh-1.dll, libstdc++-6.dll,
// libwinpthread-1.dll) resolve from the folder they ship in. Plain
// syscall.LoadLibrary searches only the process exe dir / system dirs / cwd, so
// the DLL's own folder would be missed and LoadLibrary fail with "module not
// found".
var procSetDllDirectoryW = syscall.NewLazyDLL("kernel32.dll").NewProc("SetDllDirectoryW")

// proc wraps a syscall.Proc (Windows). Call delegates to syscall.Proc.Call so
// every call site is unchanged from the pre-cross-platform implementation.
type proc struct {
	name string
	win  *syscall.Proc
}

// Call invokes the DLL export under dllMu so no two goroutines re-enter grclib
// concurrently (mirrors the single-threaded reference client; see dllMu in
// rclib.go). The third return is the syscall error (nil if the call did not set
// last_error); call sites ignore it.
func (p *proc) Call(a ...uintptr) (uintptr, uintptr, error) {
	dllMu.Lock()
	defer dllMu.Unlock()
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
	// grclib64.dll is built with MinGW and imports libgcc_s_seh-1.dll,
	// libstdc++-6.dll and libwinpthread-1.dll. LoadLibrary only searches those
	// dependencies in the process exe dir / system dirs / cwd by default — not
	// the DLL's own folder — so a "module not found" is raised even when the
	// three runtime DLLs sit right next to grclib64.dll. Adding the lib's
	// directory to the search path makes them resolve.
	if dirUTF16, e := syscall.UTF16PtrFromString(filepath.Dir(path)); e == nil {
		procSetDllDirectoryW.Call(uintptr(unsafe.Pointer(dirUTF16)))
		runtime.KeepAlive(dirUTF16)
	}
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
