package connection

import (
	"testing"
	"time"
)

func TestEditorChatCommand(t *testing.T) {
	for _, tc := range []struct {
		line, command, kind, target string
		ok                          bool
	}{
		{"/open", "/open Nullborne", "attrs", "Nullborne", true},
		{"  /Open  ", "/open Nullborne", "attrs", "Nullborne", true},
		{"/open bob", "/open bob", "attrs", "bob", true},
		{"/openrights", "/openrights Nullborne", "rights", "Nullborne", true},
		{"  /OpenRights  ", "/openrights Nullborne", "rights", "Nullborne", true},
		{"/openrights Some Name", "/openrights Some Name", "rights", "Some Name", true},
		{"/openrightsx", "", "", "", false},
		{"/openaccess bob", "", "", "", false},
		{"hello /open", "", "", "", false},
	} {
		command, kind, target, ok := editorChatCommand(tc.line, "Nullborne")
		if command != tc.command || kind != tc.kind || target != tc.target || ok != tc.ok {
			t.Errorf("editorChatCommand(%q) = %q, %q, %q, %v", tc.line, command, kind, target, ok)
		}
	}
	// Without a known account the line goes out as typed.
	if command, _, target, ok := editorChatCommand("/open", ""); command != "/open" || target != "" || !ok {
		t.Errorf("without account = %q, %q, %v", command, target, ok)
	}
}

// The Alpaxis server does not answer these commands without an account, so
// asking for this account's own data has to name it, like the Alpaxis RC does.
func TestSelfEditorCommandNamesTheAccount(t *testing.T) {
	for _, name := range []string{"/open", "/openrights", "/openaccess", "/opencomments"} {
		if got, want := selfEditorCommand(name, "Nullborne"), name+" Nullborne"; got != want {
			t.Errorf("selfEditorCommand(%q) = %q, want %q", name, got, want)
		}
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

func TestUserEditorRequestClaims(t *testing.T) {
	now := time.Now()

	s := &Service{}
	if s.claimUserEditorRequest("rights", "bob", false, now) {
		t.Fatal("a reply nobody asked for in chat must not open a window")
	}

	s.addUserEditorRequest("rights", "bob", now)
	if s.claimUserEditorRequest("attrs", "bob", false, now) {
		t.Fatal("a reply of another kind must not open a window")
	}
	if s.claimUserEditorRequest("rights", "alice", true, now) {
		t.Fatal("a reply that answered another request must not open a window")
	}
	if !s.claimUserEditorRequest("rights", "Bob", false, now) {
		t.Fatal("the reply for the typed account must open a window")
	}
	if s.claimUserEditorRequest("rights", "Bob", false, now) {
		t.Fatal("one command opens one window")
	}

	// The server resolves partial and community names to another spelling.
	s.addUserEditorRequest("rights", "someone", now)
	s.addUserEditorRequest("attrs", "bo", now)
	if !s.claimUserEditorRequest("attrs", "Bobby", false, now) {
		t.Fatal("an otherwise unattributed reply answers the typed command of its kind")
	}

	s.addUserEditorRequest("attrs", "bob", now)
	if s.claimUserEditorRequest("attrs", "bob", false, now.Add(userEditorRequestTTL+time.Second)) {
		t.Fatal("an expired command must not open a window")
	}
}

// The window opened for a typed command gets the reply that opened it, without
// asking the server again (there is no connection here).
func TestTypedOpenReplyIsHandedToTheWindowOnce(t *testing.T) {
	s := &Service{}
	var opened []string
	s.SetEditorWindowOpener(func(kind, account string) { opened = append(opened, kind+":"+account) })
	s.addUserEditorRequest("attrs", "bob", time.Now())

	if !s.deliverEditorReply("attrs", "Bob", AttrsData{Account: "Bob", EditorText: "[Stats]"}) {
		t.Fatal("the typed command takes the reply")
	}

	if len(opened) != 1 || opened[0] != "attrs:Bob" {
		t.Fatalf("opened = %v", opened)
	}
	data, err := s.OpenAttrs("bob")
	if err != nil || data.EditorText != "[Stats]" {
		t.Fatalf("OpenAttrs = %+v, %v", data, err)
	}
	if _, ok := s.takeEditorReply("attrs", "bob", time.Now()); ok {
		t.Fatal("the reply is used once")
	}
}
