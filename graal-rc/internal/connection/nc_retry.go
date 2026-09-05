package connection

import (
	"math/rand/v2"
	"time"
)

const (
	ncMaxReconnectAttempts = 4
	ncRetryBaseDelay       = 2 * time.Second
	ncRetryMaxDelay        = 30 * time.Second
	ncStableResetInterval  = 30 * time.Second
)

// resetNCRetryLocked starts a new retry budget. Caller holds mu. Briefly
// authenticated connections do not reset it, preventing an endless flap loop.
func (s *Service) resetNCRetryLocked() {
	s.ncReconnectAttempts = 0
	s.nextNCAutomaticReconnect = time.Time{}
	s.ncAttemptDeadline = time.Time{}
	s.ncAuthenticatedSince = time.Time{}
}

func ncRetryDelay(attempt int, jitter float64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	delay := ncRetryBaseDelay
	for i := 0; i < attempt && delay < ncRetryMaxDelay; i++ {
		delay *= 2
	}
	if delay > ncRetryMaxDelay {
		delay = ncRetryMaxDelay
	}
	if jitter < 0 {
		jitter = 0
	} else if jitter > 1 {
		jitter = 1
	}
	delay += time.Duration(float64(delay) * 0.25 * jitter)
	if delay > ncRetryMaxDelay {
		delay = ncRetryMaxDelay
	}
	return delay
}

func (s *Service) markNCConnectionObserved() {
	s.markNCConnectionObservedAt(time.Now())
}

func (s *Service) markNCConnectionObservedAt(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ncConnectionWasUp {
		s.ncAuthenticatedSince = now
	}
	s.ncConnectionWasUp = true
	s.ncAttemptDeadline = time.Time{}
	s.nextNCAutomaticReconnect = time.Time{}
	if !s.ncAuthenticatedSince.IsZero() && now.Sub(s.ncAuthenticatedSince) >= ncStableResetInterval {
		s.ncReconnectAttempts = 0
	}
}

func (s *Service) claimNCAutomaticReconnect() bool {
	return s.claimNCAutomaticReconnectAt(time.Now(), rand.Float64())
}

// A pending endpoint refresh or handshake owns the socket until its deadline.
// Initial failures and later drops share the same bounded retry policy.
func (s *Service) claimNCAutomaticReconnectAt(now time.Time, jitter float64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ncManuallyDisconnected || !s.nextNCConnectAttempt.IsZero() || !s.ncConnectionAttempted {
		return false
	}
	if s.ncConnectionWasUp {
		s.ncConnectionWasUp = false
		s.ncAuthenticatedSince = time.Time{}
		s.ncAttemptDeadline = time.Time{}
	}
	if s.ncReconnectAttempts >= ncMaxReconnectAttempts || (!s.ncAttemptDeadline.IsZero() && now.Before(s.ncAttemptDeadline)) {
		return false
	}
	if s.nextNCAutomaticReconnect.IsZero() {
		s.nextNCAutomaticReconnect = now.Add(ncRetryDelay(s.ncReconnectAttempts, jitter))
		return false
	}
	if now.Before(s.nextNCAutomaticReconnect) {
		return false
	}
	s.nextNCAutomaticReconnect = time.Time{}
	s.ncAttemptDeadline = now.Add(ncConnectionTimeout)
	s.ncReconnectAttempts++
	return true
}
