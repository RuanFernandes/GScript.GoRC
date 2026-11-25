package connection

import (
	"testing"
	"time"
)

func TestRightsChatCommand(t *testing.T) {
	for _, tc := range []struct {
		line, command, target string
		ok                    bool
	}{
		{"/openrights", "/openrights Nullborne", "Nullborne", true},
		{"  /OpenRights  ", "/openrights Nullborne", "Nullborne", true},
		{"/openrights bob", "/openrights bob", "bob", true},
		{"/openrights Some Name", "/openrights Some Name", "Some Name", true},
		{"/openrightsx", "", "", false},
		{"/open bob", "", "", false},
		{"hello /openrights", "", "", false},
	} {
		command, target, ok := rightsChatCommand(tc.line, "Nullborne")
		if command != tc.command || target != tc.target || ok != tc.ok {
			t.Errorf("rightsChatCommand(%q) = %q, %q, %v", tc.line, command, target, ok)
		}
	}
	// Without a known account the line goes out as typed.
	if command, target, ok := rightsChatCommand("/openrights", ""); command != "/openrights" || target != "" || !ok {
		t.Errorf("without account = %q, %q, %v", command, target, ok)
	}
}

// The account of PLI_RC_PLAYERRIGHTSGET carries a length byte (value + 32).
func TestRightsRequestArgument(t *testing.T) {
	got, err := rightsRequestArgument("Bob")
	if err != nil || got != "#Bob" {
		t.Fatalf("rightsRequestArgument(Bob) = %q, %v", got, err)
	}
	long := make([]byte, 100)
	for i := range long {
		long[i] = 'a'
	}
	got, err = rightsRequestArgument(string(long))
	if err != nil || len(got) != 101 || got[0] != 132 {
		t.Fatalf("long name: len=%d err=%v", len(got), err)
	}
	if _, err = rightsRequestArgument(string(make([]byte, 224))); err == nil {
		t.Fatal("a name longer than 223 bytes must be refused")
	}
}

func TestUserRightsRequestClaims(t *testing.T) {
	now := time.Now()

	s := &Service{}
	if s.claimUserRightsRequest("bob", false, now) {
		t.Fatal("a reply nobody asked for in chat must not open a window")
	}

	s.addUserRightsRequest("bob", now)
	if s.claimUserRightsRequest("alice", true, now) {
		t.Fatal("a reply that answered another request must not open a window")
	}
	if !s.claimUserRightsRequest("Bob", false, now) {
		t.Fatal("the reply for the typed account must open a window")
	}
	if s.claimUserRightsRequest("Bob", false, now) {
		t.Fatal("one command opens one window")
	}

	// The server resolves partial and community names to another spelling.
	s.addUserRightsRequest("bo", now)
	if !s.claimUserRightsRequest("Bobby", false, now) {
		t.Fatal("an otherwise unattributed reply answers the typed command")
	}

	s.addUserRightsRequest("bob", now)
	if s.claimUserRightsRequest("bob", false, now.Add(userRightsRequestTTL+time.Second)) {
		t.Fatal("an expired command must not open a window")
	}
}

func TestUserRightsReplyIsHandedToTheWindowOnce(t *testing.T) {
	s := &Service{}
	var opened []string
	s.SetRightsWindowOpener(func(account string) { opened = append(opened, account) })
	s.addUserRightsRequest("bob", time.Now())

	s.deliverRightsReply(RightsData{Account: "Bob", Rights: 5})

	if len(opened) != 1 || opened[0] != "Bob" {
		t.Fatalf("opened = %v", opened)
	}
	data, ok := s.takeRightsReply("bob", time.Now())
	if !ok || data.Rights != 5 {
		t.Fatalf("takeRightsReply = %+v, %v", data, ok)
	}
	if _, ok := s.takeRightsReply("bob", time.Now()); ok {
		t.Fatal("the reply is used once")
	}
}
