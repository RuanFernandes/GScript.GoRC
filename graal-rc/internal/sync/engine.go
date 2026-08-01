package sync

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ReviewItem is a script awaiting human resolution. Local/Server hold the
// cached content for the diff viewer ("" = absent on that side).
type ReviewItem struct {
	Kind   string `json:"kind"`
	Key    string `json:"key"`
	Name   string `json:"name"`
	State  State  `json:"state"`
	Local  string `json:"local"`
	Server string `json:"server"`
	Actor  string `json:"actor,omitempty"`
}

// SyncStatus is the snapshot emitted to the frontend.
type SyncStatus struct {
	Enabled          bool         `json:"enabled"`
	Paused           bool         `json:"paused"`
	NCDown           bool         `json:"ncDown"`
	OutputDirMissing bool         `json:"outputDirMissing"`
	Server           string       `json:"server"`
	OutputDir        string       `json:"outputDir"`
	LastSyncAt       int64        `json:"lastSyncAt"`
	ReviewCount      int          `json:"reviewCount"`
	Items            []ReviewItem `json:"items"`
}

// ScriptPair is the local+server content for the diff viewer.
type ScriptPair struct {
	Local  string `json:"local"`
	Server string `json:"server"`
}

const (
	watcherDebounce     = 500 * time.Millisecond
	listChangedDebounce = 2 * time.Second
	ncWaitTimeout       = 30 * time.Second
	ncPollInterval      = 500 * time.Millisecond
	rejectedSubdir      = ".rejected"
)

// Engine owns the reconcile loop, fsnotify watcher, chat channel, and poller.
type Engine struct {
	mu         sync.Mutex
	cfg        SyncConfig
	backend    ScriptBackend
	server     string
	manifest   Manifest
	lastSyncAt int64

	review      map[string]ReviewItem // key = Kind+":"+Key
	reconciling bool                  // true while a full ReconcileAll is in flight

	// reconcileMu serializes all manifest-mutating operations (ReconcileAll,
	// ReconcileOne, reconcileLocalFile, ResolveConflict). Without it, a
	// watcher/chat/resolve goroutine mutates the same manifest map that an
	// in-flight ReconcileAll is iterating → concurrent-map-write panic.
	reconcileMu sync.Mutex

	watcher       *fsnotify.Watcher
	chatCh        chan string
	listChangedCh chan struct{} // debounced *Changed push trigger
	pollTick      *time.Ticker
	reload        chan struct{} // signaled when poller/watcher are recreated
	stop          chan struct{}
	stopped       chan struct{}
	running       bool

	emit func(name string, data ...any)
	now  func() time.Time
}

// NewEngine constructs an engine. emit is used to push status/conflict events
// to the frontend (raw app.Event.Emit, like rc:codingSettings).
func NewEngine(backend ScriptBackend, server string, emit func(name string, data ...any)) *Engine {
	return &Engine{
		backend: backend,
		server:  server,
		emit:    emit,
		now:     time.Now,
		review:  map[string]ReviewItem{},
		chatCh:  make(chan string, 256),
	}
}

// ApplyConfig updates config live. Restarts the poller/watcher if running and
// signals the loop to re-snapshot their channels.
func (e *Engine) ApplyConfig(cfg SyncConfig) {
	e.mu.Lock()
	e.cfg = cfg
	e.mu.Unlock()
	if e.isRunning() {
		e.restartPoller()
		e.restartWatcher()
		e.signalReload()
	}
	e.emitStatus()
}

func (e *Engine) signalReload() {
	e.mu.Lock()
	ch := e.reload
	e.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

// SetPaused updates the pause deadline WITHOUT restarting the watcher/poller
// (unlike ApplyConfig). Used by Pause/Resume.
func (e *Engine) SetPaused(until int64) {
	e.mu.Lock()
	e.cfg.PauseUntil = until
	e.mu.Unlock()
	e.emitStatus()
}

// Start launches the engine goroutines (watcher + poll loop). No-op if disabled
// or OutputDir empty (emits outputDirMissing status). Idempotent.
func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	cfg := e.cfg
	if !cfg.Enabled || cfg.OutputDir == "" {
		e.mu.Unlock()
		e.emitStatus()
		return
	}
	e.running = true
	e.stop = make(chan struct{})
	e.stopped = make(chan struct{})
	e.reload = make(chan struct{}, 1)
	e.listChangedCh = make(chan struct{}, 1)
	e.manifest = loadManifest(cfg.OutputDir)
	// Server mismatch => first-run for the new server (existing local files
	// become initial-conflict, never clobbered).
	if e.manifest.Server != "" && e.manifest.Server != e.server {
		e.manifest = Manifest{Version: 1, Server: "", Entries: map[string]Entry{}}
	}
	e.mu.Unlock()

	e.startWatcher()
	e.restartPoller()
	go e.loop(ctx)
	// Kick off an initial reconcile shortly after start (NC may still be
	// coming up; ReconcileAll self-gates on IsNCConnected).
	go func() {
		e.waitForNC(ctx)
		e.ReconcileAll(ctx)
	}()
}

// Stop tears down goroutines + watcher. Idempotent.
func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	close(e.stop)
	stopped := e.stopped
	if e.pollTick != nil {
		e.pollTick.Stop()
	}
	w := e.watcher
	e.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	if stopped != nil {
		<-stopped
	}
}

func (e *Engine) isRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

// HandleChatLine feeds an rc:message line to the engine (non-blocking).
func (e *Engine) HandleChatLine(text string) {
	select {
	case e.chatCh <- text:
	default:
	}
}

func (e *Engine) waitForNC(ctx context.Context) {
	deadline := e.now().Add(ncWaitTimeout)
	for e.now().Before(deadline) {
		if e.backend.IsNCConnected() {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(ncPollInterval):
		case <-e.stopCh():
			return
		}
	}
}

func (e *Engine) stopCh() chan struct{} {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stop
}

// loop is the main select loop: poll tick, chat lines, watcher events.
func (e *Engine) loop(ctx context.Context) {
	defer close(e.stopped)
	ctxLoop, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-e.stop:
		case <-ctx.Done():
		}
		cancel()
	}()

	var (
		events chan fsnotify.Event
		errs   chan error
		tick   *time.Ticker
	)
	snapshot := func() {
		e.mu.Lock()
		if e.watcher != nil {
			events = e.watcher.Events
			errs = e.watcher.Errors
		} else {
			events = nil
			errs = nil
		}
		tick = e.pollTick
		e.mu.Unlock()
	}
	snapshot()
	reload := func() chan struct{} {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.reload
	}

	// debounce map: path -> timer
	debounce := map[string]*time.Timer{}
	var listTimer *time.Timer
	listCh := func() chan struct{} {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.listChangedCh
	}

	for {
		tickCh := tickChOf(tick)
		select {
		case <-ctxLoop.Done():
			return
		case <-tickCh:
			go e.ReconcileAll(ctxLoop) // goroutine: reconcileMu serializes; never block the loop
		case line := <-e.chatCh:
			go e.handleChat(ctxLoop, line)
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			e.onWatcherEvent(ctxLoop, ev, debounce)
		case _, ok := <-errs:
			if !ok {
				errs = nil
			}
		case <-reload():
			// Poller/watcher were recreated (config change); re-snapshot.
			snapshot()
		case <-listCh():
			// Debounce a burst of *Changed push events into one reconcile.
			if listTimer != nil {
				listTimer.Stop()
			}
			listTimer = time.AfterFunc(listChangedDebounce, func() {
				e.ReconcileAll(ctxLoop)
			})
		}
	}
}

// tickChOf returns the ticker channel or nil so a nil tick blocks forever.
func tickChOf(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func (e *Engine) restartPoller() {
	e.mu.Lock()
	if e.pollTick != nil {
		e.pollTick.Stop()
	}
	mins := e.cfg.PollingMinutes
	if mins < 1 {
		mins = 1
	}
	e.pollTick = time.NewTicker(time.Duration(mins) * time.Minute)
	e.mu.Unlock()
}

func (e *Engine) startWatcher() {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("sync: fsnotify watcher create failed: %v", err)
		return
	}
	e.mu.Lock()
	e.watcher = w
	cfg := e.cfg
	e.mu.Unlock()
	for _, kind := range []string{"weapon", "class", "npc"} {
		dir := filepath.Join(cfg.OutputDir, kindSubdir(kind))
		_ = os.MkdirAll(dir, 0o755)
		if err := w.Add(dir); err != nil {
			log.Printf("sync: watch %s failed: %v", dir, err)
		}
	}
}

func (e *Engine) restartWatcher() {
	e.mu.Lock()
	old := e.watcher
	e.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	e.startWatcher()
}

// onWatcherEvent debounces and triggers a targeted reconcile of the changed
// file. Engine's own atomic writes use a ".tmp" suffix which is filtered out.
func (e *Engine) onWatcherEvent(ctx context.Context, ev fsnotify.Event, debounce map[string]*time.Timer) {
	if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Remove|fsnotify.Rename) == 0 {
		return
	}
	name := filepath.Base(ev.Name)
	if strings.HasSuffix(name, ".tmp") || name == ".sync-manifest.json" || !strings.HasSuffix(name, scriptExt) {
		return
	}
	kind := kindFromDir(filepath.Dir(ev.Name))
	if kind == "" {
		return
	}
	if t, ok := debounce[ev.Name]; ok {
		t.Stop()
	}
	debounce[ev.Name] = time.AfterFunc(watcherDebounce, func() {
		fileName := strings.TrimSuffix(name, scriptExt)
		e.reconcileLocalFile(ctx, kind, fileName)
	})
}

func kindFromDir(dir string) string {
	base := filepath.Base(dir)
	switch base {
	case "weapons":
		return "weapon"
	case "classes":
		return "class"
	case "npcs":
		return "npc"
	}
	return ""
}

// handleChat parses a chat line and triggers a targeted reconcile for the
// named script. Deletes are notify-only (no mutation); the next full poll
// catches the actual server-missing state.
func (e *Engine) handleChat(ctx context.Context, line string) {
	act, ok := ParseChatLine(line)
	if !ok {
		return
	}
	if act.Kind == "delete" {
		e.emit("rc:syncActivity", map[string]any{
			"action": "deleted", "name": act.Name, "actor": act.Actor,
			"message": line,
		})
		return
	}
	// Resolve the key (npc name -> id via the cached list).
	key := act.Key
	if act.Kind == "npc" {
		id, ok := e.npcIDByName(act.Name)
		if !ok {
			return // not in our list yet; next poll catches it
		}
		key = strconv.Itoa(id)
	}
	e.ReconcileOne(ctx, act.Kind, key)
}

// npcIDByName resolves an NPC name to its id via the server list.
func (e *Engine) npcIDByName(name string) (int, bool) {
	npcs, err := e.backend.GetNPCs()
	if err != nil {
		return 0, false
	}
	for _, n := range npcs {
		if n.Name == name {
			return n.ID, true
		}
	}
	return 0, false
}

// paused reports whether the user paused sync.
func (e *Engine) paused() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.PauseUntil != 0 && e.now().Unix() < e.cfg.PauseUntil
}

// ReconcileAll fetches every server script and reconciles the full union
// (server scripts ∪ local files ∪ manifest entries). It refreshes the weapon
// list (cheap packet 115) first; class/npc caches update live via the
// *Changed push events wired into HandleListChanged. NC is NEVER reconnected
// here — on old servers a disconnect/reconnect drops the session, so a full
// re-gather of class/npc lists is intentionally not attempted. Re-entrant
// calls are coalesced (a reconcile already in flight absorbs new triggers).
func (e *Engine) ReconcileAll(ctx context.Context) {
	e.mu.Lock()
	cfg := e.cfg
	e.mu.Unlock()

	// Cheap gates first (no lock held).
	if !cfg.Enabled || cfg.OutputDir == "" {
		e.emitStatus()
		return
	}
	if e.paused() {
		e.emitStatus()
		return
	}
	if !e.backend.IsNCConnected() {
		e.emitStatusNCDown()
		return
	}
	// Claim the full-reconcile slot (coalesces overlapping triggers).
	if !e.tryBeginFull() {
		return
	}
	// Serialize against watcher/chat/resolve goroutines.
	e.reconcileMu.Lock()
	defer func() {
		e.reconcileMu.Unlock()
		e.endFull()
	}()

	_ = e.backend.RefreshWeapons()
	// Classes/NPCs have no listget packet — their caches are server-auto-pushed
	// (SADDCLASS / SNPCLISTPROPS at NC auth + live pushes), so FetchAllScripts
	// just reads GetClasses/GetNPCs from cache. No refresh call needed.
	replies, err := e.backend.FetchAllScripts(ctx, func(done, total int) {
		// Throttle: a big server emits hundreds of callbacks; only emit every
		// 8th + the final one so the frontend isn't flooded with renders.
		if total > 0 && (done%8 == 0 || done == total) {
			e.emit("rc:syncProgress", map[string]any{"done": done, "total": total})
		}
	})
	if err != nil {
		log.Printf("sync: FetchAllScripts error: %v", err)
	}
	if ctx.Err() != nil {
		return
	}

	// Build identities keyed by disk identity (kind:fileName).
	type ident struct {
		kind, key, name, fileName string
		base                      string
		server, serverHash        string
		serverOK                  bool
		local, localHash          string
		localOK                   bool
	}
	ids := map[string]*ident{}

	e.mu.Lock()
	manifest := e.manifest
	e.mu.Unlock()

	// From manifest.
	for _, en := range manifest.Entries {
		dk := en.Kind + ":" + en.FileName
		ids[dk] = &ident{kind: en.Kind, key: en.Key, name: en.Name, fileName: en.FileName, base: en.BaseHash}
	}
	// From server.
	for _, r := range replies {
		kind := r.Type
		if kind == "npcflags" || kind == "npcattr" {
			continue
		}
		key, name := r.Name, r.Name
		if kind == "npc" {
			key = strconv.Itoa(r.ID)
		}
		fn := fileNameFor(kind, key, name)
		dk := kind + ":" + fn
		it, ok := ids[dk]
		if !ok {
			it = &ident{kind: kind, key: key, name: name, fileName: fn}
			ids[dk] = it
		} else {
			if it.name == "" {
				it.name = name
			}
			if it.key == "" {
				it.key = key
			}
		}
		it.server = normalizeEOL(r.Script)
		it.serverHash = HashScript(it.server)
		it.serverOK = true
	}
	// From local files.
	for _, kind := range []string{"weapon", "class", "npc"} {
		dir := filepath.Join(cfg.OutputDir, kindSubdir(kind))
		entries, _ := os.ReadDir(dir)
		for _, fe := range entries {
			if fe.IsDir() || !strings.HasSuffix(fe.Name(), scriptExt) {
				continue
			}
			fn := strings.TrimSuffix(fe.Name(), scriptExt)
			dk := kind + ":" + fn
			it, ok := ids[dk]
			if !ok {
				it = &ident{kind: kind, key: fn, name: fn, fileName: fn}
				ids[dk] = it
			}
			content, ok := readScriptFile(filepath.Join(dir, fe.Name()))
			if ok {
				it.local = content
				it.localHash = HashScript(content)
				it.localOK = true
			}
		}
	}

	// Classify + act.
	changed := false
	for _, it := range ids {
		state := classify(it.localHash, it.serverHash, it.base)
		mutated := e.apply(ctx, it.kind, it.key, it.name, it.fileName, it.local, it.server, state)
		if mutated {
			changed = true
		}
	}

	if changed {
		e.mu.Lock()
		e.manifest.Server = e.server
		m := e.manifest
		e.mu.Unlock()
		if err := saveManifest(cfg.OutputDir, m); err != nil {
			log.Printf("sync: save manifest: %v", err)
		}
	}
	e.markSynced()
	e.emitStatus()
}

func (e *Engine) setReconciling(v bool) {
	e.mu.Lock()
	e.reconciling = v
	e.mu.Unlock()
}

// tryBeginFull atomically claims the "full reconcile" slot. Returns false if one
// is already in flight (the running one covers this trigger).
func (e *Engine) tryBeginFull() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.reconciling {
		return false
	}
	e.reconciling = true
	return true
}

func (e *Engine) endFull() { e.setReconciling(false) }

func (e *Engine) isReconciling() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reconciling
}

// HandleListChanged is the entry point for grclib *Changed push events
// (rc:weaponsChanged/classesChanged/npcsChanged) — the structured signal that a
// script was added/deleted server-side. It is debounced: a burst of pushes
// coalesces into one reconcile after listChangedDebounce of quiet.
func (e *Engine) HandleListChanged(kind string) {
	e.mu.Lock()
	ch := e.listChangedCh
	e.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (e *Engine) markSynced() {
	e.mu.Lock()
	e.lastSyncAt = e.now().Unix()
	e.mu.Unlock()
}

// reconcileLocalFile handles a single watcher event: find the manifest entry by
// fileName, fetch server content, classify, act.
func (e *Engine) reconcileLocalFile(ctx context.Context, kind, fileName string) {
	e.mu.Lock()
	cfg := e.cfg
	manifest := e.manifest
	e.mu.Unlock()
	if !cfg.Enabled || cfg.OutputDir == "" || e.paused() || !e.backend.IsNCConnected() {
		return
	}
	// A full reconcile in flight covers this; and we must serialize manifest
	// access either way.
	if e.isReconciling() {
		return
	}
	e.reconcileMu.Lock()
	defer e.reconcileMu.Unlock()

	// Find identity by fileName.
	var en Entry
	for _, m := range manifest.Entries {
		if m.Kind == kind && m.FileName == fileName {
			en = m
			break
		}
	}
	key, name := en.Key, en.Name
	if key == "" {
		key = fileName
	}
	if name == "" {
		name = fileName
	}

	local, localOK := readScriptFile(fullPath(cfg.OutputDir, kind, fileName))
	var localHash string
	if localOK {
		localHash = HashScript(local)
	}

	var server, serverHash string
	serverOK := false
	// Fetch server content (best effort).
	if kind == "npc" {
		if id, err := strconv.Atoi(key); err == nil {
			if r, err := e.backend.OpenScript("npc", strconv.Itoa(id)); err == nil {
				server = normalizeEOL(r.Script)
				serverHash = HashScript(server)
				serverOK = true
			}
		}
	} else {
		if r, err := e.backend.OpenScript(kind, key); err == nil {
			server = normalizeEOL(r.Script)
			serverHash = HashScript(server)
			serverOK = true
		}
	}
	_ = serverOK

	state := classify(localHash, serverHash, en.BaseHash)
	if e.apply(ctx, kind, key, name, fileName, local, server, state) {
		e.mu.Lock()
		e.manifest.Server = e.server
		m := e.manifest
		e.mu.Unlock()
		_ = saveManifest(cfg.OutputDir, m)
	}
	e.markSynced()
	e.emitStatus()
}

// ReconcileOne reconciles a single script by kind+key (chat-driven).
func (e *Engine) ReconcileOne(ctx context.Context, kind, key string) {
	e.mu.Lock()
	cfg := e.cfg
	manifest := e.manifest
	e.mu.Unlock()
	if !cfg.Enabled || cfg.OutputDir == "" || e.paused() || !e.backend.IsNCConnected() {
		return
	}
	if e.isReconciling() {
		return // full reconcile in flight covers this script
	}
	e.reconcileMu.Lock()
	defer e.reconcileMu.Unlock()
	en, ok := manifest.Entries[entryKey(kind, key)]
	name := key
	if ok && en.Name != "" {
		name = en.Name
	}
	fileName := en.FileName
	if fileName == "" {
		fileName = fileNameFor(kind, key, name)
	}

	local, localOK := readScriptFile(fullPath(cfg.OutputDir, kind, fileName))
	var localHash string
	if localOK {
		localHash = HashScript(local)
	}
	var server, serverHash string
	if r, err := e.backend.OpenScript(kind, key); err == nil {
		server = normalizeEOL(r.Script)
		serverHash = HashScript(server)
	}
	if e.apply(ctx, kind, key, name, fileName, local, server, classify(localHash, serverHash, en.BaseHash)) {
		e.mu.Lock()
		e.manifest.Server = e.server
		m := e.manifest
		e.mu.Unlock()
		_ = saveManifest(cfg.OutputDir, m)
	}
	e.markSynced()
	e.emitStatus()
}

// apply executes the action for a state. Returns true if the manifest changed.
//
// SERVER IS ABSOLUTE TRUTH: the local folder mirrors the server. Any content
// difference where the server has a version → pull server to local (overwriting
// the local file), including conflicts and first-run diffs. Local edits are
// never pushed. The only things that surface for review are local files with NO
// server counterpart (new-local) and local files whose server entry was deleted
// — those can't be "pulled" and we never delete the local file, so the user
// decides to quarantine them.
func (e *Engine) apply(ctx context.Context, kind, key, name, fileName, local, server string, state State) bool {
	switch state {
	case StateInSync:
		return false
	case StateConvergent:
		e.setBase(kind, key, name, fileName, HashScript(local))
		return true
	case StateLocalChanged, StateServerChanged, StateConflict, StateInitialConflict,
		StateServerOnlyNew, StateLocalMissingKeep:
		// Server wins: overwrite/create the local file from server content.
		e.pullServer(kind, key, name, fileName, server)
		return true
	case StateNewLocal, StateServerMissingKeep:
		// No server content to pull; never delete the local file → review.
		e.enqueueReview(kind, key, name, fileName, state, local, server, "")
		return false
	case StateBothDeleted:
		e.dropEntry(kind, key)
		return true
	}
	return false
}

// pullServer writes the server's content to the local file (atomic) and sets the
// baseline. No-op if server content is empty (nothing to mirror).
func (e *Engine) pullServer(kind, key, name, fileName, server string) {
	if server == "" {
		return
	}
	if err := writeFileAtomic(fullPath(e.outputDir(), kind, fileName), []byte(server)); err != nil {
		log.Printf("sync: write local %s:%s failed: %v", kind, key, err)
		return
	}
	e.setBase(kind, key, name, fileName, HashScript(server))
}

func (e *Engine) outputDir() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg.OutputDir
}

func (e *Engine) setBase(kind, key, name, fileName, hash string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.manifest.Entries == nil {
		e.manifest.Entries = map[string]Entry{}
	}
	e.manifest.Entries[entryKey(kind, key)] = Entry{
		Kind: kind, Key: key, Name: name, FileName: fileName,
		BaseHash: hash, BaseAt: e.now().Unix(),
	}
	// Resolved => clear any stale review item.
	delete(e.review, entryKey(kind, key))
}

func (e *Engine) dropEntry(kind, key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.manifest.Entries, entryKey(kind, key))
	delete(e.review, entryKey(kind, key))
}

func (e *Engine) enqueueReview(kind, key, name, fileName string, state State, local, server, actor string) {
	e.mu.Lock()
	if e.review == nil {
		e.review = map[string]ReviewItem{}
	}
	k := entryKey(kind, key)
	_, existed := e.review[k]
	item := ReviewItem{
		Kind: kind, Key: key, Name: name, State: state,
		Local: local, Server: server, Actor: actor,
	}
	e.review[k] = item
	e.mu.Unlock()
	// Only toast the FIRST time a conflict appears — otherwise every reconcile
	// that re-encounters it would re-fire the toast.
	if !existed {
		it := item
		go e.emit("rc:syncConflict", it)
	}
}

// Status returns the current snapshot for the frontend.
func (e *Engine) Status() SyncStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	cfg := e.cfg
	items := make([]ReviewItem, 0, len(e.review))
	for _, it := range e.review {
		items = append(items, it)
	}
	ncDown := cfg.Enabled && cfg.OutputDir != "" && !e.backend.IsNCConnected()
	return SyncStatus{
		Enabled:          cfg.Enabled,
		Paused:           cfg.PauseUntil != 0 && e.now().Unix() < cfg.PauseUntil,
		NCDown:           ncDown,
		OutputDirMissing: cfg.OutputDir == "",
		Server:           e.server,
		OutputDir:        cfg.OutputDir,
		LastSyncAt:       e.lastSyncAt,
		ReviewCount:      len(items),
		Items:            items,
	}
}

// GetScriptPair returns local+server content for a review item (re-fetches
// fresh server content for accuracy).
func (e *Engine) GetScriptPair(kind, key string) (ScriptPair, error) {
	e.mu.Lock()
	cfg := e.cfg
	manifest := e.manifest
	it, hasReview := e.review[entryKey(kind, key)]
	e.mu.Unlock()

	pair := ScriptPair{}
	if hasReview {
		pair.Local = it.Local
		pair.Server = it.Server
	}
	// Refresh local from disk.
	en := manifest.Entries[entryKey(kind, key)]
	fileName := en.FileName
	if fileName == "" {
		fileName = fileNameFor(kind, key, en.Name)
	}
	if fileName == "" {
		fileName = fileNameFor(kind, key, key)
	}
	if content, ok := readScriptFile(fullPath(cfg.OutputDir, kind, fileName)); ok {
		pair.Local = content
	}
	// Refresh server.
	if r, err := e.backend.OpenScript(kind, key); err == nil {
		pair.Server = normalizeEOL(r.Script)
	}
	return pair, nil
}

// ResolveConflict handles the only review states left under server-truth:
// new-local and server-missing-keep (local files with no server counterpart to
// pull). The local file is NEVER deleted — it is quarantined to .rejected so it
// stops re-triggering review, and dropped from the manifest.
func (e *Engine) ResolveConflict(kind, key, choice string) error {
	e.mu.Lock()
	cfg := e.cfg
	manifest := e.manifest
	it, ok := e.review[entryKey(kind, key)]
	e.mu.Unlock()
	if !ok {
		return fmt.Errorf("no review item for %s:%s", kind, key)
	}
	if !e.reconcileMu.TryLock() {
		return fmt.Errorf("sync busy — retry in a moment")
	}
	defer e.reconcileMu.Unlock()
	en := manifest.Entries[entryKey(kind, key)]
	fileName := en.FileName
	if fileName == "" {
		fileName = fileNameFor(kind, key, it.Name)
	}

	switch it.State {
	case StateNewLocal, StateServerMissingKeep:
		// Quarantine (never delete) + stop tracking.
		e.moveToRejected(cfg.OutputDir, kind, fileName)
		e.dropEntry(kind, key)
	default:
		// Any other state shouldn't be in review under server-truth; clear it.
		e.dropEntry(kind, key)
	}

	e.mu.Lock()
	if err := saveManifest(cfg.OutputDir, e.manifest); err != nil {
		log.Printf("sync: save manifest: %v", err)
	}
	e.mu.Unlock()
	e.emitStatus()
	return nil
}

func (e *Engine) moveToRejected(outputDir, kind, fileName string) {
	if outputDir == "" {
		return
	}
	dir := filepath.Join(outputDir, rejectedSubdir, kindSubdir(kind))
	_ = os.MkdirAll(dir, 0o755)
	src := fullPath(outputDir, kind, fileName)
	dst := filepath.Join(dir, fileName+scriptExt)
	_ = os.Rename(src, dst)
}

func (e *Engine) emitStatus() {
	if e.emit == nil {
		return
	}
	e.emit("rc:syncStatus", e.Status())
}

func (e *Engine) emitStatusNCDown() {
	if e.emit == nil {
		return
	}
	s := e.Status()
	s.NCDown = true
	e.emit("rc:syncStatus", s)
}
