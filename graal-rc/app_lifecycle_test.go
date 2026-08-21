package main

import (
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	if options.SingleInstance != nil {
		t.Fatal("single-instance options must be disabled so multiple RC connections can run")
	}

	options.OnShutdown()
	if !appIsShuttingDown(a) {
		t.Fatal("OnShutdown did not enter stopping state")
	}
	options.PostShutdown()
}

func TestHardenedWebviewWindowOptionsDisableInspection(t *testing.T) {
	options := hardenedWebviewWindowOptions(application.WebviewWindowOptions{
		DevToolsEnabled:            true,
		DefaultContextMenuDisabled: false,
		OpenInspectorOnStartup:     true,
	})
	if options.DevToolsEnabled {
		t.Fatal("devtools must remain disabled for every window")
	}
	if !options.DefaultContextMenuDisabled {
		t.Fatal("the native context menu must remain disabled")
	}
	if options.OpenInspectorOnStartup {
		t.Fatal("the inspector must not open at startup")
	}
}
