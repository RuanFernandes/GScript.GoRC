package rclib

import "testing"

func TestIsUsableScriptName(t *testing.T) {
	valid := []string{"Sword", "heheh/denvnob", "Weapon 01"}
	for _, name := range valid {
		if !IsUsableScriptName(name) {
			t.Fatalf("expected %q to be usable", name)
		}
	}
	invalid := []string{"", "   ", "\x00", "\u200b", "\ufffd", "□", string([]byte{0xff, 0xfe})}
	for _, name := range invalid {
		if IsUsableScriptName(name) {
			t.Fatalf("expected %q to be rejected", name)
		}
	}
}
