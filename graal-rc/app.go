package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	auditlib "graal-rc/internal/audit"
	"graal-rc/internal/connection"
	"graal-rc/internal/credentials"
	deploylib "graal-rc/internal/deploy"
	gallerylib "graal-rc/internal/gallery"
	"graal-rc/internal/graalscript"
	pluginlib "graal-rc/internal/plugins"
	"graal-rc/internal/sqlite"
	synclib "graal-rc/internal/sync"
	"graal-rc/rclib"
)

var (
	errNoVault            = errors.New("account vault is not available")
	errAccountNotFound    = errors.New("account not found")
	errNicknameRequired   = errors.New("session nickname is required")
	errScriptSyncRequired = errors.New("script sync is required before opening editable scripts")
)

// App is the Wails v3 service: its public methods are auto-bound to the
// frontend. It delegates session logic to the connection Service and account
// storage to the credentials Vault (Single Responsibility).
type App struct {
	app       *application.App
	sessions  *connection.Service
	vault     *credentials.Vault
	mcpServer *http.Server

	// mainWindow is the account/server/RC window; hidden to the tray instead of
	// quit when a server session is active. quitting bypasses the hide hook.
	mainWindow *application.WebviewWindow
	quitting   atomic.Bool

	// tray is the system-tray handle; its tooltip is branded with the connected
	// server name + live player count by refreshServerChrome.
	tray            *application.SystemTray
	pmMu            sync.RWMutex
	pmConversations map[int]PMConversation

	// sessionWindowsMu serializes teardown triggered by an explicit logout and
	// by an asynchronous server-disconnect callback.
	sessionWindowsMu sync.Mutex

	logMu      sync.Mutex
	logEnabled bool
	logDir     string

	// PM log config (separate from chat log). When enabled, each PM (in/out) is
	// appended to {pmLogDir}/{server}/PM_{otherAccount}_Log.txt.
	pmLogEnabled bool
	pmLogDir     string

	playerListMu     sync.Mutex
	playerListWindow *application.WebviewWindow

	// pmWindows keeps one independent frameless window per player conversation.
	// The PM state itself remains shared so every window receives the same live
	// updates without coupling its lifecycle to the player list window.
	pmWindowMu sync.Mutex
	pmWindows  map[int]*application.WebviewWindow

	scriptMgrMu     sync.Mutex
	scriptMgrWindow *application.WebviewWindow

	settingsMu     sync.Mutex
	settingsWindow *application.WebviewWindow

	pluginWindowMu sync.Mutex
	pluginWindow   *application.WebviewWindow

	pluginDocsWindowMu sync.Mutex
	pluginDocsWindow   *application.WebviewWindow

	chatLinkMu      sync.Mutex
	chatLinkWindows map[string]*application.WebviewWindow
	chatLinkSeq     uint64

	// Script Gallery authentication is deliberately native-only. The token is
	// held in memory for the current RC session and is never exposed to React
	// or persisted alongside game credentials.
	galleryMu             sync.Mutex
	galleryClient         *gallerylib.Client
	gallerySessionStore   *gallerylib.SessionStore
	galleryToken          string
	gallerySubject        string
	galleryUser           gallerylib.User
	galleryTokenExpiresAt int64
	gallerySessionVersion uint64

	pluginFileOpenMu      sync.Mutex
	pluginFileOpenWaiters map[string]pluginFileOpenRequest
	pluginFileOpenSeq     uint64

	pluginMonacoMu      sync.Mutex
	pluginMonacoWaiters map[string]pluginMonacoRequest
	pluginMonacoSeq     uint64

	pluginMonacoLanguagesMu sync.RWMutex
	pluginMonacoLanguages   map[string]PluginMonacoLanguage

	pluginUIWindowMu sync.Mutex
	pluginUIWindows  map[string]*pluginUIWindowState

	fileBrowserMu     sync.Mutex
	fileBrowserWindow *application.WebviewWindow

	editorMu      sync.Mutex
	editorWindows map[string]*application.WebviewWindow

	// playerWindows holds the /openrights, /open, /openaccess editor windows,
	// keyed "<kind>:<account>" so reopening focuses the existing window. Unlike
	// the singleton windows, several accounts can be open at once.
	playerWindowMu sync.Mutex
	playerWindows  map[string]*application.WebviewWindow

	editorCacheMu sync.Mutex
	editorCache   map[string]rclib.ScriptReply
	editorDirty   map[string]bool

	codingMu           sync.Mutex
	codingSettings     CodingSettings
	chatSettingsMu     sync.Mutex
	chatSettings       ChatSettings
	chatSettingsLoaded bool
	chatSettingsExists bool
	appThemeMu         sync.Mutex

	graalScriptLSP *graalscript.LanguageServer
	plugins        *pluginlib.Manager
	audit          *auditlib.Store
	backups        *deploylib.Store
	backupPolicyMu sync.Mutex
	retention      *changeRetentionStore

	languageMu sync.Mutex
	language   string

	fileBrowserCfgMu     sync.Mutex
	fileBrowserCfg       FileBrowserConfig
	fileBrowserCfgLoaded bool

	commandMacrosMu sync.Mutex

	// Local Sync engine + its persisted config. Config is PER-SERVER (keyed by
	// server name) so each server keeps its own output folder + settings. The
	// engine is (re)started after a server connect (once NC comes up) and stopped
	// on logout / server switch.
	syncCfgMu         sync.Mutex
	syncCfgs          map[string]synclib.SyncConfig
	syncCfgLoaded     bool
	syncEngineMu      sync.Mutex
	syncEngine        *synclib.Engine
	syncCtx           context.Context
	syncCancel        context.CancelFunc
	syncLSPMu         sync.Mutex
	syncLSPGeneration uint64
	syncReviewMu      sync.Mutex
	syncReviewWindow  *application.WebviewWindow

	deploymentMu     sync.Mutex
	deploymentWindow *application.WebviewWindow

	// Opened-file caches for the type-aware open behavior (double-click a file):
	// textCache holds .txt content for the editor window; dbFiles maps a remote
	// .db path to its local cache file (operated on by the SQLite explorer);
	// dbHeaders keeps the original 100-byte header for version preservation on
	// save. textWindows/sqliteWindows track one window per remote path.
	openMu            sync.Mutex
	textCache         map[string][]byte
	textOriginalCache map[string][]byte
	dbFiles           map[string]string
	dbHeaders         map[string][]byte
	textWindows       map[string]*application.WebviewWindow
	sqliteWindows     map[string]*application.WebviewWindow

	// externalFiles keeps the stable local copies opened through the operating
	// system for editable remote .nw/.gmap files. The watcher uses this map to
	// upload local changes back to the same remote path until the server session
	// is closed or changed.
	externalFilesMu            sync.Mutex
	externalFiles              map[string]externalFileSession
	externalFileSequence       uint64
	externalFileWatcherStarted bool
}

// NewApp creates a new App with a fresh connection service and an encrypted
// account vault. If a legacy single-credential file exists, it is imported into
// the vault and removed.
func NewApp() *App {
	vault, err := credentials.NewVault()
	if err != nil {
		log.Printf("credentials vault: %v", err)
	} else {
		migrateLegacyCredentials(vault)
	}
	gallerySessionStore, gallerySessionErr := gallerylib.NewSessionStore()
	if gallerySessionErr != nil {
		log.Printf("Script Gallery session store: %v", gallerySessionErr)
	}
	lsp := graalscript.NewLanguageServer()
	// The embedded server is usable as a standalone package in tests, but the
	// desktop integration starts locked until the current server has a local
	// Sync workspace configured.
	lsp.SetEnabled(false)
	pluginManager, pluginErr := pluginlib.NewManager()
	if pluginErr != nil {
		log.Printf("plugins: %v", pluginErr)
	}
	auditStore, backupStore, retentionStore := newLocalChangeStores()
	app := &App{
		sessions:              connection.NewService(),
		vault:                 vault,
		gallerySessionStore:   gallerySessionStore,
		editorWindows:         map[string]*application.WebviewWindow{},
		editorCache:           map[string]rclib.ScriptReply{},
		editorDirty:           map[string]bool{},
		textCache:             map[string][]byte{},
		textOriginalCache:     map[string][]byte{},
		dbFiles:               map[string]string{},
		dbHeaders:             map[string][]byte{},
		textWindows:           map[string]*application.WebviewWindow{},
		sqliteWindows:         map[string]*application.WebviewWindow{},
		externalFiles:         map[string]externalFileSession{},
		pluginFileOpenWaiters: map[string]pluginFileOpenRequest{},
		pluginMonacoWaiters:   map[string]pluginMonacoRequest{},
		pluginMonacoLanguages: map[string]PluginMonacoLanguage{},
		pluginUIWindows:       map[string]*pluginUIWindowState{},
		playerWindows:         map[string]*application.WebviewWindow{},
		pmWindows:             map[int]*application.WebviewWindow{},
		pmConversations:       map[int]PMConversation{},
		graalScriptLSP:        lsp,
		plugins:               pluginManager,
		audit:                 auditStore,
		backups:               backupStore,
		retention:             retentionStore,
	}
	if retentionStore != nil {
		settings, retentionErr := retentionStore.Load()
		if retentionErr != nil {
			log.Printf("change retention: %v", retentionErr)
		} else if cleanupErr := app.cleanupExpiredChangeData(settings); cleanupErr != nil {
			log.Printf("change retention cleanup: %v", cleanupErr)
		}
	}
	return app
}

// newWebviewWindow keeps secondary RC windows on the monitor that currently
// contains the main window. If the monitor cannot be resolved, Wails keeps its
// default placement behavior.
func (a *App) newWebviewWindow(options application.WebviewWindowOptions) *application.WebviewWindow {
	options = hardenedWebviewWindowOptions(options)
	if a.mainWindow != nil {
		if screen, err := a.mainWindow.GetScreen(); err == nil && screen != nil {
			options.Screen = screen
			options.InitialPosition = application.WindowCentered
		}
	}
	return a.app.Window.NewWithOptions(options)
}

// hardenedWebviewWindowOptions keeps release windows from exposing the
// browser inspector or its native context menu. The production Wails build
// also compiles out the platform devtools handlers; setting the options here
// protects every secondary window from accidentally opting back in.
func hardenedWebviewWindowOptions(options application.WebviewWindowOptions) application.WebviewWindowOptions {
	options.DevToolsEnabled = false
	options.DefaultContextMenuDisabled = true
	options.OpenInspectorOnStartup = false
	return options
}

func (a *App) dialogParentWindow() application.Window {
	if a.mainWindow == nil {
		return nil
	}
	return a.mainWindow
}

// attach wires the v3 application handle and the event emitter (grclib
// callbacks → app.Event.Emit) once application.New has returned. Unexported so
// it is not exposed to the frontend as a binding.
//
// Every grclib event is wrapped into a single uniform "rc:evt" event carrying a
// monotonic sequence number plus the original name/data. Wails v3 dispatches
// each app.Event.Emit to the webview on its own goroutine, so rapid emissions
// race and can arrive out of order; the frontend holds a reorder buffer keyed
// by seq and applies events strictly in seq order, restoring deterministic
// ordering (the chat burst otherwise scrambles).
func (a *App) attach(app *application.App) {
	a.app = app
	a.startMCPServer()
	if a.plugins != nil {
		a.plugins.SetRuntimeEmitter(func(name string, data any) {
			payload, err := json.Marshal(data)
			if err != nil {
				return
			}
			app.Event.Emit(name, string(payload))
		})
	}
	var seq uint64
	a.sessions.SetEmitter(func(name string, data ...any) {
		// A disconnect can arrive without a user clicking the logout button. Tear
		// down every server-scoped window before forwarding the event so the login
		// screen can never coexist with stale editor/file-browser context.
		if name == "rc:disconnected" || name == "rc:pumpError" {
			a.closeSessionWindows()
			if name == "rc:pumpError" {
				// Stop server-scoped sync work independently of the frontend. The
				// native pump can fail while a secondary window is open or while
				// the webview is still transitioning, and no background worker
				// should keep issuing requests against that handle.
				go a.stopSyncEngine()
			}
		}
		if name == "rc:pm" && len(data) >= 4 {
			id, idOK := data[0].(int)
			account, accountOK := data[1].(string)
			nick, nickOK := data[2].(string)
			message, messageOK := data[3].(string)
			if idOK && accountOK && nickOK && messageOK {
				// PM payloads use Graal's comma-text encoding for multiline
				// messages. Normalize before recording and forwarding the event so
				// the UI, plugins and local history all see the same text.
				message = normalizePMText(message)
				data[3] = message
				a.recordIncomingPM(id, account, nick, message)
			}
		}
		// Tap RC chat lines + *Changed push events into the sync engine so
		// server-side script activity drives a targeted/debounced reconcile.
		if eng := a.currentSyncEngine(); eng != nil {
			switch name {
			case "rc:message":
				if len(data) > 0 {
					if text, ok := data[0].(string); ok {
						// The RC message callback runs on the main socket pump. Chat
						// activity reconciliation performs a second request to fetch
						// the changed script, so it must not run inline here or it can
						// deadlock the pump and prevent the chat event from being
						// delivered. HandleChatLine serializes concurrent work itself.
						go eng.HandleChatLine(text)
					}
				}
			}
		}
		s := atomic.AddUint64(&seq, 1)
		payload := struct {
			Seq  uint64 `json:"seq"`
			Name string `json:"name"`
			Data []any  `json:"data"`
		}{Seq: s, Name: name, Data: data}
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		app.Event.Emit("rc:evt", string(b))
		if a.plugins != nil {
			pluginPayload := struct {
				Name string `json:"name"`
				Data []any  `json:"data"`
			}{Name: normalizePluginEvent(name), Data: data}
			if pluginBytes, err := json.Marshal(pluginPayload); err == nil {
				app.Event.Emit("plugin:event", string(pluginBytes))
			}
		}
	})
}

func normalizePluginEvent(name string) string {
	name = strings.TrimPrefix(name, "rc:")
	known := map[string]string{
		"connected": "rc.connected", "pm": "pm.received", "pmSent": "pm.sent", "irc": "irc.message", "message": "rc.message",
		"disconnected": "rc.disconnected", "weaponsChanged": "weapon.changed",
		"classesChanged": "class.changed", "npcsChanged": "npc.changed",
		"scriptReceived": "script.received", "npcFlags": "npc.flags",
		"npcAttributes": "npc.attributes", "fbFolders": "filebrowser.folders",
		"fbFiles": "filebrowser.files", "fbMessage": "filebrowser.message",
		"playerRights": "player.rights", "playerAttributes": "player.attributes",
		"banData": "player.ban", "banListData": "player.banList",
		"playerTextData": "player.text",
		"serverdata":     "nc.serverdata", "ncConnected": "nc.connected", "ncDisconnected": "nc.disconnected",
		"fbChanged": "filebrowser.changed", "fbStart": "filebrowser.started", "fbCd": "filebrowser.directory.changed",
		"fbReset":      "filebrowser.reset",
		"syncConflict": "script.conflict", "syncProgress": "sync.progress", "syncStatus": "sync.status",
		"channels": "irc.channels", "scriptIdentityChanged": "script.identity.changed", "scriptPermissionsChanged": "script.permissions.changed",
		"fbMaxUpload": "filebrowser.maxUpload",
	}
	if mapped, ok := known[name]; ok {
		return mapped
	}
	return strings.ReplaceAll(name, "_", ".")
}

func (a *App) emitPluginEvent(name string, data ...any) {
	if a.app == nil {
		return
	}
	payload := struct {
		Name string `json:"name"`
		Data []any  `json:"data"`
	}{Name: name, Data: data}
	if b, err := json.Marshal(payload); err == nil {
		a.app.Event.Emit("plugin:event", string(b))
	}
}

type PluginHTTPRequest = pluginlib.HTTPRequest
type PluginHTTPResponse = pluginlib.HTTPResponse
type PluginInfo = pluginlib.PluginInfo
type PluginPermissions = pluginlib.Permissions

func pluginIntArg(args []any, index int) (int, error) {
	if index < 0 || index >= len(args) {
		return 0, errors.New("missing plugin argument")
	}
	switch value := args[index].(type) {
	case int:
		return value, nil
	case float64:
		return int(value), nil
	default:
		return 0, errors.New("plugin argument must be an integer")
	}
}

func pluginStringArg(args []any, index int) (string, error) {
	if index < 0 || index >= len(args) {
		return "", errors.New("missing plugin argument")
	}
	value, ok := args[index].(string)
	if !ok {
		return "", errors.New("plugin argument must be a string")
	}
	return value, nil
}

func pluginJSONArg(args []any, index int, target any) error {
	if index < 0 || index >= len(args) {
		return errors.New("missing plugin argument")
	}
	b, err := json.Marshal(args[index])
	if err != nil {
		return errors.New("plugin argument is not valid JSON")
	}
	if err := json.Unmarshal(b, target); err != nil {
		return errors.New("plugin argument has an invalid shape")
	}
	return nil
}

func (a *App) GetPlugins() []PluginInfo {
	if a.plugins == nil {
		return []PluginInfo{}
	}
	return a.plugins.List()
}

func (a *App) RefreshPlugins() error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.Discover(); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("plugin:list")
	}
	return nil
}

func (a *App) SetPluginEnabled(id string, enabled bool) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.SetEnabled(id, enabled); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("plugin:list")
	}
	return nil
}

// RecordPluginFailure is used by the frontend sandbox supervisor to update
// the host-side circuit breaker. It is intentionally not part of the plugin
// SDK; only the trusted runtime calls it.
func (a *App) RecordPluginFailure(id string) (bool, error) {
	if a.plugins == nil {
		return false, errors.New("plugin manager is unavailable")
	}
	disabled, err := a.plugins.RecordPluginFailure(id)
	if err != nil {
		return false, err
	}
	if a.app != nil {
		a.app.Event.Emit("plugin:list")
	}
	return disabled, nil
}

// RecordPluginSuccess resets the trusted runtime circuit breaker after a
// plugin has completed initialization successfully.
func (a *App) RecordPluginSuccess(id string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RecordPluginSuccess(id); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("plugin:list")
	}
	return nil
}

func (a *App) ApprovePluginPermissions(id string, permissions PluginPermissions) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.Approve(id, permissions); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("plugin:list")
	}
	return nil
}

func (a *App) RemovePlugin(id string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.Remove(id); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("plugin:list")
	}
	return nil
}

func (a *App) GetPluginDirectory() string {
	if a.plugins == nil {
		return ""
	}
	return a.plugins.Root()
}

func (a *App) OpenPluginsFolder() error {
	directory := a.GetPluginDirectory()
	if directory == "" {
		return errors.New("plugin directory is unavailable")
	}
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer.exe", directory).Start()
	case "darwin":
		return exec.Command("open", directory).Start()
	default:
		return exec.Command("xdg-open", directory).Start()
	}
}

func (a *App) CreatePluginTemplate(name string) (PluginInfo, error) {
	if a.plugins == nil {
		return PluginInfo{}, errors.New("plugin manager is unavailable")
	}
	info, err := a.plugins.CreateTemplate(name)
	if err == nil && a.app != nil {
		a.app.Event.Emit("plugin:list")
	}
	return info, err
}

func (a *App) GetPluginFiles(id string) ([]string, error) {
	if a.plugins == nil {
		return nil, errors.New("plugin manager is unavailable")
	}
	return a.plugins.ListFiles(id)
}

func (a *App) ReadPluginFile(id, path string) (pluginlib.PluginFile, error) {
	if a.plugins == nil {
		return pluginlib.PluginFile{}, errors.New("plugin manager is unavailable")
	}
	return a.plugins.ReadFile(id, path)
}

func (a *App) WritePluginFile(id, path, content string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.WriteFile(id, path, content); err != nil {
		return err
	}
	a.plugins.Log(id, "info", "Saved "+path)
	if a.app != nil {
		a.app.Event.Emit("plugin:list")
	}
	return nil
}

func (a *App) BuildPlugin(id string) (pluginlib.PluginBuildResult, error) {
	if a.plugins == nil {
		return pluginlib.PluginBuildResult{}, errors.New("plugin manager is unavailable")
	}
	result, err := a.plugins.Build(id)
	if err == nil && a.app != nil {
		if result.Success {
			a.app.Event.Emit("plugin:reload", id)
		}
		a.app.Event.Emit("plugin:list")
	}
	return result, err
}

func (a *App) ReloadPlugin(id string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if _, err := a.plugins.Bundle(id); err != nil {
		return err
	}
	a.plugins.Log(id, "info", "Plugin reload requested")
	if a.app != nil {
		a.app.Event.Emit("plugin:reload", id)
	}
	return nil
}

func (a *App) GetPluginLogs(id string) []pluginlib.PluginLogEntry {
	if a.plugins == nil {
		return []pluginlib.PluginLogEntry{}
	}
	return a.plugins.Logs(id)
}

func (a *App) ClearPluginLogs(id string) {
	if a.plugins == nil {
		return
	}
	a.plugins.ClearLogs(id)
	if a.app != nil {
		a.app.Event.Emit("plugin:logs", id)
	}
}

func (a *App) AppendPluginLog(id, level, message string) {
	if a.plugins == nil {
		return
	}
	a.plugins.Log(id, level, message)
	if a.app != nil {
		a.app.Event.Emit("plugin:logs", id)
	}
}

func (a *App) ExportPlugin(id string) (string, error) {
	if a.plugins == nil {
		return "", errors.New("plugin manager is unavailable")
	}
	info, ok := func() (pluginlib.PluginInfo, bool) {
		for _, item := range a.plugins.List() {
			if item.Manifest.ID == id {
				return item, true
			}
		}
		return pluginlib.PluginInfo{}, false
	}()
	if !ok {
		return "", pluginlib.ErrPluginNotFound
	}
	path, err := a.app.Dialog.SaveFile().AttachToWindow(a.dialogParentWindow()).SetMessage("Export " + info.Manifest.Name).SetFilename(info.Manifest.ID + ".zip").PromptForSingleSelection()
	if err != nil || path == "" {
		return path, err
	}
	return a.plugins.Export(id, path)
}

func (a *App) GetPluginBundle(id string) (string, error) {
	if a.plugins == nil {
		return "", errors.New("plugin manager is unavailable")
	}
	return a.plugins.Bundle(id)
}

func (a *App) PluginStorageGet(id, key string) (string, bool, error) {
	if a.plugins == nil {
		return "", false, errors.New("plugin manager is unavailable")
	}
	return a.plugins.StorageGet(id, key)
}

func (a *App) PluginStorageSet(id, key, value string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	return a.plugins.StorageSet(id, key, value)
}

func (a *App) PluginStorageDelete(id, key string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	return a.plugins.StorageDelete(id, key)
}

func (a *App) PluginSecretGet(id, key string) (string, bool, error) {
	if a.plugins == nil {
		return "", false, errors.New("plugin manager is unavailable")
	}
	return a.plugins.SecretGet(id, key)
}

func (a *App) PluginSecretSet(id, key, value string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	return a.plugins.SecretSet(id, key, value)
}

func (a *App) PluginSecretDelete(id, key string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	return a.plugins.SecretDelete(id, key)
}

// PluginCall is the allowlisted write/action bridge. It intentionally accepts
// a narrow method name and JSON-like arguments instead of exposing Service or
// the generated Wails bindings to plugin code.
func (a *App) PluginCall(id, method string, args []any) (any, error) {
	if a.plugins == nil {
		return nil, errors.New("plugin manager is unavailable")
	}
	api := method
	if strings.HasPrefix(method, "actions.") {
		api = strings.TrimPrefix(method, "actions.")
	}
	if strings.HasPrefix(method, "pm.") {
		api = "pm.send"
	}
	if strings.HasPrefix(method, "admin.") {
		api = "admin.send"
	}
	if strings.HasPrefix(method, "nc.") {
		api = method
	}
	if strings.HasPrefix(method, "sockets.") {
		api = "network.socket"
	}
	if strings.HasPrefix(method, "plugins.") {
		api = "plugins.messaging"
	}
	if strings.HasPrefix(method, "express.") {
		api = "express.http"
	}
	if strings.HasPrefix(method, "filebrowser.read") {
		api = "filebrowser.read"
	}
	if strings.HasPrefix(method, "filebrowser.write") {
		api = "filebrowser.write"
	}
	if strings.HasPrefix(method, "filebrowser.editor") || method == "filebrowser.openResult" {
		api = "filebrowser.editor"
	}
	if strings.HasPrefix(method, "monaco.") {
		api = "monaco"
	}
	if strings.HasPrefix(method, "ui.") {
		api = "ui.window"
	}
	if err := a.plugins.RequireAPI(id, api); err != nil {
		return nil, err
	}
	switch method {
	case "pm.send":
		playerID, err := pluginIntArg(args, 0)
		if err != nil {
			return nil, err
		}
		message, err := pluginStringArg(args, 1)
		if err != nil {
			return nil, err
		}
		return nil, a.sessions.SendPrivateMessage(playerID, message)
	case "admin.send":
		playerID, err := pluginIntArg(args, 0)
		if err != nil {
			return nil, err
		}
		message, err := pluginStringArg(args, 1)
		if err != nil {
			return nil, err
		}
		return nil, a.sessions.SendAdminMessage(playerID, message)
	case "rc.execute":
		message, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, a.sessions.Execute(message)
	case "nc.saveWeapon", "nc.saveClass":
		name, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		script, err := pluginStringArg(args, 1)
		if err != nil {
			return nil, err
		}
		if method == "nc.saveWeapon" {
			return nil, a.sessions.SaveWeapon(name, script)
		}
		return nil, a.sessions.SaveClass(name, script)
	case "nc.saveNPC":
		idArg, err := pluginIntArg(args, 0)
		if err != nil {
			return nil, err
		}
		script, err := pluginStringArg(args, 1)
		if err != nil {
			return nil, err
		}
		return nil, a.sessions.SaveNPC(idArg, script)
	case "nc.saveNPCFlags":
		idArg, err := pluginIntArg(args, 0)
		if err != nil {
			return nil, err
		}
		flags, err := pluginStringArg(args, 1)
		if err != nil {
			return nil, err
		}
		return nil, a.sessions.SaveNPCFlags(idArg, flags)
	case "nc.readWeapon", "nc.readClass":
		name, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		scriptType := "weapon"
		if method == "nc.readClass" {
			scriptType = "class"
		}
		return a.sessions.OpenScript(scriptType, name)
	case "nc.readNPC":
		idArg, err := pluginIntArg(args, 0)
		if err != nil {
			return nil, err
		}
		return a.sessions.OpenScript("npc", strconv.Itoa(idArg))
	case "nc.readNPCFlags", "nc.readNPCAttributes":
		idArg, err := pluginIntArg(args, 0)
		if err != nil {
			return nil, err
		}
		if method == "nc.readNPCFlags" {
			return a.sessions.OpenNPCFlags(idArg)
		}
		return a.sessions.OpenNPCAttributes(idArg)
	case "nc.list":
		return a.sessions.GetScriptLists(true)
	case "nc.createWeapon", "nc.deleteWeapon":
		name, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		if method == "nc.createWeapon" {
			return nil, a.sessions.AddWeapon(name)
		}
		return nil, a.sessions.DeleteWeapon(name)
	case "nc.createClass", "nc.deleteClass":
		name, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		if method == "nc.createClass" {
			return nil, a.sessions.AddClass(name)
		}
		return nil, a.sessions.DeleteClass(name)
	case "nc.createNPC":
		var options struct {
			Name     string `json:"name"`
			ID       int    `json:"id"`
			Type     string `json:"type"`
			Scripter string `json:"scripter"`
			Level    string `json:"level"`
			X        string `json:"x"`
			Y        string `json:"y"`
		}
		if err := pluginJSONArg(args, 0, &options); err != nil {
			return nil, err
		}
		if strings.TrimSpace(options.Name) == "" {
			return nil, errors.New("NPC name is required")
		}
		return nil, a.sessions.CreateNPC(options.Name, options.ID, options.Type, options.Scripter, options.Level, options.X, options.Y)
	case "nc.deleteNPC", "nc.resetNPC":
		idArg, err := pluginIntArg(args, 0)
		if err != nil {
			return nil, err
		}
		if method == "nc.deleteNPC" {
			return nil, a.sessions.DeleteNPC(idArg)
		}
		return nil, a.sessions.ResetNPC(idArg)
	case "sockets.open":
		var request pluginlib.SocketRequest
		if err := pluginJSONArg(args, 0, &request); err != nil {
			return nil, err
		}
		return a.plugins.OpenSocket(id, request)
	case "sockets.send":
		socketID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		data, err := pluginStringArg(args, 1)
		if err != nil {
			return nil, err
		}
		return nil, a.plugins.SendSocket(id, socketID, data)
	case "sockets.close":
		socketID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, a.plugins.CloseSocket(id, socketID)
	case "sockets.closeAll":
		a.plugins.ClosePluginSockets(id)
		return nil, nil
	case "plugins.list":
		return a.plugins.ListPeers(id)
	case "plugins.authorize":
		targetID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, a.plugins.AuthorizePluginMessage(id, targetID)
	case "express.listen":
		return a.plugins.ExpressListen(id)
	case "express.register":
		methodName, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		path, err := pluginStringArg(args, 1)
		if err != nil {
			return nil, err
		}
		return a.plugins.RegisterExpressRoute(id, methodName, path)
	case "express.unregister":
		routeID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, a.plugins.UnregisterExpressRoute(id, routeID)
	case "express.respond":
		requestID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		var response pluginlib.ExpressResponse
		if err := pluginJSONArg(args, 1, &response); err != nil {
			return nil, err
		}
		return nil, a.plugins.RespondExpressRequest(id, requestID, response)
	case "express.close":
		a.plugins.ClosePluginHTTP(id)
		return nil, nil
	case "filebrowser.readText":
		path, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		return a.pluginReadRemoteText(id, path)
	case "filebrowser.writeText":
		var request pluginFileWriteRequest
		if err := pluginJSONArg(args, 0, &request); err != nil {
			return nil, err
		}
		return a.pluginWriteRemoteText(id, request)
	case "filebrowser.editor.register":
		var editor pluginlib.FileEditorRegistration
		if err := pluginJSONArg(args, 0, &editor); err != nil {
			return nil, err
		}
		return nil, a.plugins.RegisterFileEditor(id, editor)
	case "filebrowser.editor.unregister":
		editorID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, a.plugins.UnregisterFileEditor(id, editorID)
	case "filebrowser.editor.closeAll":
		a.plugins.ClosePluginEditors(id)
		return nil, nil
	case "filebrowser.openResult":
		requestID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		handled := false
		if len(args) > 1 {
			if value, ok := args[1].(bool); ok {
				handled = value
			}
		}
		return nil, a.pluginFileOpenResult(id, requestID, handled)
	case "monaco.language.register":
		var language PluginMonacoLanguage
		if err := pluginJSONArg(args, 0, &language); err != nil {
			return nil, err
		}
		if err := a.PluginMonacoLanguageRegister(id, language); err != nil {
			return nil, err
		}
		return nil, nil
	case "monaco.language.unregister":
		languageID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, a.PluginMonacoLanguageUnregister(id, languageID)
	case "monaco.language.closeAll":
		return nil, a.PluginMonacoLanguageCloseAll(id)
	case "monaco.provider.register":
		kind, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		language, err := pluginStringArg(args, 1)
		if err != nil {
			return nil, err
		}
		if kind != "diagnostics" && kind != "completions" {
			return nil, errors.New("unsupported Monaco provider kind")
		}
		if strings.TrimSpace(language) == "" || len(language) > 96 {
			return nil, errors.New("Monaco language is required")
		}
		return nil, nil
	case "monaco.provider.unregister":
		return nil, nil
	case "ui.window.open":
		var options PluginUIWindowOptions
		if err := pluginJSONArg(args, 0, &options); err != nil {
			return nil, err
		}
		return a.PluginUIWindowOpen(id, options)
	case "ui.window.update":
		windowID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		if len(args) < 2 {
			return nil, errors.New("plugin UI view is required")
		}
		return nil, a.PluginUIWindowUpdate(id, windowID, args[1])
	case "ui.window.close":
		windowID, err := pluginStringArg(args, 0)
		if err != nil {
			return nil, err
		}
		return nil, a.PluginUIWindowClose(id, windowID)
	case "ui.window.closeAll":
		return nil, a.PluginUIWindowCloseAll(id)
	default:
		return nil, errors.New("unknown plugin action: " + method)
	}
}

func (a *App) PluginRequest(id string, request PluginHTTPRequest) (PluginHTTPResponse, error) {
	if a.plugins == nil {
		return PluginHTTPResponse{}, errors.New("plugin manager is unavailable")
	}
	return a.plugins.Request(id, request)
}

// migrateLegacyCredentials imports the old plaintext credentials.json (written
// by earlier builds) into the encrypted vault, then deletes the legacy file.
func migrateLegacyCredentials(vault *credentials.Vault) {
	existing, err := vault.Load()
	if err != nil {
		log.Printf("vault load during migration: %v", err)
		return
	}
	if len(existing) > 0 {
		return
	}
	legacy, err := credentials.NewStore()
	if err != nil {
		return
	}
	c, ok, err := legacy.Load()
	if err != nil || !ok {
		return
	}
	if err := vault.Add(credentials.Account{
		ProfileName: profileNameForLegacyNickname(c.Nickname),
		Account:     c.Account,
		Password:    c.Password,
	}); err != nil {
		log.Printf("migrate legacy credentials: %v", err)
		return
	}
	if err := legacy.Clear(); err != nil {
		log.Printf("remove legacy credentials file: %v", err)
	}
}

// LoginRequest is the payload sent from the Add Account screen.
type LoginRequest struct {
	ProfileName string `json:"profileName"`
	Account     string `json:"account"`
	Password    string `json:"password"`
}

// AccountSummary is the password-less account projection exposed to the
// frontend. Aliased so the generated Wails model matches the credentials type.
type AccountSummary = credentials.AccountSummary

// PreagonalListserverHost is the alternate listserver endpoint, selected
// implicitly when an account's nickname is prefixed "Preagonal:". The default
// listserver (listserver.graalonline.com) is used otherwise.
const PreagonalListserverHost = "listserver.graal.in"

// preagonalPrefix marks a nickname as routing to the alternate listserver.
const preagonalPrefix = "Preagonal:"

func profileNameForLegacyNickname(nickname string) string {
	if strings.HasPrefix(nickname, preagonalPrefix) {
		return preagonalPrefix
	}
	return ""
}

// listserverForProfile returns the listserver endpoint for a profile name. A profile name
// prefixed "Preagonal:" selects the alternate endpoint; everything else uses the
// default Graal listserver. The prefix is a hidden, client-only routing key.
func listserverForProfile(profileName string) (host string, port int) {
	if strings.HasPrefix(strings.TrimSpace(profileName), preagonalPrefix) {
		return PreagonalListserverHost, rclib.DefaultListserverPort
	}
	return rclib.DefaultListserverHost, rclib.DefaultListserverPort
}

func toCreds(req LoginRequest) connection.Credentials {
	host, port := listserverForProfile(req.ProfileName)
	return connection.Credentials{Account: req.Account, Password: req.Password, Host: host, Port: port}
}

func accountToCreds(a credentials.Account) connection.Credentials {
	host, port := listserverForProfile(a.ProfileName)
	return connection.Credentials{Account: a.Account, Password: a.Password, Host: host, Port: port}
}

// ListAccounts returns the saved accounts without passwords.
func (a *App) ListAccounts() ([]AccountSummary, error) {
	if a.vault == nil {
		return nil, nil
	}
	accounts, err := a.vault.Load()
	if err != nil {
		return nil, err
	}
	out := make([]AccountSummary, len(accounts))
	for i, acc := range accounts {
		out[i] = acc.Summary()
	}
	return out, nil
}

// LoginWithAccount logs in with a previously saved account (looked up by name),
// reading its password from the vault. The password never crosses to the
// frontend.
func (a *App) LoginWithAccount(accountName, nickname string) ([]rclib.Server, error) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		return nil, errNicknameRequired
	}
	acc, err := a.findAccount(accountName)
	if err != nil {
		return nil, err
	}
	creds := accountToCreds(acc)
	creds.Nickname = nickname
	return a.sessions.Login(creds)
}

// AddAccount logs in with the supplied credentials and, on success, persists
// them to the vault. On failure nothing is saved.
func (a *App) AddAccount(req LoginRequest, nickname string) ([]rclib.Server, error) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" {
		return nil, errNicknameRequired
	}
	creds := toCreds(req)
	creds.Nickname = nickname
	servers, err := a.sessions.Login(creds)
	if err != nil {
		return nil, err
	}
	if a.vault != nil {
		if saveErr := a.vault.Add(credentials.Account{
			ProfileName: req.ProfileName,
			Account:     req.Account,
			Password:    req.Password,
		}); saveErr != nil {
			log.Printf("save account: %v", saveErr)
		}
	}
	return servers, nil
}

// RemoveAccount deletes a saved account from the vault.
func (a *App) RemoveAccount(accountName string) error {
	if a.vault == nil {
		return nil
	}
	if err := a.vault.Remove(accountName); err != nil {
		return err
	}
	if a.gallerySessionStore != nil {
		if err := a.gallerySessionStore.Delete(accountName); err != nil {
			return err
		}
	}
	return nil
}

// RenameAccount sets the client-only display label for a saved account (does
// not touch nickname/account, which are what the server receives).
func (a *App) RenameAccount(accountName, displayName string) error {
	if a.vault == nil {
		return errNoVault
	}
	return a.vault.Mutate(accountName, func(acc *credentials.Account) { acc.DisplayName = displayName })
}

// SetAccountPhoto sets the client-only avatar (a base64 data URL) for a saved
// account. Pass an empty string to clear it.
func (a *App) SetAccountPhoto(accountName, dataURL string) error {
	if a.vault == nil {
		return errNoVault
	}
	return a.vault.Mutate(accountName, func(acc *credentials.Account) { acc.Photo = dataURL })
}

// GetAccount returns the password-less summary for a saved account (used by the
// RC top header to show the active account's display name + photo).
func (a *App) GetAccount(accountName string) (AccountSummary, error) {
	if a.vault == nil {
		return AccountSummary{}, errNoVault
	}
	acc, err := a.vault.Get(accountName)
	if err != nil {
		return AccountSummary{}, err
	}
	return acc.Summary(), nil
}

func (a *App) findAccount(accountName string) (credentials.Account, error) {
	if a.vault == nil {
		return credentials.Account{}, errNoVault
	}
	accounts, err := a.vault.Load()
	if err != nil {
		return credentials.Account{}, err
	}
	for _, acc := range accounts {
		if acc.Account == accountName {
			return acc, nil
		}
	}
	return credentials.Account{}, errAccountNotFound
}

// GetServers returns the cached server list for the active session.
func (a *App) GetServers() ([]rclib.Server, error) { return a.sessions.GetServers() }

// ConnectToServer authenticates to the server at the given index. On success it
// brands the window titles and tray tooltip with the server name.
func (a *App) ConnectToServer(index int) error {
	// A running sync engine uses the shared connection backend. Tear it down
	// before switching the underlying NC session; otherwise an in-flight poll
	// from the previous server can continue after the handle starts serving the
	// newly selected server.
	a.stopSyncEngine()
	a.closeSessionWindows()
	a.clearGallerySession()
	err := a.sessions.ConnectToServer(index)
	// Do not query the native player cache on the Wails connection path. The
	// server socket is already authenticated here, and a native cache read can
	// wait for the first player snapshot on slower servers. The tray refresh
	// loop will populate the live count shortly after the RC screen opens.
	a.refreshServerChromeFast()
	if err == nil {
		a.startSyncEngine()
	}
	return err
}

// SetNewProtocol toggles newer-protocol compatibility before server login.
func (a *App) SetNewProtocol(enable bool) error { return a.sessions.SetNewProtocol(enable) }

// Logout drops the active session and restores default window/tray titles.
func (a *App) Logout() {
	a.stopSyncEngine()
	a.closeSessionWindows()
	a.clearGallerySession()
	a.sessions.Logout()
	a.clearPMState()
	a.refreshServerChrome()
}

// ConnectToNCServer explicitly opens the NC (script) socket.
func (a *App) ConnectToNCServer() error { return a.sessions.ConnectToNCServer() }

// DisconnectNC closes the NC socket.
func (a *App) DisconnectNC() error { return a.sessions.DisconnectNC() }

// NCStatus returns the NC socket snapshot.
func (a *App) NCStatus() connection.NCStatus { return a.sessions.NCStatus() }

// IrcLogin starts the IRC session for the active handle.
func (a *App) IrcLogin() error { return a.sessions.IrcLogin() }

// SendIrcText sends a raw IRC command (command + up to 3 params).
func (a *App) SendIrcText(command, p1, p2, p3 string) error {
	return a.sessions.SendIrcText(command, p1, p2, p3)
}

// Execute sends a chat line or slash command to the active server.
func (a *App) Execute(message string) error { return a.sessions.Execute(message) }

// SetNickname changes the RC nickname on the active handle.
func (a *App) SetNickname(nickname string) error { return a.sessions.SetNickname(nickname) }

// GetPlayers returns the cached player list for the active server.
func (a *App) GetPlayers() ([]rclib.Player, error) { return a.sessions.GetPlayers() }

// SendPrivateMessage sends a private message to a single player id.
func (a *App) SendPrivateMessage(playerID int, message string) error {
	return a.sessions.SendPrivateMessage(playerID, message)
}

// SendMassPM sends one bulk PM to many player ids at once.
func (a *App) SendMassPM(playerIDs []int, message string) error {
	return a.sessions.SendMassPM(playerIDs, message)
}

// SendAdminMessage sends an admin message to a single player id.
func (a *App) SendAdminMessage(playerID int, message string) error {
	return a.sessions.SendAdminMessage(playerID, message)
}

// SendAdminMessageAll sends an admin message to every player.
func (a *App) SendAdminMessageAll(message string) error {
	return a.sessions.SendAdminMessageAll(message)
}

// resolveAccount trims the account argument and resolves an empty value to the
// logged-in account for window URLs and write operations. The self rights read
// itself is handled by Service.OpenRights with the protocol's empty target.
func (a *App) resolveAccount(account string) string {
	account = strings.TrimSpace(account)
	if account == "" {
		return a.sessions.SelfAccount()
	}
	return account
}

// OpenRights opens the staff-rights editor for an account (self if empty).
func (a *App) OpenRights(account string) (connection.RightsData, error) {
	return a.sessions.OpenRights(a.resolveAccount(account))
}

// SetRights writes staff rights for an account.
func (a *App) SetRights(account string, rights int, ipRange, folderAccess string) error {
	return a.sessions.SetRights(a.resolveAccount(account), rights, ipRange, folderAccess)
}

// OpenAttrs opens the attributes editor for an account (self if empty).
func (a *App) OpenAttrs(account string) (connection.AttrsData, error) {
	return a.sessions.OpenAttrs(a.resolveAccount(account))
}

// SetAttrs writes attributes (properties JSON) for an account.
func (a *App) SetAttrs(account, propertiesJSON string) error {
	return a.sessions.SetAttrs(a.resolveAccount(account), propertiesJSON)
}

// ParseAttrsText converts an INI-style attribute document to properties JSON.
func (a *App) ParseAttrsText(text string) (string, error) {
	return a.sessions.ParseAttrsText(text)
}

// OpenBan opens the ban editor for an account (self if empty).
func (a *App) OpenBan(account string) (connection.BanData, error) {
	return a.sessions.OpenBan(a.resolveAccount(account))
}

// OpenComments opens the comments editor for an account (self if empty).
func (a *App) OpenComments(account string) (connection.CommentsData, error) {
	return a.sessions.OpenComments(a.resolveAccount(account))
}

// SetComments writes comments for an account.
func (a *App) SetComments(account, comments string) error {
	return a.sessions.SetComments(a.resolveAccount(account), comments)
}

// SetBan writes ban data for a target.
func (a *App) SetBan(target, world string, banned bool, banType, releaseTime, reason string) error {
	return a.sessions.SetBan(target, world, banned, banType, releaseTime, reason)
}

// GetBanTypes returns the available ban types/durations list.
func (a *App) GetBanTypes() (string, error) { return a.sessions.GetBanTypes() }

// RequestBanHistory returns an account's ban history text (self if empty).
func (a *App) RequestBanHistory(account string) (string, error) {
	return a.sessions.RequestBanHistory(a.resolveAccount(account))
}

// RequestStaffActivity returns an account's staff activity text (self if empty).
func (a *App) RequestStaffActivity(account string) (string, error) {
	return a.sessions.RequestStaffActivity(a.resolveAccount(account))
}

// openPlayerWindow opens (or focuses) an external editor/viewer window for the
// given player-management kind and account. Empty account resolves to self.
// Title is "<ACC>'s <Label> - <Server>". Several accounts may be open at once;
// reopening the same kind+account focuses the existing window.
func (a *App) openPlayerWindow(kind, label, account string, width, height int) error {
	account = a.resolveAccount(account)
	log.Printf("[editor] openPlayerWindow kind=%s account=%q", kind, account)
	mapKey := kind + ":" + account

	a.playerWindowMu.Lock()
	if w, ok := a.playerWindows[mapKey]; ok {
		w.Show()
		w.Focus()
		a.playerWindowMu.Unlock()
		return nil
	}
	a.playerWindowMu.Unlock()

	server := a.sessions.Status().ServerName
	title := serverWindowTitle(server, fmt.Sprintf("%s's %s", account, label))

	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             sanitizeWindowName(kind, account),
		Title:            title,
		URL:              "/#" + kind + "?a=" + url.QueryEscape(account),
		Width:            width,
		Height:           height,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.playerWindowMu.Lock()
	a.playerWindows[mapKey] = w
	a.playerWindowMu.Unlock()
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.playerWindowMu.Lock()
		if a.playerWindows[mapKey] == w {
			delete(a.playerWindows, mapKey)
		}
		a.playerWindowMu.Unlock()
	})
	return nil
}

// OpenRightsWindow opens the /openrights editor window for an account (self if
// empty). Triggered by typing /openrights in the RC chat.
func (a *App) OpenRightsWindow(account string) error {
	return a.openPlayerWindow("rights", "Rights", account, 480, 560)
}

// OpenAttrsWindow opens the /open (attributes) editor window for an account
// (self if empty). Triggered by typing /open in the RC chat.
func (a *App) OpenAttrsWindow(account string) error {
	return a.openPlayerWindow("attrs", "Attributes", account, 480, 600)
}

// OpenBanWindow opens the /openaccess (ban) editor window for an account (self
// if empty). Triggered by typing /openaccess in the RC chat.
func (a *App) OpenBanWindow(account string) error {
	if err := a.sessions.RequireBanPlayersRight(); err != nil {
		return err
	}
	return a.openPlayerWindow("ban", "Access", account, 520, 560)
}

// OpenCommentsWindow opens the /opencomments editor window for an account (self
// if empty). Triggered by typing /opencomments in the RC chat.
func (a *App) OpenCommentsWindow(account string) error {
	return a.openPlayerWindow("comments", "Comments", account, 560, 560)
}

// OpenBanHistoryWindow opens the read-only ban-history viewer window for an
// account (self if empty). Triggered from the player-list context menu.
func (a *App) OpenBanHistoryWindow(account string) error {
	if err := a.sessions.RequireBanPlayersRight(); err != nil {
		return err
	}
	return a.openPlayerWindow("banhistory", "Ban History", account, 640, 560)
}

// OpenStaffActivityWindow opens the read-only staff-activity viewer window for
// an account (self if empty). Triggered from the player-list context menu.
func (a *App) OpenStaffActivityWindow(account string) error {
	return a.openPlayerWindow("staffactivity", "Staff Activity", account, 640, 560)
}

// --- Script management (NC server) ---

// ScriptListType is "weapon" | "class" | "npc".
type ScriptListType = string

// GetWeapons returns the cached weapon list.
func (a *App) GetWeapons() ([]rclib.Weapon, error) { return a.sessions.GetWeapons() }

// GetClasses returns the cached class list.
func (a *App) GetClasses() ([]rclib.Class, error) { return a.sessions.GetClasses() }

// GetNPCs returns the cached NPC list.
func (a *App) GetNPCs() ([]rclib.NPC, error) { return a.sessions.GetNPCs() }

// GetScriptLists returns all NC script indexes, optionally restricted to
// scripts this account can read according to its cached openrights response.
func (a *App) GetScriptLists(onlyReadable bool) (connection.ScriptLists, error) {
	return a.sessions.GetScriptLists(onlyReadable)
}

// AddWeapon creates a weapon by name.
func (a *App) AddWeapon(name string) error { return a.sessions.AddWeapon(name) }

// DeleteWeapon deletes a weapon by name.
func (a *App) DeleteWeapon(name string) error { return a.sessions.DeleteWeapon(name) }

// AddClass creates a class by name and immediately gives it a valid first line.
// Some servers reject a newly-created class whose script body is empty.
func (a *App) AddClass(name string) error {
	if err := a.sessions.AddClass(name); err != nil {
		return err
	}
	if err := a.sessions.SaveClass(name, initialClassScript(a.sessions.Status())); err != nil {
		return fmt.Errorf("class %q was created but its initial script could not be written: %w", name, err)
	}
	return nil
}

func initialClassScript(status connection.Status) string {
	author := strings.TrimSpace(status.CommunityName)
	if author == "" {
		author = strings.TrimSpace(status.Account)
	}
	if author == "" {
		author = strings.TrimSpace(status.RealAccount)
	}
	if author == "" {
		author = "unknown"
	}
	return "// Scripted by " + author
}

// DeleteClass deletes a class by name.
func (a *App) DeleteClass(name string) error { return a.sessions.DeleteClass(name) }

// DeleteNPC deletes an NPC by id.
func (a *App) DeleteNPC(id int) error { return a.sessions.DeleteNPC(id) }

// CreateNPC creates a new DB NPC on the server.
func (a *App) CreateNPC(name string, id int, npcType, scripter, level, x, y string) error {
	return a.sessions.CreateNPC(name, id, npcType, scripter, level, x, y)
}

// OpenScript fetches a script (weapon/class/npc) and returns its content.
func (a *App) OpenScript(scriptType, key string) (rclib.ScriptReply, error) {
	if err := a.requireScriptSyncForEditor(scriptType); err != nil {
		return rclib.ScriptReply{}, err
	}
	reply, err := a.sessions.OpenScript(scriptType, key)
	if err == nil {
		a.emitPluginEvent("script.opened", reply)
	}
	return reply, err
}

// SaveWeapon writes a weapon's script back.
func (a *App) SaveWeapon(name, script string) error {
	return a.saveScriptWithSyncExpectation("weapon", name, script, func() error { return a.sessions.SaveWeapon(name, script) })
}

// SaveClass writes a class's script back.
func (a *App) SaveClass(name, script string) error {
	return a.saveScriptWithSyncExpectation("class", name, script, func() error { return a.sessions.SaveClass(name, script) })
}

// SaveNPC writes an NPC's script back.
func (a *App) SaveNPC(id int, script string) error {
	return a.saveScriptWithSyncExpectation("npc", strconv.Itoa(id), script, func() error { return a.sessions.SaveNPC(id, script) })
}

func (a *App) saveScriptWithSyncExpectation(kind, key, script string, save func() error) error {
	if err := a.requireScriptSyncForEditor(kind); err != nil {
		return err
	}
	previous, hasPrevious := a.editorOriginal(kind, key)
	if !hasPrevious {
		reply, err := a.fetchScript(kind, key)
		if err != nil {
			a.recordAudit("save", "script", kind+":"+key, "failed", "could not read the current version for a safety backup: "+err.Error())
			return fmt.Errorf("read current %s before saving: %w", kind, err)
		}
		previous = []byte(reply.Script)
		hasPrevious = true
	}
	backup, hasBackup, err := a.saveDeploymentBackup("script", kind+":"+key, previous, hasPrevious)
	if err != nil {
		a.recordAudit("save", "script", kind+":"+key, "failed", "safety backup failed: "+err.Error())
		return fmt.Errorf("create safety backup before saving: %w", err)
	}
	eng := a.currentSyncEngine()
	if eng != nil {
		eng.ExpectServerUpdate(kind, key, script)
	}
	if err := save(); err != nil {
		if eng != nil {
			eng.CancelExpectedServerUpdate(kind, key)
		}
		a.recordAudit("save", "script", kind+":"+key, "failed", err.Error())
		return err
	}
	a.updateEditorContent(kind, key, script)
	detail := "no previous remote content was available"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("save", "script", kind+":"+key, "success", detail)
	a.emitPluginEvent("script.saved", kind, key)
	return nil
}

// ResetNPC resets an NPC by id.
func (a *App) ResetNPC(id int) error { return a.sessions.ResetNPC(id) }

// OpenNPCFlags fetches an NPC's flags and returns them.
func (a *App) OpenNPCFlags(id int) (rclib.ScriptReply, error) { return a.sessions.OpenNPCFlags(id) }

// OpenNPCAttributes fetches an NPC's attributes and returns them.
func (a *App) OpenNPCAttributes(id int) (rclib.ScriptReply, error) {
	return a.sessions.OpenNPCAttributes(id)
}

// SaveNPCFlags writes an NPC's flags back.
func (a *App) SaveNPCFlags(id int, flags string) error {
	key := strconv.Itoa(id)
	previous, hasPrevious := a.editorOriginal("npcflags", key)
	if !hasPrevious {
		reply, err := a.sessions.OpenNPCFlags(id)
		if err != nil {
			a.recordAudit("save", "npcflags", key, "failed", "could not read the current version for a safety backup: "+err.Error())
			return fmt.Errorf("read NPC flags before saving: %w", err)
		}
		previous = []byte(reply.Script)
		hasPrevious = true
	}
	backup, hasBackup, err := a.saveDeploymentBackup("npcflags", key, previous, hasPrevious)
	if err != nil {
		a.recordAudit("save", "npcflags", key, "failed", "safety backup failed: "+err.Error())
		return fmt.Errorf("create safety backup before saving: %w", err)
	}
	if err := a.sessions.SaveNPCFlags(id, flags); err != nil {
		a.recordAudit("save", "npcflags", key, "failed", err.Error())
		return err
	}
	a.updateEditorContent("npcflags", key, flags)
	detail := "no previous remote content was available"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("save", "npcflags", key, "success", detail)
	a.emitPluginEvent("script.saved", "npcflags", key)
	return nil
}

// SaveServerText uploads a server-side text config (options/folder_config/
// flags) edited in a ScriptEditor window back to the server.
func (a *App) SaveServerText(kind, content string) error {
	previous, cacheKey, hasPrevious := a.editorOriginalForKind(kind)
	if !hasPrevious {
		reply, err := a.sessions.OpenServerText(kind)
		if err != nil {
			a.recordAudit("save", "servertext", kind, "failed", "could not read the current version for a safety backup: "+err.Error())
			return fmt.Errorf("read server text before saving: %w", err)
		}
		previous = []byte(reply.Script)
		hasPrevious = true
	}
	backup, hasBackup, err := a.saveDeploymentBackup("servertext", kind, previous, hasPrevious)
	if err != nil {
		a.recordAudit("save", "servertext", kind, "failed", "safety backup failed: "+err.Error())
		return fmt.Errorf("create safety backup before saving: %w", err)
	}
	if err := a.sessions.UploadServerText(kind, content); err != nil {
		a.recordAudit("save", "servertext", kind, "failed", err.Error())
		return err
	}
	if cacheKey != "" {
		a.updateEditorContent(kind, cacheKey, content)
	}
	detail := "no previous remote content was available"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("save", "servertext", kind, "success", detail)
	return nil
}

// WarpNPC warps an NPC to (x, y) on the given level.
func (a *App) WarpNPC(id int, x, y float64, level string) error {
	return a.sessions.WarpNPC(id, x, y, level)
}

// RefreshWeapons re-requests the weapon list from the NC server.
func (a *App) RefreshWeapons() error { return a.sessions.RefreshWeapons() }

// --- File browser (main server socket) ---

// FileBrowserStart begins a file-browser session.
func (a *App) FileBrowserStart() error {
	if err := a.sessions.StartFileBrowser(); err != nil {
		return err
	}
	a.emitPluginEvent("filebrowser.started")
	return nil
}

// FileBrowserCd changes the current browser folder.
func (a *App) FileBrowserCd(folder string) error {
	if err := a.sessions.FileBrowserCd(folder); err != nil {
		return err
	}
	a.emitPluginEvent("filebrowser.selection.changed", folder)
	return nil
}

// FileBrowserDelete deletes a remote file. A best-effort snapshot is kept when
// the current entry can be downloaded before the destructive operation.
func (a *App) FileBrowserDelete(path string) error {
	backup, hasBackup, backupErr := a.backupRemoteContent("file", path)
	if backupErr != nil {
		a.recordAudit("delete", "file", path, "failed", "safety backup failed: "+backupErr.Error())
		return fmt.Errorf("create safety backup before deleting: %w", backupErr)
	}
	if err := a.sessions.FileBrowserDelete(path); err != nil {
		a.recordAudit("delete", "file", path, "failed", err.Error())
		return err
	}
	detail := "remote entry was not readable for backup"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("delete", "file", path, "success", detail)
	return nil
}

// FileBrowserRename renames a remote file.
func (a *App) FileBrowserRename(oldPath, newPath string) error {
	if isFileBrowserDirectoryPath(oldPath) {
		return errors.New("renaming file-browser folders is not supported")
	}
	normalizedOldPath := normalizeFileBrowserPath(oldPath)
	if entries, err := a.sessions.GetFileBrowserFiles(); err == nil {
		if fileBrowserEntryIsDirectory(entries, normalizedOldPath) {
			return errors.New("renaming file-browser folders is not supported")
		}
	}
	if err := a.sessions.FileBrowserRename(oldPath, newPath); err != nil {
		a.recordAudit("rename", "file", oldPath+" -> "+newPath, "failed", err.Error())
		return err
	}
	a.recordAudit("rename", "file", oldPath+" -> "+newPath, "success", "")
	return nil
}

func normalizeFileBrowserPath(path string) string {
	path = strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	return strings.TrimRight(path, "/")
}

func isFileBrowserDirectoryPath(path string) bool {
	return strings.HasSuffix(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
}

func fileBrowserEntryIsDirectory(entries []rclib.FileBrowserEntry, path string) bool {
	path = normalizeFileBrowserPath(path)
	for _, entry := range entries {
		if normalizeFileBrowserPath(entry.Path) == path && entry.IsDirectory {
			return true
		}
	}
	return false
}

// FileBrowserMove moves a file into a destination folder.
func (a *App) FileBrowserMove(destFolder, filePath string) error {
	if err := a.sessions.FileBrowserMove(destFolder, filePath); err != nil {
		a.recordAudit("move", "file", filePath+" -> "+destFolder, "failed", err.Error())
		return err
	}
	a.recordAudit("move", "file", filePath+" -> "+destFolder, "success", "")
	return nil
}

// GetFileBrowserFolders returns the current browser folders.
func (a *App) GetFileBrowserFolders() ([]rclib.FileBrowserFolder, error) {
	return a.sessions.GetFileBrowserFolders()
}

// GetFileBrowserFiles returns the current browser files.
func (a *App) GetFileBrowserFiles() ([]rclib.FileBrowserEntry, error) {
	return a.sessions.GetFileBrowserFiles()
}

const maxImageThumbnailBytes = 500 * 1024

var imageThumbnailMIMETypes = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
	"bmp":  "image/bmp",
	"ico":  "image/x-icon",
}

func imageThumbnailMIME(remotePath string) (string, bool) {
	mimeType, ok := imageThumbnailMIMETypes[extOf(remotePath)]
	return mimeType, ok
}

// GetFileBrowserImageThumbnail returns a data URL for an eligible image in the
// current remote folder. The server-side metadata check prevents the frontend
// from turning this preview path into a general-purpose file downloader.
func (a *App) GetFileBrowserImageThumbnail(remotePath string) (string, error) {
	mimeType, ok := imageThumbnailMIME(remotePath)
	if !ok {
		return "", errors.New("unsupported image type for thumbnail")
	}

	entries, err := a.sessions.GetFileBrowserFiles()
	if err != nil {
		return "", err
	}
	normalizedPath := normalizeFileBrowserPath(remotePath)
	fileSize := -1
	for _, entry := range entries {
		if normalizeFileBrowserPath(entry.Path) != normalizedPath {
			continue
		}
		if entry.IsDirectory {
			return "", errors.New("cannot create a thumbnail for a folder")
		}
		fileSize = entry.Size
		break
	}
	if fileSize <= 0 {
		return "", errors.New("image is not available for thumbnail")
	}
	if int64(fileSize) >= maxImageThumbnailBytes {
		return "", errors.New("image is too large for a thumbnail")
	}

	content, err := a.sessions.DownloadFile(remotePath)
	if err != nil {
		return "", err
	}
	if len(content) == 0 || int64(len(content)) >= maxImageThumbnailBytes {
		return "", errors.New("image is too large for a thumbnail")
	}

	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(content), nil
}

// FileBrowserMaxUploadSize returns the server's max upload size in bytes.
func (a *App) FileBrowserMaxUploadSize() int64 { return a.sessions.MaxUploadFileSize() }

// DownloadFile downloads a remote file. With saveAs=false it writes straight to
// the configured downloads folder (which must be set); with saveAs=true it
// prompts for a destination via a native Save dialog. Returns the saved path.
func (a *App) DownloadFile(remotePath string, saveAs bool) (string, error) {
	content, err := a.sessions.DownloadFile(remotePath)
	if err != nil {
		return "", err
	}
	return a.saveDownloadedContent(remotePath, content, saveAs)
}

func (a *App) saveDownloadedContent(remotePath string, content []byte, saveAs bool) (string, error) {
	name := filepath.Base(remotePath)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "download"
	}
	var dest string
	if saveAs {
		chosen, err := a.app.Dialog.SaveFile().
			AttachToWindow(a.dialogParentWindow()).
			SetMessage("Save " + name).
			SetFilename(name).
			PromptForSingleSelection()
		if err != nil {
			return "", err
		}
		if chosen == "" {
			return "", nil // user cancelled
		}
		dest = chosen
	} else {
		dir := a.GetFileBrowserConfig().DownloadDir
		if dir == "" {
			return "", errors.New("no downloads folder set — configure it in Settings → Files")
		}
		dest = uniqueDownloadPath(dir, name)
	}
	if err := os.WriteFile(dest, content, 0o644); err != nil {
		return "", err
	}
	return dest, nil
}

// UploadFileViaDialog opens a native file picker, reads the chosen file, and
// uploads it to the current browser folder.
func (a *App) UploadFileViaDialog() error {
	chosen, err := a.app.Dialog.OpenFile().
		AttachToWindow(a.dialogParentWindow()).
		SetTitle("Select a file to upload").
		CanChooseFiles(true).
		CanChooseDirectories(false).
		PromptForSingleSelection()
	if err != nil {
		return err
	}
	if chosen == "" {
		return nil // user cancelled
	}
	content, err := os.ReadFile(chosen)
	if err != nil {
		return err
	}
	remotePath := filepath.Base(chosen)
	backup, hasBackup, backupErr := a.backupRemoteContent("file", remotePath)
	if backupErr != nil {
		return fmt.Errorf("create safety backup before uploading: %w", backupErr)
	}
	if err := a.sessions.UploadFile(remotePath, content); err != nil {
		a.recordAudit("upload", "file", remotePath, "failed", err.Error())
		return err
	}
	detail := "new remote file"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("upload", "file", remotePath, "success", detail)
	return nil
}

// UploadFileBytes uploads base64-encoded content (used for drag-in uploads from
// the webview, which reads the dropped file and sends it as base64).
func (a *App) UploadFileBytes(remotePath, b64 string) error {
	content, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("invalid file data: %w", err)
	}
	backup, hasBackup, backupErr := a.backupRemoteContent("file", remotePath)
	if backupErr != nil {
		return fmt.Errorf("create safety backup before uploading: %w", backupErr)
	}
	if err := a.sessions.UploadFile(remotePath, content); err != nil {
		a.recordAudit("upload", "file", remotePath, "failed", err.Error())
		return err
	}
	detail := "new remote file"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("upload", "file", remotePath, "success", detail)
	return nil
}

// uniqueDownloadPath returns a non-colliding path inside dir for a file named
// name, appending " (n)" before the extension when a file already exists.
func uniqueDownloadPath(dir, name string) string {
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		return dest
	}
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// --- Type-aware file open (double-click a file in the browser) ---

// fileCacheDir is where opened media/.db/.txt temp files live (so the SQLite
// explorer can operate on + re-upload the .db). Created lazily.
func fileCacheDir() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "graal-rc", "filecache")
	if err := os.MkdirAll(p, 0o755); err != nil {
		return "", err
	}
	return p, nil
}

var (
	mediaExts = map[string]bool{
		"png": true, "jpg": true, "jpeg": true, "gif": true, "webp": true, "bmp": true, "ico": true,
		"mp4": true, "webm": true, "mov": true, "avi": true, "mkv": true,
		"mp3": true, "wav": true, "ogg": true, "m4a": true, "flac": true,
	}
	textExts = map[string]bool{
		"txt": true, "ini": true, "cfg": true, "conf": true, "log": true, "md": true,
		"json": true, "js": true, "ts": true, "csv": true, "xml": true,
		"yml": true, "yaml": true, "html": true, "css": true,
	}
	dbExts = map[string]bool{"db": true, "sqlite": true, "sqlite3": true}
)

// ext returns the lower-cased extension (no dot) of a path.
func extOf(path string) string {
	e := filepath.Ext(path)
	return strings.ToLower(strings.TrimPrefix(e, "."))
}

// osOpenPath launches a file/path with the OS default application.
func osOpenPath(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// OpenRemoteFile downloads a file and opens it by type. Returns a short kind
// label for the toast. Called on a file double-click.
func (a *App) OpenRemoteFile(remotePath string) (string, error) {
	if handled, err := a.requestPluginFileOpen(remotePath); err != nil {
		return "", err
	} else if handled {
		path, pathErr := normalizePluginRemotePath(remotePath)
		if pathErr == nil {
			a.emitPluginEvent("filebrowser.file.opened", pluginFileEvent{Path: path, Name: filepath.Base(path), Extension: strings.ToLower(filepath.Ext(path)), Kind: "plugin"})
		}
		return "plugin", nil
	}
	content, err := a.sessions.DownloadFile(remotePath)
	if err != nil {
		return "", err
	}
	name := filepath.Base(remotePath)
	ext := extOf(remotePath)
	switch {
	case isExternalRemoteFile(remotePath):
		if _, err := a.openExternalRemoteFile(remotePath, content); err != nil {
			return "", err
		}
		a.emitPluginEvent("filebrowser.file.opened", pluginFileEvent{Path: remotePath, Name: name, Extension: "." + ext, Size: int64(len(content)), Kind: "external"})
		return "external", nil
	case mediaExts[ext]:
		dir, err := fileCacheDir()
		if err != nil {
			return "", err
		}
		local := filepath.Join(dir, name)
		if err := os.WriteFile(local, content, 0o644); err != nil {
			return "", err
		}
		if err := osOpenPath(local); err != nil {
			return "", err
		}
		a.emitPluginEvent("filebrowser.file.opened", pluginFileEvent{Path: remotePath, Name: name, Extension: "." + ext, Size: int64(len(content)), Kind: "media"})
		return "media", nil
	case dbExts[ext]:
		dir, err := fileCacheDir()
		if err != nil {
			return "", err
		}
		local := filepath.Join(dir, name)
		if err := os.WriteFile(local, content, 0o644); err != nil {
			return "", err
		}
		a.openMu.Lock()
		a.dbFiles[remotePath] = local
		if len(content) >= 100 {
			hdr := make([]byte, 100)
			copy(hdr, content[:100])
			a.dbHeaders[remotePath] = hdr
		}
		a.openMu.Unlock()
		if err := a.openSqliteWindow(remotePath); err != nil {
			return "", err
		}
		a.emitPluginEvent("filebrowser.file.opened", pluginFileEvent{Path: remotePath, Name: name, Extension: "." + ext, Size: int64(len(content)), Kind: "database"})
		return "database", nil
	case textExts[ext]:
		a.openMu.Lock()
		a.textCache[remotePath] = content
		a.textOriginalCache[remotePath] = append([]byte(nil), content...)
		a.openMu.Unlock()
		if err := a.openTextWindow(remotePath); err != nil {
			return "", err
		}
		a.emitPluginEvent("filebrowser.file.opened", pluginFileEvent{Path: remotePath, Name: name, Extension: "." + ext, Size: int64(len(content)), Kind: "text"})
		return "text", nil
	default:
		// Unknown binary → plain download to the configured folder.
		saved, err := a.saveDownloadedContent(remotePath, content, false)
		if err != nil {
			return "", err
		}
		if saved != "" {
			if err := osOpenPath(saved); err != nil {
				return "", err
			}
		}
		a.emitPluginEvent("filebrowser.file.opened", pluginFileEvent{Path: remotePath, Name: name, Extension: "." + ext, Size: int64(len(content)), Kind: "download"})
		return "download", nil
	}
}

// OpenRemoteFileAsText downloads a file and opens it in the Monaco text editor
// regardless of type — even binary files are shown as text (may be garbage, but
// that's the point: force a text view).
func (a *App) OpenRemoteFileAsText(remotePath string) error {
	content, err := a.sessions.DownloadFile(remotePath)
	if err != nil {
		return err
	}
	a.openMu.Lock()
	a.textCache[remotePath] = content
	a.textOriginalCache[remotePath] = append([]byte(nil), content...)
	a.openMu.Unlock()
	return a.openTextWindow(remotePath)
}

// GetTextFile returns the cached text content for a .txt editor window.
func (a *App) GetTextFile(remotePath string) (string, error) {
	a.openMu.Lock()
	b, ok := a.textCache[remotePath]
	a.openMu.Unlock()
	if !ok {
		return "", errors.New("text content not available — reopen the file")
	}
	return string(b), nil
}

// SaveTextFile uploads edited text content back to the remote path.
func (a *App) SaveTextFile(remotePath, content string) error {
	a.openMu.Lock()
	previous, hasPrevious := a.textOriginalCache[remotePath]
	previous = append([]byte(nil), previous...)
	a.openMu.Unlock()
	if !hasPrevious {
		var loadErr error
		previous, hasPrevious, loadErr = a.loadRemoteTextForBackup(remotePath)
		if loadErr != nil {
			a.recordAudit("save", "textfile", remotePath, "failed", "could not read the current version for a safety backup: "+loadErr.Error())
			return fmt.Errorf("read remote file before saving: %w", loadErr)
		}
	}
	backup, hasBackup, backupErr := a.saveDeploymentBackup("textfile", remotePath, previous, hasPrevious)
	if backupErr != nil {
		a.recordAudit("save", "textfile", remotePath, "failed", "safety backup failed: "+backupErr.Error())
		return fmt.Errorf("create safety backup before saving: %w", backupErr)
	}
	if err := a.sessions.UploadFile(remotePath, []byte(content)); err != nil {
		a.recordAudit("save", "textfile", remotePath, "failed", err.Error())
		return err
	}
	a.openMu.Lock()
	a.textCache[remotePath] = []byte(content)
	a.textOriginalCache[remotePath] = []byte(content)
	a.openMu.Unlock()
	detail := "new remote file"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("save", "textfile", remotePath, "success", detail)
	return nil
}

// openTextWindow opens (or focuses) the plain-text editor for remotePath.
func (a *App) openTextWindow(remotePath string) error {
	mapKey := "textfile:" + remotePath
	a.editorMu.Lock()
	if w, ok := a.editorWindows[mapKey]; ok {
		w.Show()
		w.Focus()
		a.editorMu.Unlock()
		return nil
	}
	a.editorMu.Unlock()
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             sanitizeWindowName("textfile", remotePath),
		Title:            editorTitle(a.sessions.Status().ServerName, "textfile", filepath.Base(remotePath)),
		URL:              "/#textfile?p=" + url.QueryEscape(remotePath),
		Width:            820,
		Height:           620,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.editorMu.Lock()
	a.editorWindows[mapKey] = w
	a.editorMu.Unlock()
	w.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		a.editorCacheMu.Lock()
		dirty := a.editorDirty[mapKey]
		a.editorCacheMu.Unlock()
		if dirty {
			a.app.Event.Emit("rc:editorConfirmClose", mapKey)
			event.Cancel()
		}
	})
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.editorMu.Lock()
		isCurrent := a.editorWindows[mapKey] == w
		if isCurrent {
			delete(a.editorWindows, mapKey)
		}
		a.editorMu.Unlock()
		if !isCurrent {
			return
		}
		a.editorCacheMu.Lock()
		delete(a.editorDirty, mapKey)
		a.editorCacheMu.Unlock()
		a.openMu.Lock()
		delete(a.textCache, remotePath)
		delete(a.textOriginalCache, remotePath)
		a.openMu.Unlock()
	})
	return nil
}

// --- SQLite explorer ---

// SqliteTable is one entry of the explorer's table list.
type SqliteTable struct {
	Name   string `json:"name"`
	Schema string `json:"schema"`
}

// SqliteInfo describes a remote .db for the explorer.
type SqliteInfo struct {
	Tables []SqliteTable `json:"tables"`
}

// SqliteResult is a query result (columns+rows for SELECT, rowsAffected for DML).
type SqliteResult struct {
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	RowsAffected int64    `json:"rowsAffected"`
}

// sqliteDB opens the cached connection for a remote .db.
func (a *App) sqliteDB(remotePath string) (*sqliteDBHandle, error) {
	a.openMu.Lock()
	local := a.dbFiles[remotePath]
	a.openMu.Unlock()
	if local == "" {
		return nil, errors.New("database not open — reopen it from the file browser")
	}
	db, err := sqlite.Open(local)
	if err != nil {
		return nil, err
	}
	return &sqliteDBHandle{db: db, local: local}, nil
}

type sqliteDBHandle struct {
	db    *sql.DB
	local string
}

// GetSqliteInfo lists the tables of a remote .db.
func (a *App) GetSqliteInfo(remotePath string) (SqliteInfo, error) {
	h, err := a.sqliteDB(remotePath)
	if err != nil {
		return SqliteInfo{}, err
	}
	tables, err := sqlite.Tables(h.db)
	if err != nil {
		return SqliteInfo{}, err
	}
	out := SqliteInfo{}
	for _, t := range tables {
		out.Tables = append(out.Tables, SqliteTable{Name: t.Name, Schema: t.Schema})
	}
	return out, nil
}

// GetSqliteSchema returns columns + foreign keys per table (for the Diagram).
func (a *App) GetSqliteSchema(remotePath string) ([]sqlite.TableSchema, error) {
	h, err := a.sqliteDB(remotePath)
	if err != nil {
		return nil, err
	}
	return sqlite.Schema(h.db)
}

// CommitSqlite applies staged changes (inserts/edits/deletes for one table) in a
// single transaction, then — on success — checkpoints, re-reads the file,
// best-effort preserves the SQLite version, and uploads it. A failing statement
// rolls back (nothing written, no upload) and returns an error naming the op.
func (a *App) CommitSqlite(remotePath string, changes sqlite.Changes) error {
	backup, hasBackup, backupErr := a.backupRemoteContent("sqlite", remotePath)
	if backupErr != nil {
		a.recordAudit("save", "sqlite", remotePath, "failed", "safety backup failed: "+backupErr.Error())
		return fmt.Errorf("create safety backup before committing SQLite changes: %w", backupErr)
	}
	h, err := a.sqliteDB(remotePath)
	if err != nil {
		return err
	}
	if err := sqlite.Commit(h.db, changes); err != nil {
		return err
	}
	// Flush WAL so the main file holds all committed edits, then read + upload.
	if _, err := h.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return err
	}
	content, err := os.ReadFile(h.local)
	if err != nil {
		return err
	}
	a.openMu.Lock()
	hdr := a.dbHeaders[remotePath]
	a.openMu.Unlock()
	if hdr != nil && len(content) >= 100 {
		copy(content[96:100], hdr[96:100]) // preserve version-valid-for
	}
	if err := a.sessions.UploadFile(remotePath, content); err != nil {
		a.recordAudit("save", "sqlite", remotePath, "failed", err.Error())
		return err
	}
	detail := "new remote database"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("save", "sqlite", remotePath, "success", detail)
	return nil
}
func (a *App) SqliteQuery(remotePath, query string, args []any) (SqliteResult, error) {
	h, err := a.sqliteDB(remotePath)
	if err != nil {
		return SqliteResult{}, err
	}
	res, err := sqlite.Query(h.db, query, args)
	if err != nil {
		return SqliteResult{}, err
	}
	return SqliteResult{Columns: res.Columns, Rows: res.Rows, RowsAffected: res.RowsAffected}, nil
}

// SqliteUpdateCell sets one cell (table.column at rowid) — inline grid edit.
func (a *App) SqliteUpdateCell(remotePath, table, column string, rowid int64, value any) error {
	h, err := a.sqliteDB(remotePath)
	if err != nil {
		return err
	}
	return sqlite.UpdateCell(h.db, table, column, rowid, value)
}

// SqliteInsertRow adds a default-values row, returns its rowid.
func (a *App) SqliteInsertRow(remotePath, table string) (int64, error) {
	h, err := a.sqliteDB(remotePath)
	if err != nil {
		return 0, err
	}
	return sqlite.InsertRow(h.db, table)
}

// SqliteDeleteRow deletes a row by rowid.
func (a *App) SqliteDeleteRow(remotePath, table string, rowid int64) error {
	h, err := a.sqliteDB(remotePath)
	if err != nil {
		return err
	}
	return sqlite.DeleteRow(h.db, table, rowid)
}

// SaveSqliteFile re-reads the (edited) local .db, best-effort preserves the
// original SQLite version (copy the header's version-valid-for bytes), and
// uploads it. modernc preserves the schema-format version it read.
func (a *App) SaveSqliteFile(remotePath string) error {
	backup, hasBackup, backupErr := a.backupRemoteContent("sqlite", remotePath)
	if backupErr != nil {
		a.recordAudit("save", "sqlite", remotePath, "failed", "safety backup failed: "+backupErr.Error())
		return fmt.Errorf("create safety backup before saving SQLite file: %w", backupErr)
	}
	h, err := a.sqliteDB(remotePath)
	if err != nil {
		return err
	}
	// Flush WAL so the main file holds all edits, then read it.
	_, _ = h.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	content, err := os.ReadFile(h.local)
	if err != nil {
		return err
	}
	a.openMu.Lock()
	hdr := a.dbHeaders[remotePath]
	a.openMu.Unlock()
	if hdr != nil && len(content) >= 100 {
		// Bytes 96–99 = "version valid for" (SQLite version that last wrote).
		copy(content[96:100], hdr[96:100])
	}
	if err := a.sessions.UploadFile(remotePath, content); err != nil {
		a.recordAudit("save", "sqlite", remotePath, "failed", err.Error())
		return err
	}
	detail := "new remote database"
	if hasBackup {
		detail = "backup " + backup.ID
	}
	a.recordAudit("save", "sqlite", remotePath, "success", detail)
	return nil
}

// openSqliteWindow opens (or focuses) the SQLite explorer for remotePath.
func (a *App) openSqliteWindow(remotePath string) error {
	a.openMu.Lock()
	if w, ok := a.sqliteWindows[remotePath]; ok {
		w.Show()
		w.Focus()
		a.openMu.Unlock()
		return nil
	}
	a.openMu.Unlock()
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             sanitizeWindowName("sqlite", remotePath),
		Title:            editorTitle(a.sessions.Status().ServerName, "sqlite", filepath.Base(remotePath)),
		URL:              "/#sqlite?p=" + url.QueryEscape(remotePath),
		Width:            960,
		Height:           640,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.openMu.Lock()
	a.sqliteWindows[remotePath] = w
	a.openMu.Unlock()
	w.Show()
	w.Focus()
	mapKey := "sqlite:" + remotePath
	// Intercept close when there are unsaved staged changes: the hook runs
	// before the internal close listener, so Cancel() prevents the close and
	// lets the frontend prompt (Save / Discard / Cancel).
	w.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		a.editorCacheMu.Lock()
		dirty := a.editorDirty[mapKey]
		a.editorCacheMu.Unlock()
		if dirty {
			a.app.Event.Emit("rc:editorConfirmClose", mapKey)
			event.Cancel()
		}
	})
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.openMu.Lock()
		if a.sqliteWindows[remotePath] != w {
			a.openMu.Unlock()
			return
		}
		local := a.dbFiles[remotePath]
		delete(a.sqliteWindows, remotePath)
		delete(a.dbFiles, remotePath)
		delete(a.dbHeaders, remotePath)
		a.openMu.Unlock()
		a.editorCacheMu.Lock()
		delete(a.editorDirty, mapKey)
		a.editorCacheMu.Unlock()
		if local != "" {
			sqlite.Close(local) // checkpoint + close, drop cached connection
		}
	})
	return nil
}

// Status returns the current session status snapshot.
func (a *App) Status() connection.Status { return a.sessions.Status() }

// refreshServerChrome reconciles the main/players/scripts/settings window titles
// and the tray tooltip with the current session state. Connected windows use
// the shared "<resource> - <server>" title format; disconnected windows return
// to their generic labels. Called on logout and by the tray refresh loop so a
// server-side disconnect (Status flips to !Connected) resets the chrome without
// a frontend round-trip.
func (a *App) refreshServerChrome() {
	a.refreshServerChromeWithPlayerCount(true)
}

// refreshServerChromeFast updates only metadata that is already available in
// the session snapshot. It is used immediately after authentication so a
// potentially delayed native player-cache read cannot hold ConnectToServer.
func (a *App) refreshServerChromeFast() {
	a.refreshServerChromeWithPlayerCount(false)
}

func (a *App) refreshServerChromeWithPlayerCount(loadPlayerCount bool) {
	st := a.sessions.Status()
	connected := st.Connected && st.Authenticated && st.ServerName != ""

	mainTitle := "Graal Remote Control"
	playersTitle := "Players"
	scriptsTitle := "Script Manager"
	settingsTitle := "Settings"
	filesTitle := "File Browser"
	tooltip := "Graal Remote Control"
	if connected {
		mainTitle = serverWindowTitle(st.ServerName, "RC")
		playersTitle = serverWindowTitle(st.ServerName, "Players")
		scriptsTitle = serverWindowTitle(st.ServerName, "Script Manager")
		settingsTitle = serverWindowTitle(st.ServerName, "Settings")
		filesTitle = serverWindowTitle(st.ServerName, "File Browser")
		if loadPlayerCount {
			count := 0
			if players, err := a.sessions.GetPlayers(); err == nil {
				count = len(players)
			}
			tooltip = st.ServerName + ":" + strconv.Itoa(count)
		} else {
			tooltip = st.ServerName
		}
	}
	if a.mainWindow != nil {
		a.mainWindow.SetTitle(mainTitle)
	}
	a.setWindowTitleLocked(&a.playerListMu, &a.playerListWindow, playersTitle)
	a.setWindowTitleLocked(&a.scriptMgrMu, &a.scriptMgrWindow, scriptsTitle)
	a.setWindowTitleLocked(&a.settingsMu, &a.settingsWindow, settingsTitle)
	a.setWindowTitleLocked(&a.fileBrowserMu, &a.fileBrowserWindow, filesTitle)
	a.pmWindowMu.Lock()
	for _, window := range a.pmWindows {
		if window != nil {
			window.SetTitle(serverWindowTitle(st.ServerName, "PM"))
		}
	}
	a.pmWindowMu.Unlock()
	if a.tray != nil {
		a.updateTrayPMBadge()
		if a.hasUnreadPM() {
			tooltip += " · New PM"
		}
		a.tray.SetTooltip(tooltip)
	}
}

// serverWindowTitle scopes a window to the active server while keeping the
// resource name first, so taskbar entries read "<resource> - <server>".
func serverWindowTitle(serverName, title string) string {
	title = strings.TrimSpace(title)
	serverName = strings.TrimSpace(serverName)
	if serverName == "" {
		return title
	}
	if title == "" {
		return serverName
	}
	return title + " - " + serverName
}

// setWindowTitleLocked snapshots a guarded window pointer under its mutex and
// applies the title outside the lock, so the native SetTitle call never holds it.
func (a *App) setWindowTitleLocked(mu *sync.Mutex, w **application.WebviewWindow, title string) {
	mu.Lock()
	win := *w
	mu.Unlock()
	if win != nil {
		win.SetTitle(title)
	}
}

// SetChatLogConfig updates whether chat logging is active and the output folder.
func (a *App) SetChatLogConfig(enabled bool, dir string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	a.logEnabled = enabled
	a.logDir = dir
}

// AppendChatLog appends a pre-formatted chat line to today's log file
// (rclog_MM_DD_YYYY.txt) under the configured folder, if logging is enabled.
func (a *App) AppendChatLog(line string) error {
	a.logMu.Lock()
	enabled, dir := a.logEnabled, a.logDir
	a.logMu.Unlock()
	if !enabled || dir == "" {
		return nil
	}
	path := filepath.Join(dir, "rclog_"+time.Now().Format("01_02_2006")+".txt")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// SetPmLogConfig updates whether PM logging is active and the output folder.
// PM logs are written under {dir}/{server}/PM_{otherAccount}_Log.txt.
func (a *App) SetPmLogConfig(enabled bool, dir string) {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	a.pmLogEnabled = enabled
	a.pmLogDir = dir
}

// sanitizeName makes an account/server string safe for use as a file/folder name.
func sanitizeName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "unknown"
	}
	repl := strings.NewReplacer(
		string(os.PathSeparator), "_", string(filepath.Separator), "_",
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_",
	)
	return repl.Replace(s)
}

// AppendPmLog appends a line to the PM log for the conversation with otherAccount
// under {pmLogDir}/{server}/PM_{otherAccount}_Log.txt. No-op if logging disabled.
func (a *App) AppendPmLog(otherAccount, line string) error {
	a.logMu.Lock()
	enabled, dir := a.pmLogEnabled, a.pmLogDir
	a.logMu.Unlock()
	if !enabled || dir == "" {
		return nil
	}
	server := sanitizeName(a.sessions.Status().ServerName)
	other := sanitizeName(otherAccount)
	if err := os.MkdirAll(filepath.Join(dir, server), 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, server, "PM_"+other+"_Log.txt")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// ChooseDirectory opens a native folder picker and returns the chosen path
// (empty if the user cancels).
func (a *App) ChooseDirectory() (string, error) {
	return a.app.Dialog.OpenFile().
		AttachToWindow(a.dialogParentWindow()).
		SetTitle("Select chat log folder").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
}

// OpenPlayerList opens (or focuses) the external Player List window. It loads
// the same SPA at the #players hash route; the frontend renders the player list
// there. Singleton: reuses the window if still open.
func (a *App) OpenPlayerList() {
	a.playerListMu.Lock()
	defer a.playerListMu.Unlock()
	if a.playerListWindow != nil {
		a.playerListWindow.Show()
		a.playerListWindow.Focus()
		return
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "players",
		Title:            serverWindowTitle(a.sessions.Status().ServerName, "Players"),
		URL:              "/#players",
		Width:            560,
		Height:           520,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.playerListWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.playerListMu.Lock()
		if a.playerListWindow == w {
			a.playerListWindow = nil
		}
		a.playerListMu.Unlock()
	})
}

// OpenPlayerListPM opens (or focuses) the independent PM window for a player.
// The method name is kept for binding compatibility with existing plugins and
// frontend clients, but PM windows no longer depend on the player list.
func (a *App) OpenPlayerListPM(playerID int) {
	if playerID < 0 {
		return
	}

	a.pmWindowMu.Lock()
	if a.pmWindows == nil {
		a.pmWindows = make(map[int]*application.WebviewWindow)
	}
	if window, ok := a.pmWindows[playerID]; ok && window != nil {
		window.Show()
		window.Focus()
		a.pmWindowMu.Unlock()
		return
	}
	a.pmWindowMu.Unlock()

	window := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "pm-" + strconv.Itoa(playerID),
		Title:            serverWindowTitle(a.sessions.Status().ServerName, "PM"),
		URL:              "/#pm?id=" + strconv.Itoa(playerID),
		Width:            520,
		Height:           560,
		MinWidth:         360,
		MinHeight:        360,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})

	a.pmWindowMu.Lock()
	// A second call can race the native window creation. Focus whichever
	// instance won the registration and close the redundant one.
	if existing, ok := a.pmWindows[playerID]; ok && existing != nil {
		existing.Show()
		existing.Focus()
		a.pmWindowMu.Unlock()
		window.Close()
		return
	}
	a.pmWindows[playerID] = window
	a.pmWindowMu.Unlock()

	window.Show()
	window.Focus()
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.pmWindowMu.Lock()
		if a.pmWindows[playerID] == window {
			delete(a.pmWindows, playerID)
		}
		a.pmWindowMu.Unlock()
	})
}

// OpenScriptManager opens (or focuses) the Script Manager window (Weapons /
// Classes / NPCs tabs). Singleton.
func (a *App) OpenScriptManager() error {
	// The initial sync runs while the engine transition mutex is held, so this
	// check cannot race the first bootstrap started during login.
	syncEngineTransitionMu.Lock()
	defer syncEngineTransitionMu.Unlock()
	if eng := a.currentSyncEngine(); eng != nil && eng.Status().InitialSync {
		return errors.New("script manager is unavailable while the initial sync is running")
	}

	a.scriptMgrMu.Lock()
	defer a.scriptMgrMu.Unlock()
	if a.scriptMgrWindow != nil {
		a.scriptMgrWindow.Show()
		a.scriptMgrWindow.Focus()
		return nil
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "scripts",
		Title:            serverWindowTitle(a.sessions.Status().ServerName, "Script Manager"),
		URL:              "/#scripts",
		Width:            720,
		Height:           560,
		MinWidth:         480,
		MinHeight:        420,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.scriptMgrWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.scriptMgrMu.Lock()
		if a.scriptMgrWindow == w {
			a.scriptMgrWindow = nil
		}
		a.scriptMgrMu.Unlock()
	})
	return nil
}

// OpenSettings opens (or focuses) the Settings window (Coding + Chat). Singleton.
func (a *App) OpenSettings() {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if a.settingsWindow != nil {
		a.settingsWindow.Show()
		a.settingsWindow.Focus()
		return
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "settings",
		Title:            serverWindowTitle(a.sessions.Status().ServerName, "Settings"),
		URL:              "/#settings",
		Width:            720,
		Height:           720,
		MinWidth:         620,
		MinHeight:        620,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.settingsWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.settingsMu.Lock()
		if a.settingsWindow == w {
			a.settingsWindow = nil
		}
		a.settingsMu.Unlock()
	})
}

// OpenPluginManager opens the dedicated, large plugin workspace window.
func (a *App) OpenPluginManager() {
	a.pluginWindowMu.Lock()
	defer a.pluginWindowMu.Unlock()
	if a.pluginWindow != nil {
		a.pluginWindow.Show()
		a.pluginWindow.Focus()
		return
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "plugins",
		Title:            serverWindowTitle(a.sessions.Status().ServerName, "Plugins"),
		URL:              "/#plugins",
		Width:            1280,
		Height:           820,
		MinWidth:         760,
		MinHeight:        560,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.pluginWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.pluginWindowMu.Lock()
		if a.pluginWindow == w {
			a.pluginWindow = nil
		}
		a.pluginWindowMu.Unlock()
	})
}

// OpenPluginDocumentation opens (or focuses) the standalone plugin SDK
// reference. Keeping documentation in its own window lets authors read the
// API while the editor stays visible in the plugin workspace.
func (a *App) OpenPluginDocumentation() {
	a.pluginDocsWindowMu.Lock()
	defer a.pluginDocsWindowMu.Unlock()
	if a.pluginDocsWindow != nil {
		a.pluginDocsWindow.Show()
		a.pluginDocsWindow.Focus()
		return
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "plugin-documentation",
		Title:            serverWindowTitle(a.sessions.Status().ServerName, "Plugin Documentation"),
		URL:              "/#plugin-docs",
		Width:            1040,
		Height:           820,
		MinWidth:         680,
		MinHeight:        560,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.pluginDocsWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.pluginDocsWindowMu.Lock()
		if a.pluginDocsWindow == w {
			a.pluginDocsWindow = nil
		}
		a.pluginDocsWindowMu.Unlock()
	})
}

// OpenFileBrowser opens (or focuses) the File Browser window. Singleton.
func (a *App) OpenFileBrowser() {
	a.fileBrowserMu.Lock()
	defer a.fileBrowserMu.Unlock()
	if a.fileBrowserWindow != nil {
		a.fileBrowserWindow.Show()
		a.fileBrowserWindow.Focus()
		return
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             "files",
		Title:            serverWindowTitle(a.sessions.Status().ServerName, "File Browser"),
		URL:              "/#files",
		Width:            920,
		Height:           600,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.fileBrowserWindow = w
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.fileBrowserMu.Lock()
		if a.fileBrowserWindow == w {
			a.fileBrowserWindow = nil
		}
		a.fileBrowserMu.Unlock()
	})
}

// scriptTypeInitial maps an editor script type to its single-letter tag for the
// window title (W/C/N/F/A). npc shares N with its flags/attrs variants in spirit
// but the variants get distinct letters so two NPC editors never collide.
func scriptTypeInitial(scriptType string) string {
	switch scriptType {
	case "weapon":
		return "W"
	case "class":
		return "C"
	case "npc":
		return "N"
	case "npcflags":
		return "F"
	case "npcattr":
		return "A"
	default:
		return ""
	}
}

// editorTitle builds an editor window title with the resource first and the
// server second (e.g. "W: sword - Zodiac" or "Server Options - Zodiac").
func editorTitle(serverName, scriptType, key string) string {
	title := strings.TrimSpace(key)
	if initial := scriptTypeInitial(scriptType); initial != "" {
		title = initial + ": " + title
	}
	if title == "" {
		title = strings.TrimSpace(scriptType)
	}
	if server := strings.TrimSpace(serverName); server != "" {
		return title + " - " + server
	}
	return title
}

// scriptEditorTitle keeps the resource label used by the editor and appends the
// active server, which makes same-named editors from different RC instances
// immediately identifiable in the taskbar.
func scriptEditorTitle(serverName, scriptType, key string) string {
	return editorTitle(serverName, scriptType, key)
}

// sanitizeWindowName turns a script type+key into a valid Wails window name.
func sanitizeWindowName(scriptType, key string) string {
	var b strings.Builder
	b.WriteString("editor-")
	b.WriteString(scriptType)
	b.WriteByte('-')
	for _, r := range key {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func isEditableScriptType(scriptType string) bool {
	switch scriptType {
	case "weapon", "class", "npc":
		return true
	default:
		return false
	}
}

// requireScriptSyncForEditor keeps the local workspace as the source of truth
// for accounts that can publish scripts. Read-only accounts can still inspect
// server scripts without configuring Sync, which preserves the LSP context
// workflow for viewers.
func (a *App) requireScriptSyncForEditor(scriptType string) error {
	if !isEditableScriptType(scriptType) {
		return nil
	}
	if a.sessions == nil {
		return errors.New("script permissions are unavailable")
	}
	session := a.sessions.Status()
	if !session.RightsReady {
		return errors.New("script permissions are still loading")
	}
	if !session.ScriptWriteAccess {
		return nil
	}

	cfg, err := a.getSyncConfig()
	if err != nil {
		return fmt.Errorf("%w: could not load the local sync configuration: %v", errScriptSyncRequired, err)
	}
	if !cfg.Enabled || strings.TrimSpace(cfg.OutputDir) == "" {
		return errScriptSyncRequired
	}

	eng := a.currentSyncEngine()
	if eng == nil {
		a.startSyncEngine()
		eng = a.currentSyncEngine()
	}
	if eng == nil {
		return errScriptSyncRequired
	}
	status := eng.Status()
	if status.InitialSync {
		return errors.New("initial script sync is still running")
	}
	if status.PanicMode {
		if status.PanicReason == "" {
			return errors.New("script sync is in panic mode")
		}
		return fmt.Errorf("script sync is in panic mode: %s", status.PanicReason)
	}
	return nil
}

// OpenScriptEditor fetches the script/flags/attributes payload from the server
// and, only on success, opens a per-script editor window for (scriptType, key).
// If the fetch fails (e.g. the account lacks read permission and the server
// never replies → timeout) the window is NOT opened and the error is returned so
// the caller can surface it. The fetched content is cached for the editor window
// to read via GetLoadedScript. If a window for this script is already open it is
// focused instead. scriptType is weapon|class|npc|npcflags|npcattr.
func (a *App) OpenScriptEditor(scriptType, key string) error {
	if err := a.requireScriptSyncForEditor(scriptType); err != nil {
		return err
	}
	mapKey := scriptType + ":" + key

	// Already open → just focus.
	a.editorMu.Lock()
	if w, ok := a.editorWindows[mapKey]; ok {
		w.Show()
		w.Focus()
		a.editorMu.Unlock()
		return nil
	}
	a.editorMu.Unlock()

	// Fetch before opening so a no-permission / no-response script never spawns a
	// blank editor window.
	reply, err := a.fetchScript(scriptType, key)
	if err != nil {
		return err
	}
	if (scriptType == "npc" || scriptType == "npcflags" || scriptType == "npcattr") && reply.Name == "" {
		reply.Name = a.npcNameByID(key)
	}

	a.editorCacheMu.Lock()
	a.editorCache[mapKey] = reply
	a.editorCacheMu.Unlock()

	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             sanitizeWindowName(scriptType, key),
		Title:            scriptEditorTitle(a.sessions.Status().ServerName, scriptType, displayScriptKey(scriptType, key, reply.Name)),
		URL:              "/#editor?t=" + scriptType + "&k=" + url.QueryEscape(key),
		Width:            820,
		Height:           620,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.editorMu.Lock()
	a.editorWindows[mapKey] = w
	a.editorMu.Unlock()
	w.Show()
	w.Focus()
	// Intercept close when there are unsaved changes: the hook runs before the
	// internal close listener, so Cancel() here prevents the window from closing
	// and gives the frontend a chance to prompt (Save / Discard / Cancel).
	w.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		a.editorCacheMu.Lock()
		dirty := a.editorDirty[mapKey]
		a.editorCacheMu.Unlock()
		if dirty {
			a.app.Event.Emit("rc:editorConfirmClose", mapKey)
			event.Cancel()
		}
	})
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.editorMu.Lock()
		isCurrent := a.editorWindows[mapKey] == w
		if isCurrent {
			delete(a.editorWindows, mapKey)
		}
		a.editorMu.Unlock()
		if !isCurrent {
			return
		}
		a.editorCacheMu.Lock()
		delete(a.editorCache, mapKey)
		delete(a.editorDirty, mapKey)
		a.editorCacheMu.Unlock()
	})
	return nil
}

func (a *App) npcNameByID(key string) string {
	id, err := strconv.Atoi(key)
	if err != nil {
		return ""
	}
	npcs, err := a.sessions.GetNPCs()
	if err != nil {
		return ""
	}
	for _, npc := range npcs {
		if npc.ID == id {
			return npc.Name
		}
	}
	return ""
}

func displayScriptKey(scriptType, key, name string) string {
	if (scriptType == "npc" || scriptType == "npcflags" || scriptType == "npcattr") && name != "" {
		return name
	}
	return key
}

// SetEditorDirty tracks whether an open editor window has unsaved changes, so
// the close-interception hook can decide whether to prompt.
func (a *App) SetEditorDirty(scriptType, key string, dirty bool) {
	a.editorCacheMu.Lock()
	if dirty {
		a.editorDirty[scriptType+":"+key] = true
	} else {
		delete(a.editorDirty, scriptType+":"+key)
	}
	a.editorCacheMu.Unlock()
}

// editorWindowsSnapshot reports whether the script editor window is open. It
// is kept separate from editorDirty because an open, clean editor still needs
// server changes surfaced before they replace its in-memory contents.
func (a *App) editorWindowsSnapshot(scriptType, key string) (*application.WebviewWindow, bool) {
	a.editorMu.Lock()
	defer a.editorMu.Unlock()
	w, ok := a.editorWindows[scriptType+":"+key]
	return w, ok
}

// CloseScriptEditor closes the editor window for (scriptType, key). Used after a
// Save/Discard confirmation; dirty must already be cleared so the close hook
// does not re-prompt.
func (a *App) CloseScriptEditor(scriptType, key string) {
	a.editorMu.Lock()
	w := a.editorWindows[scriptType+":"+key]
	a.editorMu.Unlock()
	if w != nil {
		w.Close()
	}
}

// fetchScript routes to the right Service request for the script type.
func (a *App) fetchScript(scriptType, key string) (rclib.ScriptReply, error) {
	switch scriptType {
	case "npcflags":
		id, err := strconv.Atoi(key)
		if err != nil {
			return rclib.ScriptReply{}, err
		}
		return a.sessions.OpenNPCFlags(id)
	case "npcattr":
		id, err := strconv.Atoi(key)
		if err != nil {
			return rclib.ScriptReply{}, err
		}
		return a.sessions.OpenNPCAttributes(id)
	case "options", "folder_config", "flags":
		// Server-side text configs travel on the main socket (on_server_data);
		// key is just a display label here and is ignored by the fetch.
		return a.sessions.OpenServerText(scriptType)
	default:
		return a.sessions.OpenScript(scriptType, key)
	}
}

// GetLoadedScript returns the script payload cached by a successful
// OpenScriptEditor for this window. The editor window reads it on mount instead
// of issuing another NC request.
func (a *App) GetLoadedScript(scriptType, key string) (rclib.ScriptReply, error) {
	a.editorCacheMu.Lock()
	defer a.editorCacheMu.Unlock()
	if reply, ok := a.editorCache[scriptType+":"+key]; ok {
		return reply, nil
	}
	return rclib.ScriptReply{}, errors.New("no cached script for this window")
}

// GraalScriptLSPRequest handles one complete JSON-RPC 2.0 message for the
// embedded GraalScript language server. The frontend keeps the official LSP
// wire shape even though Wails is the transport inside the desktop app; this
// makes a future stdio/VS Code adapter a transport-only change.
func (a *App) GraalScriptLSPRequest(message string) (string, error) {
	if strings.TrimSpace(message) == "" {
		return "", errors.New("empty GraalScript LSP message")
	}
	var envelope struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal([]byte(message), &envelope)
	cfg := a.GetSyncConfig()
	a.graalScriptLSP.SetEnabled(cfg.Enabled && strings.TrimSpace(cfg.OutputDir) != "")
	serverContext := a.sessions.GetServerScriptContext()
	a.graalScriptLSP.SetServerContext(graalscript.ServerScriptContext{
		ServerOptions: serverContext.ServerOptions,
		ServerFlags:   serverContext.ServerFlags,
	})
	response, err := a.graalScriptLSP.HandleJSON([]byte(message))
	if err != nil {
		if envelope.Method == "initialize" {
			a.emitGraalScriptLSPStatus("error", err.Error())
		}
		return "", err
	}
	if envelope.Method == "initialize" {
		var rpc struct {
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(response, &rpc) == nil && rpc.Error != nil {
			a.emitGraalScriptLSPStatus("error", rpc.Error.Message)
		} else {
			a.emitGraalScriptLSPStatus("ready", "")
		}
	}
	return string(response), nil
}

func (a *App) emitGraalScriptLSPStatus(state, message string) {
	if a.app == nil {
		return
	}
	payload, err := json.Marshal(struct {
		State   string `json:"state"`
		Message string `json:"message,omitempty"`
	}{State: state, Message: message})
	if err == nil {
		a.app.Event.Emit("rc:lspStatus", string(payload))
	}
}

// RefreshGraalScriptDocAPI refetches the GScript reference and the active
// server options/flags used by the embedded GraalScript LSP.
func (a *App) RefreshGraalScriptDocAPI() error {
	if err := a.sessions.RefreshServerScriptContext(); err != nil {
		return err
	}
	return a.graalScriptLSP.RefreshDefinitions()
}

// CodingSettings are the Monaco editor appearance, indentation, and external
// editor preferences, persisted to a file so every editor window (its own
// webview) reads the same values — localStorage is not reliably shared across
// Wails v3 windows.
type CodingSettings struct {
	Theme          string `json:"theme"`
	FontFamily     string `json:"fontFamily"`
	FontSize       int    `json:"fontSize"`
	TabSize        int    `json:"tabSize"`
	ExternalEditor string `json:"externalEditor"`
}

// ChatSettings are the chat colors and local logging preferences shared by
// every Wails window. They live in the backend because each window has its own
// webview and localStorage is not reliably shared between those windows.
type ChatSettings struct {
	Timestamp string `json:"timestamp"`
	RCPrefix  string `json:"rcPrefix"`
	NCPrefix  string `json:"ncPrefix"`
	IRCPrefix string `json:"ircPrefix"`
	Speaker   string `json:"speaker"`
	Content   string `json:"content"`
	LogChat   bool   `json:"logChat"`
	LogDir    string `json:"logDir"`
	PMLog     bool   `json:"pmLog"`
	PMLogDir  string `json:"pmLogDir"`
}

// ChatSettingsState tells the frontend whether the backend has a persisted
// value, so the first version using backend storage can migrate old
// localStorage preferences without overwriting them with defaults.
type ChatSettingsState struct {
	Settings ChatSettings `json:"settings"`
	Exists   bool         `json:"exists"`
}

const (
	externalEditorVSCode      = "vscode"
	externalEditorSublime     = "sublime"
	externalEditorNotepadPlus = "notepad++"
)

func isSupportedExternalEditor(editor string) bool {
	switch strings.ToLower(strings.TrimSpace(editor)) {
	case externalEditorVSCode, externalEditorSublime, externalEditorNotepadPlus:
		return true
	default:
		return false
	}
}

func normalizeExternalEditor(editor string) string {
	editor = strings.ToLower(strings.TrimSpace(editor))
	if isSupportedExternalEditor(editor) {
		return editor
	}
	return externalEditorVSCode
}

// AppTheme contains the application surface colors. Monaco themes remain
// separate because they control editor tokens rather than the RC chrome.
type AppTheme struct {
	Key    string            `json:"key"`
	Name   string            `json:"name"`
	Mode   string            `json:"mode"`
	Colors map[string]string `json:"colors"`
}

// AppThemeStore is shared by every Wails window through the backend config
// file and the rc:appTheme event.
type AppThemeStore struct {
	ActiveKey string     `json:"activeKey"`
	Themes    []AppTheme `json:"themes"`
}

var appThemeColorKeys = map[string]struct{}{
	"background": {}, "foreground": {}, "card": {}, "cardForeground": {},
	"popover": {}, "popoverForeground": {}, "primary": {}, "primaryForeground": {},
	"secondary": {}, "secondaryForeground": {}, "muted": {}, "mutedForeground": {},
	"accent": {}, "accentForeground": {}, "destructive": {}, "destructiveForeground": {},
	"border": {}, "windowBorder": {}, "input": {}, "ring": {}, "serverAccent": {},
	"chart1": {}, "chart2": {}, "chart3": {}, "chart4": {}, "chart5": {},
	"sidebar": {}, "sidebarForeground": {}, "sidebarPrimary": {},
	"sidebarPrimaryForeground": {}, "sidebarAccent": {}, "sidebarAccentForeground": {},
	"sidebarBorder": {}, "sidebarRing": {},
}

// GetLanguage returns the UI language persisted for this Windows user.
func (a *App) GetLanguage() string {
	a.languageMu.Lock()
	defer a.languageMu.Unlock()
	if a.language == "" {
		a.language = loadLanguage()
	}
	return a.language
}

// SetLanguage persists the UI language and broadcasts it to every Wails
// window, so an already-open settings/editor window updates immediately.
func (a *App) SetLanguage(language string) error {
	if language != "pt-BR" && language != "en" && language != "es" {
		return fmt.Errorf("unsupported language: %s", language)
	}
	a.languageMu.Lock()
	a.language = language
	a.languageMu.Unlock()
	path, err := languagePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(struct {
		Language string `json:"language"`
	}{language})
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("rc:language", language)
	}
	return nil
}

func languagePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "language.json"), nil
}

func loadLanguage() string {
	path, err := languagePath()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		Language string `json:"language"`
	}
	if json.Unmarshal(b, &cfg) == nil && (cfg.Language == "pt-BR" || cfg.Language == "en" || cfg.Language == "es") {
		return cfg.Language
	}
	return ""
}

// DefaultCodingSettings are the first-run defaults.
var DefaultCodingSettings = CodingSettings{
	Theme:          "gs-default-dark",
	FontFamily:     "Consolas, 'Courier New', monospace",
	FontSize:       14,
	TabSize:        2,
	ExternalEditor: externalEditorVSCode,
}

// DefaultChatSettings are the first-run chat appearance and logging values.
var DefaultChatSettings = ChatSettings{
	Timestamp: "#22c55e",
	RCPrefix:  "#22c55e",
	NCPrefix:  "#38bdf8",
	IRCPrefix: "#a78bfa",
	Speaker:   "#facc15",
	Content:   "#e5e7eb",
}

func normalizeChatColor(value, fallback string) string {
	value = strings.TrimSpace(value)
	if len(value) != 7 || value[0] != '#' {
		return fallback
	}
	for _, r := range value[1:] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return fallback
		}
	}
	return strings.ToUpper(value)
}

func normalizeChatSettings(settings ChatSettings) ChatSettings {
	defaults := DefaultChatSettings
	settings.Timestamp = normalizeChatColor(settings.Timestamp, defaults.Timestamp)
	settings.RCPrefix = normalizeChatColor(settings.RCPrefix, defaults.RCPrefix)
	settings.NCPrefix = normalizeChatColor(settings.NCPrefix, defaults.NCPrefix)
	settings.IRCPrefix = normalizeChatColor(settings.IRCPrefix, defaults.IRCPrefix)
	settings.Speaker = normalizeChatColor(settings.Speaker, defaults.Speaker)
	settings.Content = normalizeChatColor(settings.Content, defaults.Content)
	settings.LogDir = strings.TrimSpace(settings.LogDir)
	settings.PMLogDir = strings.TrimSpace(settings.PMLogDir)
	return settings
}

func chatSettingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "chat.json"), nil
}

// loadChatSettingsLocked reads chat settings from the per-user backend file.
// Caller must hold a.chatSettingsMu.
func (a *App) loadChatSettingsLocked() ChatSettings {
	a.chatSettings = normalizeChatSettings(DefaultChatSettings)
	a.chatSettingsLoaded = true
	a.chatSettingsExists = false
	path, err := chatSettingsPath()
	if err != nil {
		return a.chatSettings
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return a.chatSettings
	}
	var settings ChatSettings
	if err := json.Unmarshal(b, &settings); err == nil {
		a.chatSettings = normalizeChatSettings(settings)
		a.chatSettingsExists = true
	}
	return a.chatSettings
}

// GetChatSettings returns the shared chat preferences.
func (a *App) GetChatSettings() ChatSettingsState {
	a.chatSettingsMu.Lock()
	defer a.chatSettingsMu.Unlock()
	if !a.chatSettingsLoaded {
		a.loadChatSettingsLocked()
	}
	return ChatSettingsState{Settings: a.chatSettings, Exists: a.chatSettingsExists}
}

// SetChatSettings persists chat preferences and broadcasts them to every open
// Wails window so the RC chat updates immediately after editing Settings.
func (a *App) SetChatSettings(settings ChatSettings) error {
	settings = normalizeChatSettings(settings)

	a.chatSettingsMu.Lock()
	if !a.chatSettingsLoaded {
		a.loadChatSettingsLocked()
	}
	a.chatSettings = settings
	a.chatSettingsExists = true
	err := a.persistChatSettingsLocked(settings)
	a.chatSettingsMu.Unlock()
	if err != nil {
		return err
	}

	a.logMu.Lock()
	a.logEnabled = settings.LogChat
	a.logDir = settings.LogDir
	a.pmLogEnabled = settings.PMLog
	a.pmLogDir = settings.PMLogDir
	a.logMu.Unlock()
	return nil
}

func (a *App) persistChatSettingsLocked(settings ChatSettings) error {
	path, err := chatSettingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("rc:chatSettings", string(b))
	}
	return nil
}

// codingPath returns the coding-settings file location.
func codingPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "coding.json"), nil
}

// loadCodingSettings reads the persisted coding settings (defaults on first run
// or read error). Caller must hold a.codingMu.
func (a *App) loadCodingSettingsLocked() CodingSettings {
	a.codingSettings = DefaultCodingSettings
	path, err := codingPath()
	if err != nil {
		return a.codingSettings
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return a.codingSettings
	}
	var cs CodingSettings
	if err := json.Unmarshal(b, &cs); err == nil {
		if cs.Theme != "" {
			a.codingSettings.Theme = cs.Theme
		}
		if cs.FontFamily != "" {
			a.codingSettings.FontFamily = cs.FontFamily
		}
		if cs.FontSize > 0 {
			a.codingSettings.FontSize = cs.FontSize
		}
		if isSupportedTabSize(cs.TabSize) {
			a.codingSettings.TabSize = cs.TabSize
		}
		a.codingSettings.ExternalEditor = normalizeExternalEditor(cs.ExternalEditor)
	}
	return a.codingSettings
}

func isSupportedTabSize(size int) bool {
	return size == 1 || size == 2 || size == 4
}

// GetCodingSettings returns the cached coding settings (loading once).
func (a *App) GetCodingSettings() CodingSettings {
	a.codingMu.Lock()
	defer a.codingMu.Unlock()
	if a.codingSettings.Theme == "" {
		a.loadCodingSettingsLocked()
	}
	return a.codingSettings
}

// SetCodingSettings persists the coding settings and broadcasts them so every
// open editor window updates its theme, font, and indentation live.
func (a *App) SetCodingSettings(theme, fontFamily string, fontSize, tabSize int) error {
	if !isSupportedTabSize(tabSize) {
		return fmt.Errorf("unsupported tab size: %d", tabSize)
	}
	a.codingMu.Lock()
	defer a.codingMu.Unlock()
	if a.codingSettings.Theme == "" {
		a.loadCodingSettingsLocked()
	}
	a.codingSettings = CodingSettings{
		Theme:          theme,
		FontFamily:     fontFamily,
		FontSize:       fontSize,
		TabSize:        tabSize,
		ExternalEditor: normalizeExternalEditor(a.codingSettings.ExternalEditor),
	}
	return a.persistCodingSettings(a.codingSettings)
}

// SetExternalEditor persists one of the supported editor presets and updates
// every open Wails window through the existing coding-settings event.
func (a *App) SetExternalEditor(editor string) error {
	editor = strings.ToLower(strings.TrimSpace(editor))
	if !isSupportedExternalEditor(editor) {
		return fmt.Errorf("unsupported external editor: %s", editor)
	}

	a.codingMu.Lock()
	defer a.codingMu.Unlock()
	if a.codingSettings.Theme == "" {
		a.loadCodingSettingsLocked()
	}
	a.codingSettings.ExternalEditor = editor
	return a.persistCodingSettings(a.codingSettings)
}

func (a *App) persistCodingSettings(cs CodingSettings) error {
	path, err := codingPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(cs)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("rc:codingSettings", string(b))
	}
	return nil
}

func appThemesPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "app-themes.json"), nil
}

func normalizeAppTheme(theme AppTheme) AppTheme {
	theme.Key = strings.TrimSpace(theme.Key)
	theme.Name = strings.TrimSpace(theme.Name)
	theme.Mode = strings.TrimSpace(theme.Mode)
	if theme.Colors == nil {
		theme.Colors = map[string]string{}
	}
	colors := make(map[string]string, len(theme.Colors))
	for key, value := range theme.Colors {
		colors[key] = strings.TrimSpace(value)
	}
	theme.Colors = colors
	return theme
}

func isSafeAppThemeKey(key string) bool {
	if key == "" || len(key) > 64 {
		return false
	}
	for index, char := range key {
		if index == 0 && !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9')) {
			return false
		}
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func validateAppTheme(theme AppTheme) error {
	if !isSafeAppThemeKey(theme.Key) {
		return errors.New("theme key must contain only lowercase letters, numbers, '-' or '_'")
	}
	if theme.Name == "" || len(theme.Name) > 80 {
		return errors.New("theme name is required and must be at most 80 characters")
	}
	if theme.Mode != "light" && theme.Mode != "dark" {
		return fmt.Errorf("unsupported theme mode: %s", theme.Mode)
	}
	if len(theme.Colors) == 0 || len(theme.Colors) > len(appThemeColorKeys) {
		return errors.New("theme must contain at least one supported color")
	}
	for key, value := range theme.Colors {
		if _, ok := appThemeColorKeys[key]; !ok {
			return fmt.Errorf("unsupported theme color: %s", key)
		}
		lowerValue := strings.ToLower(value)
		if value == "" || len(value) > 160 || strings.ContainsAny(value, "{};") || strings.Contains(lowerValue, "url(") || strings.Contains(lowerValue, "expression(") || strings.Contains(lowerValue, "javascript:") {
			return fmt.Errorf("invalid value for theme color: %s", key)
		}
	}
	return nil
}

func (a *App) loadAppThemeStoreLocked() (AppThemeStore, error) {
	path, err := appThemesPath()
	if err != nil {
		return AppThemeStore{}, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return AppThemeStore{Themes: []AppTheme{}}, nil
	}
	if err != nil {
		return AppThemeStore{}, err
	}
	var store AppThemeStore
	if err := json.Unmarshal(b, &store); err != nil {
		return AppThemeStore{Themes: []AppTheme{}}, nil
	}
	validThemes := make([]AppTheme, 0, len(store.Themes))
	for _, theme := range store.Themes {
		theme = normalizeAppTheme(theme)
		if validateAppTheme(theme) == nil {
			validThemes = append(validThemes, theme)
		}
	}
	store.Themes = validThemes
	return store, nil
}

func persistAppThemeStoreLocked(store AppThemeStore) error {
	path, err := appThemesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func (a *App) broadcastAppThemeStore(store AppThemeStore) {
	if a.app == nil {
		return
	}
	if payload, err := json.Marshal(store); err == nil {
		a.app.Event.Emit("rc:appTheme", string(payload))
	}
}

// GetAppThemeStore loads persisted custom themes and the active theme key.
func (a *App) GetAppThemeStore() (AppThemeStore, error) {
	a.appThemeMu.Lock()
	defer a.appThemeMu.Unlock()
	return a.loadAppThemeStoreLocked()
}

// SaveAppTheme creates or updates a user-defined application theme.
func (a *App) SaveAppTheme(theme AppTheme) error {
	theme = normalizeAppTheme(theme)
	if err := validateAppTheme(theme); err != nil {
		return err
	}
	a.appThemeMu.Lock()
	store, err := a.loadAppThemeStoreLocked()
	if err == nil {
		found := false
		for index := range store.Themes {
			if store.Themes[index].Key == theme.Key {
				store.Themes[index] = theme
				found = true
				break
			}
		}
		if !found {
			store.Themes = append(store.Themes, theme)
		}
	}
	if err == nil {
		err = persistAppThemeStoreLocked(store)
	}
	a.appThemeMu.Unlock()
	if err != nil {
		return err
	}
	a.broadcastAppThemeStore(store)
	return nil
}

// SetActiveAppTheme selects a built-in or user-defined application theme.
func (a *App) SetActiveAppTheme(key string) error {
	key = strings.TrimSpace(key)
	if key != "" && !isSafeAppThemeKey(key) {
		return errors.New("invalid theme key")
	}
	a.appThemeMu.Lock()
	store, err := a.loadAppThemeStoreLocked()
	if err == nil {
		store.ActiveKey = key
		err = persistAppThemeStoreLocked(store)
	}
	a.appThemeMu.Unlock()
	if err != nil {
		return err
	}
	a.broadcastAppThemeStore(store)
	return nil
}

// DeleteAppTheme removes a user-defined application theme.
func (a *App) DeleteAppTheme(key string) error {
	key = strings.TrimSpace(key)
	if !isSafeAppThemeKey(key) {
		return errors.New("invalid theme key")
	}
	a.appThemeMu.Lock()
	store, err := a.loadAppThemeStoreLocked()
	if err == nil {
		filtered := store.Themes[:0]
		for _, theme := range store.Themes {
			if theme.Key != key {
				filtered = append(filtered, theme)
			}
		}
		store.Themes = filtered
		if store.ActiveKey == key {
			store.ActiveKey = ""
		}
		err = persistAppThemeStoreLocked(store)
	}
	a.appThemeMu.Unlock()
	if err != nil {
		return err
	}
	a.broadcastAppThemeStore(store)
	return nil
}

// FileBrowserConfig holds the file-browser preferences, persisted to a file so
// every file-browser window reads the same values — localStorage is not
// reliably shared across Wails v3 windows.
type FileBrowserConfig struct {
	DownloadDir         string `json:"downloadDir"`
	ShowImageThumbnails bool   `json:"showImageThumbnails"`
}

// fileBrowserPath returns the file-browser config file location.
func fileBrowserPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "filebrowser.json"), nil
}

// loadFileBrowserConfigLocked reads the persisted file-browser config. Caller
// must hold a.fileBrowserCfgMu.
func (a *App) loadFileBrowserConfigLocked() FileBrowserConfig {
	a.fileBrowserCfg = FileBrowserConfig{}
	a.fileBrowserCfgLoaded = true
	path, err := fileBrowserPath()
	if err != nil {
		return a.fileBrowserCfg
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return a.fileBrowserCfg
	}
	var cfg FileBrowserConfig
	if err := json.Unmarshal(b, &cfg); err == nil {
		a.fileBrowserCfg = cfg
	}
	return a.fileBrowserCfg
}

// GetFileBrowserConfig returns the cached file-browser config (loading once).
func (a *App) GetFileBrowserConfig() FileBrowserConfig {
	a.fileBrowserCfgMu.Lock()
	defer a.fileBrowserCfgMu.Unlock()
	if !a.fileBrowserCfgLoaded {
		a.loadFileBrowserConfigLocked()
	}
	return a.fileBrowserCfg
}

// SetFileBrowserConfig persists the downloads folder and broadcasts it so the
// file-browser window updates live.
func (a *App) SetFileBrowserConfig(downloadDir string) error {
	a.fileBrowserCfgMu.Lock()
	if !a.fileBrowserCfgLoaded {
		a.loadFileBrowserConfigLocked()
	}
	a.fileBrowserCfg.DownloadDir = downloadDir
	cfg := a.fileBrowserCfg
	a.fileBrowserCfgMu.Unlock()
	return a.persistFileBrowserConfig(cfg)
}

// SetFileBrowserImageThumbnails persists whether the File Browser may fetch
// small image files for inline previews. The limit is enforced independently by
// GetFileBrowserImageThumbnail, so this preference never enables large-file
// downloads.
func (a *App) SetFileBrowserImageThumbnails(enabled bool) error {
	a.fileBrowserCfgMu.Lock()
	if !a.fileBrowserCfgLoaded {
		a.loadFileBrowserConfigLocked()
	}
	a.fileBrowserCfg.ShowImageThumbnails = enabled
	cfg := a.fileBrowserCfg
	a.fileBrowserCfgMu.Unlock()
	return a.persistFileBrowserConfig(cfg)
}

func (a *App) persistFileBrowserConfig(cfg FileBrowserConfig) error {
	path, err := fileBrowserPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("rc:fbConfig", string(b))
	}
	return nil
}

// RemoteTheme is a cached third-party Monaco theme (from the
// brijeshb42/monaco-themes gallery): a friendly name plus the raw JSON
// IStandaloneThemeData definition, stored so the theme survives restarts and
// works offline after first selection.
type RemoteTheme struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

// CustomTheme is a user-authored Monaco theme definition. The definition is
// kept as JSON so all Monaco token rules and editor colors remain editable.
type CustomTheme struct {
	Key        string `json:"key"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

// remoteThemePath returns the cached remote-theme file location.
func remoteThemePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "remote-theme.json"), nil
}

// GetRemoteTheme returns the cached remote theme (empty if none).
func (a *App) GetRemoteTheme() (RemoteTheme, error) {
	path, err := remoteThemePath()
	if err != nil {
		return RemoteTheme{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return RemoteTheme{}, nil // none cached yet
	}
	var rt RemoteTheme
	if err := json.Unmarshal(b, &rt); err != nil {
		return RemoteTheme{}, nil
	}
	return rt, nil
}

// SaveRemoteTheme caches a remote theme and broadcasts it so open editor windows
// defineTheme + apply it live.
func (a *App) SaveRemoteTheme(name, definition string) error {
	rt := RemoteTheme{Name: name, Definition: definition}
	path, err := remoteThemePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(rt)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("rc:remoteTheme", string(b))
	}
	return nil
}

func customThemesPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "graal-rc", "custom-themes.json"), nil
}

func (a *App) GetCustomThemes() ([]CustomTheme, error) {
	path, err := customThemesPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []CustomTheme{}, nil
	}
	if err != nil {
		return nil, err
	}
	var themes []CustomTheme
	if err := json.Unmarshal(b, &themes); err != nil {
		return []CustomTheme{}, nil
	}
	return themes, nil
}

func (a *App) SaveCustomTheme(theme CustomTheme) error {
	theme.Key = strings.TrimSpace(theme.Key)
	theme.Name = strings.TrimSpace(theme.Name)
	if theme.Key == "" || theme.Name == "" || theme.Definition == "" {
		return errors.New("theme key, name and definition are required")
	}
	var definition map[string]any
	if err := json.Unmarshal([]byte(theme.Definition), &definition); err != nil {
		return fmt.Errorf("invalid theme JSON: %w", err)
	}
	if _, ok := definition["colors"]; !ok {
		definition["colors"] = map[string]any{}
	}
	themes, err := a.GetCustomThemes()
	if err != nil {
		return err
	}
	found := false
	for i := range themes {
		if themes[i].Key == theme.Key {
			themes[i] = theme
			found = true
			break
		}
	}
	if !found {
		themes = append(themes, theme)
	}
	path, err := customThemesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(themes)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return err
	}
	if a.app != nil {
		if payload, marshalErr := json.Marshal(theme); marshalErr == nil {
			a.app.Event.Emit("rc:customTheme", string(payload))
		}
	}
	return nil
}

func (a *App) DeleteCustomTheme(key string) error {
	themes, err := a.GetCustomThemes()
	if err != nil {
		return err
	}
	filtered := themes[:0]
	for _, theme := range themes {
		if theme.Key != key {
			filtered = append(filtered, theme)
		}
	}
	path, err := customThemesPath()
	if err != nil {
		return err
	}
	b, err := json.Marshal(filtered)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
