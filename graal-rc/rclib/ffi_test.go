package rclib

import (
	"strings"
	"testing"
	"unsafe"
)

func TestNativeABIStructLayouts(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("grclib is shipped only for 64-bit targets")
	}

	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"RCServer size", unsafe.Sizeof(RCServer{}), 56},
		{"RCServer.Name", unsafe.Offsetof(RCServer{}.Name), 0},
		{"RCServer.IP", unsafe.Offsetof(RCServer{}.IP), 8},
		{"RCServer.Port", unsafe.Offsetof(RCServer{}.Port), 16},
		{"RCServer.Players", unsafe.Offsetof(RCServer{}.Players), 20},
		{"RCServer.Language", unsafe.Offsetof(RCServer{}.Language), 24},
		{"RCServer.Description", unsafe.Offsetof(RCServer{}.Description), 32},
		{"RCServer.Version", unsafe.Offsetof(RCServer{}.Version), 40},
		{"RCServer.Homepage", unsafe.Offsetof(RCServer{}.Homepage), 48},
		{"RCPlayer size", unsafe.Sizeof(RCPlayer{}), 32},
		{"RCPlayer.Account", unsafe.Offsetof(RCPlayer{}.Account), 0},
		{"RCPlayer.ID", unsafe.Offsetof(RCPlayer{}.ID), 8},
		{"RCPlayer.Nick", unsafe.Offsetof(RCPlayer{}.Nick), 16},
		{"RCPlayer.Level", unsafe.Offsetof(RCPlayer{}.Level), 24},
		{"RCWeapon size", unsafe.Sizeof(RCWeapon{}), 24},
		{"RCClass size", unsafe.Sizeof(RCClass{}), 16},
		{"RCNPC size", unsafe.Sizeof(RCNPC{}), 48},
		{"RCNPC.ID", unsafe.Offsetof(RCNPC{}.ID), 0},
		{"RCNPC.Name", unsafe.Offsetof(RCNPC{}.Name), 8},
		{"RCNPC.Type", unsafe.Offsetof(RCNPC{}.Type), 16},
		{"RCNPC.Image", unsafe.Offsetof(RCNPC{}.Image), 24},
		{"RCNPC.Level", unsafe.Offsetof(RCNPC{}.Level), 40},
		{"RCFileBrowserFolder size", unsafe.Sizeof(RCFileBrowserFolder{}), 16},
		{"RCFileBrowserEntry size", unsafe.Sizeof(RCFileBrowserEntry{}), 32},
		{"RCFileBrowserEntry.Path", unsafe.Offsetof(RCFileBrowserEntry{}.Path), 0},
		{"RCFileBrowserEntry.Rights", unsafe.Offsetof(RCFileBrowserEntry{}.Rights), 8},
		{"RCFileBrowserEntry.Size", unsafe.Offsetof(RCFileBrowserEntry{}.Size), 16},
		{"RCFileBrowserEntry.Modified", unsafe.Offsetof(RCFileBrowserEntry{}.Modified), 20},
		{"RCFileBrowserEntry.IsDirectory", unsafe.Offsetof(RCFileBrowserEntry{}.IsDirectory), 24},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got != test.want {
				t.Fatalf("got ABI value %d, want %d", test.got, test.want)
			}
		})
	}
}

func TestNativePointerAndSliceBoundary(t *testing.T) {
	value := byte(0xA5)
	word := uintptr(unsafe.Pointer(&value))
	if got := *(*byte)(nativePointer(word)); got != value {
		t.Fatalf("nativePointer read %02x, want %02x", got, value)
	}

	items := []RCClass{{}}
	view, err := nativeSlice[RCClass](uintptr(unsafe.Pointer(&items[0])), len(items))
	if err != nil {
		t.Fatalf("nativeSlice returned error: %v", err)
	}
	if len(view) != 1 {
		t.Fatalf("nativeSlice length = %d, want 1", len(view))
	}
	if _, err := nativeSlice[RCClass](1, maxNativeElements+1); err == nil {
		t.Fatal("nativeSlice accepted an excessive count")
	}
	if _, err := nativeInt(minCInt); err != nil {
		t.Fatalf("nativeInt rejected minimum C int: %v", err)
	}
	if _, err := nativeInt(maxCInt); err != nil {
		t.Fatalf("nativeInt rejected maximum C int: %v", err)
	}
	if unsafe.Sizeof(uintptr(0)) == 8 {
		outOfRange := int(maxCInt)
		outOfRange++
		if _, err := nativeInt(outOfRange); err == nil {
			t.Fatal("nativeInt accepted an out-of-range value")
		}
	}
}

func TestNativeBytesAndCStringValidation(t *testing.T) {
	source := []byte("payload")
	got, err := nativeBytes(unsafe.Pointer(&source[0]), uintptr(len(source)))
	if err != nil {
		t.Fatalf("nativeBytes returned error: %v", err)
	}
	source[0] = 'X'
	if string(got) != "payload" {
		t.Fatalf("nativeBytes did not copy the callback buffer: %q", got)
	}
	if _, err := nativeBytes(nil, 1); err == nil {
		t.Fatal("nativeBytes accepted a nil non-empty buffer")
	}
	negativeLength := uintptr(^uint32(0))
	if _, err := nativeBytes(unsafe.Pointer(&source[0]), negativeLength); err == nil {
		t.Fatal("nativeBytes accepted a negative C length")
	}

	pointer, err := cString("hello")
	if err != nil {
		t.Fatalf("cString returned error: %v", err)
	}
	if got := bptrToString(pointer); got != "hello" {
		t.Fatalf("cString contents = %q, want hello", got)
	}
	if _, err := cString("bad\x00input"); err == nil || !strings.Contains(err.Error(), "NUL") {
		t.Fatalf("cString NUL error = %v", err)
	}
}

func TestCallbackPanicIsQueuedForPump(t *testing.T) {
	const testHandle = Handle(0xCA11)
	_ = takeCallbackFault(testHandle)
	func() {
		defer safeRecover("test-callback", uintptr(testHandle))
		panic("test panic")
	}()

	err := takeCallbackFault(testHandle)
	if err == nil || !strings.Contains(err.Error(), "test-callback") {
		t.Fatalf("callback fault = %v, want test-callback panic", err)
	}
	if err := takeCallbackFault(testHandle); err != nil {
		t.Fatalf("callback fault was not consumed: %v", err)
	}
}
