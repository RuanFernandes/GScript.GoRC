package main

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"sync"
	"time"

	"graal-rc/internal/connection"
	"graal-rc/rclib"
)

type ConnectionRecoveryStatus struct {
	Revision    uint64 `json:"revision"`
	Active      bool   `json:"active"`
	Phase       string `json:"phase"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"maxAttempts"`
	Reason      string `json:"reason"`
}

type connectionRecovery struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	status  ConnectionRecoveryStatus
	blocked int
}

func (r *connectionRecovery) snapshot() ConnectionRecoveryStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *connectionRecovery) stop() {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// start coalesces disconnect callbacks from a failing login attempt. The
// completion channel covers cleanup, so an explicit login/logout cannot race
// a previous automatic attempt when it reuses the connection service.
func (r *connectionRecovery) start(reason string, delays []time.Duration, attempt func(context.Context) error, publish func(ConnectionRecoveryStatus), onFailure ...func(context.Context)) bool {
	r.mu.Lock()
	if r.blocked > 0 {
		r.mu.Unlock()
		return false
	}
	if r.cancel != nil {
		r.mu.Unlock()
		return true
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	r.cancel, r.done = cancel, done
	r.status = ConnectionRecoveryStatus{Revision: r.status.Revision + 1, Active: true, Phase: "waiting", MaxAttempts: len(delays), Reason: reason}
	status := r.status
	r.mu.Unlock()
	publish(status)
	go func() {
		defer close(done)
		defer cancel()
		err := runRecoveryAttempts(ctx, delays, attempt, func(n int) {
			r.mu.Lock()
			r.status.Revision++
			r.status.Phase, r.status.Attempt = "connecting", n
			status := r.status
			r.mu.Unlock()
			publish(status)
		})
		if err != nil && ctx.Err() == nil && len(onFailure) > 0 {
			onFailure[0](ctx)
		}
		r.mu.Lock()
		r.status.Revision++
		r.status.Active = false
		r.status.Phase = "connected"
		if ctx.Err() != nil {
			r.status.Phase = "cancelled"
		} else if err != nil {
			r.status.Phase, r.status.Reason = "failed", err.Error()
		}
		status := r.status
		r.cancel = nil
		r.mu.Unlock()
		publish(status)
	}()
	return true
}

func runRecoveryAttempts(ctx context.Context, delays []time.Duration, attempt func(context.Context) error, progress func(int)) error {
	var lastErr error
	for i, delay := range delays {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		progress(i + 1)
		attemptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		lastErr = attempt(attemptCtx)
		cancel()
		if lastErr == nil || !connection.IsTransientConnectionError(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func (a *App) beginConnectionRecovery(reason string) bool {
	if a.recovery.snapshot().Active {
		return true
	}
	if appIsShuttingDown(a) || a.sessions == nil || !a.sessions.CanReconnect() || !connection.IsTransientDisconnect(reason) {
		return false
	}
	delays := make([]time.Duration, 5)
	for i := range delays {
		delays[i] = time.Second*time.Duration(1<<i) + time.Duration(rand.Int64N(int64(500*time.Millisecond)))
	}
	return a.recovery.start(reason, delays, func(ctx context.Context) error {
		a.sessionActionMu.Lock()
		defer a.sessionActionMu.Unlock()
		a.stopSyncEngine()
		if err := ctx.Err(); err != nil {
			return err
		}
		a.clearGallerySession()
		a.clearPMState()
		if err := a.sessions.ReconnectLastServerContext(ctx); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		a.refreshServerChromeFast()
		a.startSyncEngine()
		return nil
	}, a.emitConnectionRecovery, func(ctx context.Context) {
		a.sessionActionMu.Lock()
		defer a.sessionActionMu.Unlock()
		if ctx.Err() != nil {
			return
		}
		a.stopSyncEngine()
		a.closeSessionWindows()
		a.sessions.Logout()
		a.refreshServerChromeFast()
	})
}

func (a *App) stopConnectionRecovery() {
	a.recovery.mu.Lock()
	a.recovery.blocked++
	a.recovery.mu.Unlock()
	a.recovery.stop()
	a.recovery.mu.Lock()
	a.recovery.status = ConnectionRecoveryStatus{Revision: a.recovery.status.Revision + 1, Phase: "idle"}
	status := a.recovery.status
	a.recovery.mu.Unlock()
	a.emitConnectionRecovery(status)
}

func (a *App) allowConnectionRecovery() {
	a.recovery.mu.Lock()
	a.recovery.blocked--
	a.recovery.mu.Unlock()
}

func (a *App) emitConnectionRecovery(status ConnectionRecoveryStatus) {
	if a.app == nil {
		return
	}
	data, err := json.Marshal(status)
	if err == nil {
		a.app.Event.Emit("rc:recovery", string(data))
	}
}

// GetConnectionRecovery remains available even when the native library stalls.
func (a *App) GetConnectionRecovery() ConnectionRecoveryStatus { return a.recovery.snapshot() }

func (a *App) GetNativeCallStats() rclib.NativeCallStats { return rclib.ReadNativeCallStats() }

func (a *App) startNativeMonitor() {
	a.nativeMonitorStop, a.nativeMonitorDone = make(chan struct{}), make(chan struct{})
	go func() {
		defer close(a.nativeMonitorDone)
		rclib.MonitorNativeCalls(a.nativeMonitorStop)
	}()
}

func (a *App) stopNativeMonitor() {
	if a.nativeMonitorStop != nil {
		close(a.nativeMonitorStop)
		<-a.nativeMonitorDone
	}
}
