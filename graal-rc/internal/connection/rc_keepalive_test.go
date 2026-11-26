package connection

import (
	"testing"
	"time"
)

// As in the Alpaxis Remote Control: one empty packet 162 every 60 seconds while
// logged in, the first one 60 seconds after the login.
func TestRCKeepaliveIsDueEverySixtySeconds(t *testing.T) {
	if rcKeepalivePacketID != 162 || rcKeepaliveInterval != 60*time.Second {
		t.Fatalf("keep-alive = packet %d every %s", rcKeepalivePacketID, rcKeepaliveInterval)
	}
	s := NewService()
	start := time.Unix(1000, 0)
	steps := []struct {
		after time.Duration
		due   bool
	}{
		{0, false}, {59 * time.Second, false}, {60 * time.Second, true}, {61 * time.Second, false},
		{119 * time.Second, false}, {120 * time.Second, true},
		// A late pump sends one packet, not one for every interval it missed.
		{500 * time.Second, true}, {501 * time.Second, false}, {560 * time.Second, true},
	}
	for _, step := range steps {
		if got := s.claimRCKeepalive(start.Add(step.after), 1, true); got != step.due {
			t.Fatalf("at %s: due = %v, want %v", step.after, got, step.due)
		}
	}
}

func TestRCKeepaliveStartsOverWithANewLogin(t *testing.T) {
	s := NewService()
	start := time.Unix(1000, 0)
	s.claimRCKeepalive(start, 1, true)

	// Logged out: nothing is sent, however late it is.
	if s.claimRCKeepalive(start.Add(2*time.Minute), 1, false) {
		t.Fatal("sent while not authenticated")
	}
	if s.claimRCKeepalive(start.Add(3*time.Minute), 1, true) {
		t.Fatal("sent as soon as it was authenticated again")
	}
	// A new session does not inherit the previous deadline.
	if s.claimRCKeepalive(start.Add(10*time.Minute), 2, true) {
		t.Fatal("the new session inherited the old deadline")
	}
	if !s.claimRCKeepalive(start.Add(11*time.Minute), 2, true) {
		t.Fatal("the new session never sends")
	}
}
