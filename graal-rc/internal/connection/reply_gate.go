package connection

import (
	"errors"
	"strings"
	"sync"
	"time"
)

// replyQuarantine is how long after a timeout the next request of the same kind
// still waits for the late reply.
const replyQuarantine = 15 * time.Second

var (
	errReplyTimeout = errors.New("reply timed out")
	errReplyBusy    = errors.New("an earlier request is still waiting for its reply")
)

// replyGate keeps one request in flight for protocol replies that grclib labels
// with the latest request rather than with data from the server: NPC attributes
// (rc_request_npc_attributes keeps one pending id) and ban data (one pending
// account). A second request would relabel the first reply with its own id.
type replyGate struct {
	mu      sync.Mutex
	slot    chan struct{}
	current *replyTicket
}

type replyTicket struct {
	key      string
	answered chan struct{}
	closed   bool // guarded by replyGate.mu
}

func (g *replyGate) slotChannel() chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.slot == nil {
		g.slot = make(chan struct{}, 1)
	}
	return g.slot
}

// delivered reports a reply for key, which ends the wait for a late one.
func (g *replyGate) delivered(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	ticket := g.current
	if ticket != nil && !ticket.closed && strings.EqualFold(ticket.key, key) {
		ticket.closed = true
		close(ticket.answered)
	}
}

// exchange sends one request and waits for reply to close. done ends the server
// session. On errReplyTimeout the gate stays taken until the late reply is
// delivered, the quarantine ends or the session does; the caller returns at once.
// errReplyBusy means nothing was sent.
func (g *replyGate) exchange(done <-chan struct{}, key string, send func() error, reply <-chan struct{}, timeout, quarantine time.Duration) error {
	slot := g.slotChannel()
	queued := time.NewTimer(timeout + quarantine)
	defer queued.Stop()
	select {
	case slot <- struct{}{}:
	case <-reply:
		return nil
	case <-done:
		return errConnectionSessionChanged
	case <-queued.C:
		return errReplyBusy
	}

	ticket := &replyTicket{key: key, answered: make(chan struct{})}
	g.mu.Lock()
	g.current = ticket
	g.mu.Unlock()
	release := func() {
		g.mu.Lock()
		g.current = nil
		g.mu.Unlock()
		<-slot
	}

	// A late reply to an earlier request for the same key may have answered
	// this one while it was queued.
	select {
	case <-reply:
		release()
		return nil
	default:
	}
	if sessionCanceled(done) {
		release()
		return errConnectionSessionChanged
	}
	if err := send(); err != nil {
		release()
		return err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-reply:
		release()
		return nil
	case <-done:
		release()
		return errConnectionSessionChanged
	case <-timer.C:
		go func() {
			defer release()
			select {
			case <-ticket.answered:
			case <-done:
			case <-time.After(quarantine):
			}
		}()
		return errReplyTimeout
	}
}
