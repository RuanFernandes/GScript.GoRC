package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"graal-rc/internal/connection"
)

func TestRecoveryRetriesOnlyTransportErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want int
	}{
		{"transport", errors.New("connection reset by peer"), 3},
		{"authentication", connection.ErrReconnectRejected, 1},
		{"unknown", errors.New("server policy"), 1},
		{"success", nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			err := runRecoveryAttempts(context.Background(), []time.Duration{0, 0, 0}, func(context.Context) error { calls++; return test.err }, func(int) {})
			if calls != test.want || !errors.Is(err, test.err) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestRecoveryStopCancelsAndJoinsActiveAttempt(t *testing.T) {
	var recovery connectionRecovery
	started := make(chan struct{})
	var calls atomic.Int32
	recovery.start("timeout", []time.Duration{0, 0}, func(ctx context.Context) error {
		calls.Add(1)
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, func(ConnectionRecoveryStatus) {})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("attempt did not start")
	}
	recovery.start("another timeout", []time.Duration{0}, func(context.Context) error {
		calls.Add(100)
		return nil
	}, func(ConnectionRecoveryStatus) {})
	recovery.stop()
	if calls.Load() != 1 {
		t.Fatalf("duplicate recovery started: %d", calls.Load())
	}
	status := recovery.snapshot()
	if status.Active || status.Phase != "cancelled" {
		t.Fatalf("status=%+v", status)
	}
	recovery.stop()
}

func TestRecoveryCancellationSkipsQueuedAttempt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := runRecoveryAttempts(ctx, []time.Duration{time.Hour}, func(context.Context) error {
		t.Fatal("attempt ran after cancellation")
		return nil
	}, func(int) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestManualActionInvalidatesHistoricalFailureAndBlocksRecovery(t *testing.T) {
	app := &App{}
	app.recovery.status = ConnectionRecoveryStatus{Revision: 9, Phase: "failed"}
	app.stopConnectionRecovery()
	defer app.allowConnectionRecovery()
	status := app.GetConnectionRecovery()
	if status.Phase != "idle" || status.Revision <= 9 {
		t.Fatalf("historical failure retained: %+v", status)
	}
	if app.recovery.start("timeout", []time.Duration{0}, func(context.Context) error {
		t.Error("recovery started during a manual session action")
		return nil
	}, func(ConnectionRecoveryStatus) {}) {
		t.Fatal("accepted recovery during manual action")
	}
}

func TestRecoveryCleansBackendBeforePublishingFailure(t *testing.T) {
	var recovery connectionRecovery
	var cleaned atomic.Bool
	recovery.start("timeout", []time.Duration{0}, func(context.Context) error { return connection.ErrReconnectRejected }, func(status ConnectionRecoveryStatus) {
		if status.Phase == "failed" && !cleaned.Load() {
			t.Error("failure published before backend cleanup")
		}
	}, func(context.Context) { cleaned.Store(true) })
	recovery.mu.Lock()
	done := recovery.done
	recovery.mu.Unlock()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("recovery did not finish")
	}
	if !cleaned.Load() {
		t.Fatal("terminal failure did not clean the backend")
	}
}

// finishRecovery runs a recovery to completion and returns every status.
func finishRecovery(t *testing.T, recovery *connectionRecovery, delays []time.Duration, attempt func(context.Context) error, onFailure ...func(context.Context)) []ConnectionRecoveryStatus {
	t.Helper()
	var mu sync.Mutex
	var published []ConnectionRecoveryStatus
	recovery.start("timeout", delays, attempt, func(status ConnectionRecoveryStatus) {
		mu.Lock()
		published = append(published, status)
		mu.Unlock()
	}, onFailure...)
	recovery.mu.Lock()
	done := recovery.done
	recovery.mu.Unlock()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("recovery did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	return published
}

func TestRecoveryReconnectsAgainWhenTheNewSessionDropsBeforeItFinishes(t *testing.T) {
	var recovery connectionRecovery
	var calls atomic.Int32
	finishRecovery(t, &recovery, []time.Duration{0, 0, 0}, func(context.Context) error {
		if calls.Add(1) == 1 {
			// Reconnected, then dropped while the attempt was finishing.
			recovery.noteDisconnect("connection reset by peer")
		}
		return nil
	})
	if calls.Load() != 2 {
		t.Fatalf("attempts = %d, want a second reconnect", calls.Load())
	}
	if status := recovery.snapshot(); status.Active || status.Phase != "connected" {
		t.Fatalf("status = %+v", status)
	}
}

func TestRecoveryDoesNotReportConnectedAfterAKickWhileFinishing(t *testing.T) {
	var recovery connectionRecovery
	var calls atomic.Int32
	var cleaned atomic.Bool
	published := finishRecovery(t, &recovery, []time.Duration{0, 0}, func(context.Context) error {
		calls.Add(1)
		recovery.noteDisconnect("you have been kicked")
		return nil
	}, func(context.Context) { cleaned.Store(true) })
	for _, status := range published {
		if status.Phase == "connected" {
			t.Fatalf("reported connected on a dead session: %+v", status)
		}
	}
	if status := recovery.snapshot(); status.Phase != "failed" || calls.Load() != 1 || !cleaned.Load() {
		t.Fatalf("status=%+v calls=%d cleaned=%v", status, calls.Load(), cleaned.Load())
	}
}

func TestRecoveryFailsWhenTheLastAttemptDropsWhileFinishing(t *testing.T) {
	var recovery connectionRecovery
	finishRecovery(t, &recovery, []time.Duration{0}, func(context.Context) error {
		recovery.noteDisconnect("timeout")
		return nil
	})
	if status := recovery.snapshot(); status.Phase != "failed" {
		t.Fatalf("status = %+v", status)
	}
}

func TestRecoveryIgnoresDisconnectsOfEarlierAttempts(t *testing.T) {
	var recovery connectionRecovery
	var calls atomic.Int32
	finishRecovery(t, &recovery, []time.Duration{0, 0, 0}, func(context.Context) error {
		if calls.Add(1) == 1 {
			recovery.noteDisconnect("connection reset by peer")
			return errors.New("connection reset by peer")
		}
		return nil
	})
	if status := recovery.snapshot(); calls.Load() != 2 || status.Phase != "connected" {
		t.Fatalf("status=%+v calls=%d", status, calls.Load())
	}
}

func TestNoteDisconnectWithoutRecovery(t *testing.T) {
	var recovery connectionRecovery
	if recovery.noteDisconnect("timeout") {
		t.Fatal("an idle recovery claimed the disconnect")
	}
}
