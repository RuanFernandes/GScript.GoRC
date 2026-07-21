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
	mu         sync.Mutex
	handle     rclib.Handle
	creds      Credentials
	pumpCancel context.CancelFunc
}

// NewService returns an empty service.
func NewService() *Service { return &Service{} }

// startPump spawns a goroutine that pumps rc_process_events for the handle so
// connection callbacks (on_connected/on_disconnected) are delivered. A previous
// pump is stopped first.
func (s *Service) startPump(h rclib.Handle) {
	s.stopPump()
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
			}
		}
	}()
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
	log.Printf("login account=%q rc_connect handle=%d err=%v", creds.Account, h, err)
	if err != nil {
		return nil, err
	}

	servers, err := rclib.GetServers(h)
	lastErr := rclib.LastError(h)
	log.Printf("login account=%q rc_get_servers count=%d err=%v last_error=%q", creds.Account, len(servers), err, lastErr)
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
		},
	})
	s.startPump(h)

	// Kick off the server login; the result arrives asynchronously via events.
	if err := rclib.ConnectToServer(h, index); err != nil {
		return err
	}

	select {
	case <-connected:
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
