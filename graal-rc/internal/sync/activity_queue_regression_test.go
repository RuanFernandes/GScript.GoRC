package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	stdsync "sync"
	"testing"
	"time"

	"graal-rc/rclib"
)

type blockingActivityStatusBackend struct {
	permissionBackendStub
	entered chan struct{}
	release chan struct{}
	once    stdsync.Once
}

func (b *blockingActivityStatusBackend) IsNCConnected() bool {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return true
}

type failedActivitySnapshotBackend struct {
	permissionBackendStub
	fetched chan struct{}
}

func (b *failedActivitySnapshotBackend) FetchAllScripts(context.Context, func(string, string) bool, func(int, int)) ([]rclib.ScriptReply, error) {
	b.fetches++
	select {
	case b.fetched <- struct{}{}:
	default:
	}
	return nil, errors.New("temporary snapshot failure")
}

// Construct only the activity worker's lifecycle, without starting a watcher or
// bootstrap. These tests control when its queue and backend become available.
func newActivityRegressionEngine(backend ScriptBackend, dir string) *Engine {
	engine := NewEngine(backend, "TestServer", nil)
	engine.cfg = SyncConfig{Enabled: true, OutputDir: dir, AutoPullServer: true}
	engine.running = true
	engine.permissionsReady = true
	engine.activityPending = make(map[string]Activity)
	engine.activityWake = make(chan struct{}, 1)
	engine.activityDone = make(chan struct{})
	engine.stop = make(chan struct{})
	return engine
}

func TestStatusAllowsActivityEnqueueWhileNativeStatusIsBlocked(t *testing.T) {
	backend := &blockingActivityStatusBackend{entered: make(chan struct{}), release: make(chan struct{})}
	engine := newActivityRegressionEngine(backend, t.TempDir())
	statusDone := make(chan SyncStatus, 1)
	go func() { statusDone <- engine.Status() }()
	select {
	case <-backend.entered:
	case <-time.After(time.Second):
		close(backend.release)
		t.Fatal("Status did not reach the simulated native query")
	}

	// The native query models a call waiting for dllMu. The pump which owns
	// dllMu must still be able to enqueue activity and return its callback.
	enqueued := make(chan struct{})
	go func() {
		engine.EnqueueChatLine("Script Example updated by Test")
		close(enqueued)
	}()
	select {
	case <-enqueued:
	case <-time.After(time.Second):
		t.Error("Status held the engine mutex across the native query, blocking the event pump")
	}
	close(backend.release)
	select {
	case status := <-statusDone:
		if status.NCDown {
			t.Error("Status lost the native query result")
		}
	case <-time.After(time.Second):
		t.Fatal("Status did not finish after the native query was released")
	}
	select {
	case <-enqueued:
	case <-time.After(time.Second):
		t.Fatal("activity enqueue did not finish after Status returned")
	}
	engine.mu.RLock()
	_, queued := engine.activityPending["class:Example"]
	engine.mu.RUnlock()
	if !queued {
		t.Error("the event pump's activity was lost")
	}
}

func TestOldActivityWorkerDoesNotConsumeRestartedEngineQueue(t *testing.T) {
	engine := newActivityRegressionEngine(&permissionBackendStub{}, t.TempDir())
	path := filepath.Join(engine.cfg.OutputDir, "Example.gs2")
	if err := os.WriteFile(path, []byte("new session content"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine.refs[path] = scriptRef{kind: "class", key: "Example", name: "Example", path: path}
	engine.EnqueueChatLine("Script Example deleted by Test")

	// The old worker has already received a wake just as a new execution
	// replaces the shared queue. running is true for the new execution.
	oldWake := make(chan struct{}, 1)
	oldWake <- struct{}{}
	oldStop, oldDone := make(chan struct{}), make(chan struct{})
	go engine.activityLoop(context.Background(), oldStop, oldWake, oldDone)
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Error("old worker remained active after its queue generation was replaced")
	}
	close(oldStop)
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("old worker could not stop")
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "new session content" {
		t.Errorf("old worker applied a new session's deletion: content=%q err=%v", content, err)
	}
	engine.mu.RLock()
	queued := len(engine.activityPending)
	engine.mu.RUnlock()
	if queued != 1 {
		t.Errorf("old worker consumed the new queue: pending=%d, want 1", queued)
	}
}

func TestFailedOverflowSnapshotRemainsPendingAndStopCancelsRetry(t *testing.T) {
	backend := &failedActivitySnapshotBackend{fetched: make(chan struct{}, 1)}
	engine := newActivityRegressionEngine(backend, t.TempDir())
	engine.activityOverflow = true
	engine.activityWake <- struct{}{}
	go engine.activityLoop(context.Background(), engine.stop, engine.activityWake, engine.activityDone)
	select {
	case <-backend.fetched:
	case <-time.After(time.Second):
		engine.Stop()
		t.Fatal("overflow did not request a complete snapshot")
	}

	// Fetch runs under workMu. Acquiring it here waits until the attempt has
	// failed and its pending state has been updated, without waiting for retry.
	engine.workMu.Lock()
	engine.mu.RLock()
	pending, generation, lastError := engine.activityOverflow, engine.syncGeneration, engine.lastError
	engine.mu.RUnlock()
	engine.workMu.Unlock()
	if !pending || generation != 0 || lastError == "" {
		t.Errorf("failed overflow snapshot was forgotten: pending=%t generation=%d error=%q", pending, generation, lastError)
	}

	stopped := make(chan struct{})
	go func() {
		engine.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop waited for the five-second overflow retry instead of canceling it")
	}
	if backend.fetches != 1 {
		t.Errorf("overflow retried without backoff or after Stop: fetches=%d, want 1", backend.fetches)
	}
}

func TestPausedActivityKeepsSnapshotPendingWithoutApplyingDeletion(t *testing.T) {
	for _, panicMode := range []bool{false, true} {
		name := "pause"
		if panicMode {
			name = "panic"
		}
		t.Run(name, func(t *testing.T) {
			engine := newActivityRegressionEngine(&permissionBackendStub{}, t.TempDir())
			engine.panicMode = panicMode
			if !panicMode {
				engine.cfg.PauseUntil = time.Now().Add(time.Hour).Unix()
			}
			path := filepath.Join(engine.cfg.OutputDir, "Example.gs2")
			if err := os.WriteFile(path, []byte("preserved while paused"), 0o600); err != nil {
				t.Fatal(err)
			}
			engine.refs[path] = scriptRef{kind: "class", key: "Example", name: "Example", path: path}
			engine.EnqueueChatLine("Script Example deleted by Test")
			go engine.activityLoop(context.Background(), engine.stop, engine.activityWake, engine.activityDone)

			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(time.Millisecond)
			defer tick.Stop()
			pending := false
		waitForPending:
			for {
				engine.mu.RLock()
				pending = engine.activityOverflow
				engine.mu.RUnlock()
				if pending {
					break
				}
				select {
				case <-deadline.C:
					break waitForPending
				case <-tick.C:
				}
			}
			engine.Stop()
			if !pending {
				t.Error("activity received during pause was discarded without scheduling a later snapshot")
			}
			if content, err := os.ReadFile(path); err != nil || string(content) != "preserved while paused" {
				t.Errorf("paused worker applied deletion: content=%q err=%v", content, err)
			}
		})
	}
}
