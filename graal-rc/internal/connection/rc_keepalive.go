package connection

import (
	"log"
	"time"

	"graal-rc/rclib"
)

// The Alpaxis Remote Control, like the C++ client before it, sends the empty
// PLI_RC_UNKNOWN162 on the RC socket every minute. Without it, the first request
// after some minutes without traffic found the connection closed, typically a
// File Browser one: scripts use the NC socket, which has its own keep-alive.
const (
	rcKeepalivePacketID = 162
	rcKeepaliveInterval = time.Minute
)

// rcKeepalive is guarded by Service.mu.
type rcKeepalive struct {
	next  time.Time
	epoch uint64 // the session next belongs to
}

// claimRCKeepalive reports whether a keep-alive is due now and schedules the
// following one. The first is due one interval after the login.
func (s *Service) claimRCKeepalive(now time.Time, epoch uint64, authenticated bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !authenticated {
		s.rcKeepalive = rcKeepalive{}
		return false
	}
	if s.rcKeepalive.next.IsZero() || s.rcKeepalive.epoch != epoch {
		s.rcKeepalive = rcKeepalive{next: now.Add(rcKeepaliveInterval), epoch: epoch}
		return false
	}
	if now.Before(s.rcKeepalive.next) {
		return false
	}
	s.rcKeepalive.next = now.Add(rcKeepaliveInterval)
	return true
}

// processRCKeepalive runs on the event-pump goroutine. Like every native call it
// holds the library's lock, so it cannot split an upload in progress.
func (s *Service) processRCKeepalive(h rclib.Handle) {
	s.mu.Lock()
	active := s.handle == h && s.serverName != "" && !sessionCanceled(s.sessionDone)
	epoch := s.serverEpoch
	s.mu.Unlock()
	if !active || !s.claimRCKeepalive(time.Now(), epoch, rclib.IsAuthenticated(h)) {
		return
	}
	if err := rclib.SendRawPacket(h, rcKeepalivePacketID, nil); err != nil {
		log.Printf("[rc heartbeat] packet %d failed: %v", rcKeepalivePacketID, err)
		return
	}
	log.Printf("[rc heartbeat] packet %d sent", rcKeepalivePacketID)
}
