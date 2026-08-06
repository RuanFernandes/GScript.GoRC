package sync

import "testing"

func TestParseChatLineWeaponActivityWithRenderedPrefix(t *testing.T) {
	got, ok := ParseChatLine("[13:19] [RC] Weapon/GUI-script -Yzd/Test added/updated by Repinho")
	if !ok {
		t.Fatal("expected weapon activity to be parsed")
	}
	if got.Kind != "weapon" || got.Key != "-Yzd/Test" || got.Action != "updated" || got.Actor != "Repinho" {
		t.Fatalf("unexpected activity: %+v", got)
	}
}

func TestParseChatLineKeepsNonActivityMessagesIgnored(t *testing.T) {
	if _, ok := ParseChatLine("[13:19] [RC] hello everyone"); ok {
		t.Fatal("ordinary RC chat must not be treated as sync activity")
	}
}

func TestParseChatLineDeleteActivities(t *testing.T) {
	tests := []struct {
		line, kind, name string
	}{
		{"Script personal_graal5766947_anc deleted by Repinho", "class", "personal_graal5766947_anc"},
		{"Weapon Shared/Testtttttt deleted by Repinho", "weapon", "Shared/Testtttttt"},
		{"The npc Graal5766947 has been deleted by Repinho", "npc", "Graal5766947"},
	}
	for _, test := range tests {
		got, ok := ParseChatLine(test.line)
		if !ok || got.Kind != test.kind || got.Name != test.name || got.Action != "deleted" {
			t.Errorf("ParseChatLine(%q) = %+v, ok=%v", test.line, got, ok)
		}
	}
}
