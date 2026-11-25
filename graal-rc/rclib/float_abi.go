package rclib

import "math"

// purego.SyscallN cannot call a function that mixes integer and float
// parameters: it copies argument word i into both integer register i and float
// register i. Under SysV AMD64 and AAPCS64 the two register files are filled
// independently, so the callee would read the float bits as its next pointer.
// The Windows x64 convention is positional and works with the generic call.
//
// rc_warp_npc is the only such export used here.

// warpNPCSignature is int rc_warp_npc(RCHandle, int, float, float, const char*).
type warpNPCSignature = func(connection uintptr, npcID int32, x, y float32, level uintptr) int32

// floatABINative returns a native function with typed registers for the named
// export, built with bind, or false when the generic word-based call is
// correct. It takes the same words as the generic call, floats as their bits.
func floatABINative(name string, bind func(fptr any)) (func([]uintptr) uintptr, bool) {
	if name != "rc_warp_npc" {
		return nil, false
	}
	var warp warpNPCSignature
	bind(&warp)
	return func(a []uintptr) uintptr {
		return uintptr(warp(
			arg(a, 0),
			int32(arg(a, 1)),
			math.Float32frombits(uint32(arg(a, 2))),
			math.Float32frombits(uint32(arg(a, 3))),
			arg(a, 4),
		))
	}, true
}
