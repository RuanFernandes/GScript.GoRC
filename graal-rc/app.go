package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"sync"
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
	return &App{sessions: connection.NewService(), vault: vault}
}

// attach wires the v3 application handle and the event emitter (grclib
// callbacks → app.Event.Emit) once application.New has returned. Unexported so
// it is not exposed to the frontend as a binding. Each event payload is
// JSON-encoded as a single string so the frontend can uniformly JSON.parse it.
func (a *App) attach(app *application.App) {
	a.app = app
	a.sessions.SetEmitter(func(name string, data ...any) {
		b, err := json.Marshal(data)
		if err != nil {
			return
		}
		app.Event.Emit(name, string(b))
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
