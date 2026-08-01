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
