package sync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"graal-rc/rclib"
)

func TestIncompleteSnapshotsNeverChangeLocalFiles(t *testing.T) {
	for _, bootstrap := range []bool{true, false} {
		t.Run(fmt.Sprintf("bootstrap=%t", bootstrap), func(t *testing.T) {
			dir := t.TempDir()
			classDir := filepath.Join(dir, "classes")
			if err := os.MkdirAll(classDir, 0o700); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"Received", "TimedOut"} {
				if err := os.WriteFile(filepath.Join(classDir, name+scriptExt), []byte("local "+name), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			backend := &permissionBackendStub{
				fetchErr:     errors.New("one script timed out"),
				fetchReplies: []rclib.ScriptReply{{Type: "class", Name: "Received", Script: "remote"}},
			}
			engine := NewEngine(backend, "server", nil)
			engine.ApplyConfig(SyncConfig{Enabled: true, OutputDir: dir, AutoPullServer: true})
			engine.MarkPermissionsReady()
			if bootstrap {
				if err := engine.bootstrap(context.Background(), dir); err == nil {
					t.Fatal("partial bootstrap succeeded")
				}
			} else {
				engine.reconcileLocked(context.Background(), false)
			}
			for _, name := range []string{"Received", "TimedOut"} {
				data, err := os.ReadFile(filepath.Join(classDir, name+scriptExt))
				if err != nil || string(data) != "local "+name {
					t.Fatalf("%s changed after partial fetch: %q, %v", name, data, err)
				}
			}
			status := engine.Status()
			if status.SyncGeneration != 0 || status.LastError == "" || status.Progress.Active {
				t.Fatalf("incomplete snapshot reported success: %+v", status)
			}
		})
	}
}

func queuedEngine() *Engine {
	engine := NewEngine(&permissionBackendStub{}, "server", nil)
	engine.running = true
	engine.activityPending = make(map[string]Activity)
	engine.activityWake = make(chan struct{}, 1)
	return engine
}

func TestActivityQueueCoalescesAndBoundsBursts(t *testing.T) {
	engine := queuedEngine()
	for i := 0; i < 10000; i++ {
		engine.EnqueueChatLine("Script Example updated by Test")
	}
	engine.EnqueueChatLine("Script Example deleted by Test")
	if len(engine.activityPending) != 1 || len(engine.activityOrder) != 1 || engine.activityPending["class:Example"].Action != "deleted" {
		t.Fatal("repeated activity was not coalesced to the latest action")
	}
	for i := 0; i < maxPendingActivities+100; i++ {
		engine.EnqueueChatLine(fmt.Sprintf("Script Example%d updated by Test", i))
	}
	if len(engine.activityPending) != maxPendingActivities || len(engine.activityOrder) != maxPendingActivities || !engine.activityOverflow {
		t.Fatal("burst did not request a full snapshot at the queue limit")
	}
}

func TestStoppedActivityWorkerDoesNotApplyQueuedDeletion(t *testing.T) {
	engine := queuedEngine()
	dir := t.TempDir()
	path := filepath.Join(dir, "Example.gs2")
	if err := os.WriteFile(path, []byte("unsaved local"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine.refs[path] = scriptRef{kind: "class", key: "Example", name: "Example", path: path}
	engine.EnqueueChatLine("Script Example deleted by Test")
	engine.running = false
	stop, done := make(chan struct{}), make(chan struct{})
	close(stop)
	go engine.activityLoop(context.Background(), stop, engine.activityWake, done)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("activity worker did not stop")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale activity deleted the file: %v", err)
	}
	engine.EnqueueChatLine("Script Another updated by Test")
	if len(engine.activityPending) != 1 {
		t.Fatal("stopped engine accepted new activity")
	}
}
