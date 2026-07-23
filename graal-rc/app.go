package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

	"graal-rc/internal/connection"
	"graal-rc/internal/credentials"
	"graal-rc/internal/sqlite"
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

	// mainWindow is the account/server/RC window; hidden to the tray instead of
	// quit when a server session is active. quitting bypasses the hide hook.
	mainWindow *application.WebviewWindow
	quitting   atomic.Bool

	// tray is the system-tray handle; its tooltip is branded with the connected
	// server name + live player count by refreshServerChrome.
	tray *application.SystemTray

	logMu      sync.Mutex
	logEnabled bool
	logDir     string

	playerListMu     sync.Mutex
	playerListWindow *application.WebviewWindow

	scriptMgrMu     sync.Mutex
	scriptMgrWindow *application.WebviewWindow

	settingsMu     sync.Mutex
	settingsWindow *application.WebviewWindow

	fileBrowserMu     sync.Mutex
	fileBrowserWindow *application.WebviewWindow

	editorMu      sync.Mutex
	editorWindows map[string]*application.WebviewWindow

	editorCacheMu sync.Mutex
	editorCache   map[string]rclib.ScriptReply
	editorDirty   map[string]bool

	codingMu       sync.Mutex
	codingSettings CodingSettings

	fileBrowserCfgMu sync.Mutex
	fileBrowserCfg   FileBrowserConfig

	// Opened-file caches for the type-aware open behavior (double-click a file):
	// textCache holds .txt content for the editor window; dbFiles maps a remote
	// .db path to its local cache file (operated on by the SQLite explorer);
	// dbHeaders keeps the original 100-byte header for version preservation on
	// save. textWindows/sqliteWindows track one window per remote path.
	openMu        sync.Mutex
	textCache     map[string][]byte
	dbFiles       map[string]string
	dbHeaders     map[string][]byte
	textWindows   map[string]*application.WebviewWindow
	sqliteWindows map[string]*application.WebviewWindow
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
	return &App{
		sessions:       connection.NewService(),
		vault:          vault,
		editorWindows:  map[string]*application.WebviewWindow{},
		editorCache:    map[string]rclib.ScriptReply{},
		editorDirty:    map[string]bool{},
		textCache:      map[string][]byte{},
		dbFiles:        map[string]string{},
		dbHeaders:      map[string][]byte{},
		textWindows:    map[string]*application.WebviewWindow{},
		sqliteWindows:  map[string]*application.WebviewWindow{},
	}
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

// PreagonalListserverHost is the alternate listserver endpoint, selected
// implicitly when an account's nickname is prefixed "Preagonal:". The default
// listserver (listserver.graalonline.com) is used otherwise.
const PreagonalListserverHost = "listserver.graal.in"

// preagonalPrefix marks a nickname as routing to the alternate listserver.
const preagonalPrefix = "Preagonal:"

// listserverForName returns the listserver endpoint for a nickname. A nickname
// prefixed "Preagonal:" selects the alternate endpoint; everything else uses the
// default Graal listserver. The prefix is a hidden, client-only routing key.
func listserverForName(name string) (host string, port int) {
	if strings.HasPrefix(name, preagonalPrefix) {
		return PreagonalListserverHost, rclib.DefaultListserverPort
	}
	return rclib.DefaultListserverHost, rclib.DefaultListserverPort
}

// nicknameForServer strips the routing prefix so only the real nickname is sent
// to the server (the prefix is a client-side listserver selector, not part of
// the in-game nick).
func nicknameForServer(name string) string {
	return strings.TrimPrefix(name, preagonalPrefix)
}

func toCreds(req LoginRequest) connection.Credentials {
	host, port := listserverForName(req.Nickname)
	return connection.Credentials{Nickname: nicknameForServer(req.Nickname), Account: req.Account, Password: req.Password, Host: host, Port: port}
}

func accountToCreds(a credentials.Account) connection.Credentials {
	host, port := listserverForName(a.Nickname)
	return connection.Credentials{Nickname: nicknameForServer(a.Nickname), Account: a.Account, Password: a.Password, Host: host, Port: port}
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
	err := a.sessions.ConnectToServer(index)
	a.refreshServerChrome()
	return err
}

// SetNewProtocol toggles newer-protocol compatibility before server login.
func (a *App) SetNewProtocol(enable bool) error { return a.sessions.SetNewProtocol(enable) }

// Logout drops the active session and restores default window/tray titles.
func (a *App) Logout() {
	a.sessions.Logout()
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

// SaveServerText uploads a server-side text config (options/folder_config/
// flags) edited in a ScriptEditor window back to the server.
func (a *App) SaveServerText(kind, content string) error {
	return a.sessions.UploadServerText(kind, content)
}

// WarpNPC warps an NPC to (x, y) on the given level.
func (a *App) WarpNPC(id int, x, y float64, level string) error {
	return a.sessions.WarpNPC(id, x, y, level)
}

// RefreshWeapons re-requests the weapon list from the NC server.
func (a *App) RefreshWeapons() error { return a.sessions.RefreshWeapons() }

// --- File browser (main server socket) ---

// FileBrowserStart begins a file-browser session.
func (a *App) FileBrowserStart() error { return a.sessions.StartFileBrowser() }

// FileBrowserCd changes the current browser folder.
func (a *App) FileBrowserCd(folder string) error { return a.sessions.FileBrowserCd(folder) }

// FileBrowserDelete deletes a remote file.
func (a *App) FileBrowserDelete(path string) error { return a.sessions.FileBrowserDelete(path) }

// FileBrowserRename renames a remote file.
func (a *App) FileBrowserRename(oldPath, newPath string) error {
	return a.sessions.FileBrowserRename(oldPath, newPath)
}

// FileBrowserMove moves a file into a destination folder.
func (a *App) FileBrowserMove(destFolder, filePath string) error {
	return a.sessions.FileBrowserMove(destFolder, filePath)
}

// GetFileBrowserFolders returns the current browser folders.
func (a *App) GetFileBrowserFolders() ([]rclib.FileBrowserFolder, error) {
	return a.sessions.GetFileBrowserFolders()
}

// GetFileBrowserFiles returns the current browser files.
func (a *App) GetFileBrowserFiles() ([]rclib.FileBrowserEntry, error) {
	return a.sessions.GetFileBrowserFiles()
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
	name := filepath.Base(remotePath)
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "download"
	}
	var dest string
	if saveAs {
		chosen, err := a.app.Dialog.SaveFile().
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
	return a.sessions.UploadFile(filepath.Base(chosen), content)
}

// UploadFileBytes uploads base64-encoded content (used for drag-in uploads from
// the webview, which reads the dropped file and sends it as base64).
func (a *App) UploadFileBytes(remotePath, b64 string) error {
	content, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("invalid file data: %w", err)
	}
	return a.sessions.UploadFile(remotePath, content)
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
		return exec.Command("cmd", "/c", "start", "", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// OpenRemoteFile downloads a file and opens it by type. Returns a short kind
// label for the toast. Called on a file double-click.
func (a *App) OpenRemoteFile(remotePath string) (string, error) {
	content, err := a.sessions.DownloadFile(remotePath)
	if err != nil {
		return "", err
	}
	name := filepath.Base(remotePath)
	ext := extOf(remotePath)
	switch {
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
		return "database", nil
	case textExts[ext]:
		a.openMu.Lock()
		a.textCache[remotePath] = content
		a.openMu.Unlock()
		if err := a.openTextWindow(remotePath); err != nil {
			return "", err
		}
		return "text", nil
	default:
		// Unknown binary → plain download to the configured folder.
		saved, err := a.DownloadFile(remotePath, false)
		if err != nil {
			return "", err
		}
		if saved != "" {
			osOpenPath(saved)
		}
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
	if err := a.sessions.UploadFile(remotePath, []byte(content)); err != nil {
		return err
	}
	a.openMu.Lock()
	a.textCache[remotePath] = []byte(content)
	a.openMu.Unlock()
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
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             sanitizeWindowName("textfile", remotePath),
		Title:            editorTitle(a.sessions.Status().ServerName, "textfile", filepath.Base(remotePath)),
		URL:              "/#textfile?p=" + url.QueryEscape(remotePath),
		Width:            820,
		Height:           620,
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
		delete(a.editorWindows, mapKey)
		a.editorMu.Unlock()
		a.editorCacheMu.Lock()
		delete(a.editorDirty, mapKey)
		a.editorCacheMu.Unlock()
		a.openMu.Lock()
		delete(a.textCache, remotePath)
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
	return a.sessions.UploadFile(remotePath, content)
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
	return a.sessions.UploadFile(remotePath, content)
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
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             sanitizeWindowName("sqlite", remotePath),
		Title:            editorTitle(a.sessions.Status().ServerName, "sqlite", filepath.Base(remotePath)),
		URL:              "/#sqlite?p=" + url.QueryEscape(remotePath),
		Width:            960,
		Height:           640,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.openMu.Lock()
	a.sqliteWindows[remotePath] = w
	a.openMu.Unlock()
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
// and the tray tooltip with the current session state: branded with the server
// name + live player count when connected to an authenticated server, default
// labels otherwise. Called on connect/logout and by the tray refresh loop so a
// server-side disconnect (Status flips to !Connected) resets the chrome without a
// frontend round-trip.
func (a *App) refreshServerChrome() {
	st := a.sessions.Status()
	connected := st.Connected && st.Authenticated && st.ServerName != ""

	mainTitle := "Graal Remote Control"
	playersTitle := "Players"
	scriptsTitle := "Script Manager"
	settingsTitle := "Settings"
	filesTitle := "File Browser"
	tooltip := "Graal Remote Control"
	if connected {
		mainTitle = st.ServerName + " RC"
		playersTitle = st.ServerName + " Players"
		scriptsTitle = st.ServerName + " Script Manager"
		settingsTitle = st.ServerName + " Settings"
		filesTitle = st.ServerName + " File Browser"
		count := 0
		if players, err := a.sessions.GetPlayers(); err == nil {
			count = len(players)
		}
		tooltip = st.ServerName + ":" + strconv.Itoa(count)
	}
	if a.mainWindow != nil {
		a.mainWindow.SetTitle(mainTitle)
	}
	a.setWindowTitleLocked(&a.playerListMu, &a.playerListWindow, playersTitle)
	a.setWindowTitleLocked(&a.scriptMgrMu, &a.scriptMgrWindow, scriptsTitle)
	a.setWindowTitleLocked(&a.settingsMu, &a.settingsWindow, settingsTitle)
	a.setWindowTitleLocked(&a.fileBrowserMu, &a.fileBrowserWindow, filesTitle)
	if a.tray != nil {
		a.tray.SetTooltip(tooltip)
	}
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

// OpenFileBrowser opens (or focuses) the File Browser window. Singleton.
func (a *App) OpenFileBrowser() {
	a.fileBrowserMu.Lock()
	defer a.fileBrowserMu.Unlock()
	if a.fileBrowserWindow != nil {
		a.fileBrowserWindow.Show()
		a.fileBrowserWindow.Focus()
		return
	}
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "files",
		Title:            "File Browser",
		URL:              "/#files",
		Width:            920,
		Height:           600,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.fileBrowserWindow = w
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.fileBrowserMu.Lock()
		a.fileBrowserWindow = nil
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
		return "?"
	}
}

// editorTitle builds an editor window title as "{server} {TYPE}:key" when a
// server is connected, else "{TYPE}:key" (e.g. "Zodiac W:sword", "N:42").
func editorTitle(serverName, scriptType, key string) string {
	title := scriptTypeInitial(scriptType) + ":" + key
	if serverName != "" {
		return serverName + " " + title
	}
	return title
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
		Title:            editorTitle(a.sessions.Status().ServerName, scriptType, key),
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

// FileBrowserConfig holds the file-browser preferences (the required downloads
// folder), persisted to a file so every file-browser window reads the same
// value — localStorage is not reliably shared across Wails v3 windows.
type FileBrowserConfig struct {
	DownloadDir string `json:"downloadDir"`
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
	if a.fileBrowserCfg.DownloadDir == "" {
		a.loadFileBrowserConfigLocked()
	}
	return a.fileBrowserCfg
}

// SetFileBrowserConfig persists the downloads folder and broadcasts it so the
// file-browser window updates live.
func (a *App) SetFileBrowserConfig(downloadDir string) error {
	a.fileBrowserCfgMu.Lock()
	a.fileBrowserCfg = FileBrowserConfig{DownloadDir: downloadDir}
	cfg := a.fileBrowserCfg
	a.fileBrowserCfgMu.Unlock()

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
