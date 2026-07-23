// Package connection owns the live grclib session: a single handle plus the
// listserver endpoint, guarded by a mutex. It is the only layer that mutates
// connection state, keeping the Wails binding layer (App) free of such logic
// (Single Responsibility). The raw DLL bindings live in package rclib.
package connection

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"graal-rc/rclib"
)

// Credentials are the values captured from the login screen. Host/Port select
// the listserver endpoint (derived from the account type by the App layer).
type Credentials struct {
	Nickname string
	Account  string
	Password string
	Host     string
	Port     int
}

// Status describes the current session for the frontend.
type Status struct {
	Loaded        bool   `json:"loaded"`
	DLLPath       string `json:"dllPath"`
	Connected     bool   `json:"connected"`
	Authenticated bool   `json:"authenticated"`
	Account       string `json:"account"`
	Nickname      string `json:"nickname"`
	ServerName    string `json:"serverName"`
}

// Service manages the grclib connection handle and the credentials in use.
// Methods are safe to call from Wails-bound goroutines.
type Service struct {
	mu            sync.Mutex
	handle        rclib.Handle
	creds         Credentials
	serverName    string // name of the server selected in ConnectToServer; "" when none
	pumpCancel    context.CancelFunc
	lastNCAttempt time.Time // last ConnectToNCServer attempt; throttles retries
	emit          func(name string, data ...any)

	// maxUpload is the latest server-reported max upload size (bytes), pushed via
	// the MaxUploadSize callback. 0 means unknown. Guarded by mu.
	maxUpload int64

	// pendingFiles correlates a file download request (by remote path) to its
	// content bytes, delivered asynchronously via the FileReceived callback.
	pendingFilesMu sync.Mutex
	pendingFiles   map[string]chan []byte
	// channels is the authoritative set of joined IRC channels, derived from the
	// join/left marker lines. It is the single source of truth for which IRC
	// tabs the frontend should show; the frontend reconciles its tabs against a
	// snapshot of this set, so React batching/event ordering cannot desync them.
	// Removals are debounced (channelLeaveCooldown): the server always sends a
	// PART before the matching JOIN (to avoid duplicates), so a leave is only
	// committed after the cooldown elapses with no rejoin — a JOIN in the window
	// cancels the pending leave and the tab survives.
	channels map[string]*channelState

	// pending maps a script/flags/attributes request key to its reply channel.
	// A request (OpenScript/OpenNPCFlags/OpenNPCAttributes) registers under a key
	// like "weapon:name" / "npc:<id>" / "npcflags:<id>" / "npcattr:<id>" and the
	// matching pump-goroutine callback resolves it. Replies arrive asynchronously
	// from the NC server.
	pendingMu sync.Mutex
	pending   map[string]chan rclib.ScriptReply
}

// scriptTimeout is how long OpenScript/OpenNPC* waits for the NC server reply.
const scriptTimeout = 15 * time.Second

// downloadTimeout is how long a file download waits for the full content. File
// transfers can be large and the server slow (30 MB over a sluggish link can
// take minutes; grclib streams "Received chunk" progress meanwhile), so this is
// far longer than scriptTimeout.
const downloadTimeout = 10 * time.Minute

// pendingKey builds the correlation key for a script/flags/attributes request.
func pendingKey(kind, idOrName string) string { return kind + ":" + idOrName }

// registerPending installs a reply channel for key, returning it and a cleanup.
// If a waiter already exists for the same key (e.g. duplicate open), it is
// replaced — the old one times out.
func (s *Service) registerPending(key string) chan rclib.ScriptReply {
	ch := make(chan rclib.ScriptReply, 1)
	s.pendingMu.Lock()
	if s.pending == nil {
		s.pending = map[string]chan rclib.ScriptReply{}
	}
	s.pending[key] = ch
	s.pendingMu.Unlock()
	return ch
}

// resolvePending delivers a reply to the waiter for key (if any) and drops it.
// Called from pump-goroutine callbacks; non-blocking (buffered channel).
func (s *Service) resolvePending(key string, reply rclib.ScriptReply) {
	s.pendingMu.Lock()
	ch, ok := s.pending[key]
	if ok {
		delete(s.pending, key)
	}
	s.pendingMu.Unlock()
	if ok {
		select {
		case ch <- reply:
		default:
		}
	}
}

// registerFile installs a content channel for a download keyed by remote path,
// returning it. Called before FileBrowserDownload so the matching FileReceived
// callback resolves it.
func (s *Service) registerFile(path string) chan []byte {
	ch := make(chan []byte, 1)
	s.pendingFilesMu.Lock()
	if s.pendingFiles == nil {
		s.pendingFiles = map[string]chan []byte{}
	}
	s.pendingFiles[path] = ch
	s.pendingFilesMu.Unlock()
	return ch
}

// resolveFile delivers downloaded content to the waiter for path (if any) and
// drops it. Called from the pump-goroutine FileReceived callback; non-blocking.
func (s *Service) resolveFile(path string, content []byte) {
	s.pendingFilesMu.Lock()
	ch, ok := s.pendingFiles[path]
	if ok {
		delete(s.pendingFiles, path)
	}
	s.pendingFilesMu.Unlock()
	if ok {
		select {
		case ch <- content:
		default:
		}
	}
}

// channelState tracks one IRC channel's join state plus a pending (debounced)
// leave deadline. leaveAt is the zero time when no leave is pending.
type channelState struct {
	joined  bool
	leaveAt time.Time
}

// channelLeaveCooldown is how long a PART waits before it actually removes the
// channel, giving a rejoin (JOIN) time to cancel it.
const channelLeaveCooldown = 600 * time.Millisecond

// NewService returns an empty service.
func NewService() *Service { return &Service{} }

// displayServerName strips a raw listserver server name's single-letter type
// prefix + space (e.g. "H Testbed3d" → "Testbed3d", "P …" gold, "U …" classic).
// Applied where the session stores the server name so every window/tray title
// shows the clean name.
func displayServerName(name string) string {
	if len(name) >= 3 && name[1] == ' ' && name[0] >= 'A' && name[0] <= 'Z' {
		return name[2:]
	}
	return name
}

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

// handleIrcMessage forwards every IRC line to the frontend for rendering and,
// for join/left marker lines, updates the authoritative joined-channel set and
// emits a rc:channels snapshot the frontend reconciles its tabs against.
func (s *Service) handleIrcMessage(channel, line string) {
	snapshot := s.applyChannelDelta(channel, line)
	s.emitEvent("rc:irc", channel, line)
	if snapshot != nil {
		s.emitEvent("rc:channels", snapshot)
	}
}

// applyChannelDelta updates the joined set per the marker line and returns the
// new sorted snapshot if the visibly-joined set changed, nil otherwise.
//   - JOIN: cancels any pending leave, marks joined. Snapshot emitted only on a
//     real false->true transition.
//   - PART: ignored for a channel we're not in (handles the login burst's
//     part-before-join). For a joined channel it schedules a debounced leave
//     (no immediate snapshot); settleChannelLeaves commits it after the cooldown
//     unless a JOIN cancels it first.
//   - chat lines (no marker) do not touch the set.
func (s *Service) applyChannelDelta(channel, line string) []string {
	if channel == "" {
		return nil
	}
	text := strings.TrimSpace(line)
	isJoin := strings.HasPrefix(text, "* Joined")
	isPart := strings.HasPrefix(text, "* Left")
	if !isJoin && !isPart {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.channels == nil {
		s.channels = map[string]*channelState{}
	}
	cs := s.channels[channel]
	switch {
	case isJoin:
		if cs == nil {
			s.channels[channel] = &channelState{joined: true}
			return s.snapshotLocked()
		}
		cs.leaveAt = time.Time{} // cancel any pending leave (rejoin)
		if !cs.joined {
			cs.joined = true
			return s.snapshotLocked()
		}
		return nil // already joined: nothing visibly changed
	case isPart:
		if cs == nil || !cs.joined {
			return nil // not joined: ignore
		}
		if cs.leaveAt.IsZero() {
			cs.leaveAt = time.Now().Add(channelLeaveCooldown)
		}
		return nil // deferred: settleChannelLeaves emits the snapshot
	}
	return nil
}

// snapshotLocked returns the sorted list of currently-joined channels. Caller
// must hold s.mu.
func (s *Service) snapshotLocked() []string {
	out := make([]string, 0, len(s.channels))
	for ch, cs := range s.channels {
		if cs.joined {
			out = append(out, ch)
		}
	}
	sort.Strings(out)
	return out
}

// settleChannelLeaves commits any debounced leaves whose cooldown has elapsed,
// emitting a single rc:channels snapshot if anything was removed. Called from
// the pump goroutine.
func (s *Service) settleChannelLeaves() {
	s.mu.Lock()
	now := time.Now()
	changed := false
	for ch, cs := range s.channels {
		if !cs.leaveAt.IsZero() && now.After(cs.leaveAt) {
			delete(s.channels, ch)
			changed = true
		}
	}
	if !changed {
		s.mu.Unlock()
		return
	}
	snap := s.snapshotLocked()
	s.mu.Unlock()
	s.emitEvent("rc:channels", snap)
}

// resetChannels clears the joined set under lock and returns the empty snapshot
// to emit, used on disconnect/logout/relogin so the frontend drops all IRC tabs.
func (s *Service) resetChannels() []string {
	s.mu.Lock()
	s.channels = nil
	s.mu.Unlock()
	return []string{}
}

// NCStatus is the NC (script) socket snapshot for the frontend.
type NCStatus struct {
	HasNc         bool `json:"hasNc"`
	Connected     bool `json:"connected"`
	Authenticated bool `json:"authenticated"`
}

// startPump spawns a goroutine that pumps rc_process_events for the handle so
// connection + chat/IRC/data callbacks are delivered. A previous pump is
// stopped first. It also lazily opens the NC (script) socket once the server
// reports one (mirroring the reference client's pump loop).
func (s *Service) startPump(h rclib.Handle) {
	s.stopPump()
	s.mu.Lock()
	s.lastNCAttempt = time.Time{}
	s.mu.Unlock()
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
				s.settleChannelLeaves()
			}
		}
	}()
}

// ncReconnectInterval caps how often maybeConnectNC retries ConnectToNCServer
// after a failure. The pump ticks every 15ms; without throttling a transient
// failure would re-attempt ~66x/sec. HasNCServer is the rights gate — it is
// false for accounts the server did not expose an NC socket to, so those never
// enter the retry loop at all.
const ncReconnectInterval = 2 * time.Second

// maybeConnectNC opens the NC socket when the server exposes one to this
// account (HasNCServer) and it is not yet connected. Unlike a one-shot latch,
// it retries on failure (throttled by ncReconnectInterval) so a transient
// first-attempt miss — common with the release build's timing — does not leave
// the NC socket dead for the whole session.
func (s *Service) maybeConnectNC(h rclib.Handle) {
	// Rights gate: no NC exposed to this account, or already connected.
	if !rclib.HasNCServer(h) || rclib.IsNCConnected(h) {
		return
	}
	s.mu.Lock()
	if !s.lastNCAttempt.IsZero() && time.Since(s.lastNCAttempt) < ncReconnectInterval {
		s.mu.Unlock()
		return
	}
	s.lastNCAttempt = time.Now()
	s.mu.Unlock()
	if err := rclib.ConnectToNCServer(h); err != nil {
		log.Printf("nc connect (will retry in %s): %v", ncReconnectInterval, err)
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

	host := creds.Host
	port := creds.Port
	if host == "" {
		host = rclib.DefaultListserverHost
	}
	if port == 0 {
		port = rclib.DefaultListserverPort
	}
	h, err := rclib.Connect(host, port, creds.Account, creds.Password)
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
	s.channels = nil
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

	// Resolve the server name from the cached listserver list by index, so the
	// App layer can brand window/tray titles with it once connected. Strip the
	// single-letter type prefix (e.g. "H Testbed3d" → "Testbed3d") — mirrors the
	// reference client's getServerListName and the frontend serverDisplay helper.
	var serverName string
	if servers, err := rclib.GetServers(h); err == nil && index >= 0 && index < len(servers) {
		serverName = displayServerName(servers[index].Name)
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
			s.mu.Lock()
			s.serverName = ""
			s.mu.Unlock()
			s.emitEvent("rc:disconnected", reason)
			s.emitEvent("rc:channels", s.resetChannels())
		},
		Message:    func(text string) { s.emitEvent("rc:message", text) },
		IrcMessage: func(channel, line string) { s.handleIrcMessage(channel, line) },
		ServerData: func(dataType, content string) {
			// Server-side text configs (options/folder_config/flags) are fetched
			// via OpenServerText, which registers a pending waiter keyed by the
			// grclib data_type. Resolve it here and do NOT forward to rc:serverdata
			// — otherwise useChat dumps the whole config body into the chat log.
			if isServerTextKind(dataType) {
				s.resolvePending(pendingKey("serverdata", dataType), rclib.ScriptReply{Type: dataType, Script: content})
				return
			}
			// Log every non-text server-data packet to the Go terminal (raw
			// protocol lines like [PLO_RC_PLAYERPROPSCHANGE] '2$ arrive here and
			// are shown gray in chat; mirror them to stdout for later tooling).
			log.Printf("[serverdata] %s: %q", dataType, content)
			s.emitEvent("rc:serverdata", dataType, content)
		},
		ScriptReceived: func(scriptType, name string, id int, script string) {
			s.handleScriptReceived(scriptType, name, id, script)
		},
		WeaponChanged: func(name string) { s.emitEvent("rc:weaponsChanged", name) },
		ClassChanged:  func(name string) { s.emitEvent("rc:classesChanged", name) },
		NPCChanged:    func(id int) { s.emitEvent("rc:npcsChanged", id) },
		NPCFlags: func(id int, flags string) {
			s.resolvePending(pendingKey("npcflags", strconv.Itoa(id)), rclib.ScriptReply{Type: "npcflags", ID: id, Script: flags})
		},
		NPCAttributes: func(id int, attrs string) {
			s.resolvePending(pendingKey("npcattr", strconv.Itoa(id)), rclib.ScriptReply{Type: "npcattr", ID: id, Script: attrs})
		},
		FileBrowserFolders: func(count int) { s.emitEvent("rc:fbFolders", count) },
		FileBrowserFiles:   func(folder string, count int) { s.emitEvent("rc:fbFiles", folder, count) },
		FileBrowserMessage: func(message string) { s.emitEvent("rc:fbMessage", message) },
		MaxUploadSize: func(maxSize int64) {
			s.mu.Lock()
			s.maxUpload = maxSize
			s.mu.Unlock()
			s.emitEvent("rc:fbMaxUpload", maxSize)
		},
		FileReceived: func(path string, content []byte) { s.resolveFile(path, content) },
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
		s.mu.Lock()
		s.serverName = serverName
		s.mu.Unlock()
		return nil
	case reason := <-disconnected:
		s.mu.Lock()
		s.serverName = ""
		s.mu.Unlock()
		if reason == "" {
			reason = "disconnected by server"
		}
		return errors.New(reason)
	case <-time.After(30 * time.Second):
		s.mu.Lock()
		s.serverName = ""
		s.mu.Unlock()
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

// handleScriptReceived resolves a pending OpenScript request (weapon/class keyed
// by name, npc keyed by id). Called on the pump goroutine.
func (s *Service) handleScriptReceived(scriptType, name string, id int, script string) {
	key := name
	if scriptType == "npc" {
		key = strconv.Itoa(id)
	}
	if key == "" {
		return
	}
	s.resolvePending(pendingKey(scriptType, key), rclib.ScriptReply{Type: scriptType, Name: name, ID: id, Script: script})
}

// requireHandle returns the active handle or an error.
func (s *Service) requireHandle() (rclib.Handle, error) {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return 0, errors.New("not connected: log in first")
	}
	return h, nil
}

// requireNC returns the active handle after ensuring the NC (script) socket is
// connected. It is the guard for NC-dependent writes (SaveWeapon/Class/NPC, NC
// packet sends): if NC is exposed by the server but not yet up, it triggers an
// immediate connect attempt rather than letting grclib fail with an opaque
// runtime error. A clear, user-facing error is returned when NC is down or not
// exposed to this account.
func (s *Service) requireNC() (rclib.Handle, error) {
	h, err := s.requireHandle()
	if err != nil {
		return 0, err
	}
	if rclib.IsNCConnected(h) {
		return h, nil
	}
	if !rclib.HasNCServer(h) {
		return 0, errors.New("this account has no NC rights on this server")
	}
	// NC exposed but not connected: the pump retries every ncReconnectInterval,
	// but force one attempt now so a save right after login is not lost to the
	// throttle window.
	s.mu.Lock()
	s.lastNCAttempt = time.Now()
	s.mu.Unlock()
	if err := rclib.ConnectToNCServer(h); err != nil {
		return 0, fmt.Errorf("NC server not connected: %w", err)
	}
	return h, nil
}

// GetWeapons returns the cached weapon list for the NC server.
func (s *Service) GetWeapons() ([]rclib.Weapon, error) {
	h, err := s.requireNC()
	if err != nil {
		return nil, err
	}
	return rclib.GetWeapons(h)
}

// GetClasses returns the cached class list for the NC server.
func (s *Service) GetClasses() ([]rclib.Class, error) {
	h, err := s.requireNC()
	if err != nil {
		return nil, err
	}
	return rclib.GetClasses(h)
}

// GetNPCs returns the cached NPC list for the NC server.
func (s *Service) GetNPCs() ([]rclib.NPC, error) {
	h, err := s.requireNC()
	if err != nil {
		return nil, err
	}
	return rclib.GetNPCs(h)
}

// AddWeapon creates a weapon by name.
func (s *Service) AddWeapon(name string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.AddWeapon(h, name)
}

// DeleteWeapon deletes a weapon by name.
func (s *Service) DeleteWeapon(name string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.DeleteWeapon(h, name)
}

// AddClass creates a class by name.
func (s *Service) AddClass(name string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.AddClass(h, name)
}

// DeleteClass deletes a class by name.
func (s *Service) DeleteClass(name string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.DeleteClass(h, name)
}

// DeleteNPC deletes an NPC by id.
func (s *Service) DeleteNPC(id int) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.DeleteNPC(h, id)
}

// CreateNPC creates a new DB NPC on the server.
func (s *Service) CreateNPC(name string, id int, npcType, scripter, level, x, y string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	log.Printf("create npc: name=%q id=%d type=%q scripter=%q level=%q x=%q y=%q nc_connected=%v",
		name, id, npcType, scripter, level, x, y, rclib.IsNCConnected(h))
	err = rclib.CreateNPC(h, name, id, npcType, scripter, level, x, y)
	if err != nil {
		log.Printf("create npc failed: %v (last_error: %s)", err, rclib.LastError(h))
	}
	return err
}

// SaveWeapon writes a weapon's script back to the server.
func (s *Service) SaveWeapon(name, script string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.UpdateWeapon(h, name, script)
}

// SaveClass writes a class's script back to the server.
func (s *Service) SaveClass(name, script string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.UpdateClass(h, name, script)
}

// SaveNPC writes an NPC's script back to the server.
func (s *Service) SaveNPC(id int, script string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.UpdateNPC(h, id, script)
}

// OpenScript requests a script from the server and waits for the reply. For
// weapon/class, key is the name; for npc, key is the stringified id.
func (s *Service) OpenScript(scriptType, key string) (rclib.ScriptReply, error) {
	h, err := s.requireHandle()
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	switch scriptType {
	case "weapon":
		err = rclib.RequestWeaponScript(h, key)
	case "class":
		err = rclib.RequestClassScript(h, key)
	case "npc":
		id, convErr := strconv.Atoi(key)
		if convErr != nil {
			return rclib.ScriptReply{}, convErr
		}
		err = rclib.RequestNPCScript(h, id)
	default:
		return rclib.ScriptReply{}, errors.New("unknown script type: " + scriptType)
	}
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	ch := s.registerPending(pendingKey(scriptType, key))
	select {
	case reply := <-ch:
		return reply, nil
	case <-time.After(scriptTimeout):
		s.pendingMu.Lock()
		delete(s.pending, pendingKey(scriptType, key))
		s.pendingMu.Unlock()
		return rclib.ScriptReply{}, errors.New("script request timed out")
	}
}

// isServerTextKind reports whether a grclib on_server_data data_type names one of
// the three server-side text configs editable via OpenServerText. The strings
// match grclib.cpp's emitted data_type exactly.
func isServerTextKind(dataType string) bool {
	switch dataType {
	case "options", "folder_config", "flags":
		return true
	}
	return false
}

// OpenServerText requests a server-side text config (options/folder_config/
// flags) and waits for the on_server_data reply. kind must be one of those three.
// Unlike OpenScript, this travels on the main server socket (not NC), so it is
// available to accounts without NC/script rights.
func (s *Service) OpenServerText(kind string) (rclib.ScriptReply, error) {
	h, err := s.requireHandle()
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	switch kind {
	case "options":
		err = rclib.RequestServerOptions(h)
	case "folder_config":
		err = rclib.RequestFolderConfig(h)
	case "flags":
		err = rclib.RequestServerFlags(h)
	default:
		return rclib.ScriptReply{}, errors.New("unknown server text kind: " + kind)
	}
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	ch := s.registerPending(pendingKey("serverdata", kind))
	select {
	case reply := <-ch:
		return reply, nil
	case <-time.After(scriptTimeout):
		s.pendingMu.Lock()
		delete(s.pending, pendingKey("serverdata", kind))
		s.pendingMu.Unlock()
		return rclib.ScriptReply{}, errors.New("server text request timed out")
	}
}

// UploadServerText writes a server-side text config (options/folder_config/
// flags) back to the server.
func (s *Service) UploadServerText(kind, content string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	switch kind {
	case "options":
		return rclib.UploadServerOptions(h, content)
	case "folder_config":
		return rclib.UploadFolderConfig(h, content)
	case "flags":
		return rclib.UploadServerFlags(h, content)
	}
	return errors.New("unknown server text kind: " + kind)
}

// ResetNPC resets an NPC by id.
func (s *Service) ResetNPC(id int) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.ResetNPC(h, id)
}

// OpenNPCFlags requests an NPC's flags and waits for the reply.
func (s *Service) OpenNPCFlags(id int) (rclib.ScriptReply, error) {
	h, err := s.requireHandle()
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	if err := rclib.GetNPCFlags(h, id); err != nil {
		return rclib.ScriptReply{}, err
	}
	key := strconv.Itoa(id)
	ch := s.registerPending(pendingKey("npcflags", key))
	select {
	case reply := <-ch:
		return reply, nil
	case <-time.After(scriptTimeout):
		s.pendingMu.Lock()
		delete(s.pending, pendingKey("npcflags", key))
		s.pendingMu.Unlock()
		return rclib.ScriptReply{}, errors.New("npc flags request timed out")
	}
}

// OpenNPCAttributes requests an NPC's attributes and waits for the reply.
func (s *Service) OpenNPCAttributes(id int) (rclib.ScriptReply, error) {
	h, err := s.requireHandle()
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	if err := rclib.RequestNPCAttributes(h, id); err != nil {
		return rclib.ScriptReply{}, err
	}
	key := strconv.Itoa(id)
	ch := s.registerPending(pendingKey("npcattr", key))
	select {
	case reply := <-ch:
		return reply, nil
	case <-time.After(scriptTimeout):
		s.pendingMu.Lock()
		delete(s.pending, pendingKey("npcattr", key))
		s.pendingMu.Unlock()
		return rclib.ScriptReply{}, errors.New("npc attributes request timed out")
	}
}

// SaveNPCFlags writes an NPC's flags to the server.
func (s *Service) SaveNPCFlags(id int, flags string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.SetNPCFlags(h, id, flags)
}

// WarpNPC warps an NPC to (x, y) on the given level.
func (s *Service) WarpNPC(id int, x, y float64, level string) error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.WarpNPC(h, id, x, y, level)
}

// RefreshWeapons re-requests the weapon list (grclib only sends it once at NC
// auth), forcing the server to repopulate the cache and re-emit add events.
func (s *Service) RefreshWeapons() error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.SendNCPacket(h, weaponListGetPacket)
}

// weaponListGetPacket is PLI_NC_WEAPONLISTGET (IEnums.h) — re-request the weapon
// list from the NC server.
const weaponListGetPacket = 115

// --- File browser (main server socket) ---

// StartFileBrowser begins a file-browser session. The folder/file data arrives
// asynchronously via the rc:fbFolders / rc:fbFiles events.
func (s *Service) StartFileBrowser() error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.FileBrowserStart(h)
}

// FileBrowserCd changes the current browser folder.
func (s *Service) FileBrowserCd(folder string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.FileBrowserCd(h, folder)
}

// FileBrowserDelete deletes a remote file.
func (s *Service) FileBrowserDelete(path string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	if err := rclib.FileBrowserDelete(h, path); err != nil {
		return err
	}
	s.emitEvent("rc:fbChanged")
	return nil
}

// FileBrowserRename renames a remote file.
func (s *Service) FileBrowserRename(oldPath, newPath string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	if err := rclib.FileBrowserRename(h, oldPath, newPath); err != nil {
		return err
	}
	s.emitEvent("rc:fbChanged")
	return nil
}

// FileBrowserMove moves a file into a destination folder.
func (s *Service) FileBrowserMove(destFolder, filePath string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	if err := rclib.FileBrowserMove(h, destFolder, filePath); err != nil {
		return err
	}
	s.emitEvent("rc:fbChanged")
	return nil
}

// GetFileBrowserFolders returns the current browser folders (snapshotted from
// the DLL cache). Call after an rc:fbFolders event.
func (s *Service) GetFileBrowserFolders() ([]rclib.FileBrowserFolder, error) {
	h, err := s.requireHandle()
	if err != nil {
		return nil, err
	}
	return rclib.CopyFileBrowserFolders(h)
}

// GetFileBrowserFiles returns the current browser files (snapshotted from the
// DLL cache). Call after an rc:fbFiles event.
func (s *Service) GetFileBrowserFiles() ([]rclib.FileBrowserEntry, error) {
	h, err := s.requireHandle()
	if err != nil {
		return nil, err
	}
	return rclib.CopyFileBrowserFiles(h)
}

// MaxUploadFileSize returns the latest server-reported max upload size (bytes),
// or 0 if unknown.
func (s *Service) MaxUploadFileSize() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxUpload
}

// DownloadFile requests a file and waits for its content via the FileReceived
// callback (correlated by remote path). Returns the raw bytes.
func (s *Service) DownloadFile(path string) ([]byte, error) {
	h, err := s.requireHandle()
	if err != nil {
		return nil, err
	}
	ch := s.registerFile(path)
	if err := rclib.FileBrowserDownload(h, path); err != nil {
		s.pendingFilesMu.Lock()
		delete(s.pendingFiles, path)
		s.pendingFilesMu.Unlock()
		return nil, err
	}
	select {
	case content := <-ch:
		if content == nil {
			return nil, errors.New("server returned no file content")
		}
		return content, nil
	case <-time.After(downloadTimeout):
		s.pendingFilesMu.Lock()
		delete(s.pendingFiles, path)
		s.pendingFilesMu.Unlock()
		return nil, errors.New("file download timed out (no response from server)")
	}
}

// UploadFile uploads raw bytes to a remote path. When the max upload size is
// known, oversize uploads are rejected up front with a clear error.
func (s *Service) UploadFile(path string, content []byte) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	if max := s.MaxUploadFileSize(); max > 0 && int64(len(content)) > max {
		return fmt.Errorf("file is %d bytes; server max upload is %d bytes", len(content), max)
	}
	if err := rclib.UploadFile(h, path, content); err != nil {
		return err
	}
	s.emitEvent("rc:fbChanged")
	return nil
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
	s.serverName = ""
	s.channels = nil
	s.mu.Unlock()
}

// Status returns a snapshot of the current session state.
func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := Status{
		Account:    s.creds.Account,
		Nickname:   s.creds.Nickname,
		ServerName: s.serverName,
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
