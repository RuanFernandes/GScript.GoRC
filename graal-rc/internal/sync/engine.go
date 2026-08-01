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
	"graal-rc/rclib"
)

type ReviewItem struct {
	Kind   string `json:"kind"`
	Key    string `json:"key"`
	Name   string `json:"name"`
	State  State  `json:"state"`
	Local  string `json:"local"`
	Server string `json:"server"`
	Actor  string `json:"actor,omitempty"`
}

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
	Progress         SyncProgress `json:"progress"`
	NextSyncAt       int64        `json:"nextSyncAt"`
}

type SyncProgress struct {
	Active    bool   `json:"active"`
	Phase     string `json:"phase"`
	Current   string `json:"current"`
	Completed int    `json:"completed"`
	Total     int    `json:"total"`
}

type ScriptPair struct {
	Local  string `json:"local"`
	Server string `json:"server"`
}

const (
	watcherDebounce   = 500 * time.Millisecond
	recentDownloadTTL = 3 * time.Second
	ncWaitTimeout     = 30 * time.Second
)

type scriptRef struct{ kind, key, name, path string }
type expectedServerUpdate struct {
	expires time.Time
	hash    string
}

// Engine implements the intentionally small two-way sync protocol. hashes is
// the only baseline: a path is changed only when its current MD5 differs from
// the last successful server/local operation.
type Engine struct {
	mu              sync.RWMutex
	cfg             SyncConfig
	backend         ScriptBackend
	server          string
	hashes          map[string]string
	refs            map[string]scriptRef
	recentDownloads map[string]time.Time
	review          map[string]ReviewItem
	lastSyncAt      int64
	progress        SyncProgress
	nextSyncAt      int64
	running         bool
	stop            chan struct{}
	stopped         chan struct{}
	watcher         *fsnotify.Watcher
	emit            func(string, ...any)
	now             func() time.Time
	isEditing       func(string, string) bool
	localActor      func() string
	expectedUpdates map[string]expectedServerUpdate
	workMu          sync.Mutex
}

func NewEngine(backend ScriptBackend, server string, emit func(string, ...any)) *Engine {
	return &Engine{backend: backend, server: server, emit: emit, now: time.Now,
		hashes: map[string]string{}, refs: map[string]scriptRef{},
		recentDownloads: map[string]time.Time{}, review: map[string]ReviewItem{}, expectedUpdates: map[string]expectedServerUpdate{}}
}

func (e *Engine) SetEditorChecker(fn func(kind, key string) bool) {
	e.mu.Lock()
	e.isEditing = fn
	e.mu.Unlock()
}

func (e *Engine) SetLocalActor(fn func() string) {
	e.mu.Lock()
	e.localActor = fn
	e.mu.Unlock()
}

func (e *Engine) ExpectServerUpdate(kind, key, content string) {
	e.mu.Lock()
	e.expectedUpdates[entryKey(kind, key)] = expectedServerUpdate{expires: e.now().Add(15 * time.Second), hash: HashScript(content)}
	e.mu.Unlock()
}

func (e *Engine) CancelExpectedServerUpdate(kind, key string) {
	e.mu.Lock()
	delete(e.expectedUpdates, entryKey(kind, key))
	e.mu.Unlock()
}

func (e *Engine) ApplyConfig(cfg SyncConfig) {
	e.mu.Lock()
	e.cfg = cfg
	running := e.running
	e.mu.Unlock()
	if running {
		e.Stop()
		e.Start(context.Background())
	}
	e.emitStatus()
}

func (e *Engine) SetPaused(until int64) {
	e.mu.Lock()
	e.cfg.PauseUntil = until
	e.mu.Unlock()
	e.emitStatus()
}

func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running || !e.cfg.Enabled || e.cfg.OutputDir == "" {
		e.mu.Unlock()
		e.emitStatus()
		return
	}
	e.running = true
	e.stop = make(chan struct{})
	e.stopped = make(chan struct{})
	stop, stopped, cfg := e.stop, e.stopped, e.cfg
	e.nextSyncAt = 0
	e.mu.Unlock()
	// NC connects asynchronously after the main server login. The first
	// bootstrap therefore cannot assume the NC socket is ready yet; retrying
	// here makes a configured sync start immediately when NC becomes available
	// instead of waiting for the polling interval.
	initialErr := e.bootstrap(ctx, cfg.OutputDir)
	if initialErr != nil {
		log.Printf("sync bootstrap: %v", initialErr)
		go e.retryBootstrap(ctx, cfg.OutputDir, stop)
	} else {
		e.setNextSyncAt(e.now().Add(pollDuration(cfg)))
	}
	if err := e.startWatcher(cfg.OutputDir); err != nil {
		log.Printf("sync watcher: %v", err)
	}
	go e.loop(ctx, stop, stopped)
}

func (e *Engine) retryBootstrap(ctx context.Context, dir string, stop <-chan struct{}) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			if !e.backend.IsNCConnected() {
				continue
			}
			if err := e.bootstrap(ctx, dir); err != nil {
				log.Printf("sync bootstrap retry: %v", err)
				continue
			}
			e.setNextSyncAt(e.now().Add(pollDuration(e.config())))
			return
		}
	}
}

func (e *Engine) config() SyncConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

func (e *Engine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	close(e.stop)
	w := e.watcher
	stopped := e.stopped
	e.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	if stopped != nil {
		<-stopped
	}
}

func (e *Engine) bootstrap(ctx context.Context, dir string) error {
	if !e.backend.IsNCConnected() {
		return fmt.Errorf("NC is not connected")
	}
	e.setProgress("Downloading", 0, 0, "")
	// The NC socket can report connected before its cached script lists have
	// arrived. Refreshing the weapon list here gives the first bootstrap a
	// chance to populate those caches instead of treating 0/0 as success.
	_ = e.backend.RefreshWeapons()
	replies, err := e.backend.FetchAllScripts(ctx, func(done, total int) {
		e.setProgress("Downloading", done, total, "")
	})
	if err != nil && len(replies) == 0 {
		return err
	}
	if len(replies) == 0 {
		return fmt.Errorf("script lists are not ready")
	}
	for i, r := range replies {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		ref := refFromReply(r)
		if ref.kind == "" {
			continue
		}
		content := normalizeEOL(r.Script)
		path := fullPath(dir, ref.kind, fileNameFor(ref.kind, ref.key, ref.name))
		e.setProgress("Writing", i+1, len(replies), ref.name)
		if err := writeFileAtomic(path, []byte(content)); err != nil {
			return err
		}
		e.remember(ref, path, HashScript(content))
		e.markDownload(path)
	}
	e.markSynced()
	e.finishProgress(len(replies))
	e.emitStatus()
	return err
}

func refFromReply(r rclib.ScriptReply) scriptRef {
	if r.Type != "weapon" && r.Type != "class" && r.Type != "npc" {
		return scriptRef{}
	}
	key, name := r.Name, r.Name
	if r.Type == "npc" {
		key = strconv.Itoa(r.ID)
	}
	return scriptRef{kind: r.Type, key: key, name: name}
}

func (e *Engine) startWatcher(dir string) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	for _, kind := range []string{"weapon", "class", "npc"} {
		sub := filepath.Join(dir, kindSubdir(kind))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			_ = w.Close()
			return err
		}
		if err := w.Add(sub); err != nil {
			_ = w.Close()
			return err
		}
	}
	e.mu.Lock()
	e.watcher = w
	e.mu.Unlock()
	return nil
}

func (e *Engine) loop(ctx context.Context, stop <-chan struct{}, stopped chan<- struct{}) {
	defer close(stopped)
	e.mu.RLock()
	w := e.watcher
	mins := e.cfg.PollingMinutes
	e.mu.RUnlock()
	if mins < 1 {
		mins = 1
	}
	ticker := time.NewTicker(time.Duration(mins) * time.Minute)
	defer ticker.Stop()
	timers := map[string]*time.Timer{}
	for {
		var events <-chan fsnotify.Event
		var errs <-chan error
		if w != nil {
			events, errs = w.Events, w.Errors
		}
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			e.setNextSyncAt(time.Now().Add(time.Duration(mins) * time.Minute))
			go e.poll(ctx)
		case ev, ok := <-events:
			if ok {
				e.scheduleLocal(ctx, ev, timers)
			}
		case _, ok := <-errs:
			if !ok {
				errs = nil
			}
		}
	}
}

func (e *Engine) scheduleLocal(ctx context.Context, ev fsnotify.Event, timers map[string]*time.Timer) {
	if ev.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename) == 0 || strings.HasSuffix(ev.Name, ".tmp") {
		return
	}
	kind := kindFromDir(filepath.Dir(ev.Name))
	if kind == "" || filepath.Ext(ev.Name) != scriptExt {
		return
	}
	if t := timers[ev.Name]; t != nil {
		t.Stop()
	}
	timers[ev.Name] = time.AfterFunc(watcherDebounce, func() { e.pushLocal(ctx, ev.Name) })
}

func (e *Engine) pushLocal(ctx context.Context, path string) {
	if e.isRecentDownload(path) {
		return
	}
	content, ok := readScriptFile(path)
	if !ok {
		return
	}
	hash := HashScript(content)
	e.mu.RLock()
	old := e.hashes[path]
	ref := e.refs[path]
	cfg := e.cfg
	e.mu.RUnlock()
	if old == hash || ref.kind == "" || !cfg.Enabled || !cfg.AutoPushLocal || e.paused() {
		return
	}
	if ctx.Err() != nil || !e.backend.IsNCConnected() {
		return
	}
	if err := e.upload(ref, content); err != nil {
		log.Printf("sync upload %s: %v", path, err)
		return
	}
	e.mu.Lock()
	e.hashes[path] = hash
	e.mu.Unlock()
	e.markSynced()
	e.emitStatus()
}

func (e *Engine) upload(ref scriptRef, content string) error {
	switch ref.kind {
	case "weapon":
		return e.backend.SaveWeapon(ref.key, content)
	case "class":
		return e.backend.SaveClass(ref.key, content)
	case "npc":
		id, err := strconv.Atoi(ref.key)
		if err != nil {
			return err
		}
		return e.backend.SaveNPC(id, content)
	}
	return fmt.Errorf("unknown script type %q", ref.kind)
}

func (e *Engine) poll(ctx context.Context) {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.pollLocked(ctx)
}

func (e *Engine) pollLocked(ctx context.Context) {
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()
	if !cfg.Enabled || cfg.OutputDir == "" || e.paused() || !e.backend.IsNCConnected() {
		e.emitStatus()
		return
	}
	e.setNextSyncAt(e.now().Add(pollDuration(cfg)))
	e.setProgress("Downloading", 0, 0, "")
	_ = e.backend.RefreshWeapons()
	replies, err := e.backend.FetchAllScripts(ctx, func(done, total int) {
		e.setProgress("Downloading", done, total, "")
	})
	if err != nil && len(replies) == 0 {
		log.Printf("sync poll: %v", err)
		return
	}
	if len(replies) == 0 {
		log.Printf("sync poll: script lists are not ready")
		return
	}
	for i, r := range replies {
		if ctx.Err() != nil {
			return
		}
		ref := refFromReply(r)
		if ref.kind == "" {
			continue
		}
		content := normalizeEOL(r.Script)
		path := fullPath(cfg.OutputDir, ref.kind, fileNameFor(ref.kind, ref.key, ref.name))
		e.setProgress("Comparing", i+1, len(replies), ref.name)
		serverHash := HashScript(content)
		e.remember(ref, path, serverHash)
		local, exists := readScriptFile(path)
		if !exists {
			if err := writeFileAtomic(path, []byte(content)); err == nil {
				e.markDownload(path)
				e.mu.Lock()
				e.hashes[path] = serverHash
				e.mu.Unlock()
			}
			continue
		}
		if HashScript(local) == serverHash {
			e.mu.Lock()
			e.hashes[path] = serverHash
			e.mu.Unlock()
			continue
		}
		if cfg.AutoPullServer {
			if e.editorIsOpen(ref) && !e.isExpectedServerUpdate(ref, serverHash) {
				e.enqueueReview(ref, path, local, content, "server changed while editing")
				continue
			}
			e.writeServerVersion(ref, path, content, serverHash)
		}
	}
	e.markSynced()
	e.finishProgress(len(replies))
	e.emitStatus()
}

func pollDuration(cfg SyncConfig) time.Duration {
	mins := cfg.PollingMinutes
	if mins < 1 {
		mins = 1
	}
	return time.Duration(mins) * time.Minute
}

func (e *Engine) setNextSyncAt(at time.Time) {
	e.mu.Lock()
	e.nextSyncAt = at.Unix()
	e.mu.Unlock()
	e.emitStatus()
}

func (e *Engine) setProgress(phase string, completed, total int, current string) {
	e.mu.Lock()
	e.progress = SyncProgress{Active: true, Phase: phase, Current: current, Completed: completed, Total: total}
	e.mu.Unlock()
	if e.emit != nil {
		e.emit("rc:syncProgress", e.progress)
		e.emitStatus()
	}
}

func (e *Engine) finishProgress(total int) {
	e.mu.Lock()
	e.progress.Active = false
	e.progress.Completed = total
	e.progress.Total = total
	e.mu.Unlock()
}

func (e *Engine) remember(ref scriptRef, path, hash string) {
	e.mu.Lock()
	ref.path = path
	e.refs[path] = ref
	if _, ok := e.hashes[path]; !ok {
		e.hashes[path] = hash
	}
	e.mu.Unlock()
}

func (e *Engine) editorIsOpen(ref scriptRef) bool {
	e.mu.RLock()
	fn := e.isEditing
	e.mu.RUnlock()
	return fn != nil && fn(ref.kind, ref.key)
}

func (e *Engine) isSelfServerUpdate(ref scriptRef, actor, serverHash string) bool {
	if e.isExpectedServerUpdate(ref, serverHash) {
		return true
	}
	e.mu.RLock()
	fn := e.localActor
	e.mu.RUnlock()
	if fn == nil {
		return false
	}
	local := strings.TrimSpace(fn())
	return local != "" && strings.EqualFold(local, strings.TrimSpace(actor))
}

func (e *Engine) isExpectedServerUpdate(ref scriptRef, serverHash string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := entryKey(ref.kind, ref.key)
	if pending, ok := e.expectedUpdates[key]; ok {
		if e.now().After(pending.expires) {
			delete(e.expectedUpdates, key)
			return false
		}
		if pending.hash != serverHash {
			return false
		}
		delete(e.expectedUpdates, key)
		return true
	}
	return false
}
func (e *Engine) markDownload(path string) {
	e.mu.Lock()
	e.recentDownloads[path] = e.now().Add(recentDownloadTTL)
	e.mu.Unlock()
}
func (e *Engine) isRecentDownload(path string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	until, ok := e.recentDownloads[path]
	if ok && e.now().Before(until) {
		return true
	}
	delete(e.recentDownloads, path)
	return false
}
func (e *Engine) markSynced() { e.mu.Lock(); e.lastSyncAt = e.now().Unix(); e.mu.Unlock() }
func (e *Engine) paused() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg.PauseUntil != 0 && e.now().Unix() < e.cfg.PauseUntil
}

func (e *Engine) HandleChatLine(line string) {
	act, ok := ParseChatLine(line)
	if !ok || act.Kind == "delete" {
		return
	}
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.handleActivity(act)
}
func (e *Engine) handleActivity(act Activity) {
	key := act.Key
	if act.Kind == "npc" {
		id, ok := e.npcIDByName(act.Name)
		if !ok {
			return
		}
		key = strconv.Itoa(id)
	}
	r, err := e.backend.OpenScript(act.Kind, key)
	if err != nil {
		return
	}
	content := normalizeEOL(r.Script)
	e.mu.RLock()
	cfg := e.cfg
	e.mu.RUnlock()
	ref := scriptRef{kind: act.Kind, key: key, name: act.Name}
	path := fullPath(cfg.OutputDir, act.Kind, fileNameFor(act.Kind, key, act.Name))
	e.remember(ref, path, HashScript(content))
	local, ok := readScriptFile(path)
	serverHash := HashScript(content)
	if !ok || HashScript(local) == serverHash {
		if !ok && cfg.AutoPullServer {
			if err := writeFileAtomic(path, []byte(content)); err == nil {
				e.markDownload(path)
				e.mu.Lock()
				e.hashes[path] = serverHash
				e.mu.Unlock()
			}
		}
		return
	}
	if cfg.AutoPullServer {
		if e.editorIsOpen(ref) && !e.isSelfServerUpdate(ref, act.Actor, serverHash) {
			e.enqueueReview(ref, path, local, content, act.Actor)
			return
		}
		e.writeServerVersion(ref, path, content, serverHash)
	}
}

func (e *Engine) writeServerVersion(ref scriptRef, path, content, hash string) {
	if err := writeFileAtomic(path, []byte(content)); err != nil {
		log.Printf("sync download %s: %v", path, err)
		return
	}
	e.markDownload(path)
	e.mu.Lock()
	e.hashes[path] = hash
	e.mu.Unlock()
	e.markSynced()
	e.emitStatus()
}
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
func (e *Engine) HandleListChanged(_ string) { go e.poll(context.Background()) }

// ReconcileAll is kept as the public immediate-sync entry point used by the
// Wails binding and the Sync Now button.
func (e *Engine) ReconcileAll(ctx context.Context) {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.pollLocked(ctx)
}

func (e *Engine) enqueueReview(ref scriptRef, path, local, server, actor string) {
	e.mu.Lock()
	key := entryKey(ref.kind, ref.key)
	_, exists := e.review[key]
	item := ReviewItem{Kind: ref.kind, Key: ref.key, Name: ref.name, State: StateConflict, Local: local, Server: server, Actor: actor}
	e.review[key] = item
	e.mu.Unlock()
	if !exists {
		e.emit("rc:syncConflict", item)
	}
	_ = path
	e.emitStatus()
}

func (e *Engine) Status() SyncStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	items := make([]ReviewItem, 0, len(e.review))
	for _, it := range e.review {
		items = append(items, it)
	}
	ncDown := e.cfg.Enabled && e.cfg.OutputDir != "" && !e.backend.IsNCConnected()
	return SyncStatus{Enabled: e.cfg.Enabled, Paused: e.cfg.PauseUntil != 0 && e.now().Unix() < e.cfg.PauseUntil, NCDown: ncDown, OutputDirMissing: e.cfg.OutputDir == "", Server: e.server, OutputDir: e.cfg.OutputDir, LastSyncAt: e.lastSyncAt, ReviewCount: len(items), Items: items, Progress: e.progress, NextSyncAt: e.nextSyncAt}
}

func (e *Engine) GetScriptPair(kind, key string) (ScriptPair, error) {
	e.mu.RLock()
	it, ok := e.review[entryKey(kind, key)]
	e.mu.RUnlock()
	if !ok {
		return ScriptPair{}, nil
	}
	return ScriptPair{Local: it.Local, Server: it.Server}, nil
}

// ResolveConflict is the only manual path for a collision. choice may be
// local, server, or merge; mergeContent is used only for the latter.
func (e *Engine) ResolveConflict(kind, key, choice, mergeContent string) error {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.mu.RLock()
	it, ok := e.review[entryKey(kind, key)]
	cfg := e.cfg
	ref := e.refs[fullPath(cfg.OutputDir, kind, fileNameFor(kind, key, it.Name))]
	e.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no review item for %s:%s", kind, key)
	}
	if ref.kind == "" {
		ref = scriptRef{kind: kind, key: key, name: it.Name}
	}
	content := it.Server
	if choice == "local" {
		content = it.Local
	}
	if choice == "merge" {
		if mergeContent == "" {
			return fmt.Errorf("merged content is required")
		}
		content = mergeContent
	}
	if err := e.upload(ref, content); err != nil {
		return err
	}
	path := fullPath(cfg.OutputDir, kind, fileNameFor(kind, key, it.Name))
	if err := writeFileAtomic(path, []byte(normalizeEOL(content))); err != nil {
		return err
	}
	hash := HashScript(content)
	e.mu.Lock()
	e.hashes[path] = hash
	delete(e.review, entryKey(kind, key))
	e.mu.Unlock()
	e.markDownload(path)
	e.markSynced()
	e.emitStatus()
	return nil
}

func (e *Engine) emitStatus() {
	if e.emit != nil {
		e.emit("rc:syncStatus", e.Status())
	}
}
