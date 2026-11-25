package connection

import (
	"errors"
	"testing"
	"time"
)

// gateRequest runs one exchange in the background, reporting when it is sent.
func gateRequest(g *replyGate, key string, reply chan struct{}, timeout, quarantine time.Duration) (sent chan struct{}, result chan error) {
	sent, result = make(chan struct{}), make(chan error, 1)
	go func() {
		result <- g.exchange(nil, key, func() error { close(sent); return nil }, reply, timeout, quarantine)
	}()
	return sent, result
}

func mustHappen[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not happen", what)
		panic("unreachable")
	}
}

func mustNotHappen[T any](t *testing.T, ch <-chan T, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s happened too early", what)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestReplyGateKeepsOneRequestInFlight(t *testing.T) {
	var gate replyGate
	replyA, replyB := make(chan struct{}), make(chan struct{})
	sentA, doneA := gateRequest(&gate, "1", replyA, time.Minute, time.Minute)
	mustHappen(t, sentA, "sending A")

	sentB, doneB := gateRequest(&gate, "2", replyB, time.Minute, time.Minute)
	mustNotHappen(t, sentB, "sending B while A is unanswered")

	close(replyA)
	if err := mustHappen(t, doneA, "finishing A"); err != nil {
		t.Fatal(err)
	}
	mustHappen(t, sentB, "sending B")
	close(replyB)
	if err := mustHappen(t, doneB, "finishing B"); err != nil {
		t.Fatal(err)
	}
}

func TestReplyGateHoldsTheNextRequestUntilTheLateReplyArrives(t *testing.T) {
	var gate replyGate
	_, doneA := gateRequest(&gate, "1", make(chan struct{}), 20*time.Millisecond, time.Minute)
	if err := mustHappen(t, doneA, "timing out A"); !errors.Is(err, errReplyTimeout) {
		t.Fatalf("A error = %v, want a reply timeout", err)
	}

	sentB, _ := gateRequest(&gate, "2", make(chan struct{}), time.Minute, time.Minute)
	mustNotHappen(t, sentB, "sending B while A's reply may still arrive")

	gate.delivered("2") // not the outstanding request
	mustNotHappen(t, sentB, "sending B after an unrelated reply")

	gate.delivered("1")
	mustHappen(t, sentB, "sending B after A's late reply")
}

func TestReplyGateStopsWaitingForALostReply(t *testing.T) {
	var gate replyGate
	_, doneA := gateRequest(&gate, "1", make(chan struct{}), 10*time.Millisecond, 150*time.Millisecond)
	mustHappen(t, doneA, "timing out A")

	sentB, _ := gateRequest(&gate, "2", make(chan struct{}), time.Minute, time.Minute)
	mustNotHappen(t, sentB, "sending B during the quarantine")
	mustHappen(t, sentB, "sending B after the quarantine")
}

func TestReplyGateReleasesOnSessionEndAndSendFailure(t *testing.T) {
	var gate replyGate
	failure := errors.New("send failed")
	if err := gate.exchange(nil, "1", func() error { return failure }, nil, time.Second, time.Minute); !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the send failure", err)
	}

	done := make(chan struct{})
	sent, result := make(chan struct{}), make(chan error, 1)
	go func() {
		result <- gate.exchange(done, "2", func() error { close(sent); return nil }, nil, time.Minute, time.Minute)
	}()
	mustHappen(t, sent, "sending after a failed send")
	close(done)
	if err := mustHappen(t, result, "ending the session"); !errors.Is(err, errConnectionSessionChanged) {
		t.Fatalf("error = %v, want a session change", err)
	}

	next, _ := gateRequest(&gate, "3", make(chan struct{}), time.Minute, time.Minute)
	mustHappen(t, next, "sending after the session ended")
}

func TestReplyGateDoesNotSendARequestAlreadyAnswered(t *testing.T) {
	var gate replyGate
	reply := make(chan struct{})
	close(reply)
	err := gate.exchange(nil, "1", func() error { t.Error("sent an answered request"); return nil }, reply, time.Second, time.Second)
	if err != nil {
		t.Fatal(err)
	}
}
