package main

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"graal-rc/internal/sqlite"
	"graal-rc/rclib"
)

// closeSessionWindows closes every secondary window whose content can refer to
// the active server. The main window is deliberately kept alive because it is
// the login surface shown after Logout. Window metadata and server-scoped
// caches are cleared before Close is called, so dirty-editor hooks cannot veto
// the context teardown and a fast reconnect cannot reuse old state.
func (a *App) closeSessionWindows() {
	if a == nil {
		return
	}

	a.sessionWindowsMu.Lock()
	defer a.sessionWindowsMu.Unlock()

	windows := make([]*application.WebviewWindow, 0, 16)
	seen := make(map[*application.WebviewWindow]struct{})
	add := func(window *application.WebviewWindow) {
		if window == nil {
			return
		}
		if _, exists := seen[window]; exists {
			return
		}
		seen[window] = struct{}{}
		windows = append(windows, window)
	}

	// Singleton secondary windows.
	a.playerListMu.Lock()
	add(a.playerListWindow)
	a.playerListWindow = nil
	a.playerListMu.Unlock()

	a.scriptMgrMu.Lock()
	add(a.scriptMgrWindow)
	a.scriptMgrWindow = nil
	a.scriptMgrMu.Unlock()

	a.settingsMu.Lock()
	add(a.settingsWindow)
	a.settingsWindow = nil
	a.settingsMu.Unlock()

	a.pluginWindowMu.Lock()
	add(a.pluginWindow)
	a.pluginWindow = nil
	a.pluginWindowMu.Unlock()

	a.pluginDocsWindowMu.Lock()
	add(a.pluginDocsWindow)
	a.pluginDocsWindow = nil
	a.pluginDocsWindowMu.Unlock()

	a.fileBrowserMu.Lock()
	add(a.fileBrowserWindow)
	a.fileBrowserWindow = nil
	a.fileBrowserMu.Unlock()

	a.syncReviewMu.Lock()
	add(a.syncReviewWindow)
	a.syncReviewWindow = nil
	a.syncReviewMu.Unlock()

	a.deploymentMu.Lock()
	add(a.deploymentWindow)
	a.deploymentWindow = nil
	a.deploymentMu.Unlock()

	// Per-resource editors and player-management windows.
	a.editorMu.Lock()
	for _, window := range a.editorWindows {
		add(window)
	}
	a.editorWindows = make(map[string]*application.WebviewWindow)
	a.editorMu.Unlock()

	a.playerWindowMu.Lock()
	for _, window := range a.playerWindows {
		add(window)
	}
	a.playerWindows = make(map[string]*application.WebviewWindow)
	a.playerWindowMu.Unlock()

	// Clear dirty flags before closing: Wails close hooks intentionally prompt
	// during normal user-initiated closes, but a server-context teardown must be
	// unconditional so stale editors cannot keep the old server alive visually.
	a.editorCacheMu.Lock()
	a.editorCache = make(map[string]rclib.ScriptReply)
	a.editorDirty = make(map[string]bool)
	a.editorCacheMu.Unlock()

	var sqliteLocals []string
	a.openMu.Lock()
	for _, window := range a.textWindows {
		add(window)
	}
	for _, window := range a.sqliteWindows {
		add(window)
	}
	for _, local := range a.dbFiles {
		if local != "" {
			sqliteLocals = append(sqliteLocals, local)
		}
	}
	a.textWindows = make(map[string]*application.WebviewWindow)
	a.sqliteWindows = make(map[string]*application.WebviewWindow)
	a.textCache = make(map[string][]byte)
	a.textOriginalCache = make(map[string][]byte)
	a.dbFiles = make(map[string]string)
	a.dbHeaders = make(map[string][]byte)
	a.openMu.Unlock()
	for _, local := range sqliteLocals {
		sqlite.Close(local)
	}

	// Plugin-created windows are also tied to the active RC session. Remove
	// their registry entries first to prevent a plugin callback racing teardown
	// from finding and reusing an old native window.
	a.pluginUIWindowMu.Lock()
	for key, state := range a.pluginUIWindows {
		if state != nil {
			add(state.Window)
		}
		delete(a.pluginUIWindows, key)
	}
	a.pluginUIWindows = make(map[string]*pluginUIWindowState)
	a.pluginUIWindowMu.Unlock()

	a.chatLinkMu.Lock()
	for key, window := range a.chatLinkWindows {
		add(window)
		delete(a.chatLinkWindows, key)
	}
	a.chatLinkWindows = make(map[string]*application.WebviewWindow)
	a.chatLinkMu.Unlock()

	for _, window := range windows {
		window.Close()
	}
}
