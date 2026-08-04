package connection

import (
	"testing"
	"time"
)

// snapshotLockedForTest is a test-only view of the joined set under the lock.
func (s *Service) snapshotLockedForTest() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

// leaveDeadlinePassed returns a leave deadline already in the past, used to
// force a deferred leave to be committable by settleChannelLeaves.
func leaveDeadlinePassed() time.Time { return time.Now().Add(-time.Second) }

// TestApplyChannelDelta_LoginBurst proves the reported bug stays fixed: the
// login burst sends Left then Joined for a channel we were never in. The Left
// is ignored so the following Joined keeps the channel.
func TestApplyChannelDelta_LoginBurst(t *testing.T) {
	s := NewService()

	// Left for a never-joined channel: ignored, no snapshot emitted.
	if got := s.applyChannelDelta("#a", "* Left #a"); got != nil {
		t.Fatalf("Left before any Join should be ignored, got snapshot %v", got)
	}
	// Following Joined creates the channel and emits a snapshot.
	if got := s.applyChannelDelta("#a", "* Joined #a"); !eq(got, []string{"#a"}) {
		t.Fatalf("after Join: got %v want [#a]", got)
	}
	// Duplicate Joined is idempotent: no snapshot.
	if s.applyChannelDelta("#a", "* Joined #a") != nil {
		t.Fatalf("duplicate Join should not emit snapshot")
	}
}

// TestApplyChannelDelta_PartDebounced proves a PART does not remove the channel
// immediately: it stays joined (pending leave) so a rejoin within the cooldown
// cancels it and the tab survives.
func TestApplyChannelDelta_PartDebounced(t *testing.T) {
	s := NewService()
	s.applyChannelDelta("#a", "* Joined #a")
	s.applyChannelDelta("#b", "* Joined #b")

	// Real part: deferred, no immediate snapshot, channel still joined.
	if got := s.applyChannelDelta("#a", "* Left #a"); got != nil {
		t.Fatalf("Part should be deferred (nil snapshot), got %v", got)
	}
	if got := s.snapshotLockedForTest(); !eq(got, []string{"#a", "#b"}) {
		t.Fatalf("channel still joined after deferred part: got %v want [#a #b]", got)
	}

	// Rejoin within cooldown cancels the pending leave; #a stays joined.
	s.applyChannelDelta("#a", "* Joined #a")
	if got := s.snapshotLockedForTest(); !eq(got, []string{"#a", "#b"}) {
		t.Fatalf("after rejoin: got %v want [#a #b]", got)
	}
}

// TestSettleChannelLeaves proves that once the cooldown elapses the deferred
// leave is committed and the channel is dropped.
func TestSettleChannelLeaves(t *testing.T) {
	s := NewService()
	s.applyChannelDelta("#a", "* Joined #a")
	s.applyChannelDelta("#b", "* Joined #b")
	s.applyChannelDelta("#a", "* Left #a") // schedule leave for #a

	// Force #a's leave deadline into the past, then settle.
	s.mu.Lock()
	if cs := s.channels["#a"]; cs != nil {
		cs.leaveAt = leaveDeadlinePassed()
	}
	s.mu.Unlock()

	s.settleChannelLeaves()
	if got := s.snapshotLockedForTest(); !eq(got, []string{"#b"}) {
		t.Fatalf("after settle: got %v want [#b]", got)
	}
}

// TestApplyChannelDelta_NonMarker proves normal chat lines do not touch the set.
func TestApplyChannelDelta_NonMarker(t *testing.T) {
	s := NewService()
	s.applyChannelDelta("#a", "* Joined #a")
	if got := s.applyChannelDelta("#a", "hello there"); got != nil {
		t.Fatalf("chat line should not emit snapshot, got %v", got)
	}
}

// TestApplyChannelDelta_EmptyChannel proves server-channel lines are ignored.
func TestApplyChannelDelta_EmptyChannel(t *testing.T) {
	s := NewService()
	if got := s.applyChannelDelta("", "* Joined "); got != nil {
		t.Fatalf("empty channel should not emit snapshot, got %v", got)
	}
}

func TestRightsChangedMessageDetection(t *testing.T) {
	for _, text := range []string{
		"[08:51] ruanf has set rights of (offline) player ruanf",
		"ADMIN HAS SET RIGHTS OF (offline) player Test",
	} {
		if !isRightsChangedMessage(text) {
			t.Fatalf("rights notification was not detected: %q", text)
		}
	}
	if got := rightsChangedTarget("[08:51] ruanf has set rights of (offline) player ruanf"); got != "ruanf" {
		t.Fatalf("rights target = %q, want ruanf", got)
	}
	if got := rightsChangedTarget("ADMIN HAS SET RIGHTS OF (online) player Test"); got != "Test" {
		t.Fatalf("online rights target = %q, want Test", got)
	}
	if isRightsChangedMessage("ruanf changed the player rights") {
		t.Fatal("unrelated rights message matched the notification marker")
	}
}

func TestParseLoadedRightsMessage(t *testing.T) {
	withCommunity, ok := parseLoadedRightsMessage("[08:56 ] Repinho loaded the rights of Repinho (Graal5766947)")
	if !ok {
		t.Fatal("community identity message was not parsed")
	}
	if withCommunity.Actor != "Repinho" || withCommunity.Target != "Repinho" || withCommunity.Account != "Graal5766947" || withCommunity.CommunityName != "Repinho" || !withCommunity.ExplicitAccount {
		t.Fatalf("unexpected community identity: %+v", withCommunity)
	}

	withoutCommunity, ok := parseLoadedRightsMessage("[08:57 ] ruanf loaded the rights of ruanf")
	if !ok {
		t.Fatal("account-only identity message was not parsed")
	}
	if withoutCommunity.Actor != "ruanf" || withoutCommunity.Target != "ruanf" || withoutCommunity.Account != "ruanf" || withoutCommunity.CommunityName != "" || withoutCommunity.ExplicitAccount {
		t.Fatalf("unexpected account-only identity: %+v", withoutCommunity)
	}
}

func TestCaptureSelfRightsIdentityIgnoresOtherTarget(t *testing.T) {
	s := NewService()
	s.creds = Credentials{Account: "Repinho", Nickname: "Repinho"}

	self, ok := parseLoadedRightsMessage("Repinho loaded the rights of Repinho (Graal5766947)")
	if !ok {
		t.Fatal("self identity message was not parsed")
	}
	s.captureSelfRightsIdentity(self)

	s.rightsMu.RLock()
	account, community := s.selfRightsAccount, s.selfRightsCommunityName
	s.rightsMu.RUnlock()
	if account != "Graal5766947" || community != "Repinho" {
		t.Fatalf("captured identity = community %q account %q", community, account)
	}

	other, ok := parseLoadedRightsMessage("ruanf loaded the rights of ruanf")
	if !ok {
		t.Fatal("other identity message was not parsed")
	}
	s.captureSelfRightsIdentity(other)

	s.rightsMu.RLock()
	account, community = s.selfRightsAccount, s.selfRightsCommunityName
	s.rightsMu.RUnlock()
	if account != "Graal5766947" || community != "Repinho" {
		t.Fatalf("other player's identity replaced self identity: community %q account %q", community, account)
	}
}

func TestCaptureSelfRightsIdentityWithoutCommunity(t *testing.T) {
	s := NewService()
	s.creds = Credentials{Account: "ruanf", Nickname: "ruanf"}

	message, ok := parseLoadedRightsMessage("ruanf loaded the rights of ruanf")
	if !ok {
		t.Fatal("account-only identity message was not parsed")
	}
	s.captureSelfRightsIdentity(message)

	s.rightsMu.RLock()
	account, community := s.selfRightsAccount, s.selfRightsCommunityName
	s.rightsMu.RUnlock()
	if account != "ruanf" || community != "" {
		t.Fatalf("captured account-only identity = community %q account %q", community, account)
	}
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
