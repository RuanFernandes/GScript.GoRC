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
	Enabled           bool         `json:"enabled"`
	Paused            bool         `json:"paused"`
	NCDown            bool         `json:"ncDown"`
	OutputDirMissing  bool         `json:"outputDirMissing"`
	InitialSync       bool         `json:"initialSync"`
	PanicMode         bool         `json:"panicMode"`
	Server            string       `json:"server"`
	OutputDir         string       `json:"outputDir"`
	LastSyncAt        int64        `json:"lastSyncAt"`
	SyncGeneration    uint64       `json:"syncGeneration"`
	PanicReason       string       `json:"panicReason,omitempty"`
	PanicAt           int64        `json:"panicAt,omitempty"`
	ReviewCount       int          `json:"reviewCount"`
	Items             []ReviewItem `json:"items"`
	Progress          SyncProgress `json:"progress"`
	NextSyncAt        int64        `json:"nextSyncAt"`
	PermissionsReady  bool         `json:"permissionsReady"`
	PermissionsError  string       `json:"permissionsError,omitempty"`
	ScriptRightsReady bool         `json:"scriptRightsReady"`
	ScriptWriteAccess bool         `json:"scriptWriteAccess"`
	SyncRequired      bool         `json:"syncRequired"`
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

// PullBackupFunc stores the previous local bytes before a server pull
// overwrites them. It returns the backup ID for the audit detail.
type PullBackupFunc func(kind, key string, previous []byte) (string, error)

// PullAuditFunc records a completed or failed server-to-local pull.
type PullAuditFunc func(kind, key, outcome, detail string)

// Engine implements the intentionally small two-way sync protocol. hashes is
// the only baseline: a path is changed only when its current MD5 differs from
// the last successful server/local operation.
type Engine struct {
	mu                sync.RWMutex
	cfg               SyncConfig
	backend           ScriptBackend
	server            string
	hashes            map[string]string
	refs              map[string]scriptRef
	recentDownloads   map[string]time.Time
	review            map[string]ReviewItem
	lastSyncAt        int64
	progress          SyncProgress
	nextSyncAt        int64
	permissionsReady  bool
	permissionsError  string
	initialSyncActive bool
	initialSyncDone   bool
	syncGeneration    uint64
	panicMode         bool
	panicReason       string
	panicAt           int64
	uploadActive      bool
	panicHandler      func(string, int64)
	pullBackup        PullBackupFunc
	pullAudit         PullAuditFunc
	running           bool
	stop              chan struct{}
	stopped           chan struct{}
	watcher           *fsnotify.Watcher
	emit              func(string, ...any)
	now               func() time.Time
	isEditing         func(string, string) bool
	localActor        func() string
	classScriptHeader func() string
	expectedUpdates   map[string]expectedServerUpdate
	workMu            sync.Mutex
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

func (e *Engine) SetClassScriptHeader(fn func() string) {
	e.mu.Lock()
	e.classScriptHeader = fn
	e.mu.Unlock()
}

const concurrentUploadPanicReason = "overlapping script uploads were detected"

func (e *Engine) SetPanicHandler(fn func(string, int64)) {
	e.mu.Lock()
	e.panicHandler = fn
	e.mu.Unlock()
}

// SetPullRecorder installs the application-owned safety history hooks used by
// server-to-local sync pulls. The engine remains usable without these hooks
// in package-level tests and non-desktop consumers.
func (e *Engine) SetPullRecorder(backup PullBackupFunc, audit PullAuditFunc) {
	e.mu.Lock()
	e.pullBackup = backup
	e.pullAudit = audit
	e.mu.Unlock()
}

func (e *Engine) SetPanicState(active bool, reason string, at int64) {
	e.mu.Lock()
	e.panicMode = active
	if active {
		e.panicReason = reason
		e.panicAt = at
	} else {
		e.panicReason = ""
		e.panicAt = 0
	}
	e.mu.Unlock()
	e.emitStatus()
}

func (e *Engine) ResetPanic() error {
	e.mu.Lock()
	if e.uploadActive {
		e.mu.Unlock()
		return fmt.Errorf("cannot reset sync panic mode while a script upload is still active")
	}
	e.panicMode = false
	e.panicReason = ""
	e.panicAt = 0
	e.mu.Unlock()
	e.emitStatus()
	return nil
}

func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

func (e *Engine) beginUpload() (func(), error) {
	e.mu.Lock()
	if e.panicMode {
		reason := e.panicReason
		e.mu.Unlock()
		if reason == "" {
			reason = concurrentUploadPanicReason
		}
		return nil, fmt.Errorf("sync panic mode active: %s", reason)
	}
	if e.uploadActive {
		reason := concurrentUploadPanicReason
		at := e.now().Unix()
		e.panicMode = true
		e.panicReason = reason
		e.panicAt = at
		handler := e.panicHandler
		e.mu.Unlock()
		if handler != nil {
			handler(reason, at)
		}
		e.emitStatus()
		return nil, fmt.Errorf("sync panic mode activated: %s", reason)
	}
	e.uploadActive = true
	e.mu.Unlock()
	return func() {
		e.mu.Lock()
		e.uploadActive = false
		e.mu.Unlock()
	}, nil
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

// MarkPermissionsReady adopts the permission snapshot loaded by the session
// before the engine was created. This avoids issuing the same openrights
// request again during the first bootstrap.
func (e *Engine) MarkPermissionsReady() {
	e.mu.Lock()
	e.permissionsReady = true
	e.permissionsError = ""
	e.mu.Unlock()
}

func (e *Engine) SetPaused(until int64) {
	e.mu.Lock()
	e.cfg.PauseUntil = until
	e.mu.Unlock()
	e.emitStatus()
}

func (e *Engine) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		e.emitStatus()
		return
	}
	if !e.cfg.Enabled || e.cfg.OutputDir == "" {
		e.initialSyncActive = false
		e.mu.Unlock()
		e.emitStatus()
		return
	}
	if e.panicMode {
		e.mu.Unlock()
		e.emitStatus()
		return
	}
	e.running = true
	e.stop = make(chan struct{})
	e.stopped = make(chan struct{})
	stop, stopped, cfg := e.stop, e.stopped, e.cfg
	e.nextSyncAt = 0
	e.initialSyncActive = !e.initialSyncDone
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
				e.markPermissionsStale()
				continue
			}
			if !e.backend.IsNCAuthenticated() {
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
	if e.running {
		e.running = false
		e.initialSyncActive = false
		close(e.stop)
	}
	w := e.watcher
	stopped := e.stopped
	e.mu.Unlock()
	if w != nil {
		_ = w.Close()
	}
	if stopped != nil {
		<-stopped
	}
	// Polls, chat activity and list-change reconciles are serialized by
	// workMu, but they may be running outside the loop goroutine. Wait for the
	// active operation before the shared connection can be switched to another
	// server, so its late replies cannot be applied to the new session.
	e.workMu.Lock()
	e.workMu.Unlock()
}

func (e *Engine) bootstrap(ctx context.Context, dir string) error {
	if !e.backend.IsNCConnected() {
		return fmt.Errorf("NC is not connected")
	}
	if !e.backend.IsNCAuthenticated() {
		return fmt.Errorf("NC is not authenticated")
	}
	if !e.permissionsReadySnapshot() {
		if err := e.refreshPermissions(); err != nil {
			return err
		}
	} else {
		log.Printf("[sync bootstrap] reusing already-loaded script permissions")
	}
	e.setProgress("Downloading", 0, 0, "")
	// The NC socket can report connected before its cached script lists have
	// arrived. Refreshing the weapon list here gives the first bootstrap a
	// chance to populate those caches instead of treating 0/0 as success.
	log.Printf("[sync bootstrap] fetching readable scripts after openrights")
	replies, err := e.fetchScripts(ctx, func(done, total int) {
		e.setProgress("Downloading", done, total, "")
	})
	log.Printf("[sync bootstrap] readable script fetch finished replies=%d err=%v", len(replies), err)
	if err != nil && len(replies) == 0 {
		return err
	}
	if len(replies) == 0 {
		// An account may legitimately have no readable scripts. Treat an empty
		// permission-filtered result as a completed bootstrap; the next regular
		// poll will pick up lists that were still warming up on the NC socket.
		e.removeUnlistedLocalFiles(dir, serverPaths(nil))
		e.markReconcileCompleted()
		e.finishProgress(0)
		e.emitStatus()
		return nil
	}
	var pullErr error
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
		trace := traceCompareItem(i, len(replies))
		started := time.Now()
		if trace {
			log.Printf("[sync bootstrap compare] start %d/%d kind=%s name=%q bytes=%d", i+1, len(replies), ref.kind, ref.name, len(content))
		}
		e.setProgress("Writing", i+1, len(replies), ref.name)
		if err := e.pullLocalVersion(ref, path, content, HashScript(content), nil); err != nil {
			// One local path may be removed or temporarily unavailable while a
			// snapshot is being materialized. Keep processing the rest of the
			// server snapshot; the next poll can retry this path.
			log.Printf("sync bootstrap write %s: %v", path, err)
			if pullErr == nil {
				pullErr = err
			}
			continue
		}
		e.rememberRef(ref, path)
		if trace {
			log.Printf("[sync bootstrap compare] done %d/%d kind=%s name=%q elapsed=%s", i+1, len(replies), ref.kind, ref.name, time.Since(started))
		}
	}
	e.removeUnlistedLocalFiles(dir, serverPaths(replies))
	if err == nil && pullErr != nil {
		err = pullErr
	}
	if err == nil {
		e.markReconcileCompleted()
	} else {
		e.markSynced()
	}
	e.finishProgress(len(replies))
	e.emitStatus()
	return err
}

func (e *Engine) permissionsReadySnapshot() bool {
	e.mu.RLock()
	ready := e.permissionsReady
	e.mu.RUnlock()
	return ready
}

func (e *Engine) markPermissionsStale() {
	e.mu.Lock()
	changed := e.permissionsReady
	e.permissionsReady = false
	e.mu.Unlock()
	if changed {
		e.emitStatus()
	}
}

func (e *Engine) refreshPermissions() error {
	started := time.Now()
	log.Printf("[sync rights] refresh requested")
	err := e.backend.RefreshSelfFolderRights()
	e.mu.Lock()
	if err != nil {
		e.permissionsReady = false
		e.permissionsError = err.Error()
	} else {
		e.permissionsReady = true
		e.permissionsError = ""
	}
	e.mu.Unlock()
	e.emitStatus()
	if err != nil {
		log.Printf("[sync rights] refresh failed after %s: %v", time.Since(started), err)
		return fmt.Errorf("refresh script permissions: %w", err)
	}
	log.Printf("[sync rights] refresh succeeded after %s; continuing with script fetch", time.Since(started))
	return nil
}

func refFromReply(r rclib.ScriptReply) scriptRef {
	if r.Type != "weapon" && r.Type != "class" && r.Type != "npc" {
		return scriptRef{}
	}
	if !rclib.IsUsableScriptName(r.Name) {
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
		mins = DefaultPollingMinutes
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
		case watchErr, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			if watchErr != nil {
				log.Printf("sync watcher: %v", watchErr)
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
	// A watcher callback can run while a scheduled/manual reconcile is
	// comparing or removing files. Serialize it with the server-side pass so a
	// newly edited file is not deleted or uploaded from a stale snapshot.
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.pushLocalLocked(ctx, path)
}

func (e *Engine) pushLocalLocked(ctx context.Context, path string) {
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
	if old == hash || !cfg.Enabled || !cfg.AutoPushLocal || e.paused() {
		return
	}
	if ctx.Err() != nil || !e.backend.IsNCConnected() {
		return
	}
	releaseUpload, err := e.beginUpload()
	if err != nil {
		log.Printf("sync upload blocked: %v", err)
		return
	}
	defer releaseUpload()
	if ref.kind == "" {
		if kindFromDir(filepath.Dir(path)) != "weapon" && kindFromDir(filepath.Dir(path)) != "class" {
			return
		}
		name := decodeName(strings.TrimSuffix(filepath.Base(path), scriptExt))
		ref = scriptRef{kind: kindFromDir(filepath.Dir(path)), key: name, name: name, path: path}
		if ref.kind == "class" && strings.TrimSpace(content) == "" {
			e.mu.RLock()
			header := e.classScriptHeader
			e.mu.RUnlock()
			if header == nil {
				return
			}
			content = strings.TrimRight(header(), "\r\n")
			if content == "" {
				return
			}
			if err := writeFileAtomic(path, []byte(content)); err != nil {
				log.Printf("sync initialize class %s: %v", name, err)
				return
			}
			hash = HashScript(content)
			e.markDownload(path)
		}
		if !e.backend.CanWriteScript(ref.kind, ref.name) {
			return
		}
		if ref.kind == "weapon" {
			if err := e.backend.AddWeapon(ref.name); err != nil {
				log.Printf("sync create weapon %s: %v", ref.name, err)
				return
			}
		} else if err := e.backend.AddClass(ref.name); err != nil {
			log.Printf("sync create class %s: %v", ref.name, err)
			return
		}
	}
	if err := e.uploadUnchecked(ref, content); err != nil {
		log.Printf("sync upload %s: %v", path, err)
		return
	}
	e.mu.Lock()
	e.refs[path] = ref
	e.hashes[path] = hash
	e.mu.Unlock()
	e.markSynced()
	e.emitStatus()
}

func (e *Engine) upload(ref scriptRef, content string) error {
	releaseUpload, err := e.beginUpload()
	if err != nil {
		return err
	}
	defer releaseUpload()
	return e.uploadUnchecked(ref, content)
}

func (e *Engine) uploadUnchecked(ref scriptRef, content string) error {
	if !e.backend.CanWriteScript(ref.kind, ref.name) {
		return fmt.Errorf("no write permission for %s %q", ref.kind, ref.name)
	}
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
	// Scheduled polling deliberately reuses the permission snapshot. Rights
	// refreshes are reserved for bootstrap and the explicit Sync Now action.
	e.pollLocked(ctx, false)
}

func (e *Engine) pollLocked(ctx context.Context, refreshRights bool) {
	e.mu.RLock()
	cfg := e.cfg
	panicMode := e.panicMode
	e.mu.RUnlock()
	if panicMode {
		e.emitStatus()
		return
	}
	if !cfg.Enabled || cfg.OutputDir == "" || e.paused() || !e.backend.IsNCConnected() {
		e.emitStatus()
		return
	}
	// Keep a baseline from before any server I/O. A local edit can happen while
	// the script snapshot is being fetched; comparing only with the first read
	// inside the apply loop would treat that edit as the old local version and
	// allow a stale server snapshot to overwrite it.
	localBaseline := snapshotLocalScripts(cfg.OutputDir)
	if !e.backend.IsNCAuthenticated() {
		e.emitStatus()
		return
	}
	if refreshRights {
		if err := e.refreshPermissions(); err != nil {
			log.Printf("sync manual permissions: %v", err)
			return
		}
	} else if !e.permissionsReadySnapshot() {
		log.Printf("[sync poll] skipping: script permissions are not loaded")
		e.emitStatus()
		return
	} else {
		log.Printf("[sync poll] using cached script permissions")
	}
	e.setNextSyncAt(e.now().Add(pollDuration(cfg)))
	e.setProgress("Downloading", 0, 0, "")
	log.Printf("[sync poll] fetching readable scripts after openrights")
	replies, err := e.fetchScripts(ctx, func(done, total int) {
		e.setProgress("Downloading", done, total, "")
	})
	log.Printf("[sync poll] readable script fetch finished replies=%d err=%v", len(replies), err)
	if err != nil && len(replies) == 0 {
		log.Printf("sync poll: %v", err)
		return
	}
	if len(replies) == 0 {
		log.Printf("sync poll: script lists are not ready")
		if err == nil {
			e.removeUnlistedLocalFiles(cfg.OutputDir, serverPaths(nil))
		}
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
		trace := traceCompareItem(i, len(replies))
		started := time.Now()
		if trace {
			log.Printf("[sync poll compare] start %d/%d kind=%s name=%q bytes=%d", i+1, len(replies), ref.kind, ref.name, len(content))
		}
		e.setProgress("Comparing", i+1, len(replies), ref.name)
		serverHash := HashScript(content)
		e.rememberRef(ref, path)
		local, exists := readScriptFile(path)
		if localChangedSince(path, localBaseline) {
			log.Printf("[sync poll compare] preserving local change made during sync path=%q", path)
			e.preserveLocalChange(path, localBaseline)
			continue
		}
		if !exists {
			if err := e.pullLocalVersion(ref, path, content, serverHash, nil); err != nil {
				log.Printf("[sync poll compare] create failed %d/%d path=%q: %v", i+1, len(replies), path, err)
			}
			if trace {
				log.Printf("[sync poll compare] done %d/%d kind=%s name=%q action=created elapsed=%s", i+1, len(replies), ref.kind, ref.name, time.Since(started))
			}
			continue
		}
		if HashScript(local) == serverHash {
			e.mu.Lock()
			e.hashes[path] = serverHash
			e.mu.Unlock()
			if trace {
				log.Printf("[sync poll compare] done %d/%d kind=%s name=%q action=unchanged elapsed=%s", i+1, len(replies), ref.kind, ref.name, time.Since(started))
			}
			continue
		}
		if cfg.AutoPullServer {
			if e.editorIsOpen(ref) && !e.isExpectedServerUpdate(ref, serverHash) {
				e.enqueueReview(ref, path, local, content, "server changed while editing")
				if trace {
					log.Printf("[sync poll compare] done %d/%d kind=%s name=%q action=review elapsed=%s", i+1, len(replies), ref.kind, ref.name, time.Since(started))
				}
				continue
			}
			e.writeServerVersion(ref, path, content, serverHash, &local)
		}
		if trace {
			log.Printf("[sync poll compare] done %d/%d kind=%s name=%q action=changed elapsed=%s", i+1, len(replies), ref.kind, ref.name, time.Since(started))
		}
	}
	if err == nil {
		e.removeUnlistedLocalFiles(cfg.OutputDir, serverPaths(replies), changedLocalPaths(cfg.OutputDir, localBaseline))
		e.markReconcileCompleted()
	} else {
		e.markSynced()
	}
	e.finishProgress(len(replies))
	e.emitStatus()
}

func traceCompareItem(index, total int) bool {
	return index%50 == 0 || index >= total-5
}

const (
	scriptListWarmupAttempts = 5
	scriptListWarmupDelay    = 250 * time.Millisecond
)

// fetchScripts gives the NC server a short warm-up window after authentication.
// The socket can be authenticated before the initial weapon/class/NPC lists
// have been copied into grclib's caches; treating that first empty snapshot as
// a successful sync loses the initial bootstrap until the next poll.
func (e *Engine) fetchScripts(ctx context.Context, progress func(done, total int)) ([]rclib.ScriptReply, error) {
	var replies []rclib.ScriptReply
	var err error
	for attempt := 1; attempt <= scriptListWarmupAttempts; attempt++ {
		if attempt > 1 {
			timer := time.NewTimer(scriptListWarmupDelay)
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return replies, ctx.Err()
			case <-timer.C:
			}
		}
		if refreshErr := e.backend.RefreshWeapons(); refreshErr != nil {
			// Never continue with a partial cache: a classes/NPC-only snapshot
			// would make reconciliation remove valid local weapons.
			return replies, fmt.Errorf("refresh weapons attempt=%d/%d: %w", attempt, scriptListWarmupAttempts, refreshErr)
		}
		replies, err = e.backend.FetchAllScripts(ctx, e.backend.CanReadScript, progress)
		if err != nil || len(replies) > 0 || attempt == scriptListWarmupAttempts {
			return replies, err
		}
		log.Printf("[sync fetch] script lists returned no readable replies; warming up attempt=%d/%d", attempt, scriptListWarmupAttempts)
	}
	return replies, err
}

func pollDuration(cfg SyncConfig) time.Duration {
	mins := cfg.PollingMinutes
	if mins < 1 {
		mins = DefaultPollingMinutes
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

func (e *Engine) rememberRef(ref scriptRef, path string) {
	e.mu.Lock()
	ref.path = path
	e.refs[path] = ref
	e.mu.Unlock()
}

func (e *Engine) setHash(path, hash string) {
	e.mu.Lock()
	e.hashes[path] = hash
	e.mu.Unlock()
}

func (e *Engine) pullLocalVersion(ref scriptRef, path, content, hash string, expectedLocal *string) error {
	current, exists := readScriptFile(path)
	if expectedLocal != nil && exists && HashScript(current) != HashScript(*expectedLocal) {
		log.Printf("sync download skipped %s: local file changed during sync", path)
		return nil
	}
	if exists && HashScript(current) == hash {
		e.setHash(path, hash)
		return nil
	}

	backupID := ""
	if exists {
		e.mu.RLock()
		backup := e.pullBackup
		e.mu.RUnlock()
		if backup != nil {
			var err error
			backupID, err = backup(ref.kind, ref.key, []byte(current))
			if err != nil {
				detail := fmt.Sprintf("could not back up the previous local version before pulling %s: %v", ref.name, err)
				e.auditPull(ref, "failed", detail)
				return err
			}
		}
	}
	if err := writeFileAtomic(path, []byte(content)); err != nil {
		detail := fmt.Sprintf("could not write the server version locally: %v", err)
		e.auditPull(ref, "failed", detail)
		return err
	}
	e.markDownload(path)
	e.setHash(path, hash)
	action := "created"
	if exists {
		action = "updated"
	}
	detail := fmt.Sprintf("pulled server version; local copy %s", action)
	if backupID != "" {
		detail += "; backup " + backupID
	} else if exists {
		detail += "; no previous-version backup was available"
	}
	e.auditPull(ref, "success", detail)
	return nil
}

func (e *Engine) auditPull(ref scriptRef, outcome, detail string) {
	e.mu.RLock()
	audit := e.pullAudit
	e.mu.RUnlock()
	if audit != nil {
		audit(ref.kind, ref.key, outcome, detail)
	}
}

func serverPaths(replies []rclib.ScriptReply) map[string]bool {
	paths := make(map[string]bool, len(replies))
	for _, reply := range replies {
		ref := refFromReply(reply)
		if ref.kind == "" {
			continue
		}
		relative := filepath.Join(kindSubdir(ref.kind), fileNameFor(ref.kind, ref.key, ref.name)+scriptExt)
		paths[filepath.ToSlash(relative)] = true
	}
	return paths
}

// removeUnlistedLocalFiles enforces server priority after a complete server
// snapshot. Files created while the watcher is active are uploaded first, so
// their paths appear in the next server snapshot.
func (e *Engine) removeUnlistedLocalFiles(outputDir string, listed map[string]bool, protected ...map[string]bool) {
	protectedPaths := map[string]bool{}
	if len(protected) > 0 && protected[0] != nil {
		protectedPaths = protected[0]
	}
	for _, kind := range []string{"weapon", "class", "npc"} {
		dir := filepath.Join(outputDir, kindSubdir(kind))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != scriptExt {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			relative, err := filepath.Rel(outputDir, path)
			if err != nil || listed[filepath.ToSlash(relative)] || protectedPaths[path] {
				continue
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				log.Printf("sync remove server-missing local script %q: %v", path, err)
				continue
			}
			e.mu.Lock()
			delete(e.hashes, path)
			delete(e.refs, path)
			delete(e.review, entryKey(kind, decodeName(strings.TrimSuffix(entry.Name(), scriptExt))))
			e.mu.Unlock()
		}
	}
}

func snapshotLocalScripts(outputDir string) map[string]string {
	baseline := map[string]string{}
	for _, kind := range []string{"weapon", "class", "npc"} {
		dir := filepath.Join(outputDir, kindSubdir(kind))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != scriptExt {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if content, ok := readScriptFile(path); ok {
				baseline[path] = HashScript(content)
			}
		}
	}
	return baseline
}

func localChangedSince(path string, baseline map[string]string) bool {
	content, exists := readScriptFile(path)
	initialHash, hadFile := baseline[path]
	if !hadFile {
		return exists
	}
	return !exists || HashScript(content) != initialHash
}

func changedLocalPaths(outputDir string, baseline map[string]string) map[string]bool {
	changed := map[string]bool{}
	for path := range baseline {
		if localChangedSince(path, baseline) {
			changed[path] = true
		}
	}
	for _, kind := range []string{"weapon", "class", "npc"} {
		// New files are not in the baseline, so inspect the parent directory to
		// protect them from server-priority cleanup until the watcher uploads them.
		dir := filepath.Join(outputDir, kindSubdir(kind))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != scriptExt {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if localChangedSince(path, baseline) {
				changed[path] = true
			}
		}
	}
	return changed
}

func (e *Engine) preserveLocalChange(path string, baseline map[string]string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if initialHash, ok := baseline[path]; ok {
		e.hashes[path] = initialHash
	} else {
		delete(e.hashes, path)
	}
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

func (e *Engine) markReconcileCompleted() {
	e.mu.Lock()
	e.lastSyncAt = e.now().Unix()
	e.syncGeneration++
	e.initialSyncDone = true
	e.initialSyncActive = false
	e.mu.Unlock()
}
func (e *Engine) paused() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg.PauseUntil != 0 && e.now().Unix() < e.cfg.PauseUntil
}

func (e *Engine) HandleChatLine(line string) {
	act, ok := ParseChatLine(line)
	if !ok {
		return
	}
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.mu.RLock()
	panicMode := e.panicMode
	e.mu.RUnlock()
	if panicMode {
		return
	}
	if act.Action == "deleted" {
		e.handleDeleteActivity(act)
		return
	}
	e.handleActivity(act)
}

func (e *Engine) handleDeleteActivity(act Activity) {
	e.mu.RLock()
	var paths []string
	for path, ref := range e.refs {
		if ref.kind == act.Kind && (ref.name == act.Name || ref.key == act.Key) {
			paths = append(paths, path)
		}
	}
	e.mu.RUnlock()
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("sync delete server-removed script %q: %v", path, err)
			continue
		}
		e.mu.Lock()
		delete(e.hashes, path)
		delete(e.refs, path)
		delete(e.review, entryKey(act.Kind, act.Key))
		e.mu.Unlock()
	}
	if len(paths) > 0 {
		e.markSynced()
		e.emitStatus()
	}
}
func (e *Engine) handleActivity(act Activity) {
	if !e.backend.CanReadScript(act.Kind, act.Name) {
		return
	}
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
	e.mu.RLock()
	_, hadBaseline := e.hashes[path]
	e.mu.RUnlock()
	e.rememberRef(ref, path)
	local, ok := readScriptFile(path)
	serverHash := HashScript(content)
	if !ok || HashScript(local) == serverHash {
		if !ok && cfg.AutoPullServer {
			if err := e.pullLocalVersion(ref, path, content, serverHash, nil); err != nil {
				log.Printf("sync chat download %s: %v", path, err)
			}
		} else {
			e.setHash(path, serverHash)
		}
		return
	}
	if !cfg.AutoPullServer && !hadBaseline {
		e.setHash(path, serverHash)
	}
	if cfg.AutoPullServer {
		if e.editorIsOpen(ref) && !e.isSelfServerUpdate(ref, act.Actor, serverHash) {
			if !hadBaseline {
				e.setHash(path, serverHash)
			}
			e.enqueueReview(ref, path, local, content, act.Actor)
			return
		}
		e.writeServerVersion(ref, path, content, serverHash, &local)
	}
}

func (e *Engine) writeServerVersion(ref scriptRef, path, content, hash string, expectedLocal *string) {
	if err := e.pullLocalVersion(ref, path, content, hash, expectedLocal); err != nil {
		log.Printf("sync download %s: %v", path, err)
		return
	}
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

// ReconcileAll is the public immediate-sync entry point used by the Wails
// binding and the Sync Now button. Unlike scheduled polling, it refreshes the
// self permission snapshot before fetching scripts.
func (e *Engine) ReconcileAll(ctx context.Context) {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	e.pollLocked(ctx, true)
}

// PrepareRebuild deletes the contents of the configured Local Sync directory
// and resets the in-memory baseline. The selected directory itself is kept so
// it can be watched again after the engine restarts. The panic circuit remains
// active until the caller has persisted the recovery decision and explicitly
// resets it.
func (e *Engine) PrepareRebuild() error {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	cfg := e.config()
	if cfg.OutputDir == "" {
		return fmt.Errorf("sync output directory is not configured")
	}
	if err := clearSyncContents(cfg.OutputDir); err != nil {
		return err
	}
	e.mu.Lock()
	e.hashes = map[string]string{}
	e.refs = map[string]scriptRef{}
	e.recentDownloads = map[string]time.Time{}
	e.review = map[string]ReviewItem{}
	e.expectedUpdates = map[string]expectedServerUpdate{}
	e.lastSyncAt = 0
	e.nextSyncAt = 0
	e.progress = SyncProgress{}
	e.permissionsReady = false
	e.permissionsError = ""
	e.initialSyncDone = false
	e.initialSyncActive = false
	e.mu.Unlock()
	e.emitStatus()
	return nil
}

// NormalizeLocalState keeps the managed files but adopts their current bytes
// as the local baseline. It prevents an already-present stale file from being
// uploaded immediately after the operator chooses to recover without a full
// delete-and-download rebuild.
func (e *Engine) NormalizeLocalState() error {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	cfg := e.config()
	if cfg.OutputDir == "" {
		return fmt.Errorf("sync output directory is not configured")
	}
	baseline := snapshotLocalScripts(cfg.OutputDir)
	e.mu.Lock()
	refs := make(map[string]scriptRef, len(baseline))
	for path := range baseline {
		if ref, ok := e.refs[path]; ok {
			refs[path] = ref
			continue
		}
		kind := kindFromDir(filepath.Dir(path))
		if kind != "weapon" && kind != "class" {
			continue
		}
		name := decodeName(strings.TrimSuffix(filepath.Base(path), scriptExt))
		refs[path] = scriptRef{kind: kind, key: name, name: name, path: path}
	}
	e.hashes = baseline
	e.refs = refs
	e.review = map[string]ReviewItem{}
	e.recentDownloads = map[string]time.Time{}
	e.expectedUpdates = map[string]expectedServerUpdate{}
	e.lastSyncAt = 0
	e.nextSyncAt = 0
	e.progress = SyncProgress{}
	e.mu.Unlock()
	e.emitStatus()
	return nil
}

func clearSyncContents(outputDir string) error {
	trimmed := strings.TrimSpace(outputDir)
	if trimmed == "" {
		return fmt.Errorf("sync output directory is empty")
	}
	root, err := filepath.Abs(filepath.Clean(trimmed))
	if err != nil {
		return fmt.Errorf("resolve sync output directory: %w", err)
	}
	volumeRoot := filepath.VolumeName(root) + string(filepath.Separator)
	if root == string(filepath.Separator) || root == volumeRoot {
		return fmt.Errorf("refusing to clear a filesystem root as the sync directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read sync output directory: %w", err)
	}
	for _, entry := range entries {
		target := filepath.Join(root, entry.Name())
		relative, err := filepath.Rel(root, target)
		if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid sync cleanup path %q", target)
		}
		if err := os.RemoveAll(target); err != nil {
			return fmt.Errorf("clear sync directory %q: %w", target, err)
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("recreate sync output directory: %w", err)
	}
	return nil
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
	return SyncStatus{Enabled: e.cfg.Enabled, Paused: e.cfg.PauseUntil != 0 && e.now().Unix() < e.cfg.PauseUntil, NCDown: ncDown, OutputDirMissing: e.cfg.OutputDir == "", InitialSync: e.initialSyncActive && e.cfg.Enabled && e.cfg.OutputDir != "", PanicMode: e.panicMode, PanicReason: e.panicReason, PanicAt: e.panicAt, Server: e.server, OutputDir: e.cfg.OutputDir, LastSyncAt: e.lastSyncAt, SyncGeneration: e.syncGeneration, ReviewCount: len(items), Items: items, Progress: e.progress, NextSyncAt: e.nextSyncAt, PermissionsReady: e.permissionsReady, PermissionsError: e.permissionsError}
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
