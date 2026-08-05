package main

import (
	"testing"
	"time"
)

func TestShutdownStopsRegisteredBackgroundWork(t *testing.T) {
	a := &App{}
	lifecycle := lifecycleFor(a)
	started := make(chan struct{})
	stopped := make(chan struct{})
	if !lifecycle.startBackground(func(stop <-chan struct{}) {
		close(started)
		<-stop
		close(stopped)
	}) {
		t.Fatal("background worker was not registered")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background worker did not start")
	}

	a.shutdown()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not stop background worker")
	}
	if lifecycle.startBackground(func(<-chan struct{}) {}) {
		t.Fatal("background worker registered after shutdown")
	}

	a.shutdown()
	a.postShutdown()
	lifecycle.mu.Lock()
	completed := lifecycle.completed
	lifecycle.mu.Unlock()
	if !completed {
		t.Fatal("post-shutdown hook did not mark lifecycle complete")
	}
}

func TestApplicationOptionsConfigureWailsLifecycle(t *testing.T) {
	a := &App{}
	options := applicationOptions(a)
	if options.OnShutdown == nil {
		t.Fatal("OnShutdown was not configured")
	}
	if options.PostShutdown == nil {
		t.Fatal("PostShutdown was not configured")
	}
	if options.SingleInstance == nil || options.SingleInstance.UniqueID == "" {
		t.Fatal("single-instance options were not configured")
	}
	if options.SingleInstance.OnSecondInstanceLaunch == nil {
		t.Fatal("second-instance callback was not configured")
	}

	options.OnShutdown()
	if !appIsShuttingDown(a) {
		t.Fatal("OnShutdown did not enter stopping state")
	}
	options.PostShutdown()
}
