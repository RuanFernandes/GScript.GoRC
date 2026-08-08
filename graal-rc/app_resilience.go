package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"
)

const (
	reconnectMaxAttempts = 8
	reconnectBaseDelay   = time.Second
	reconnectMaxDelay    = 30 * time.Second
)

// ReconnectStatus is the redacted, user-facing state of automatic recovery.
// It intentionally contains no credentials or native handles.
type ReconnectStatus struct {
	Active        bool   `json:"active"`
	Attempt       int    `json:"attempt"`
	MaxAttempts   int    `json:"maxAttempts"`
	ServerIndex   int    `json:"serverIndex"`
	ServerName    string `json:"serverName"`
	LastError     string `json:"lastError,omitempty"`
	NextAttemptAt int64  `json:"nextAttemptAt,omitempty"`
}

// GetReconnectStatus returns a snapshot suitable for a status banner or the
// diagnostics window.
func (a *App) GetReconnectStatus() ReconnectStatus {
	a.reconnectMu.Lock()
	defer a.reconnectMu.Unlock()
	return a.reconnectState
}

// ReconnectNow cancels the current backoff and starts an immediate recovery
// attempt for the last selected server.
func (a *App) ReconnectNow() error {
	a.reconnectMu.Lock()
	selected := a.selectedServerSet
	a.reconnectMu.Unlock()
	if !selected {
		return errors.New("no server is available for reconnection")
	}
	a.startReconnect(true)
	return nil
}

// CancelReconnect stops recovery and closes the native session. The frontend
// can then return to account/server selection without leaving a stale handle.
func (a *App) CancelReconnect() {
	a.reconnectMu.Lock()
	cancel := a.reconnectCancel
	state := a.reconnectState
	a.reconnectCancel = nil
	a.reconnectGeneration++
	a.reconnectState.Active = false
	a.reconnectState.NextAttemptAt = 0
	a.reconnectMu.Unlock()
	if cancel != nil {
		cancel()
	}

	a.connectionMu.Lock()
	a.sessionActive.Store(false)
	a.stopSyncEngine()
	a.sessions.Logout()
	a.connectionMu.Unlock()
	a.refreshServerChrome()

	state.Active = false
	state.NextAttemptAt = 0
	a.emitResilienceEvent("rc:reconnectCancelled", state)
}

// stopReconnect is used by explicit connect/logout operations. It is silent
// because those flows have their own user-facing status and error handling.
func (a *App) stopReconnect() {
	a.reconnectMu.Lock()
	cancel := a.reconnectCancel
	a.reconnectCancel = nil
	a.reconnectGeneration++
	a.reconnectState = ReconnectStatus{}
	a.reconnectMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) startReconnect(force bool) {
	if a.quitting.Load() {
		return
	}
	if !force && !a.sessionActive.Load() {
		return
	}

	a.reconnectMu.Lock()
	if !a.selectedServerSet {
		a.reconnectMu.Unlock()
		return
	}
	if a.reconnectCancel != nil && !force {
		a.reconnectMu.Unlock()
		return
	}
	if a.reconnectCancel != nil {
		a.reconnectCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.reconnectGeneration++
	generation := a.reconnectGeneration
	index := a.selectedServer
	state := ReconnectStatus{
		Active:      true,
		MaxAttempts: reconnectMaxAttempts,
		ServerIndex: index,
		ServerName:  a.selectedServerName,
	}
	a.reconnectCancel = cancel
	a.reconnectState = state
	a.reconnectMu.Unlock()

	a.emitResilienceEvent("rc:reconnect", state)
	go a.runReconnect(ctx, generation, index)
}

func (a *App) runReconnect(ctx context.Context, generation uint64, index int) {
	lastError := ""
	for attempt := 1; attempt <= reconnectMaxAttempts; attempt++ {
		delay := reconnectDelay(attempt)
		nextAttemptAt := time.Now().Add(delay).UnixMilli()
		if !a.updateReconnectState(generation, func(state *ReconnectStatus) {
			state.Active = true
			state.Attempt = attempt
			state.MaxAttempts = reconnectMaxAttempts
			state.NextAttemptAt = nextAttemptAt
			state.LastError = lastError
		}) {
			return
		}
		if !waitReconnect(ctx, delay) {
			a.finishReconnect(generation)
			return
		}
		if !a.updateReconnectState(generation, func(state *ReconnectStatus) {
			state.NextAttemptAt = 0
		}) {
			return
		}

		a.connectionMu.Lock()
		if ctx.Err() != nil {
			a.connectionMu.Unlock()
			a.finishReconnect(generation)
			return
		}
		a.sessionActive.Store(false)
		a.stopSyncEngine()
		err := a.sessions.ConnectToServer(index)
		a.refreshServerChrome()
		if err == nil && ctx.Err() == nil {
			a.sessionActive.Store(true)
			a.reconnectMu.Lock()
			a.selectedServerName = a.sessions.Status().ServerName
			a.reconnectMu.Unlock()
			a.startSyncEngine()
		}
		if err == nil && ctx.Err() != nil {
			a.sessionActive.Store(false)
			a.stopSyncEngine()
			a.sessions.Logout()
		}
		a.connectionMu.Unlock()

		if ctx.Err() != nil {
			a.finishReconnect(generation)
			return
		}
		if err == nil {
			if state, ok := a.finishReconnect(generation); ok {
				a.emitResilienceEvent("rc:reconnected", state)
			}
			return
		}
		lastError = err.Error()
	}

	state, ok := a.failReconnect(generation, lastError)
	if ok {
		a.sessionActive.Store(false)
		a.emitResilienceEvent("rc:reconnectFailed", state)
	}
}

func reconnectDelay(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	seconds := float64(reconnectBaseDelay) * math.Pow(2, float64(attempt-2))
	delay := time.Duration(seconds)
	if delay > reconnectMaxDelay {
		return reconnectMaxDelay
	}
	return delay
}

func waitReconnect(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (a *App) updateReconnectState(generation uint64, update func(*ReconnectStatus)) bool {
	a.reconnectMu.Lock()
	if generation != a.reconnectGeneration || a.reconnectCancel == nil {
		a.reconnectMu.Unlock()
		return false
	}
	update(&a.reconnectState)
	state := a.reconnectState
	a.reconnectMu.Unlock()
	a.emitResilienceEvent("rc:reconnect", state)
	return true
}

func (a *App) finishReconnect(generation uint64) (ReconnectStatus, bool) {
	a.reconnectMu.Lock()
	if generation != a.reconnectGeneration {
		state := a.reconnectState
		a.reconnectMu.Unlock()
		return state, false
	}
	if a.reconnectCancel != nil {
		a.reconnectCancel = nil
	}
	a.reconnectState.Active = false
	a.reconnectState.NextAttemptAt = 0
	state := a.reconnectState
	a.reconnectMu.Unlock()
	return state, true
}

func (a *App) failReconnect(generation uint64, lastError string) (ReconnectStatus, bool) {
	a.reconnectMu.Lock()
	defer a.reconnectMu.Unlock()
	if generation != a.reconnectGeneration {
		return ReconnectStatus{}, false
	}
	a.reconnectCancel = nil
	a.reconnectState.Active = false
	a.reconnectState.NextAttemptAt = 0
	a.reconnectState.LastError = lastError
	return a.reconnectState, true
}

func (a *App) emitResilienceEvent(name string, value any) {
	if a.app == nil {
		return
	}
	b, err := json.Marshal(value)
	if err != nil {
		return
	}
	a.app.Event.Emit(name, string(b))
}
