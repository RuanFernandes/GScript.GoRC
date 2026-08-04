package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"graal-rc/internal/sync"
)

// syncPath returns the sync config file location (mirrors codingPath/
// fileBrowserPath). One file holds ALL servers' configs (a server→config map).
func syncPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "sync.json"), nil
}

// syncFile is the on-disk shape: a map of server name → config. Each server
// (Zodiac, Era, …) keeps its own output folder + settings.
type syncFile struct {
	Servers map[string]sync.SyncConfig `json:"servers"`
}

// currentSyncServer returns the connected server name ("" if none). Each
// Get/Set binds to the currently-connected server's config.
func (a *App) currentSyncServer() string {
	return a.sessions.Status().ServerName
}

// ensureSyncCfgsLoaded lazily loads the per-server config map from disk.
// Caller holds a.syncCfgMu.
func (a *App) ensureSyncCfgsLoaded() {
	if a.syncCfgLoaded {
		return
	}
	a.syncCfgLoaded = true
	a.syncCfgs = map[string]sync.SyncConfig{}
	path, err := syncPath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// Back-compat: an older single-config file (a bare SyncConfig JSON) is
	// migrated under the "" key so it is not lost.
	var single sync.SyncConfig
	if json.Unmarshal(b, &single) == nil && single.PollingMinutes != 0 {
		if single.PollingMinutes < 1 {
			single.PollingMinutes = 5
		}
		a.syncCfgs[""] = single
		return
	}
	var sf syncFile
	if json.Unmarshal(b, &sf) == nil && sf.Servers != nil {
		for k, c := range sf.Servers {
			if c.PollingMinutes < 1 {
				c.PollingMinutes = 5
			}
			a.syncCfgs[k] = c
		}
	}
}

// persistSyncCfgsLocked writes the whole server map to disk. Caller holds
// a.syncCfgMu (or call the unlocked wrapper).
func (a *App) persistSyncCfgsLocked() {
	path, err := syncPath()
	if err != nil {
		return
	}
	b, err := json.MarshalIndent(syncFile{Servers: a.syncCfgs}, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, b, 0o644)
}

// normalizeSyncCfg fills sane defaults for zero values.
func normalizeSyncCfg(c sync.SyncConfig) sync.SyncConfig {
	if c.PollingMinutes < 1 {
		c.PollingMinutes = 5
	}
	// Sync is intentionally always bidirectional. Keeping these enabled avoids
	// a stale local copy after an upload or a stale server copy after a local
	// edit; the UI exposes one coherent sync mode instead of two conflicting
	// switches.
	c.AutoPushLocal = true
	c.AutoPullServer = true
	return c
}

// GetSyncConfig returns the CURRENT server's sync config (defaults if unset).
func (a *App) GetSyncConfig() sync.SyncConfig {
	a.syncCfgMu.Lock()
	defer a.syncCfgMu.Unlock()
	a.ensureSyncCfgsLoaded()
	srv := a.currentSyncServer()
	if c, ok := a.syncCfgs[srv]; ok {
		return normalizeSyncCfg(c)
	}
	return sync.DefaultSyncConfig()
}

// SetSyncConfig persists the CURRENT server's sync config, applies it live to a
// running engine, and broadcasts it so the Settings window updates.
func (a *App) SetSyncConfig(enabled bool, outputDir string, pollingMinutes int, autoPush, autoPull bool) error {
	if pollingMinutes < 1 {
		pollingMinutes = 5
	}
	a.syncCfgMu.Lock()
	a.ensureSyncCfgsLoaded()
	srv := a.currentSyncServer()
	prev, _ := a.syncCfgs[srv]
	cfg := normalizeSyncCfg(sync.SyncConfig{
		Enabled:        enabled,
		OutputDir:      outputDir,
		PollingMinutes: pollingMinutes,
		AutoPushLocal:  autoPush,
		AutoPullServer: autoPull,
		PauseUntil:     prev.PauseUntil,
	})
	a.syncCfgs[srv] = cfg
	a.persistSyncCfgsLocked()
	a.syncCfgMu.Unlock()

	// Apply live (only if the running engine belongs to this server).
	wasEnabled := prev.Enabled && prev.OutputDir != ""
	nowEnabled := cfg.Enabled && cfg.OutputDir != ""
	a.graalScriptLSP.SetEnabled(nowEnabled)
	switch {
	case nowEnabled && a.hasSyncEngine():
		a.applySyncConfig(cfg)
	case nowEnabled && !wasEnabled:
		a.startSyncEngine()
	case !nowEnabled && wasEnabled:
		a.stopSyncEngine()
	}

	if a.app != nil {
		b, _ := json.Marshal(cfg)
		a.app.Event.Emit("rc:syncConfig", string(b))
	}
	return nil
}

// currentSyncEngine returns the engine under lock (nil if none).
func (a *App) currentSyncEngine() *sync.Engine {
	a.syncEngineMu.Lock()
	defer a.syncEngineMu.Unlock()
	return a.syncEngine
}

func (a *App) hasSyncEngine() bool {
	return a.currentSyncEngine() != nil
}

// syncEmitter builds the engine's emit callback: marshals the single payload to
// JSON and emits a raw event (mirrors rc:fbConfig/rc:codingSettings).
func (a *App) syncEmitter() func(string, ...any) {
	return func(name string, data ...any) {
		if name == "rc:syncStatus" && len(data) > 0 {
			if status, ok := data[0].(sync.SyncStatus); ok && !status.Progress.Active {
				// The sync engine writes server changes to disk asynchronously. Once
				// a reconcile finishes, refresh the semantic summaries so a still-open
				// editor sees the new NPC/class public API without reopening it.
				go a.refreshGraalScriptWorkspace()
			}
		}
		if a.app == nil || len(data) == 0 {
			return
		}
		b, err := json.Marshal(data[0])
		if err != nil {
			return
		}
		a.app.Event.Emit(name, string(b))
	}
}

func (a *App) refreshGraalScriptWorkspace() {
	cfg := a.GetSyncConfig()
	if !cfg.Enabled || cfg.OutputDir == "" {
		return
	}
	if err := a.graalScriptLSP.RefreshWorkspace(); err != nil {
		log.Printf("graalscript LSP workspace refresh: %v", err)
	}
}

// startSyncEngine constructs + starts the engine for the CURRENT server. If an
// engine is already running for a different server it is stopped first (each
// server keeps its own engine + baseline + config).
func (a *App) startSyncEngine() {
	srv := a.currentSyncServer()
	a.syncEngineMu.Lock()
	if a.syncEngine != nil {
		// Engine for a different server still running — tear it down first.
		old := a.syncEngine
		a.syncEngine = nil
		oldCancel := a.syncCancel
		a.syncCancel = nil
		a.syncEngineMu.Unlock()
		if oldCancel != nil {
			oldCancel()
		}
		old.Stop()
	} else {
		a.syncEngineMu.Unlock()
	}

	cfg := a.GetSyncConfig()
	// Keep a disabled server from retaining an inert engine. Apart from making
	// status truthful, this lets the Enable Sync toggle start a fresh engine
	// when the user enables it later in the session.
	if !cfg.Enabled || cfg.OutputDir == "" {
		a.graalScriptLSP.SetEnabled(false)
		if a.app != nil {
			b, _ := json.Marshal(cfg)
			a.app.Event.Emit("rc:syncConfig", string(b))
		}
		return
	}
	a.graalScriptLSP.SetEnabled(true)
	ctx, cancel := context.WithCancel(context.Background())
	eng := sync.NewEngine(a.sessions, srv, a.syncEmitter())
	eng.SetEditorChecker(func(kind, key string) bool {
		_, open := a.editorWindowsSnapshot(kind, key)
		return open
	})
	eng.SetLocalActor(func() string { return a.sessions.Status().Nickname })

	a.syncEngineMu.Lock()
	// Another start may have raced ahead; prefer the latest.
	if a.syncEngine != nil {
		a.syncEngine.Stop()
	}
	a.syncEngine = eng
	a.syncCtx = ctx
	a.syncCancel = cancel
	a.syncEngineMu.Unlock()

	eng.ApplyConfig(cfg)
	eng.Start(ctx)
	if a.app != nil {
		b, _ := json.Marshal(cfg)
		a.app.Event.Emit("rc:syncConfig", string(b))
	}
}

// stopSyncEngine tears the engine down.
func (a *App) stopSyncEngine() {
	a.graalScriptLSP.SetEnabled(false)
	a.syncEngineMu.Lock()
	eng := a.syncEngine
	a.syncEngine = nil
	a.syncCtx = nil
	cancel := a.syncCancel
	a.syncCancel = nil
	a.syncEngineMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if eng != nil {
		eng.Stop()
	}
}

func (a *App) applySyncConfig(cfg sync.SyncConfig) {
	if eng := a.currentSyncEngine(); eng != nil {
		eng.ApplyConfig(cfg)
	}
}

// --- Bound methods exposed to the frontend ---

// SyncNow triggers a full reconcile immediately.
func (a *App) SyncNow() error {
	eng := a.currentSyncEngine()
	if eng == nil {
		// Auto-start if configured + enabled.
		cfg := a.GetSyncConfig()
		if cfg.Enabled && cfg.OutputDir != "" {
			a.startSyncEngine()
			eng = a.currentSyncEngine()
		}
	}
	if eng == nil {
		return nil
	}
	// Sync Now runs the full reconcile in the background (refresh weapon list +
	// pipelined bulk fetch + classify). NC is not reconnected — that drops the
	// session on old servers; class/npc lists refresh via live *Changed pushes.
	a.syncEngineMu.Lock()
	ctx := a.syncCtx
	a.syncEngineMu.Unlock()
	if ctx == nil {
		return nil
	}
	go eng.ReconcileAll(ctx)
	return nil
}

// GetSyncStatus returns the engine's status snapshot.
func (a *App) GetSyncStatus() sync.SyncStatus {
	if eng := a.currentSyncEngine(); eng != nil {
		return eng.Status()
	}
	c := a.GetSyncConfig()
	return sync.SyncStatus{
		Enabled:          c.Enabled,
		Server:           a.currentSyncServer(),
		OutputDir:        c.OutputDir,
		OutputDirMissing: c.OutputDir == "",
	}
}

// GetSyncScriptPair returns local+server content for the diff viewer.
func (a *App) GetSyncScriptPair(kind, key string) (sync.ScriptPair, error) {
	if eng := a.currentSyncEngine(); eng != nil {
		return eng.GetScriptPair(kind, key)
	}
	return sync.ScriptPair{}, nil
}

// ResolveConflict applies the user's choice ("local"|"server") for a review item.
func (a *App) ResolveConflict(kind, key, choice, mergeContent string) error {
	if eng := a.currentSyncEngine(); eng != nil {
		return eng.ResolveConflict(kind, key, choice, mergeContent)
	}
	return nil
}

// PauseSync pauses reconcile for an hour (current server).
func (a *App) PauseSync() error {
	until := time.Now().Add(time.Hour).Unix()
	a.syncCfgMu.Lock()
	a.ensureSyncCfgsLoaded()
	srv := a.currentSyncServer()
	c := a.syncCfgs[srv]
	c.PauseUntil = until
	a.syncCfgs[srv] = c
	a.persistSyncCfgsLocked()
	a.syncCfgMu.Unlock()
	if eng := a.currentSyncEngine(); eng != nil {
		eng.SetPaused(until) // lightweight: no watcher restart
	}
	return nil
}

// ResumeSync clears the pause (current server).
func (a *App) ResumeSync() error {
	a.syncCfgMu.Lock()
	a.ensureSyncCfgsLoaded()
	srv := a.currentSyncServer()
	c := a.syncCfgs[srv]
	c.PauseUntil = 0
	a.syncCfgs[srv] = c
	a.persistSyncCfgsLocked()
	a.syncCfgMu.Unlock()
	if eng := a.currentSyncEngine(); eng != nil {
		eng.SetPaused(0)
	}
	return nil
}

// OpenSyncReview opens (or focuses) the Sync Review window. Singleton.
func (a *App) OpenSyncReview() {
	a.syncReviewMu.Lock()
	defer a.syncReviewMu.Unlock()
	if a.syncReviewWindow != nil {
		a.syncReviewWindow.Show()
		a.syncReviewWindow.Focus()
		return
	}
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "syncreview",
		Title:            "Sync Review",
		URL:              "/#sync",
		Width:            960,
		Height:           660,
		MinWidth:         460,
		MinHeight:        360,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.syncReviewWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.syncReviewMu.Lock()
		a.syncReviewWindow = nil
		a.syncReviewMu.Unlock()
	})
}
