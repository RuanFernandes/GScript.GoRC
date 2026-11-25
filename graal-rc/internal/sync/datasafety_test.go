package sync

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"graal-rc/rclib"
)

// safetyBackend records everything the engine writes to the server.
type safetyBackend struct {
	permissionBackendStub
	saved   []string // "kind:key=content"
	added   []string // "kind:name"
	server  map[string]string
	npcs    []rclib.NPC
	saveErr error
	onFetch func()
	onSave  func()
}

func (b *safetyBackend) save(id, content string) error {
	if b.saveErr != nil {
		return b.saveErr
	}
	b.saved = append(b.saved, id+"="+content)
	if b.server == nil {
		b.server = map[string]string{}
	}
	b.server[id] = content
	if b.onSave != nil {
		b.onSave()
	}
	return nil
}
func (b *safetyBackend) SaveWeapon(name, content string) error {
	return b.save("weapon:"+name, content)
}
func (b *safetyBackend) SaveClass(name, content string) error {
	return b.save("class:"+name, content)
}
func (b *safetyBackend) SaveNPC(id int, content string) error {
	return b.save("npc:"+strconv.Itoa(id), content)
}
func (b *safetyBackend) AddWeapon(name string) error {
	b.added = append(b.added, "weapon:"+name)
	return nil
}
func (b *safetyBackend) AddClass(name string) error {
	b.added = append(b.added, "class:"+name)
	return nil
}
func (b *safetyBackend) GetNPCs() ([]rclib.NPC, error) { return b.npcs, nil }
func (b *safetyBackend) OpenScript(kind, key string) (rclib.ScriptReply, error) {
	return rclib.ScriptReply{Type: kind, Name: key, Script: b.server[kind+":"+key]}, nil
}
func (b *safetyBackend) FetchAllScripts(ctx context.Context, allowed func(string, string) bool, progress func(int, int)) ([]rclib.ScriptReply, error) {
	if b.onFetch != nil {
		b.onFetch()
	}
	return b.permissionBackendStub.FetchAllScripts(ctx, allowed, progress)
}

// serve makes one weapon the whole server listing.
func (b *safetyBackend) serve(content string) {
	b.fetchReplies = []rclib.ScriptReply{{Type: "weapon", Name: "Sword", Script: content}}
	if b.server == nil {
		b.server = map[string]string{}
	}
	b.server["weapon:Sword"] = content
}

type safetyEvents struct{ conflicts []ReviewItem }

func newSafetyEngine(backend ScriptBackend, server, dir string, events *safetyEvents) *Engine {
	engine := NewEngine(backend, server, func(name string, data ...any) {
		if events != nil && name == "rc:syncConflict" {
			events.conflicts = append(events.conflicts, data[0].(ReviewItem))
		}
	})
	engine.ApplyConfig(SyncConfig{Enabled: true, OutputDir: dir, AutoPullServer: true, AutoPushLocal: true})
	engine.MarkPermissionsReady()
	return engine
}

func swordPath(dir string) string { return filepath.Join(dir, "weapons", "Sword"+scriptExt) }

func mustBootstrap(t *testing.T, engine *Engine, dir string) {
	t.Helper()
	if err := engine.bootstrap(context.Background(), dir); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != want {
		t.Fatalf("%s = %q, %v; want %q", filepath.Base(path), data, err, want)
	}
}

// filesContaining returns every file under dir whose content is exactly want.
func filesContaining(t *testing.T, dir, want string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if string(data) == want {
			found = append(found, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func assertRecoverable(t *testing.T, dir, original, content string) {
	t.Helper()
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Fatalf("%s was not taken out of the sync folder: %v", original, err)
	}
	found := filesContaining(t, dir, content)
	if len(found) != 1 {
		t.Fatalf("content %q is not recoverable: found %v", content, found)
	}
	if ext := filepath.Ext(found[0]); ext == scriptExt || ext == ".gscript" {
		t.Fatalf("recovery file %s would be indexed as a live script", found[0])
	}
}

// A: server priority cleanup must never destroy content.

func TestSnapshotNeverDestroysUnlistedLocalFiles(t *testing.T) {
	for name, replies := range map[string][]rclib.ScriptReply{
		"empty listing": nil,
		"other listing": {{Type: "weapon", Name: "Sword", Script: "server sword"}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "weapons", "Offline"+scriptExt)
			writeLocal(t, path, "written while the app was closed")
			backend := &safetyBackend{}
			backend.fetchReplies = replies
			engine := newSafetyEngine(backend, "server", dir, nil)
			mustBootstrap(t, engine, dir)
			assertRecoverable(t, dir, path, "written while the app was closed")

			// The same name retired twice keeps both versions.
			writeLocal(t, path, "second version")
			engine.reconcileLocked(context.Background(), false)
			assertRecoverable(t, dir, path, "second version")
			if len(filesContaining(t, dir, "written while the app was closed")) != 1 {
				t.Fatal("the first recovery copy was overwritten")
			}
		})
	}
}

func TestServerDeleteActivityKeepsARecoverableCopy(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("server sword")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	writeLocal(t, swordPath(dir), "unpushed work")

	engine.handleDeleteActivity(Activity{Kind: "weapon", Key: "Sword", Name: "Sword", Action: "deleted"})

	assertRecoverable(t, dir, swordPath(dir), "unpushed work")
}

func TestRebuildKeepsRecoveryCopies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weapons", "Offline"+scriptExt)
	writeLocal(t, path, "only copy")
	engine := newSafetyEngine(&safetyBackend{}, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	if err := engine.PrepareRebuild(); err != nil {
		t.Fatal(err)
	}
	if len(filesContaining(t, dir, "only copy")) != 1 {
		t.Fatal("rebuild destroyed the recovery copy")
	}
}

func TestBootstrapKeepsAFileCreatedDuringTheFetch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "weapons", "New"+scriptExt)
	backend := &safetyBackend{}
	backend.serve("server sword")
	backend.onFetch = func() { writeLocal(t, path, "created during fetch") }
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	assertFile(t, path, "created during fetch")
}

// B: a real save right after a pull must be uploaded.

func TestSaveRightAfterAPullIsUploaded(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("server sword")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)

	// The watcher echo of the pull itself is not an upload.
	engine.pushLocal(context.Background(), swordPath(dir))
	if len(backend.saved) != 0 {
		t.Fatalf("the pull echoed back to the server: %v", backend.saved)
	}
	writeLocal(t, swordPath(dir), "my edit")
	engine.pushLocal(context.Background(), swordPath(dir))
	if len(backend.saved) != 1 || backend.saved[0] != "weapon:Sword=my edit" {
		t.Fatalf("saved = %v; the save right after the pull was dropped", backend.saved)
	}
}

// C: server pulls must not overwrite local edits that were never pushed.

func assertConflict(t *testing.T, engine *Engine, local, server string) {
	t.Helper()
	items := engine.Status().Items
	if len(items) != 1 || items[0].Local != local || items[0].Server != server {
		t.Fatalf("review items = %+v; want local %q against server %q", items, local, server)
	}
}

func TestUnpushedEditSurvivesRestartAsAConflict(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	mustBootstrap(t, newSafetyEngine(backend, "server", dir, nil), dir)

	writeLocal(t, swordPath(dir), "my unpushed edit")
	backend.serve("colleague v2")
	events := &safetyEvents{}
	restarted := newSafetyEngine(backend, "server", dir, events)
	mustBootstrap(t, restarted, dir)

	assertFile(t, swordPath(dir), "my unpushed edit")
	assertConflict(t, restarted, "my unpushed edit", "colleague v2")
	if len(events.conflicts) != 1 {
		t.Fatalf("conflict events = %d, want 1", len(events.conflicts))
	}
}

func TestUnchangedLocalIsPulledNormallyAfterRestart(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	mustBootstrap(t, newSafetyEngine(backend, "server", dir, nil), dir)

	backend.serve("v2")
	restarted := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, restarted, dir)

	assertFile(t, swordPath(dir), "v2")
	if items := restarted.Status().Items; len(items) != 0 {
		t.Fatalf("review items = %+v", items)
	}
}

func TestBaselinesAreNotSharedBetweenServers(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	mustBootstrap(t, newSafetyEngine(backend, "ServerA", dir, nil), dir)

	// No baseline for ServerB: the server wins, with a backup.
	writeLocal(t, swordPath(dir), "local")
	backend.serve("server B version")
	var backups []string
	other := newSafetyEngine(backend, "ServerB", dir, nil)
	other.SetPullRecorder(func(_, _ string, previous []byte) (string, error) {
		backups = append(backups, string(previous))
		return "id", nil
	}, nil)
	mustBootstrap(t, other, dir)

	assertFile(t, swordPath(dir), "server B version")
	if len(backups) != 1 || backups[0] != "local" {
		t.Fatalf("backups = %v", backups)
	}
}

func TestReconcileDoesNotOverwriteAnUnpushedEdit(t *testing.T) {
	for _, serverNow := range []string{"v1", "colleague v2"} {
		t.Run(serverNow, func(t *testing.T) {
			dir := t.TempDir()
			backend := &safetyBackend{}
			backend.serve("v1")
			engine := newSafetyEngine(backend, "server", dir, nil)
			mustBootstrap(t, engine, dir)
			writeLocal(t, swordPath(dir), "still in debounce")
			backend.serve(serverNow)

			engine.reconcileLocked(context.Background(), false)

			assertFile(t, swordPath(dir), "still in debounce")
			assertConflict(t, engine, "still in debounce", serverNow)
		})
	}
}

func swordActivity(engine *Engine) {
	engine.handleActivity(Activity{Kind: "weapon", Key: "Sword", Name: "Sword", Actor: "colleague", Action: "updated"})
}

func TestServerActivityDoesNotOverwriteAnUnpushedEdit(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	writeLocal(t, swordPath(dir), "unpushed")
	backend.serve("colleague v2")

	swordActivity(engine)

	assertFile(t, swordPath(dir), "unpushed")
	assertConflict(t, engine, "unpushed", "colleague v2")
}

// The chat echo of an earlier upload can arrive while the next save is still in
// debounce. The server has not moved, so the save must still be delivered.
func TestServerActivityWithoutAServerChangeLeavesThePendingSaveAlone(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	writeLocal(t, swordPath(dir), "next save")

	swordActivity(engine)
	assertFile(t, swordPath(dir), "next save")
	engine.pushLocal(context.Background(), swordPath(dir))

	if len(backend.saved) != 1 || len(engine.Status().Items) != 0 {
		t.Fatalf("saved = %v items = %+v", backend.saved, engine.Status().Items)
	}
}

func TestEditDuringFetchConflictsWhenTheServerAlsoChanged(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	backend.serve("colleague v2")
	backend.onFetch = func() { writeLocal(t, swordPath(dir), "edit during fetch") }

	engine.reconcileLocked(context.Background(), false)
	engine.pushLocal(context.Background(), swordPath(dir))

	assertFile(t, swordPath(dir), "edit during fetch")
	assertConflict(t, engine, "edit during fetch", "colleague v2")
	if len(backend.saved) != 0 {
		t.Fatalf("saved = %v; the colleague's version was overwritten", backend.saved)
	}
}

func TestFailedPushIsVisibleInStatusUntilItSucceeds(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	backend.saveErr = errors.New("socket closed")
	writeLocal(t, swordPath(dir), "v2")

	engine.pushLocal(context.Background(), swordPath(dir))
	if got := engine.Status().LastError; !strings.Contains(got, "socket closed") || !strings.Contains(got, "Sword") {
		t.Fatalf("lastError = %q", got)
	}
	backend.saveErr = nil
	engine.pushLocal(context.Background(), swordPath(dir))
	if got := engine.Status().LastError; got != "" {
		t.Fatalf("lastError = %q after the upload succeeded", got)
	}
}

// D: a pending conflict blocks pushes, and resolving it is not stale.

func conflictedEngine(t *testing.T) (*Engine, *safetyBackend, *safetyEvents, string, *[]string) {
	t.Helper()
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	events := &safetyEvents{}
	engine := newSafetyEngine(backend, "server", dir, events)
	mustBootstrap(t, engine, dir)
	backups := &[]string{}
	engine.SetPullRecorder(func(_, _ string, previous []byte) (string, error) {
		*backups = append(*backups, string(previous))
		return "id", nil
	}, nil)
	writeLocal(t, swordPath(dir), "mine")
	backend.serve("theirs")
	swordActivity(engine)
	if len(engine.Status().Items) != 1 {
		t.Fatalf("setup: no conflict: %+v", engine.Status().Items)
	}
	return engine, backend, events, dir, backups
}

func TestSavingAConflictedScriptDoesNotUpload(t *testing.T) {
	engine, backend, events, dir, _ := conflictedEngine(t)
	writeLocal(t, swordPath(dir), "mine, saved again")

	engine.pushLocal(context.Background(), swordPath(dir))

	if len(backend.saved) != 0 || len(backend.added) != 0 {
		t.Fatalf("saved=%v added=%v: the colleague's version was overwritten", backend.saved, backend.added)
	}
	assertConflict(t, engine, "mine, saved again", "theirs")
	if len(events.conflicts) != 2 || events.conflicts[1].Local != "mine, saved again" {
		t.Fatalf("conflict events = %+v; the editor was not told about the new local content", events.conflicts)
	}
	engine.pushLocal(context.Background(), swordPath(dir))
	if len(events.conflicts) != 2 {
		t.Fatalf("unchanged content re-emitted the conflict: %d events", len(events.conflicts))
	}
}

func TestConflictIsReemittedWhenTheServerChangesAgain(t *testing.T) {
	engine, backend, events, dir, _ := conflictedEngine(t)
	backend.serve("theirs again")
	swordActivity(engine)

	assertFile(t, swordPath(dir), "mine")
	assertConflict(t, engine, "mine", "theirs again")
	if len(events.conflicts) != 2 || events.conflicts[1].Server != "theirs again" {
		t.Fatalf("conflict events = %+v", events.conflicts)
	}
}

func TestResolvingLocalUploadsTheCurrentFileAndBacksUpTheServerVersion(t *testing.T) {
	engine, backend, _, dir, backups := conflictedEngine(t)
	// Saved after the review was raised; the watcher has not run yet.
	writeLocal(t, swordPath(dir), "mine, newest")

	if err := engine.ResolveConflict("weapon", "Sword", "local", ""); err != nil {
		t.Fatal(err)
	}

	if len(backend.saved) != 1 || backend.saved[0] != "weapon:Sword=mine, newest" {
		t.Fatalf("saved = %v", backend.saved)
	}
	assertFile(t, swordPath(dir), "mine, newest")
	if len(*backups) != 1 || (*backups)[0] != "theirs" {
		t.Fatalf("backups = %v; the replaced server version was not kept", *backups)
	}
	if len(engine.Status().Items) != 0 {
		t.Fatal("review was not cleared")
	}
}

func TestResolvingServerBacksUpTheReplacedLocalFile(t *testing.T) {
	engine, _, _, dir, backups := conflictedEngine(t)
	writeLocal(t, swordPath(dir), "mine, newest")

	if err := engine.ResolveConflict("weapon", "Sword", "server", ""); err != nil {
		t.Fatal(err)
	}

	assertFile(t, swordPath(dir), "theirs")
	if len(*backups) != 1 || (*backups)[0] != "mine, newest" {
		t.Fatalf("backups = %v", *backups)
	}
}

func TestResolvingAgainstAStaleServerSnapshotIsRefused(t *testing.T) {
	for _, choice := range []string{"local", "server", "merge"} {
		t.Run(choice, func(t *testing.T) {
			engine, backend, events, dir, _ := conflictedEngine(t)
			backend.server["weapon:Sword"] = "theirs, unseen" // no chat line arrived

			if err := engine.ResolveConflict("weapon", "Sword", choice, "merged"); err == nil {
				t.Fatal("a resolution reviewed against an old server version was applied")
			}

			if len(backend.saved) != 0 {
				t.Fatalf("saved = %v", backend.saved)
			}
			assertFile(t, swordPath(dir), "mine")
			assertConflict(t, engine, "mine", "theirs, unseen")
			if got := events.conflicts[len(events.conflicts)-1].Server; got != "theirs, unseen" {
				t.Fatalf("last conflict event server = %q", got)
			}
		})
	}
}

func TestResolveDoesNotOverwriteASaveMadeDuringTheUpload(t *testing.T) {
	engine, backend, _, dir, _ := conflictedEngine(t)
	backend.onSave = func() { writeLocal(t, swordPath(dir), "saved during upload") }

	if err := engine.ResolveConflict("weapon", "Sword", "merge", "merged"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, swordPath(dir), "saved during upload")

	backend.onSave = nil
	engine.pushLocal(context.Background(), swordPath(dir))
	if got := backend.server["weapon:Sword"]; got != "saved during upload" {
		t.Fatalf("server = %q; the newer save was not delivered", got)
	}
}

// E: names that differ only by case share one file on Windows.

func TestCaseCollidingScriptsAreNeverWrittenOrUploaded(t *testing.T) {
	for name, test := range map[string]struct {
		replies []rclib.ScriptReply
		file    string
	}{
		"weapons": {replies: []rclib.ScriptReply{
			{Type: "weapon", Name: "Foo", Script: "upper"},
			{Type: "weapon", Name: "foo", Script: "lower"},
		}, file: "weapons/Foo.gs2"},
		"npcs": {replies: []rclib.ScriptReply{
			{Type: "npc", ID: 1, Name: "Guard", Script: "first"},
			{Type: "npc", ID: 2, Name: "Guard", Script: "second"},
		}, file: "npcs/Guard.gs2"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, filepath.FromSlash(test.file))
			writeLocal(t, path, "local")
			backend := &safetyBackend{}
			backend.fetchReplies = test.replies
			engine := newSafetyEngine(backend, "server", dir, nil)

			mustBootstrap(t, engine, dir)
			assertFile(t, path, "local")
			engine.reconcileLocked(context.Background(), false)
			assertFile(t, path, "local")

			writeLocal(t, path, "local edit")
			engine.pushLocal(context.Background(), path)
			if len(backend.saved) != 0 || len(backend.added) != 0 {
				t.Fatalf("cross upload: saved=%v added=%v", backend.saved, backend.added)
			}
			if got := len(engine.Status().Unreadable); got != 2 {
				t.Fatalf("blocked scripts reported = %d, want 2", got)
			}
		})
	}
}

func TestACaseVariantOfAKnownScriptIsNeitherRemovedNorUploaded(t *testing.T) {
	dir := t.TempDir()
	variant := filepath.Join(dir, "weapons", "SWORD"+scriptExt)
	writeLocal(t, variant, "server sword")
	backend := &safetyBackend{}
	backend.serve("server sword")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	if len(filesContaining(t, filepath.Join(dir, "weapons"), "server sword")) == 0 {
		t.Fatal("the file of a listed script was removed because its name differs by case")
	}

	other := filepath.Join(dir, "weapons", "sword"+scriptExt)
	writeLocal(t, other, "different")
	engine.pushLocal(context.Background(), other)
	if len(backend.saved) != 0 || len(backend.added) != 0 {
		t.Fatalf("saved=%v added=%v", backend.saved, backend.added)
	}
}

func TestAScriptCreatedLaterThatCollidesByCaseIsBlocked(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("server sword")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	backend.server["weapon:sword"] = "the other script"

	engine.handleActivity(Activity{Kind: "weapon", Key: "sword", Name: "sword", Actor: "colleague", Action: "added"})

	assertFile(t, swordPath(dir), "server sword")
	writeLocal(t, swordPath(dir), "edit")
	engine.pushLocal(context.Background(), swordPath(dir))
	if len(backend.saved) != 0 || len(backend.added) != 0 {
		t.Fatalf("saved=%v added=%v", backend.saved, backend.added)
	}
}

// F: nothing is uploaded until one snapshot has succeeded.

func TestNoUploadsUntilASnapshotSucceeds(t *testing.T) {
	dir := t.TempDir()
	writeLocal(t, swordPath(dir), "stale")
	backend := &safetyBackend{}
	backend.fetchErr = errors.New("script snapshot is incomplete")
	engine := newSafetyEngine(backend, "server", dir, nil)
	if err := engine.bootstrap(context.Background(), dir); err == nil {
		t.Fatal("setup: bootstrap should fail")
	}

	writeLocal(t, swordPath(dir), "stale, saved")
	engine.pushLocal(context.Background(), swordPath(dir))
	if len(backend.saved) != 0 || len(backend.added) != 0 {
		t.Fatalf("saved=%v added=%v before any successful snapshot", backend.saved, backend.added)
	}

	backend.fetchErr = nil
	engine.reconcileLocked(context.Background(), false)
	fresh := filepath.Join(dir, "weapons", "Fresh"+scriptExt)
	writeLocal(t, fresh, "new weapon")
	engine.pushLocal(context.Background(), fresh)
	if len(backend.saved) != 1 || backend.saved[0] != "weapon:Fresh=new weapon" {
		t.Fatalf("saved = %v after a successful snapshot", backend.saved)
	}

	// A different output directory needs its own snapshot.
	next := t.TempDir()
	engine.ApplyConfig(SyncConfig{Enabled: true, OutputDir: next, AutoPushLocal: true})
	elsewhere := filepath.Join(next, "weapons", "Fresh"+scriptExt)
	writeLocal(t, elsewhere, "unverified")
	engine.pushLocal(context.Background(), elsewhere)
	if len(backend.saved) != 1 {
		t.Fatalf("saved = %v for an output directory with no snapshot", backend.saved)
	}
}

// A path whose server version could not be written locally is stale.
func TestAScriptThatCouldNotBePulledIsNotUploaded(t *testing.T) {
	dir := t.TempDir()
	writeLocal(t, swordPath(dir), "stale")
	backend := &safetyBackend{}
	backend.serve("newer on server")
	engine := newSafetyEngine(backend, "server", dir, nil)
	engine.SetPullRecorder(func(_, _ string, _ []byte) (string, error) { return "", os.ErrPermission }, nil)
	if err := engine.bootstrap(context.Background(), dir); err == nil {
		t.Fatal("setup: the pull should fail")
	}
	writeLocal(t, swordPath(dir), "stale, saved")
	engine.pushLocal(context.Background(), swordPath(dir))
	if len(backend.saved) != 0 || len(backend.added) != 0 {
		t.Fatalf("saved=%v added=%v", backend.saved, backend.added)
	}
}

// The same through the review raised when an editor window is open.
func TestAnEditorReviewBlocksPushesAndStaysCurrent(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	events := &safetyEvents{}
	engine := newSafetyEngine(backend, "server", dir, events)
	mustBootstrap(t, engine, dir)
	engine.SetEditorChecker(func(string, string) bool { return true })
	backend.serve("theirs")
	swordActivity(engine)
	backend.serve("theirs again")
	swordActivity(engine)
	if len(events.conflicts) != 2 || events.conflicts[1].Server != "theirs again" {
		t.Errorf("conflict events = %+v; the editor was not told about the new server version", events.conflicts)
	}

	// Well after the bootstrap pull.
	engine.now = func() time.Time { return time.Now().Add(time.Minute) }
	writeLocal(t, swordPath(dir), "mine")
	engine.pushLocal(context.Background(), swordPath(dir))

	if len(backend.saved) != 0 {
		t.Fatalf("saved = %v; saving a conflicted file overwrote the server", backend.saved)
	}
	assertConflict(t, engine, "mine", "theirs again")
}

func TestASuccessfulPushDoesNotHideASnapshotError(t *testing.T) {
	dir := t.TempDir()
	backend := &safetyBackend{}
	backend.serve("v1")
	engine := newSafetyEngine(backend, "server", dir, nil)
	mustBootstrap(t, engine, dir)
	backend.saveErr = errors.New("socket closed")
	writeLocal(t, swordPath(dir), "v2")
	engine.pushLocal(context.Background(), swordPath(dir))
	backend.fetchErr = errors.New("script snapshot is incomplete")
	engine.reconcileLocked(context.Background(), false)
	backend.saveErr = nil
	engine.pushLocal(context.Background(), swordPath(dir))
	if got := engine.Status().LastError; got != "script snapshot is incomplete" {
		t.Fatalf("lastError = %q; the snapshot failure was hidden", got)
	}
}
