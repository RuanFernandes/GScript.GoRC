package connection

import (
	"errors"
	"testing"
)

func editorResolved(w *editorWait) bool {
	select {
	case <-w.done:
		return true
	default:
		return false
	}
}

func timeOutEditor(s *Service, key string) {
	w, _ := s.registerEditor(key)
	s.cancelEditor(key, w, errors.New("request timed out"))
}

func TestSelfRightsWaiterRejectsAnotherAccountOnceCanonicalIsKnown(t *testing.T) {
	s := NewService()
	s.creds = Credentials{Account: "login"}
	s.selfRightsAccount = "Graal1"
	w, _ := s.registerEditor(selfRightsPendingKey)

	if s.resolveEditorForAccount("rights", "SomeoneElse", RightsData{Account: "SomeoneElse"}) || editorResolved(w) {
		t.Fatal("another account's rights answered the self rights request")
	}
	if !s.resolveEditorForAccount("rights", "graal1", RightsData{Account: "graal1"}) {
		t.Fatal("the canonical self account was rejected")
	}
}

func TestLateReplyDoesNotAnswerAnotherAccountsRequest(t *testing.T) {
	s := NewService()
	timeOutEditor(s, editorKey("attrs", "alice"))
	bob, _ := s.registerEditor(editorKey("attrs", "bob"))

	if s.resolveEditorForAccount("attrs", "Alice", AttrsData{Account: "Alice"}) || editorResolved(bob) {
		t.Fatal("alice's late reply answered bob's request")
	}
	if !s.resolveEditorForAccount("attrs", "bob", AttrsData{Account: "bob"}) {
		t.Fatal("bob's own reply was rejected")
	}
}

func TestLateReplyDoesNotAnswerSelfRequest(t *testing.T) {
	s := NewService()
	timeOutEditor(s, editorKey("rights", "alice"))
	self, _ := s.registerEditor(selfRightsPendingKey)

	if s.resolveEditorForAccount("rights", "alice", RightsData{Account: "alice"}) || editorResolved(self) {
		t.Fatal("alice's late rights replaced the self rights")
	}
}

// A community-name request that timed out is answered under the canonical name,
// which nothing links to it. While it may still arrive, an unlinked reply is
// ambiguous.
func TestUnlinkedReplyIsAmbiguousWhileAnotherRequestIsUnanswered(t *testing.T) {
	s := NewService()
	timeOutEditor(s, editorKey("comments", "Community Alice"))
	bob, _ := s.registerEditor(editorKey("comments", "bob"))

	if s.resolveEditorForAccount("comments", "Graal100", CommentsData{Account: "Graal100"}) || editorResolved(bob) {
		t.Fatal("an unlinked late reply answered bob's request")
	}
}

func TestRetryOfTimedOutCommunityNameAcceptsCanonicalReply(t *testing.T) {
	s := NewService()
	timeOutEditor(s, editorKey("comments", "Community Alice"))
	retry, _ := s.registerEditor(editorKey("comments", "Community Alice"))

	if !s.resolveEditorForAccount("comments", "Graal100", CommentsData{Account: "Graal100"}) || !editorResolved(retry) {
		t.Fatal("canonical reply to a retried community request was rejected")
	}
}

func TestCommunityNameRequestAcceptsCanonicalReply(t *testing.T) {
	s := NewService()
	w, _ := s.registerEditor(editorKey("attrs", "Some Community"))
	if !s.resolveEditorForAccount("attrs", "Graal999", AttrsData{Account: "Graal999"}) || !editorResolved(w) {
		t.Fatal("canonical reply to a community-name request was rejected")
	}
}

func TestOnlineAccountRequestRejectsADifferentAccount(t *testing.T) {
	s := NewService()
	s.rememberPlayerProperty(3, "account", "Bob")
	w, _ := s.registerEditor(editorKey("attrs", "bob"))

	if s.resolveEditorForAccount("attrs", "alice", AttrsData{Account: "alice"}) || editorResolved(w) {
		t.Fatal("a different account answered a request for a known online account")
	}
}

func TestPlayerCommunityLinksReplyAmongSeveralRequests(t *testing.T) {
	s := NewService()
	s.rememberPlayerProperty(4, "account", "Graal4")
	s.rememberPlayerProperty(4, "community", "Four")
	four, _ := s.registerEditor(editorKey("comments", "Four"))
	other, _ := s.registerEditor(editorKey("comments", "other"))

	if !s.resolveEditorForAccount("comments", "Graal4", CommentsData{Account: "Graal4"}) {
		t.Fatal("a reply linked by the player list was rejected")
	}
	if !editorResolved(four) || editorResolved(other) {
		t.Fatal("the linked reply reached the wrong request")
	}
}

func TestSelfReplyNeverAnswersAnotherAccountsRequest(t *testing.T) {
	s := NewService()
	s.creds = Credentials{Account: "me"}
	bob, _ := s.registerEditor(editorKey("rights", "bob"))

	if s.resolveEditorForAccount("rights", "me", RightsData{Account: "me"}) || editorResolved(bob) {
		t.Fatal("own rights answered the request for bob")
	}
}

func TestUnknownReplyWithSelfAndOtherRequestsPendingFailsClosed(t *testing.T) {
	s := NewService()
	self, _ := s.registerEditor(selfRightsPendingKey)
	other, _ := s.registerEditor(editorKey("rights", "Community B"))

	if s.resolveEditorForAccount("rights", "Graal2", RightsData{Account: "Graal2"}) || editorResolved(self) || editorResolved(other) {
		t.Fatal("an ambiguous reply was assigned to a request")
	}
}

func TestBanTypesCacheDoesNotSurviveTheSession(t *testing.T) {
	s := NewService()
	s.editorMu.Lock()
	s.banTypes = "old server,60"
	s.editorMu.Unlock()

	s.cancelWaiters(errConnectionSessionChanged)

	s.editorMu.Lock()
	defer s.editorMu.Unlock()
	if s.banTypes != "" {
		t.Fatalf("ban types of the previous server survived: %q", s.banTypes)
	}
}
