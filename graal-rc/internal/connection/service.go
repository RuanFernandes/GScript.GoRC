// Package connection owns the live grclib session: a single handle plus the
// listserver endpoint, guarded by a mutex. It is the only layer that mutates
// connection state, keeping the Wails binding layer (App) free of such logic
// (Single Responsibility). The raw DLL bindings live in package rclib.
package connection

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"graal-rc/rclib"
)

// Credentials are the values captured from the login screen.
type Credentials struct {
	Nickname string
	Account  string
	Password string
}

// Status describes the current session for the frontend.
type Status struct {
	Loaded        bool   `json:"loaded"`
	DLLPath       string `json:"dllPath"`
	Connected     bool   `json:"connected"`
	Authenticated bool   `json:"authenticated"`
	Account       string `json:"account"`
	Nickname      string `json:"nickname"`
}

// Service manages the grclib connection handle and the credentials in use.
// Methods are safe to call from Wails-bound goroutines.
type Service struct {
	mu          sync.Mutex
	handle      rclib.Handle
	creds       Credentials
	pumpCancel  context.CancelFunc
	ncAttempted bool
	emit        func(name string, data ...any)
}

// NewService returns an empty service.
func NewService() *Service { return &Service{} }

// SetEmitter wires the bridge used to push grclib callbacks to the frontend
// (Wails runtime.EventsEmit). Must be set before ConnectToServer so chat/IRC/
// server-data events raised on the pump goroutine can reach the UI.
func (s *Service) SetEmitter(fn func(name string, data ...any)) { s.emit = fn }

// emitEvent is a nil-safe helper for the pump-goroutine callbacks.
func (s *Service) emitEvent(name string, data ...any) {
	if s.emit != nil {
		s.emit(name, data...)
	}
}

// NCStatus is the NC (script) socket snapshot for the frontend.
type NCStatus struct {
	HasNc        bool `json:"hasNc"`
	Connected    bool `json:"connected"`
	Authenticated bool `json:"authenticated"`
}

// startPump spawns a goroutine that pumps rc_process_events for the handle so
// connection + chat/IRC/data callbacks are delivered. A previous pump is
// stopped first. It also lazily opens the NC (script) socket once the server
// reports one (mirroring the reference client's pump loop).
func (s *Service) startPump(h rclib.Handle) {
	s.stopPump()
	s.ncAttempted = false
	ctx, cancel := context.WithCancel(context.Background())
	s.pumpCancel = cancel
	go func() {
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rclib.ProcessEvents(h)
				s.maybeConnectNC(h)
			}
		}
	}()
}

// maybeConnectNC opens the NC socket once per session when the server exposes
// one and it is not yet connected.
func (s *Service) maybeConnectNC(h rclib.Handle) {
	s.mu.Lock()
	attempt := !s.ncAttempted
	s.mu.Unlock()
	if !attempt {
		return
	}
	if !rclib.HasNCServer(h) || rclib.IsNCConnected(h) {
		return
	}
	s.mu.Lock()
	s.ncAttempted = true
	s.mu.Unlock()
	if err := rclib.ConnectToNCServer(h); err != nil {
		log.Printf("nc connect: %v", err)
	}
}

// stopPump stops the active event pump, if any.
func (s *Service) stopPump() {
	if s.pumpCancel != nil {
		s.pumpCancel()
		s.pumpCancel = nil
	}
}

// hasHandle reports whether a listserver connection is currently held.
func (s *Service) hasHandle() bool { return s.handle != 0 }

// Login connects to the listserver with the given credentials and keeps the
// resulting handle. A previous handle is dropped first. Returns the server
// list produced by the listserver login.
func (s *Service) Login(creds Credentials) ([]rclib.Server, error) {
	if creds.Account == "" || creds.Password == "" {
		return nil, errors.New("account and password are required")
	}

	h, err := rclib.Connect(rclib.DefaultListserverHost, rclib.DefaultListserverPort, creds.Account, creds.Password)
	if err != nil {
		return nil, err
	}

	servers, err := rclib.GetServers(h)
	lastErr := rclib.LastError(h)
	if err != nil {
		rclib.Disconnect(h)
		return nil, err
	}
	if len(servers) == 0 {
		rclib.Disconnect(h)
		// grclib signals auth rejection (wrong password, banned, etc.) via a
		// non-empty last_error. An empty last_error means the account
		// authenticated but has no staff roles anywhere.
		if lastErr != "" {
			return nil, errors.New(lastErr)
		}
		return nil, errors.New("You're not staff in any server.")
	}

	s.mu.Lock()
	if s.handle != 0 {
		s.stopPump()
		rclib.UnregisterCallbacks(s.handle)
		rclib.Disconnect(s.handle)
	}
	s.handle = h
	s.creds = creds
	s.mu.Unlock()

	return servers, nil
}

// GetServers returns a fresh copy of the cached server list for the active
// handle. The list is refreshed by reconnecting the listserver (grclib caches
// the list at login), so this reuses the stored handle.
func (s *Service) GetServers() ([]rclib.Server, error) {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return nil, errors.New("not connected: log in first")
	}
	return rclib.GetServers(h)
}

// ConnectToServer authenticates to the server at the given list index on the
// active handle. It registers connection callbacks, starts the event pump, then
// waits for either on_connected (success) or on_disconnected (failure, with the
// server's reason such as "IP not approved") before returning. This is what
// surfaces async login failures to the UI.
func (s *Service) ConnectToServer(index int) error {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}

	connected := make(chan struct{}, 1)
	disconnected := make(chan string, 1)
	rclib.RegisterCallbacks(h, &rclib.EventCallbacks{
		Connected: func() {
			select {
			case connected <- struct{}{}:
			default:
			}
		},
		Disconnected: func(reason string) {
			select {
			case disconnected <- reason:
			default:
			}
			s.emitEvent("rc:disconnected", reason)
		},
		Message: func(text string) { s.emitEvent("rc:message", text) },
		IrcMessage: func(channel, line string) {
			s.emitEvent("rc:irc", channel, line)
		},
		ServerData: func(dataType, content string) { s.emitEvent("rc:serverdata", dataType, content) },
	})
	s.startPump(h)

	// Kick off the server login; the result arrives asynchronously via events.
	if err := rclib.ConnectToServer(h, index); err != nil {
		return err
	}

	select {
	case <-connected:
		// The server does not adopt our nickname until we send it (mirrors the
		// reference client, which calls rc_set_nickname in its onConnected).
		if nick := s.creds.Nickname; nick != "" {
			if err := rclib.SetNickname(h, nick); err != nil {
				log.Printf("set nickname %q: %v", nick, err)
			}
		}
		return nil
	case reason := <-disconnected:
		if reason == "" {
			reason = "disconnected by server"
		}
		return errors.New(reason)
	case <-time.After(30 * time.Second):
		return errors.New("server connection timed out")
	}
}

// SetNewProtocol toggles newer-protocol compatibility before server login.
func (s *Service) SetNewProtocol(enable bool) error {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	return rclib.SetNewProtocol(h, enable)
}

// ConnectToNCServer explicitly opens the NC (script) socket.
func (s *Service) ConnectToNCServer() error {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	return rclib.ConnectToNCServer(h)
}

// DisconnectNC closes the NC socket.
func (s *Service) DisconnectNC() error {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	return rclib.DisconnectNC(h)
}

// NCStatus returns the NC socket snapshot for the active handle.
func (s *Service) NCStatus() NCStatus {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return NCStatus{}
	}
	return NCStatus{
		HasNc:         rclib.HasNCServer(h),
		Connected:     rclib.IsNCConnected(h),
		Authenticated: rclib.IsNCAuthenticated(h),
	}
}

// IrcLogin starts the IRC session for the active handle.
func (s *Service) IrcLogin() error {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	return rclib.IrcLogin(h)
}

// SendIrcText sends a raw IRC command on the active handle.
func (s *Service) SendIrcText(command, p1, p2, p3 string) error {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	return rclib.SendIrcText(h, command, p1, p2, p3)
}

// Execute sends a chat line or slash command to the active server.
func (s *Service) Execute(message string) error {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	return rclib.Execute(h, message)
}

// SetNickname changes the RC nickname on the active handle.
func (s *Service) SetNickname(nickname string) error {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	return rclib.SetNickname(h, nickname)
}

// GetPlayers returns the cached player list for the active server.
func (s *Service) GetPlayers() ([]rclib.Player, error) {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return nil, errors.New("not connected: log in first")
	}
	return rclib.GetPlayers(h)
}

// Logout drops the active handle and clears credentials.
func (s *Service) Logout() {
	s.stopPump()
	s.mu.Lock()
	if s.handle != 0 {
		rclib.UnregisterCallbacks(s.handle)
		rclib.Disconnect(s.handle)
		s.handle = 0
	}
	s.creds = Credentials{}
	s.mu.Unlock()
}

// Status returns a snapshot of the current session state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := Status{
		Account:  s.creds.Account,
		Nickname: s.creds.Nickname,
	}
	if dllPath, err := rclib.DLLPath(); err == nil {
		st.Loaded = true
		st.DLLPath = dllPath
	}
	if s.handle != 0 {
		st.Connected = rclib.IsConnected(s.handle)
		st.Authenticated = rclib.IsAuthenticated(s.handle)
	}
	return st
}
