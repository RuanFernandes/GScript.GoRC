package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	stdsync "sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"graal-rc/internal/fileutil"
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
	if a.sessions == nil {
		return ""
	}
	return a.sessions.Status().ServerName
}

// ensureSyncCfgsLoaded lazily loads the per-server config map from disk.
// Caller holds a.syncCfgMu.
func (a *App) ensureSyncCfgsLoaded() error {
	if a.syncCfgLoaded {
		return nil
	}
	path, err := syncPath()
	if err != nil {
		return err
	}
	configs, err := loadSyncCfgsFromPath(path)
	if err != nil {
		return fmt.Errorf("load sync configuration: %w", err)
	}
	a.syncCfgs = configs
	a.syncCfgLoaded = true
	return nil
}

func loadSyncCfgsFromPath(path string) (map[string]sync.SyncConfig, error) {
	b, ok, err := fileutil.ReadAndRecover(path, 0o600, func(data []byte) error {
		_, err := decodeSyncConfigs(data)
		return err
	})
	if err != nil {
		return nil, err
	}
	if !ok {
		return map[string]sync.SyncConfig{}, nil
	}
	configs, err := decodeSyncConfigs(b)
	if err != nil {
		return nil, err
	}
	return configs, nil
}

func decodeSyncConfigs(data []byte) (map[string]sync.SyncConfig, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("configuration must be a JSON object")
	}
	if servers, ok := raw["servers"]; ok {
		var sf syncFile
		if err := json.Unmarshal(servers, &sf.Servers); err != nil {
			return nil, fmt.Errorf("servers: %w", err)
		}
		if sf.Servers == nil {
			return nil, fmt.Errorf("servers must be a JSON object")
		}
		for k, c := range sf.Servers {
			if c.PollingMinutes < 1 {
				c.PollingMinutes = 5
			}
			sf.Servers[k] = c
		}
		return sf.Servers, nil
	}

	// Back-compat: an older single-config file (a bare SyncConfig JSON) is
	// migrated under the "" key so it is not lost.
	if _, ok := raw["enabled"]; !ok {
		if _, ok := raw["outputDir"]; !ok {
			if _, ok := raw["pollingMinutes"]; !ok {
				return nil, fmt.Errorf("missing servers configuration")
			}
		}
	}
	var single sync.SyncConfig
	if err := json.Unmarshal(data, &single); err != nil {
		return nil, err
	}
	if single.PollingMinutes < 1 {
		single.PollingMinutes = 5
	}
	return map[string]sync.SyncConfig{"": single}, nil
}

// persistSyncCfgsLocked writes the whole server map to disk. Caller holds
// a.syncCfgMu (or call the unlocked wrapper).
func (a *App) persistSyncCfgsLocked() error {
	path, err := syncPath()
	if err != nil {
		return err
	}
	b, err := json.MarshalIndent(syncFile{Servers: a.syncCfgs}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return fileutil.AtomicWriteFileWithValidator(path, b, 0o600, func(current []byte) error {
		_, err := decodeSyncConfigs(current)
		return err
	})
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
// The legacy binding keeps its no-error return shape; callers that need to
// distinguish an unavailable/corrupt file should use GetSyncConfigWithError.
func (a *App) GetSyncConfig() sync.SyncConfig {
	cfg, err := a.getSyncConfig()
	if err != nil {
		log.Printf("sync config: %v", err)
		return sync.DefaultSyncConfig()
	}
	return cfg
}

// GetSyncConfigWithError exposes persistence failures without breaking the
// existing frontend binding contract of GetSyncConfig.
func (a *App) GetSyncConfigWithError() (sync.SyncConfig, error) {
	return a.getSyncConfig()
}

func (a *App) getSyncConfig() (sync.SyncConfig, error) {
	a.syncCfgMu.Lock()
	defer a.syncCfgMu.Unlock()
	if err := a.ensureSyncCfgsLoaded(); err != nil {
		return sync.SyncConfig{}, err
	}
	srv := a.currentSyncServer()
	if c, ok := a.syncCfgs[srv]; ok {
		return normalizeSyncCfg(c), nil
	}
	return sync.DefaultSyncConfig(), nil
}

// SetSyncConfig persists the CURRENT server's sync config, applies it live to a
// running engine, and broadcasts it so the Settings window updates.
func (a *App) SetSyncConfig(enabled bool, outputDir string, pollingMinutes int, autoPush, autoPull bool) error {
	if err := ensureAppRunning(a); err != nil {
		return err
	}
	if pollingMinutes < 1 {
		pollingMinutes = 5
	}
	a.syncCfgMu.Lock()
	if err := a.ensureSyncCfgsLoaded(); err != nil {
		a.syncCfgMu.Unlock()
		return fmt.Errorf("load sync configuration: %w", err)
	}
	srv := a.currentSyncServer()
	prev, hadPrev := a.syncCfgs[srv]
	cfg := normalizeSyncCfg(sync.SyncConfig{
		Enabled:        enabled,
		OutputDir:      outputDir,
		PollingMinutes: pollingMinutes,
		AutoPushLocal:  autoPush,
		AutoPullServer: autoPull,
		PauseUntil:     prev.PauseUntil,
	})
	a.syncCfgs[srv] = cfg
	if err := a.persistSyncCfgsLocked(); err != nil {
		if hadPrev {
			a.syncCfgs[srv] = prev
		} else {
			delete(a.syncCfgs, srv)
		}
		a.syncCfgMu.Unlock()
		return fmt.Errorf("save sync configuration: %w", err)
	}
	a.syncCfgMu.Unlock()

	// Apply live (only if the running engine belongs to this server).
	wasEnabled := prev.Enabled && prev.OutputDir != ""
	nowEnabled := cfg.Enabled && cfg.OutputDir != ""
	if a.graalScriptLSP != nil {
		a.graalScriptLSP.SetEnabled(nowEnabled)
	}
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
		if appIsShuttingDown(a) {
			return
		}
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
		a.emitPluginEvent(normalizePluginEvent(name), data...)
	}
}

func (a *App) refreshGraalScriptWorkspace() {
	if appIsShuttingDown(a) || a.graalScriptLSP == nil {
		return
	}
	cfg, err := a.getSyncConfig()
	if err != nil {
		log.Printf("sync config for workspace refresh: %v", err)
		return
	}
	if !cfg.Enabled || cfg.OutputDir == "" {
		return
	}
	if err := a.graalScriptLSP.RefreshWorkspace(); err != nil {
		log.Printf("graalscript LSP workspace refresh: %v", err)
	}
}

var syncEngineTransitionMu stdsync.Mutex

// startSyncEngine constructs + starts the engine for the CURRENT server. The
// transition mutex serializes start/apply/stop operations, while the engine
// mutex is held only long enough to swap pointers. Engine.Stop can wait for
// network work, so it is never called while syncEngineMu is held.
func (a *App) startSyncEngine() {
	syncEngineTransitionMu.Lock()
	defer syncEngineTransitionMu.Unlock()
	if appIsShuttingDown(a) {
		return
	}

	a.stopSyncEngineLocked()
	srv := a.currentSyncServer()
	cfg, err := a.getSyncConfig()
	if err != nil {
		log.Printf("start sync engine: %v", err)
		return
	}
	// Keep a disabled server from retaining an inert engine. Apart from making
	// status truthful, this lets the Enable Sync toggle start a fresh engine
	// when the user enables it later in the session.
	if !cfg.Enabled || cfg.OutputDir == "" {
		if a.graalScriptLSP != nil {
			a.graalScriptLSP.SetEnabled(false)
		}
		if a.app != nil {
			if b, marshalErr := json.Marshal(cfg); marshalErr == nil {
				a.app.Event.Emit("rc:syncConfig", string(b))
			}
		}
		return
	}
	if a.graalScriptLSP != nil {
		a.graalScriptLSP.SetEnabled(true)
	}
	parent := context.Background()
	if a.app != nil {
		parent = a.app.Context()
	}
	ctx, cancel := context.WithCancel(parent)
	eng := sync.NewEngine(a.sessions, srv, a.syncEmitter())
	eng.SetEditorChecker(func(kind, key string) bool {
		_, open := a.editorWindowsSnapshot(kind, key)
		return open
	})
	eng.SetLocalActor(func() string {
		if a.sessions == nil {
			return ""
		}
		return a.sessions.Status().Nickname
	})

	a.syncEngineMu.Lock()
	a.syncEngine = eng
	a.syncCtx = ctx
	a.syncCancel = cancel
	a.syncEngineMu.Unlock()

	eng.ApplyConfig(cfg)
	eng.Start(ctx)
	if a.app != nil {
		if b, marshalErr := json.Marshal(cfg); marshalErr == nil {
			a.app.Event.Emit("rc:syncConfig", string(b))
		}
	}
}

// stopSyncEngine tears the engine down.
func (a *App) stopSyncEngine() {
	syncEngineTransitionMu.Lock()
	defer syncEngineTransitionMu.Unlock()
	a.stopSyncEngineLocked()
}

func (a *App) stopSyncEngineLocked() {
	if a.graalScriptLSP != nil {
		a.graalScriptLSP.SetEnabled(false)
	}
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
	syncEngineTransitionMu.Lock()
	defer syncEngineTransitionMu.Unlock()
	if appIsShuttingDown(a) {
		return
	}
	if eng := a.currentSyncEngine(); eng != nil {
		eng.ApplyConfig(cfg)
	}
}

// --- Bound methods exposed to the frontend ---

// SyncNow triggers a full reconcile immediately.
func (a *App) SyncNow() error {
	if err := ensureAppRunning(a); err != nil {
		return err
	}
	eng := a.currentSyncEngine()
	if eng == nil {
		// Auto-start if configured + enabled.
		cfg, err := a.getSyncConfig()
		if err != nil {
			return fmt.Errorf("load sync configuration: %w", err)
		}
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
	if err := ensureAppRunning(a); err != nil {
		return err
	}
	until := time.Now().Add(time.Hour).Unix()
	a.syncCfgMu.Lock()
	if err := a.ensureSyncCfgsLoaded(); err != nil {
		a.syncCfgMu.Unlock()
		return fmt.Errorf("load sync configuration: %w", err)
	}
	srv := a.currentSyncServer()
	previous, hadPrevious := a.syncCfgs[srv]
	c := previous
	c.PauseUntil = until
	a.syncCfgs[srv] = c
	if err := a.persistSyncCfgsLocked(); err != nil {
		if hadPrevious {
			a.syncCfgs[srv] = previous
		} else {
			delete(a.syncCfgs, srv)
		}
		a.syncCfgMu.Unlock()
		return fmt.Errorf("save sync pause: %w", err)
	}
	a.syncCfgMu.Unlock()
	if eng := a.currentSyncEngine(); eng != nil {
		eng.SetPaused(until) // lightweight: no watcher restart
	}
	return nil
}

// ResumeSync clears the pause (current server).
func (a *App) ResumeSync() error {
	if err := ensureAppRunning(a); err != nil {
		return err
	}
	a.syncCfgMu.Lock()
	if err := a.ensureSyncCfgsLoaded(); err != nil {
		a.syncCfgMu.Unlock()
		return fmt.Errorf("load sync configuration: %w", err)
	}
	srv := a.currentSyncServer()
	previous, hadPrevious := a.syncCfgs[srv]
	c := previous
	c.PauseUntil = 0
	a.syncCfgs[srv] = c
	if err := a.persistSyncCfgsLocked(); err != nil {
		if hadPrevious {
			a.syncCfgs[srv] = previous
		} else {
			delete(a.syncCfgs, srv)
		}
		a.syncCfgMu.Unlock()
		return fmt.Errorf("save sync resume: %w", err)
	}
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
