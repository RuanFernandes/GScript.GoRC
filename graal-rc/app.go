package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"graal-rc/internal/connection"
	"graal-rc/internal/credentials"
	"graal-rc/rclib"
)

var (
	errNoVault         = errors.New("account vault is not available")
	errAccountNotFound = errors.New("account not found")
)

// App is the Wails v3 service: its public methods are auto-bound to the
// frontend. It delegates session logic to the connection Service and account
// storage to the credentials Vault (Single Responsibility).
type App struct {
	app      *application.App
	sessions *connection.Service
	vault    *credentials.Vault

	logMu      sync.Mutex
	logEnabled bool
	logDir     string

	playerListMu     sync.Mutex
	playerListWindow *application.WebviewWindow

	scriptMgrMu     sync.Mutex
	scriptMgrWindow *application.WebviewWindow

	settingsMu     sync.Mutex
	settingsWindow *application.WebviewWindow

	editorMu    sync.Mutex
	editorWindows map[string]*application.WebviewWindow

	editorCacheMu sync.Mutex
	editorCache   map[string]rclib.ScriptReply
	editorDirty   map[string]bool

	codingMu       sync.Mutex
	codingSettings CodingSettings
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
	return &App{sessions: connection.NewService(), vault: vault, editorWindows: map[string]*application.WebviewWindow{}, editorCache: map[string]rclib.ScriptReply{}, editorDirty: map[string]bool{}}
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
	var seq uint64
	a.sessions.SetEmitter(func(name string, data ...any) {
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
	})
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
		Nickname: c.Nickname,
		Account:  c.Account,
		Password: c.Password,
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
	Nickname string `json:"nickname"`
	Account  string `json:"account"`
	Password string `json:"password"`
}

// AccountSummary is the password-less account projection exposed to the
// frontend. Aliased so the generated Wails model matches the credentials type.
type AccountSummary = credentials.AccountSummary

func toCreds(req LoginRequest) connection.Credentials {
	return connection.Credentials{Nickname: req.Nickname, Account: req.Account, Password: req.Password}
}

func accountToCreds(a credentials.Account) connection.Credentials {
	return connection.Credentials{Nickname: a.Nickname, Account: a.Account, Password: a.Password}
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
func (a *App) LoginWithAccount(accountName string) ([]rclib.Server, error) {
	acc, err := a.findAccount(accountName)
	if err != nil {
		return nil, err
	}
	return a.sessions.Login(accountToCreds(acc))
}

// AddAccount logs in with the supplied credentials and, on success, persists
// them to the vault. On failure nothing is saved.
func (a *App) AddAccount(req LoginRequest) ([]rclib.Server, error) {
	servers, err := a.sessions.Login(toCreds(req))
	if err != nil {
		return nil, err
	}
	if a.vault != nil {
		if saveErr := a.vault.Add(credentials.Account{
			Nickname: req.Nickname,
			Account:  req.Account,
			Password: req.Password,
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
	return a.vault.Remove(accountName)
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

// ConnectToServer authenticates to the server at the given index.
func (a *App) ConnectToServer(index int) error { return a.sessions.ConnectToServer(index) }

// SetNewProtocol toggles newer-protocol compatibility before server login.
func (a *App) SetNewProtocol(enable bool) error { return a.sessions.SetNewProtocol(enable) }

// Logout drops the active session.
func (a *App) Logout() { a.sessions.Logout() }

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

// --- Script management (NC server) ---

// ScriptListType is "weapon" | "class" | "npc".
type ScriptListType = string

// GetWeapons returns the cached weapon list.
func (a *App) GetWeapons() ([]rclib.Weapon, error) { return a.sessions.GetWeapons() }

// GetClasses returns the cached class list.
func (a *App) GetClasses() ([]rclib.Class, error) { return a.sessions.GetClasses() }

// GetNPCs returns the cached NPC list.
func (a *App) GetNPCs() ([]rclib.NPC, error) { return a.sessions.GetNPCs() }

// AddWeapon creates a weapon by name.
func (a *App) AddWeapon(name string) error { return a.sessions.AddWeapon(name) }

// DeleteWeapon deletes a weapon by name.
func (a *App) DeleteWeapon(name string) error { return a.sessions.DeleteWeapon(name) }

// AddClass creates a class by name.
func (a *App) AddClass(name string) error { return a.sessions.AddClass(name) }

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
	return a.sessions.OpenScript(scriptType, key)
}

// SaveWeapon writes a weapon's script back.
func (a *App) SaveWeapon(name, script string) error {
	return a.sessions.SaveWeapon(name, script)
}

// SaveClass writes a class's script back.
func (a *App) SaveClass(name, script string) error { return a.sessions.SaveClass(name, script) }

// SaveNPC writes an NPC's script back.
func (a *App) SaveNPC(id int, script string) error { return a.sessions.SaveNPC(id, script) }

// ResetNPC resets an NPC by id.
func (a *App) ResetNPC(id int) error { return a.sessions.ResetNPC(id) }

// OpenNPCFlags fetches an NPC's flags and returns them.
func (a *App) OpenNPCFlags(id int) (rclib.ScriptReply, error) { return a.sessions.OpenNPCFlags(id) }

// OpenNPCAttributes fetches an NPC's attributes and returns them.
func (a *App) OpenNPCAttributes(id int) (rclib.ScriptReply, error) {
	return a.sessions.OpenNPCAttributes(id)
}

// SaveNPCFlags writes an NPC's flags back.
func (a *App) SaveNPCFlags(id int, flags string) error { return a.sessions.SaveNPCFlags(id, flags) }

// WarpNPC warps an NPC to (x, y) on the given level.
func (a *App) WarpNPC(id int, x, y float64, level string) error {
	return a.sessions.WarpNPC(id, x, y, level)
}

// RefreshWeapons re-requests the weapon list from the NC server.
func (a *App) RefreshWeapons() error { return a.sessions.RefreshWeapons() }

// Status returns the current session status snapshot.
func (a *App) Status() connection.Status { return a.sessions.Status() }

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

// ChooseDirectory opens a native folder picker and returns the chosen path
// (empty if the user cancels).
func (a *App) ChooseDirectory() (string, error) {
	return a.app.Dialog.OpenFile().
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
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "players",
		Title:            "Players",
		URL:              "/#players",
		Width:            560,
		Height:           520,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.playerListWindow = w
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.playerListMu.Lock()
		a.playerListWindow = nil
		a.playerListMu.Unlock()
	})
}

// OpenScriptManager opens (or focuses) the Script Manager window (Weapons /
// Classes / NPCs tabs). Singleton.
func (a *App) OpenScriptManager() {
	a.scriptMgrMu.Lock()
	defer a.scriptMgrMu.Unlock()
	if a.scriptMgrWindow != nil {
		a.scriptMgrWindow.Show()
		a.scriptMgrWindow.Focus()
		return
	}
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "scripts",
		Title:            "Script Manager",
		URL:              "/#scripts",
		Width:            720,
		Height:           560,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.scriptMgrWindow = w
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.scriptMgrMu.Lock()
		a.scriptMgrWindow = nil
		a.scriptMgrMu.Unlock()
	})
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
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "settings",
		Title:            "Settings",
		URL:              "/#settings",
		Width:            520,
		Height:           620,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.settingsWindow = w
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.settingsMu.Lock()
		a.settingsWindow = nil
		a.settingsMu.Unlock()
	})
}

// editorTitle builds a human window title for an editor (script / flags / attrs).
func editorTitle(scriptType, key string) string {
	switch scriptType {
	case "npcflags":
		return "Edit Flags — NPC " + key
	case "npcattr":
		return "Attributes — NPC " + key
	default:
		return scriptType + ": " + key
	}
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

// OpenScriptEditor fetches the script/flags/attributes payload from the server
// and, only on success, opens a per-script editor window for (scriptType, key).
// If the fetch fails (e.g. the account lacks read permission and the server
// never replies → timeout) the window is NOT opened and the error is returned so
// the caller can surface it. The fetched content is cached for the editor window
// to read via GetLoadedScript. If a window for this script is already open it is
// focused instead. scriptType is weapon|class|npc|npcflags|npcattr.
func (a *App) OpenScriptEditor(scriptType, key string) error {
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

	a.editorCacheMu.Lock()
	a.editorCache[mapKey] = reply
	a.editorCacheMu.Unlock()

	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             sanitizeWindowName(scriptType, key),
		Title:            editorTitle(scriptType, key),
		URL:              "/#editor?t=" + scriptType + "&k=" + url.QueryEscape(key),
		Width:            820,
		Height:           620,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.editorMu.Lock()
	a.editorWindows[mapKey] = w
	a.editorMu.Unlock()
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
		delete(a.editorWindows, mapKey)
		a.editorMu.Unlock()
		a.editorCacheMu.Lock()
		delete(a.editorCache, mapKey)
		delete(a.editorDirty, mapKey)
		a.editorCacheMu.Unlock()
	})
	return nil
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

// CodingSettings are the Monaco editor appearance prefs, persisted to a file so
// every editor window (its own webview) reads the same values — localStorage is
// not reliably shared across Wails v3 windows.
type CodingSettings struct {
	Theme      string `json:"theme"`
	FontFamily string `json:"fontFamily"`
	FontSize   int    `json:"fontSize"`
}

// DefaultCodingSettings are the first-run defaults.
var DefaultCodingSettings = CodingSettings{
	Theme:      "vs-dark",
	FontFamily: "Consolas, 'Courier New', monospace",
	FontSize:   14,
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
	}
	return a.codingSettings
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
// open editor window updates its theme/font live.
func (a *App) SetCodingSettings(theme, fontFamily string, fontSize int) error {
	a.codingMu.Lock()
	a.codingSettings = CodingSettings{Theme: theme, FontFamily: fontFamily, FontSize: fontSize}
	cs := a.codingSettings
	a.codingMu.Unlock()

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

// RemoteTheme is a cached third-party Monaco theme (from the
// brijeshb42/monaco-themes gallery): a friendly name plus the raw JSON
// IStandaloneThemeData definition, stored so the theme survives restarts and
// works offline after first selection.
type RemoteTheme struct {
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
