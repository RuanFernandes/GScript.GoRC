package main

import (
	"context"
	"errors"
	"log"

	"graal-rc/internal/connection"
	"graal-rc/internal/credentials"
	"graal-rc/rclib"
)

var (
	errNoVault        = errors.New("account vault is not available")
	errAccountNotFound = errors.New("account not found")
)

// App is the Wails binding surface. It delegates all session logic to the
// connection Service and account storage to the credentials Vault, so the
// binding layer stays thin (Single Responsibility).
type App struct {
	ctx      context.Context
	sessions *connection.Service
	vault    *credentials.Vault
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

// migrateLegacyCredentials imports the old plaintext credentials.json (written
// by earlier builds) into the encrypted vault, then deletes the legacy file.
func migrateLegacyCredentials(vault *credentials.Vault) {
	existing, err := vault.Load()
	if err != nil {
		log.Printf("vault load during migration: %v", err)
		return
	}
	if len(existing) > 0 {
		return // already populated; nothing to migrate
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

// startup saves the Wails context for runtime calls.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
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

// ListAccounts returns the saved accounts without passwords. ok=false / empty
// list when the vault is empty.
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
// them to the vault so the account appears in the Select screen on future
// launches. On failure nothing is saved.
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

// findAccount loads the vault and returns the account matching accountName.
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
func (a *App) GetServers() ([]rclib.Server, error) {
	return a.sessions.GetServers()
}

// ConnectToServer authenticates to the server at the given index.
func (a *App) ConnectToServer(index int) error {
	return a.sessions.ConnectToServer(index)
}

// SetNewProtocol toggles newer-protocol compatibility (call before
// ConnectToServer).
func (a *App) SetNewProtocol(enable bool) error {
	return a.sessions.SetNewProtocol(enable)
}

// Logout drops the active session.
func (a *App) Logout() {
	a.sessions.Logout()
}

// Status returns the current session status snapshot.
func (a *App) Status() connection.Status {
	return a.sessions.Status()
}
