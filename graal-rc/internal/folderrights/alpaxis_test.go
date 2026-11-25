package folderrights

import "testing"

// Alpaxis sends lowercase rules, and a script name may contain slashes.
func TestAlpaxisScriptRights(t *testing.T) {
	access, err := Parse("rw npcs/*\nrw scripts/*\nrw weapons/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"-Staff/DevTools", "-Plain", "a/b/c"} {
		if !access.CanRead("weapon", name) || !access.CanWrite("weapon", name) {
			t.Errorf("weapons/* must cover weapon %q", name)
		}
	}
	if !access.CanWrite("npc", "Control-NPC") {
		t.Error("npcs/* must cover an NPC")
	}
	if access.CanRead("class", "player_base") {
		t.Error("no rule names classes")
	}
	if !access.HasWriteAccessForScriptTypes("weapon") || !access.HasWriteAccessForScriptTypes("npc") {
		t.Error("lowercase write rules must enable the local sync")
	}
}

func TestAlpaxisScriptRightsKeepNarrowAndDenyRules(t *testing.T) {
	access, err := Parse("rw weapons/-Staff/*\n-r weapons/-staff/secret*\nr Weapons/Public?")
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"-Staff/DevTools": true,
		"-staff/devtools": true,
		"-Staff/SecretX":  false,
		"-Other/DevTools": false,
		"Public1":         true,
		"Public12":        false,
	} {
		if got := access.CanRead("weapon", name); got != want {
			t.Errorf("CanRead(%q) = %v, want %v", name, got, want)
		}
	}
	if access.CanWrite("weapon", "Public1") {
		t.Error("a read rule grants no write")
	}
}
