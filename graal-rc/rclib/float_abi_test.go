package rclib

import (
	"math"
	"testing"
)

// purego.SyscallN loads each word into both the integer and the float
// registers by position, which the SysV and AAPCS64 conventions do not do for
// mixed signatures. rc_warp_npc therefore needs a typed binding.
func TestWarpNPCBindingPassesTypedFloats(t *testing.T) {
	var conn, level uintptr
	var id int32
	var x, y float32
	native, ok := floatABINative("rc_warp_npc", func(fptr any) {
		target, matches := fptr.(*warpNPCSignature)
		if !matches {
			t.Fatalf("bound %T", fptr)
		}
		*target = func(c uintptr, npcID int32, warpX, warpY float32, levelPointer uintptr) int32 {
			conn, id, x, y, level = c, npcID, warpX, warpY, levelPointer
			return 1
		}
	})
	if !ok {
		t.Fatal("rc_warp_npc has no typed binding")
	}
	negative := int32(-1)
	result := native([]uintptr{11, uintptr(negative), uintptr(math.Float32bits(30.5)), uintptr(math.Float32bits(-2.25)), 99})
	if result != 1 || conn != 11 || id != -1 || x != 30.5 || y != -2.25 || level != 99 {
		t.Fatalf("result=%d conn=%d id=%d x=%v y=%v level=%d", result, conn, id, x, y, level)
	}
}

func TestOtherExportsUseTheGenericBinding(t *testing.T) {
	if _, ok := floatABINative("rc_execute", func(any) { t.Fatal("bound an integer-only export") }); ok {
		t.Fatal("rc_execute does not need a typed binding")
	}
}
