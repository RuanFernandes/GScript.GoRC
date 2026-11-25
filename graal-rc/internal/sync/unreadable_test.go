package sync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"graal-rc/rclib"
)

// unreadableFetchError stands in for connection.UnreadableScriptsError: a
// snapshot that is complete except for scripts the server did not deliver.
type unreadableFetchError struct{ scripts []rclib.ScriptReply }

func (e *unreadableFetchError) Error() string {
	return "script snapshot is incomplete: scripts were not delivered by the server"
}

func (e *unreadableFetchError) UnreadableScripts() ([]rclib.ScriptReply, []string) {
	reasons := make([]string, len(e.scripts))
	for i := range reasons {
		reasons[i] = "script request timed out"
	}
	return e.scripts, reasons
}

// unreadableBackend counts what the engine writes to the server.
type unreadableBackend struct {
	permissionBackendStub
	saved     []string
	added     []string
	openReply rclib.ScriptReply
}

func (b *unreadableBackend) SaveWeapon(name, _ string) error {
	b.saved = append(b.saved, name)
	return nil
}
func (b *unreadableBackend) AddWeapon(name string) error { b.added = append(b.added, name); return nil }
func (b *unreadableBackend) OpenScript(string, string) (rclib.ScriptReply, error) {
	return b.openReply, nil
}

var stuckWeapon = rclib.ScriptReply{Type: "weapon", Name: "Staff/News"}

func stuckWeaponPath(dir string) string {
	return filepath.Join(dir, "weapons", "Staff%047News"+scriptExt)
}

func writeLocal(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newUnreadableEngine(t *testing.T, backend ScriptBackend) (*Engine, string) {
	t.Helper()
	dir := t.TempDir()
	engine := NewEngine(backend, "server", func(string, ...any) {})
	engine.ApplyConfig(SyncConfig{Enabled: true, OutputDir: dir, AutoPullServer: true, AutoPushLocal: true})
	engine.MarkPermissionsReady()
	return engine, dir
}

func TestBootstrapFinishesWithoutScriptsTheServerDoesNotDeliver(t *testing.T) {
	backend := &unreadableBackend{}
	backend.fetchReplies = []rclib.ScriptReply{{Type: "weapon", Name: "Other", Script: "server other"}}
	backend.fetchErr = &unreadableFetchError{scripts: []rclib.ScriptReply{stuckWeapon}}
	engine, dir := newUnreadableEngine(t, backend)
	writeLocal(t, stuckWeaponPath(dir), "local news the server never sent")

	if err := engine.bootstrap(context.Background(), dir); err != nil {
		t.Fatalf("bootstrap failed over a script the server does not deliver: %v", err)
	}

	if data, err := os.ReadFile(stuckWeaponPath(dir)); err != nil || string(data) != "local news the server never sent" {
		t.Fatalf("local file of the unreadable script = %q, %v; it must be left exactly as it was", data, err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "weapons", "Other"+scriptExt)); err != nil || string(data) != "server other" {
		t.Fatalf("the script that was read was not written: %q, %v", data, err)
	}
	status := engine.Status()
	if status.SyncGeneration == 0 || status.LastError != "" || status.InitialSync || status.Progress.Active {
		t.Fatalf("the sync did not complete: %+v", status)
	}
	if len(status.Unreadable) != 1 || status.Unreadable[0].Kind != "weapon" || status.Unreadable[0].Name != "Staff/News" || status.Unreadable[0].Reason == "" {
		t.Fatalf("unreadable = %+v; want the one script, with its reason", status.Unreadable)
	}
}

func TestReconcileKeepsTheLocalFileOfAnUnreadableScriptEvenWhenNothingElseIsListed(t *testing.T) {
	backend := &unreadableBackend{}
	backend.fetchErr = &unreadableFetchError{scripts: []rclib.ScriptReply{stuckWeapon}}
	engine, dir := newUnreadableEngine(t, backend)
	writeLocal(t, stuckWeaponPath(dir), "local news")
	// A file the server really no longer lists is still removed.
	gone := filepath.Join(dir, "weapons", "Gone"+scriptExt)
	writeLocal(t, gone, "deleted on the server")

	engine.reconcileLocked(context.Background(), false)

	if _, err := os.Stat(stuckWeaponPath(dir)); err != nil {
		t.Fatalf("the file of an unreadable script was removed as if it were deleted on the server: %v", err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("a script missing from the server was kept: %v", err)
	}
	if got := len(engine.Status().Unreadable); got != 1 {
		t.Fatalf("unreadable count = %d, want 1", got)
	}
}

func TestEditingAnUnreadableScriptLocallyIsNotUploaded(t *testing.T) {
	backend := &unreadableBackend{}
	backend.fetchErr = &unreadableFetchError{scripts: []rclib.ScriptReply{stuckWeapon}}
	engine, dir := newUnreadableEngine(t, backend)
	path := stuckWeaponPath(dir)
	writeLocal(t, path, "local news")
	if err := engine.bootstrap(context.Background(), dir); err != nil {
		t.Fatal(err)
	}

	writeLocal(t, path, "local news, edited")
	engine.pushLocalLocked(context.Background(), path)

	if len(backend.saved) != 0 || len(backend.added) != 0 {
		t.Fatalf("saved=%v added=%v: an upload would replace a server version nobody has read", backend.saved, backend.added)
	}

	// Any other script still uploads.
	other := filepath.Join(dir, "weapons", "Other"+scriptExt)
	writeLocal(t, other, "first version")
	engine.pushLocalLocked(context.Background(), other)
	if len(backend.added) != 1 || backend.added[0] != "Other" || len(backend.saved) != 1 || backend.saved[0] != "Other" {
		t.Fatalf("a script that is not unreadable did not upload: saved=%v added=%v", backend.saved, backend.added)
	}
}

func TestAnUnreadableScriptThatIsReadLaterIsClearedAndNeverSilentlyOverwritten(t *testing.T) {
	backend := &unreadableBackend{}
	backend.fetchErr = &unreadableFetchError{scripts: []rclib.ScriptReply{stuckWeapon}}
	engine, dir := newUnreadableEngine(t, backend)
	path := stuckWeaponPath(dir)
	writeLocal(t, path, "my local news")
	if err := engine.bootstrap(context.Background(), dir); err != nil {
		t.Fatal(err)
	}

	// The server now answers (after the other developer split the script) and
	// announces a change to it in the chat.
	backend.openReply = rclib.ScriptReply{Type: "weapon", Name: "Staff/News", Script: "server news"}
	engine.handleActivity(Activity{Kind: "weapon", Key: "Staff/News", Name: "Staff/News", Actor: "someone", Action: "updated"})

	if got := len(engine.Status().Unreadable); got != 0 {
		t.Fatalf("unreadable count = %d after the script was read", got)
	}
	if data, _ := os.ReadFile(path); string(data) != "my local news" {
		t.Fatalf("local file = %q; with no common baseline the two versions must be reviewed, not overwritten", data)
	}
	items := engine.Status().Items
	if len(items) != 1 || items[0].Local != "my local news" || items[0].Server != "server news" {
		t.Fatalf("review items = %+v; want the two versions side by side", items)
	}
}

func TestACleanSnapshotClearsTheUnreadableList(t *testing.T) {
	backend := &unreadableBackend{}
	backend.fetchErr = &unreadableFetchError{scripts: []rclib.ScriptReply{stuckWeapon}}
	engine, dir := newUnreadableEngine(t, backend)
	if err := engine.bootstrap(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if len(engine.Status().Unreadable) != 1 {
		t.Fatal("setup: the script was not reported as unreadable")
	}

	backend.fetchErr = nil
	backend.fetchReplies = []rclib.ScriptReply{{Type: "weapon", Name: "Staff/News", Script: "now delivered"}}
	engine.reconcileLocked(context.Background(), false)

	if got := engine.Status().Unreadable; len(got) != 0 {
		t.Fatalf("unreadable = %+v after a snapshot that read everything", got)
	}
	if data, _ := os.ReadFile(stuckWeaponPath(dir)); string(data) != "now delivered" {
		t.Fatalf("the script that was finally read was not written: %q", data)
	}
}

func TestOtherSnapshotFailuresStillFailTheSync(t *testing.T) {
	backend := &unreadableBackend{}
	backend.fetchErr = errors.New("script snapshot is incomplete: 40 of 3702 script reads failed")
	engine, dir := newUnreadableEngine(t, backend)
	writeLocal(t, stuckWeaponPath(dir), "local news")

	if err := engine.bootstrap(context.Background(), dir); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("a plain incomplete snapshot returned %v; it must still fail", err)
	}
	status := engine.Status()
	if status.SyncGeneration != 0 || status.LastError == "" || len(status.Unreadable) != 0 {
		t.Fatalf("a failed snapshot was reported as done or as unreadable scripts: %+v", status)
	}
}
