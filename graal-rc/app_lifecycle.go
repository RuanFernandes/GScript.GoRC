package main

import (
	"errors"
	"sync"
)

var errAppShuttingDown = errors.New("application is shutting down")

// appLifecycle owns goroutines started by the desktop integration. App is
// defined in app.go, which is outside this ownership boundary, so lifecycle
// state is kept here and keyed by the App instance instead of adding fields to
// that type.
type appLifecycle struct {
	mu           sync.Mutex
	stopping     bool
	completed    bool
	stop         chan struct{}
	background   sync.WaitGroup
	shutdownOnce sync.Once
}

var appLifecycles sync.Map

func lifecycleFor(a *App) *appLifecycle {
	if a == nil {
		return &appLifecycle{stop: make(chan struct{})}
	}
	if value, ok := appLifecycles.Load(a); ok {
		return value.(*appLifecycle)
	}
	created := &appLifecycle{stop: make(chan struct{})}
	actual, _ := appLifecycles.LoadOrStore(a, created)
	return actual.(*appLifecycle)
}

func appIsShuttingDown(a *App) bool {
	if a == nil {
		return true
	}
	lifecycle := lifecycleFor(a)
	lifecycle.mu.Lock()
	defer lifecycle.mu.Unlock()
	return lifecycle.stopping
}

func ensureAppRunning(a *App) error {
	if appIsShuttingDown(a) {
		return errAppShuttingDown
	}
	return nil
}

// startBackground registers a goroutine before launching it. The registration
// and shutdown flag are protected by the same mutex, so shutdown cannot miss a
// ticker that starts concurrently with application teardown.
func (l *appLifecycle) startBackground(run func(<-chan struct{})) bool {
	if run == nil {
		return false
	}
	l.mu.Lock()
	if l.stopping {
		l.mu.Unlock()
		return false
	}
	l.background.Add(1)
	stop := l.stop
	l.mu.Unlock()

	go func() {
		defer l.background.Done()
		run(stop)
	}()
	return true
}

func (l *appLifecycle) beginStop() {
	l.mu.Lock()
	if !l.stopping {
		l.stopping = true
		close(l.stop)
	}
	l.mu.Unlock()
}

func (l *appLifecycle) stopBackground() {
	l.beginStop()
	l.background.Wait()
}

// postShutdown is registered through Wails Options.PostShutdown. Wails has
// already destroyed its native application at this point, so this hook only
// records completion and performs no Wails calls.
func (a *App) postShutdown() {
	a.stopMCPServer()
	lifecycle := lifecycleFor(a)
	lifecycle.mu.Lock()
	lifecycle.completed = true
	lifecycle.mu.Unlock()
}
