package folderrights

import "testing"

func TestParseAndMatchFolderRights(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		scriptType string
		scriptName string
		read       bool
		write      bool
	}{
		{
			name:       "single segment wildcard does not include nested scripts",
			raw:        "rw WEAPONS/*",
			scriptType: "weapon",
			scriptName: "Rifle",
			read:       true,
			write:      true,
		},
		{
			name:       "single segment wildcard excludes slash names",
			raw:        "rw WEAPONS/*",
			scriptType: "weapon",
			scriptName: "Ruan/SQL",
			read:       false,
			write:      false,
		},
		{
			name:       "nested wildcard includes slash names",
			raw:        "rw WEAPONS/*/*",
			scriptType: "weapon",
			scriptName: "Ruan/SQL",
			read:       true,
			write:      true,
		},
		{
			name:       "later deny removes an earlier grant",
			raw:        "rw WEAPONS/*\n-rw WEAPONS/Kek",
			scriptType: "weapon",
			scriptName: "Kek",
			read:       false,
			write:      false,
		},
		{
			name:       "later grant restores a denied right",
			raw:        "rw WEAPONS/*\n-rw WEAPONS/Kek\nr WEAPONS/Kek",
			scriptType: "weapon",
			scriptName: "Kek",
			read:       true,
			write:      false,
		},
		{
			name:       "write-only rule is not readable",
			raw:        "w WEAPONS/NewWeapon",
			scriptType: "weapon",
			scriptName: "NewWeapon",
			read:       false,
			write:      true,
		},
		{
			name:       "empty list is unrestricted",
			raw:        "",
			scriptType: "npc",
			scriptName: "Any/NPC",
			read:       true,
			write:      true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access, err := Parse(test.raw)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got := access.CanRead(test.scriptType, test.scriptName); got != test.read {
				t.Errorf("CanRead() = %v, want %v", got, test.read)
			}
			if got := access.CanWrite(test.scriptType, test.scriptName); got != test.write {
				t.Errorf("CanWrite() = %v, want %v", got, test.write)
			}
		})
	}
}

func TestParseQuotedEntries(t *testing.T) {
	access, err := Parse(`"rw WEAPONS/*","r CLASSES/*"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !access.CanRead("weapon", "Sword") || !access.CanWrite("weapon", "Sword") {
		t.Fatal("quoted weapon rule was not parsed")
	}
	if !access.CanRead("class", "Movement") || access.CanWrite("class", "Movement") {
		t.Fatal("quoted class rule was not parsed")
	}
}

func TestParseUnquotedCommaEntries(t *testing.T) {
	access, err := Parse("rw WEAPONS/*,r CLASSES/*")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !access.CanRead("weapon", "Sword") || !access.CanWrite("weapon", "Sword") {
		t.Fatal("unquoted comma-separated weapon rule was not parsed")
	}
	if !access.CanRead("class", "Movement") || access.CanWrite("class", "Movement") {
		t.Fatal("unquoted comma-separated class rule was not parsed")
	}
}

func TestHasWriteAccessForScriptTypes(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{name: "weapon wildcard", raw: "rw WEAPONS/*", want: true},
		{name: "exact class path", raw: "w CLASSES/Movement", want: true},
		{name: "npc wildcard", raw: "rw NPCS/*", want: true},
		{name: "read only script paths", raw: "r WEAPONS/*,r CLASSES/*,r NPCS/*", want: false},
		{name: "write rule outside script paths", raw: "rw FILES/*", want: false},
		{name: "empty list is unrestricted", raw: "", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			access, err := Parse(test.raw)
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if got := access.HasWriteAccessForScriptTypes("weapon", "class", "npc"); got != test.want {
				t.Fatalf("HasWriteAccessForScriptTypes() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAccessEqualComparesParsedRules(t *testing.T) {
	first, err := Parse("rw WEAPONS/*\n-rw WEAPONS/Legacy")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	same, err := Parse(`"rw WEAPONS/*","-rw WEAPONS/Legacy"`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !first.Equal(same) {
		t.Fatal("equivalent parsed rules should compare equal")
	}

	differentOrder, err := Parse("-rw WEAPONS/Legacy\nrw WEAPONS/*")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if first.Equal(differentOrder) {
		t.Fatal("rule order must remain significant")
	}

	differentRights, err := Parse("r WEAPONS/*\n-rw WEAPONS/Legacy")
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if first.Equal(differentRights) {
		t.Fatal("different rights should not compare equal")
	}
}
