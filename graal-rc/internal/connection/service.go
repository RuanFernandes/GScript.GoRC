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
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"graal-rc/internal/folderrights"
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
	RealAccount   string `json:"realAccount"`
	CommunityName string `json:"communityName"`
}

// Service manages the grclib connection handle and the credentials in use.
// Methods are safe to call from Wails-bound goroutines.
type Service struct {
	mu              sync.Mutex
	handle          rclib.Handle
	creds           Credentials
	serverName      string // name of the server selected in ConnectToServer; "" when none
	pumpCancel      context.CancelFunc
	lastNCAttempt   time.Time  // last ConnectToNCServer attempt; throttles retries
	lastNCKeepalive time.Time  // last silent NC keepalive (weapon-list ping)
	ncRequestMu     sync.Mutex // serializes brief NC sends and synchronous mutations
	emit            func(name string, data ...any)

	// maxUpload is the latest server-reported max upload size (bytes), pushed via
	// the MaxUploadSize callback. 0 means unknown. Guarded by mu.
	maxUpload int64

	// selfRights is the cached folder_config returned by openrights for the
	// logged-in account on the current server. It is deliberately fail-closed:
	// until a fresh response is cached, script reads and writes are rejected.
	rightsMu          sync.RWMutex
	rightsRefreshMu   sync.Mutex
	rightsRequestSeq  uint64
	selfRights        folderrights.Access
	selfRightsLoaded  bool
	selfRightsError   string
	selfRightsAccount string
	// selfRightsCommunityName is the optional community name associated with
	// selfRightsAccount. Both values come from the server's RC chat identity
	// notification and are used to correlate later rights-change messages.
	selfRightsCommunityName string
	selfRightsServer        string
	selfRightsUpdated       time.Time

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

	// editor correlates a player-editor request (rights/attrs/ban/banhistory/
	// staffactivity/bantypes/comments) keyed by "<kind>:<account>" to its reply.
	// Replies arrive asynchronously on the main server pump goroutine. A wait is
	// a done-channel closed when the reply lands, so multiple callers sharing one
	// in-flight request (e.g. a window re-mounting) all observe the same reply
	// instead of racing one value across two waiters.
	editorMu sync.Mutex
	editor   map[string]*editorWait

	// banTypes is the latest server-reported ban-types list (name,seconds pairs),
	// pushed via the BanListData callback with data_type=="bantypes". Guarded by
	// editorMu. Empty until requested via GetBanTypes.
	banTypes string
}

const remoteControlBuildDate = "2026/08/03"

// RightsData is the reply payload for an OpenRights request.
type RightsData struct {
	Account      string `json:"account"`
	Rights       int    `json:"rights"`
	IPRange      string `json:"ipRange"`
	FolderAccess string `json:"folderAccess"`
}

// ScriptLists is the complete NC script index, optionally filtered by the
// current account's read rights.
type ScriptLists struct {
	Weapons []rclib.Weapon `json:"weapons"`
	Classes []rclib.Class  `json:"classes"`
	NPCs    []rclib.NPC    `json:"npcs"`
}

// AttrsData is the reply payload for an OpenAttrs request.
type AttrsData struct {
	Account        string `json:"account"`
	PropertiesJSON string `json:"propertiesJson"`
	EditorText     string `json:"editorText"`
}

// BanData is the reply payload for an OpenBan request.
type BanData struct {
	Account    string `json:"account"`
	ComputerID string `json:"computerId"`
	Details    string `json:"details"`
}

// CommentsData is the reply payload for an OpenComments request.
type CommentsData struct {
	Account string `json:"account"`
	Content string `json:"content"`
}

// scriptTimeout is how long OpenScript/OpenNPC* waits for the NC server reply.
const scriptTimeout = 15 * time.Second

const (
	rightsChangedMessage = "has set rights of"
	rightsLoadedMessage  = "loaded the rights of"
)

// downloadTimeout is how long a file download waits for the full content. File
// transfers can be large and the server slow (30 MB over a sluggish link can
// take minutes; grclib streams "Received chunk" progress meanwhile), so this is
// far longer than scriptTimeout.
const downloadTimeout = 10 * time.Minute

// pendingKey builds the correlation key for a script/flags/attributes request.
func pendingKey(kind, idOrName string) string { return kind + ":" + idOrName }

// selfRightsPendingKey is deliberately independent of the listserver login
// account. Some servers return the canonical in-game account from /openrights
// (for example, "Graal5766947") even when the RC/listserver credential is a
// different alias (for example, "Repinho").
const selfRightsPendingKey = "rights:self"

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

// editorWait is a fan-out reply slot: done is closed when reply lands, so every
// caller sharing the in-flight request observes it. closed guards against a
// double close when the same key is resolved more than once.
type editorWait struct {
	done   chan struct{}
	reply  any
	closed bool
}

// registerEditor installs a wait for an editor request key, returning it plus
// whether this call created a fresh waiter. If a not-yet-resolved waiter exists
// for the key (e.g. the window re-mounted), it is shared and isNew is false —
// the caller must then NOT re-issue the request, avoiding duplicate packets.
func (s *Service) registerEditor(key string) (*editorWait, bool) {
	s.editorMu.Lock()
	defer s.editorMu.Unlock()
	if s.editor == nil {
		s.editor = map[string]*editorWait{}
	}
	if w, ok := s.editor[key]; ok && !w.closed {
		return w, false
	}
	w := &editorWait{done: make(chan struct{})}
	s.editor[key] = w
	return w, true
}

// resolveEditor delivers a reply to all waiters for key (if any) and drops it.
// Called from pump-goroutine callbacks. The bool reports whether a waiter was
// actually resolved, which lets callbacks try a safe protocol-specific alias.
func (s *Service) resolveEditor(key string, reply any) bool {
	s.editorMu.Lock()
	w, ok := s.editor[key]
	resolved := ok && !w.closed
	if ok && !w.closed {
		w.reply = reply
		w.closed = true
		close(w.done)
	}
	if ok && w.closed {
		delete(s.editor, key)
	}
	s.editorMu.Unlock()
	return resolved
}

// dropEditor removes a timed-out waiter. Only the owning (isNew) caller drops.
func (s *Service) dropEditor(key string) {
	s.editorMu.Lock()
	delete(s.editor, key)
	s.editorMu.Unlock()
}

// awaitEditor blocks on the wait's reply or the timeout, returning the typed
// reply. Only the owner (isNew) drops the key on timeout.
func (s *Service) awaitEditor(w *editorWait, key string, isNew bool, kind string) (any, error) {
	select {
	case <-w.done:
		return w.reply, nil
	case <-time.After(scriptTimeout):
		if isNew {
			s.dropEditor(key)
		}
		return nil, errors.New(kind + " request timed out")
	}
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

// dropPending removes a waiter only if it is still the request that registered
// it. A late timeout must not delete a newer request that reused the same key.
func (s *Service) dropPending(key string, ch chan rclib.ScriptReply) {
	s.pendingMu.Lock()
	if current, ok := s.pending[key]; ok && current == ch {
		delete(s.pending, key)
	}
	s.pendingMu.Unlock()
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

type loadedRightsMessage struct {
	Actor           string
	Target          string
	Account         string
	CommunityName   string
	ExplicitAccount bool
}

// parseLoadedRightsMessage extracts the identity displayed by the RC after an
// openrights request. The optional parenthesized value is the canonical
// account when the target is a community name:
// "Repinho loaded the rights of Repinho (Graal5766947)".
func parseLoadedRightsMessage(text string) (loadedRightsMessage, bool) {
	lower := strings.ToLower(text)
	marker := strings.Index(lower, rightsLoadedMessage)
	if marker < 0 {
		return loadedRightsMessage{}, false
	}

	actor := strings.TrimSpace(text[:marker])
	if strings.HasPrefix(actor, "[") {
		if timestampEnd := strings.Index(actor, "]"); timestampEnd >= 0 {
			actor = strings.TrimSpace(actor[timestampEnd+1:])
		}
	}
	targetText := strings.TrimSpace(text[marker+len(rightsLoadedMessage):])
	if actor == "" || targetText == "" {
		return loadedRightsMessage{}, false
	}

	message := loadedRightsMessage{Actor: actor, Target: targetText, Account: targetText}
	if open := strings.LastIndex(targetText, " ("); strings.HasSuffix(targetText, ")") && open >= 0 {
		target := strings.TrimSpace(targetText[:open])
		account := strings.TrimSpace(targetText[open+2 : len(targetText)-1])
		if target != "" && account != "" {
			message.Target = target
			message.Account = account
			message.ExplicitAccount = true
			if !strings.EqualFold(target, account) {
				message.CommunityName = target
			}
		}
	}

	return message, true
}

// rightsChangedTarget extracts the account/community target from a rights
// mutation notification. The actor is intentionally ignored: another staff
// member may perform the change on behalf of the logged-in player.
func rightsChangedTarget(text string) string {
	lower := strings.ToLower(text)
	marker := strings.Index(lower, rightsChangedMessage)
	if marker < 0 {
		return ""
	}

	target := strings.TrimSpace(text[marker+len(rightsChangedMessage):])
	for _, prefix := range []string{"(offline) player ", "(online) player ", "player "} {
		if strings.HasPrefix(strings.ToLower(target), prefix) {
			target = strings.TrimSpace(target[len(prefix):])
			break
		}
	}
	return target
}

func isRightsChangedMessage(text string) bool {
	return rightsChangedTarget(text) != ""
}

func equalFoldAny(value string, candidates ...string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, candidate := range candidates {
		if strings.EqualFold(value, strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

// selfRightsAliases returns every identity that can be used by the server for
// the current session. The login account/nickname are available immediately;
// the canonical account and optional community name are learned from RC chat.
func (s *Service) selfRightsAliases() []string {
	s.mu.Lock()
	aliases := []string{s.creds.Account, s.creds.Nickname}
	s.mu.Unlock()

	s.rightsMu.RLock()
	aliases = append(aliases, s.selfRightsAccount, s.selfRightsCommunityName)
	s.rightsMu.RUnlock()
	return aliases
}

func (s *Service) isSelfRightsAlias(value string) bool {
	return equalFoldAny(value, s.selfRightsAliases()...)
}

// captureSelfRightsIdentity accepts only a message generated by the current
// RC identity. This prevents a staff member viewing another player's rights
// from replacing the local account/community cache.
func (s *Service) captureSelfRightsIdentity(message loadedRightsMessage) {
	aliases := s.selfRightsAliases()
	if !equalFoldAny(message.Actor, aliases...) {
		return
	}
	if !equalFoldAny(message.Target, aliases...) && !equalFoldAny(message.Account, aliases...) {
		return
	}

	account := strings.TrimSpace(message.Account)
	if account == "" {
		return
	}
	community := strings.TrimSpace(message.CommunityName)

	s.rightsMu.Lock()
	changed := false
	if message.ExplicitAccount || s.selfRightsAccount == "" {
		if s.selfRightsAccount != account {
			s.selfRightsAccount = account
			changed = true
		}
	}
	if community != "" && s.selfRightsCommunityName != community {
		s.selfRightsCommunityName = community
		changed = true
	}
	s.rightsMu.Unlock()

	if changed {
		log.Printf("[rights] self identity captured actor=%q community=%q account=%q", message.Actor, community, account)
		s.emitEvent("rc:scriptIdentityChanged")
	}
}

// handleRCMessage watches the server chat for rights identity/change
// notifications. The callback runs on grclib's pump, so a refresh must stay
// asynchronous: OpenRights waits for another callback from that pump.
func (s *Service) handleRCMessage(text string) {
	if loaded, ok := parseLoadedRightsMessage(text); ok {
		s.captureSelfRightsIdentity(loaded)
		return
	}

	target := rightsChangedTarget(text)
	if target == "" || !s.isSelfRightsAlias(target) {
		return
	}
	log.Printf("[rights] detected self rights change target=%q; refreshing self folder rights", target)
	go func() {
		if err := s.RefreshSelfFolderRights(); err != nil {
			log.Printf("[rights] refresh after chat notification failed: %v", err)
		}
	}()
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
	s.lastNCKeepalive = time.Time{}
	s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	s.pumpCancel = cancel
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[pump] fatal panic (event pump stopped): %v\n%s", r, debug.Stack())
			}
		}()
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				rclib.ProcessEvents(h)
				s.maybeConnectNC(h)
				s.ncKeepalive(h)
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

// ncKeepaliveInterval is how often a silent NC packet is sent to keep the NC
// (script) socket alive. The Graal server drops an idle NC session after a
// while (surfacing as "[NC] DISCONNECT: You don't have admin rights."); issuing
// a lightweight NC round-trip periodically prevents that idle timeout.
const ncKeepaliveInterval = 30 * time.Second

// ncFetchConcurrency bounds the number of in-flight OpenScript requests during
// a bulk fetch. The send is serialized on dllMu, but the wait for the reply is
// not, so pipelining many requests is much faster than strict serial fetches.
// 16 keeps the server from being flooded while still saturating the round-trip
// pipeline.
const ncFetchConcurrency = 16

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

// ncKeepalive sends a silent NC round-trip (weapon-list request, PLI 115) every
// ncKeepaliveInterval while NC is connected. The response just refreshes the
// cached list; nothing is surfaced to the UI or logs, so it acts purely as a
// ping that keeps the idle NC socket from being dropped by the server. Skipped
// when NC is down so it never triggers a reconnect itself.
func (s *Service) ncKeepalive(h rclib.Handle) {
	s.mu.Lock()
	if !s.lastNCKeepalive.IsZero() && time.Since(s.lastNCKeepalive) < ncKeepaliveInterval {
		s.mu.Unlock()
		return
	}
	s.lastNCKeepalive = time.Now()
	s.mu.Unlock()

	hasNc := rclib.HasNCServer(h)
	connected := rclib.IsNCConnected(h)
	if !hasNc || !connected {
		return
	}
	// Silent: ignore errors — this is best-effort keepalive, not a user action.
	_ = rclib.SendNCPacket(h, weaponListGetPacket)
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
	s.clearSelfFolderRights()

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
	s.clearSelfFolderRights()
	// ConnectToServer reuses the same grclib handle. The NC socket belongs to
	// the previously selected server, so it must be closed before switching the
	// main connection; otherwise the next sync can observe the old NC lists and
	// fetch scripts from the wrong server.
	if rclib.IsNCConnected(h) {
		log.Printf("[connection] disconnecting NC before switching server")
		if err := rclib.DisconnectNC(h); err != nil {
			log.Printf("[connection] disconnect old NC before server switch: %v", err)
		}
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
			s.clearSelfFolderRights()
			s.emitEvent("rc:disconnected", reason)
			s.emitEvent("rc:channels", s.resetChannels())
		},
		Message: func(text string) {
			s.handleRCMessage(text)
			s.emitEvent("rc:message", text)
		},
		IrcMessage: func(channel, line string) { s.handleIrcMessage(channel, line) },
		PrivateMessage: func(playerID int, account, nick, message string) {
			s.emitEvent("rc:pm", playerID, account, nick, message)
		},
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
		PlayerRights: func(account string, rights int, ipRange, folderAccess string) {
			log.Printf("[rights callback] account=%q rights=%d folderAccessLen=%d folderAccess=%q", account, rights, len(folderAccess), folderAccess)
			data := RightsData{
				Account: account, Rights: rights, IPRange: ipRange, FolderAccess: folderAccess,
			}
			resolved := s.resolveEditor(pendingKey("rights", account), data)
			// A self request is sent with an empty target, while the server may
			// return a canonical account that differs entirely from the
			// listserver credential. Resolve the protocol-specific self waiter
			// only when the account-specific waiter did not already match.
			if !resolved {
				resolved = s.resolveEditor(selfRightsPendingKey, data)
			}
			if resolved {
				log.Printf("[rights callback] resolved pending request account=%q", account)
			} else {
				log.Printf("[rights callback] no pending waiter for account=%q", account)
			}
		},
		PlayerAttributes: func(account, propertiesJSON, editorText string) {
			log.Printf("[editor-cb] player_attributes account=%q editorLen=%d", account, len(editorText))
			s.resolveEditor(pendingKey("attrs", account), AttrsData{
				Account: account, PropertiesJSON: propertiesJSON, EditorText: editorText,
			})
		},
		BanData: func(account, computerID, details string) {
			log.Printf("[editor-cb] ban_data account=%q", account)
			s.resolveEditor(pendingKey("ban", account), BanData{
				Account: account, ComputerID: computerID, Details: details,
			})
		},
		BanListData: func(dataType, account, content string) {
			log.Printf("[editor-cb] ban_list_data type=%q account=%q len=%d", dataType, account, len(content))
			switch dataType {
			case "bantypes":
				// Global list (no account correlation): cache for GetBanTypes.
				s.editorMu.Lock()
				s.banTypes = content
				s.editorMu.Unlock()
				s.resolveEditor(pendingKey("bantypes", ""), content)
			case "banhistory":
				s.resolveEditor(pendingKey("banhistory", account), content)
			case "staffactivity":
				s.resolveEditor(pendingKey("staffactivity", account), content)
			default:
				log.Printf("[banlistdata] %s %s: %q", dataType, account, content)
			}
		},
		PlayerTextData: func(dataType, account, content string) {
			log.Printf("[editor-cb] player_text_data type=%q account=%q len=%d", dataType, account, len(content))
			switch dataType {
			case "comments":
				s.resolveEditor(pendingKey("comments", account), CommentsData{Account: account, Content: content})
			default:
				log.Printf("[playertextdata] %s %s: %q", dataType, account, content)
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
		// The server does not adopt our nickname until we send it (mirrors the
		// reference client, which calls rc_set_nickname in its onConnected).
		if nick := s.creds.Nickname; nick != "" {
			if err := rclib.SetNickname(h, nick); err != nil {
				log.Printf("set nickname %q: %v", nick, err)
			}
		}
		// Match the reference Remote Control client: announce the client build
		// date through the RC chat packet immediately after authentication.
		if err := rclib.Execute(h, "/npc newrc,"+remoteControlBuildDate); err != nil {
			log.Printf("announce Remote Control build date %q: %v", remoteControlBuildDate, err)
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

// SendPrivateMessage sends a private message to a single player id on the
// active server.
func (s *Service) SendPrivateMessage(playerID int, message string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SendPrivateMessage(h, playerID, message)
}

// SendMassPM sends one bulk PM packet to every id in playerIDs (single server
// round-trip, mirrors the reference client's Mass PM button).
func (s *Service) SendMassPM(playerIDs []int, message string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SendMassPM(h, playerIDs, message)
}

// SendAdminMessage sends an admin message to a single player id.
func (s *Service) SendAdminMessage(playerID int, message string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SendAdminMessage(h, playerID, message)
}

// SendAdminMessageAll sends an admin message to every player on the server.
func (s *Service) SendAdminMessageAll(message string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SendAdminMessageAll(h, message)
}

// SelfAccount returns the logged-in account name.
func (s *Service) SelfAccount() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.creds.Account
}

// clearSelfFolderRights invalidates the permission snapshot when the active
// account or server changes. Clearing instead of reusing the previous value is
// important because the same account can have different folder rules per
// server.
func (s *Service) clearSelfFolderRights() {
	s.rightsMu.Lock()
	s.selfRights = folderrights.Access{}
	s.selfRightsLoaded = false
	s.selfRightsError = ""
	s.selfRightsAccount = ""
	s.selfRightsCommunityName = ""
	s.selfRightsServer = ""
	s.selfRightsUpdated = time.Time{}
	s.rightsMu.Unlock()
	s.emitEvent("rc:scriptPermissionsChanged")
}

func (s *Service) invalidateSelfFolderRights(err error) {
	s.rightsMu.Lock()
	s.selfRights = folderrights.Access{}
	s.selfRightsLoaded = false
	s.selfRightsError = ""
	if err != nil {
		s.selfRightsError = err.Error()
	}
	s.selfRightsUpdated = time.Time{}
	s.rightsMu.Unlock()
}

// RefreshSelfFolderRights explicitly asks the current server for this
// account's folder access and replaces the local snapshot. Sync calls this at
// bootstrap and before every polling pass.
func (s *Service) RefreshSelfFolderRights() error {
	s.rightsRefreshMu.Lock()
	defer s.rightsRefreshMu.Unlock()

	account := strings.TrimSpace(s.SelfAccount())
	started := time.Now()
	log.Printf("[rights] refresh start account=%q", account)
	if account == "" {
		err := errors.New("cannot refresh script permissions: not logged in")
		log.Printf("[rights] refresh failed after %s: %v", time.Since(started), err)
		s.invalidateSelfFolderRights(err)
		return err
	}
	// For the current account, the RC protocol expects an empty target and lets
	// the server resolve the caller. Sending the cached/login account string can
	// be treated as a request for another player when its canonical casing
	// differs from the server session's accountName.
	data, err := s.OpenRights("")
	if err != nil {
		err = fmt.Errorf("openrights for %q: %w", account, err)
		log.Printf("[rights] refresh failed after %s: %v", time.Since(started), err)
		s.invalidateSelfFolderRights(err)
		return err
	}
	log.Printf("[rights] openrights completed account=%q folderAccessLen=%d", data.Account, len(data.FolderAccess))
	returnedAccount := strings.TrimSpace(data.Account)
	if returnedAccount == "" {
		err = fmt.Errorf("openrights returned an empty account, expected the current server account")
		log.Printf("[rights] refresh failed after %s: %v", time.Since(started), err)
		s.invalidateSelfFolderRights(err)
		return err
	}
	access, err := folderrights.Parse(data.FolderAccess)
	if err != nil {
		err = fmt.Errorf("parse folder access for %q: %w", account, err)
		log.Printf("[rights] refresh failed after %s: %v", time.Since(started), err)
		s.invalidateSelfFolderRights(err)
		return err
	}

	s.mu.Lock()
	server := s.serverName
	s.mu.Unlock()
	s.rightsMu.Lock()
	s.selfRights = access
	s.selfRightsLoaded = true
	s.selfRightsError = ""
	s.selfRightsAccount = returnedAccount
	s.selfRightsServer = server
	s.selfRightsUpdated = time.Now()
	s.rightsMu.Unlock()
	log.Printf("[rights] refresh success localAccount=%q serverAccount=%q folderAccessLen=%d elapsed=%s", account, returnedAccount, len(data.FolderAccess), time.Since(started))
	s.emitEvent("rc:scriptPermissionsChanged")
	return nil
}

func (s *Service) ensureSelfFolderRights() error {
	s.rightsMu.RLock()
	loaded := s.selfRightsLoaded
	s.rightsMu.RUnlock()
	if loaded {
		return nil
	}
	return s.RefreshSelfFolderRights()
}

func (s *Service) selfFolderRights() (folderrights.Access, bool) {
	s.rightsMu.RLock()
	access, loaded := s.selfRights, s.selfRightsLoaded
	s.rightsMu.RUnlock()
	return access, loaded
}

// CanReadScript reports the cached read permission for one logical script.
// It returns false while the cache is unavailable, so callers cannot
// accidentally use a stale or unknown permission state.
func (s *Service) CanReadScript(scriptType, name string) bool {
	access, loaded := s.selfFolderRights()
	return loaded && access.CanRead(scriptType, name)
}

// CanWriteScript reports the cached write permission for one logical script.
func (s *Service) CanWriteScript(scriptType, name string) bool {
	access, loaded := s.selfFolderRights()
	return loaded && access.CanWrite(scriptType, name)
}

// OpenRights requests an account's staff rights and waits for the reply. An
// empty account sends /openrights without an argument so the server resolves
// the current RC session to itself; explicit accounts use the direct rights
// request packet.
func (s *Service) OpenRights(account string) (RightsData, error) {
	account = strings.TrimSpace(account)
	selfAccount := strings.TrimSpace(s.SelfAccount())
	selfRequest := account == "" || (selfAccount != "" && strings.EqualFold(account, selfAccount))
	expectedAccount := account
	if selfRequest {
		expectedAccount = selfAccount
	}
	if expectedAccount == "" {
		return RightsData{}, errors.New("account is required")
	}
	requestID := atomic.AddUint64(&s.rightsRequestSeq, 1)
	started := time.Now()
	h, err := s.requireHandle()
	if err != nil {
		log.Printf("[rights #%d] OpenRights rejected target=%q self=%v: %v", requestID, account, selfRequest, err)
		return RightsData{}, err
	}
	key := pendingKey("rights", expectedAccount)
	if selfRequest {
		key = selfRightsPendingKey
	}
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[rights #%d] dispatch target=%q self=%v expected=%q command=%q", requestID, account, selfRequest, expectedAccount, func() string {
			if selfRequest {
				return "/openrights"
			}
			return "PLI_RC_PLAYERRIGHTSGET"
		}())
		var requestErr error
		if selfRequest {
			// The server's /openrights command resolves an omitted account to
			// the current RC session. Passing the cached account string can be
			// rejected when its casing differs from the server's canonical name.
			requestErr = rclib.Execute(h, "/openrights")
		} else {
			requestErr = rclib.RequestPlayerRights(h, account)
		}
		if requestErr != nil {
			log.Printf("[rights #%d] dispatch failed after %s: %v", requestID, time.Since(started), requestErr)
			s.dropEditor(key)
			return RightsData{}, requestErr
		}
		log.Printf("[rights #%d] dispatched; waiting for callback key=%q timeout=%s", requestID, key, scriptTimeout)
	} else {
		log.Printf("[rights #%d] joined in-flight request target=%q self=%v key=%q", requestID, account, selfRequest, key)
	}
	reply, err := s.awaitEditor(w, key, isNew, "player rights")
	if err != nil {
		log.Printf("[rights #%d] callback wait failed after %s key=%q: %v", requestID, time.Since(started), key, err)
		return RightsData{}, err
	}
	data, ok := reply.(RightsData)
	if !ok {
		err = fmt.Errorf("unexpected player rights callback type %T", reply)
		log.Printf("[rights #%d] callback invalid after %s key=%q: %v", requestID, time.Since(started), key, err)
		return RightsData{}, err
	}
	log.Printf("[rights #%d] callback received after %s account=%q folderAccessLen=%d", requestID, time.Since(started), data.Account, len(data.FolderAccess))
	return data, nil
}

// SetRights writes rights flags + ip range + folder access for an account.
func (s *Service) SetRights(account string, rights int, ipRange, folderAccess string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SetPlayerRights(h, account, rights, ipRange, folderAccess)
}

// OpenAttrs requests an account's attributes and waits for the reply.
func (s *Service) OpenAttrs(account string) (AttrsData, error) {
	h, err := s.requireHandle()
	if err != nil {
		return AttrsData{}, err
	}
	key := pendingKey("attrs", account)
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request attrs account=%q (self creds.Account=%q nickname=%q)", account, s.creds.Account, s.creds.Nickname)
		if err := rclib.RequestPlayerAttrs(h, account); err != nil {
			s.dropEditor(key)
			return AttrsData{}, err
		}
	} else {
		log.Printf("[editor] dedup attrs account=%q (awaiting in-flight request)", account)
	}
	reply, err := s.awaitEditor(w, key, isNew, "player attributes")
	if err != nil {
		return AttrsData{}, err
	}
	return reply.(AttrsData), nil
}

// SetAttrs writes the properties JSON blob for an account.
func (s *Service) SetAttrs(account, propertiesJSON string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SetPlayerAttributes(h, account, propertiesJSON)
}

// ParseAttrsText converts an INI-style attribute editor document into the
// properties JSON blob the protocol expects.
func (s *Service) ParseAttrsText(text string) (string, error) {
	return rclib.ParsePlayerAttributesText(text)
}

// OpenBan requests an account's ban data and waits for the reply. Use
// GetBanTypes separately (or before) to populate the duration dropdown.
func (s *Service) OpenBan(account string) (BanData, error) {
	h, err := s.requireHandle()
	if err != nil {
		return BanData{}, err
	}
	key := pendingKey("ban", account)
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request ban account=%q (self creds.Account=%q nickname=%q)", account, s.creds.Account, s.creds.Nickname)
		if err := rclib.RequestPlayerBanByAccount(h, account); err != nil {
			s.dropEditor(key)
			return BanData{}, err
		}
	} else {
		log.Printf("[editor] dedup ban account=%q (awaiting in-flight request)", account)
	}
	reply, err := s.awaitEditor(w, key, isNew, "player ban")
	if err != nil {
		return BanData{}, err
	}
	return reply.(BanData), nil
}

// SetBan writes ban data for a target. world is "local" or "all"; target is the
// account or "pc:<computerID>"; releaseTime "" resets the ban timer.
func (s *Service) SetBan(target, world string, banned bool, banType, releaseTime, reason string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SetBan(h, target, world, banned, banType, releaseTime, reason)
}

// OpenComments requests an account's comments text and waits for the reply.
func (s *Service) OpenComments(account string) (CommentsData, error) {
	h, err := s.requireHandle()
	if err != nil {
		return CommentsData{}, err
	}
	key := pendingKey("comments", account)
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request comments account=%q (self creds.Account=%q nickname=%q)", account, s.creds.Account, s.creds.Nickname)
		if err := rclib.RequestPlayerComments(h, account); err != nil {
			s.dropEditor(key)
			return CommentsData{}, err
		}
	} else {
		log.Printf("[editor] dedup comments account=%q (awaiting in-flight request)", account)
	}
	reply, err := s.awaitEditor(w, key, isNew, "player comments")
	if err != nil {
		return CommentsData{}, err
	}
	return reply.(CommentsData), nil
}

// SetComments writes the comments text for an account.
func (s *Service) SetComments(account, comments string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SetPlayerComments(h, account, comments)
}

// GetBanTypes requests the available ban types/durations and waits for the
// reply. Returns the raw "name,seconds\\n..." list.
func (s *Service) GetBanTypes() (string, error) {
	h, err := s.requireHandle()
	if err != nil {
		return "", err
	}
	// Serve from cache if a previous request already populated it.
	s.editorMu.Lock()
	cached := s.banTypes
	s.editorMu.Unlock()
	if cached != "" {
		return cached, nil
	}
	key := pendingKey("bantypes", "")
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request bantypes")
		if err := rclib.RequestBanTypes(h); err != nil {
			s.dropEditor(key)
			return "", err
		}
	}
	reply, err := s.awaitEditor(w, key, isNew, "ban types")
	if err != nil {
		return "", err
	}
	if txt, ok := reply.(string); ok {
		return txt, nil
	}
	return "", nil
}

// RequestBanHistory asks for an account's ban history and waits for the reply.
func (s *Service) RequestBanHistory(account string) (string, error) {
	h, err := s.requireHandle()
	if err != nil {
		return "", err
	}
	key := pendingKey("banhistory", account)
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request banhistory account=%q", account)
		if err := rclib.RequestBanHistory(h, account); err != nil {
			s.dropEditor(key)
			return "", err
		}
	}
	reply, err := s.awaitEditor(w, key, isNew, "ban history")
	if err != nil {
		return "", err
	}
	if txt, ok := reply.(string); ok {
		return txt, nil
	}
	return "", nil
}

// RequestStaffActivity asks for an account's staff activity and waits for the reply.
func (s *Service) RequestStaffActivity(account string) (string, error) {
	h, err := s.requireHandle()
	if err != nil {
		return "", err
	}
	key := pendingKey("staffactivity", account)
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request staffactivity account=%q", account)
		if err := rclib.RequestStaffActivity(h, account); err != nil {
			s.dropEditor(key)
			return "", err
		}
	}
	reply, err := s.awaitEditor(w, key, isNew, "staff activity")
	if err != nil {
		return "", err
	}
	if txt, ok := reply.(string); ok {
		return txt, nil
	}
	return "", nil
}

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

// GetScriptLists returns the NC script index. When onlyReadable is true, the
// lists are filtered using the cached self folder rights; the cache is loaded
// on demand if this is the first permission-aware request in the session.
func (s *Service) GetScriptLists(onlyReadable bool) (ScriptLists, error) {
	var access folderrights.Access
	if onlyReadable {
		if err := s.ensureSelfFolderRights(); err != nil {
			return ScriptLists{}, err
		}
		var loaded bool
		access, loaded = s.selfFolderRights()
		if !loaded {
			return ScriptLists{}, errors.New("script permissions are not loaded")
		}
	}

	weapons, err := s.GetWeapons()
	if err != nil {
		return ScriptLists{}, err
	}
	classes, err := s.GetClasses()
	if err != nil {
		return ScriptLists{}, err
	}
	npcs, err := s.GetNPCs()
	if err != nil {
		return ScriptLists{}, err
	}
	if !onlyReadable {
		return ScriptLists{Weapons: weapons, Classes: classes, NPCs: npcs}, nil
	}

	filteredWeapons := make([]rclib.Weapon, 0, len(weapons))
	for _, weapon := range weapons {
		if access.CanRead("weapon", weapon.Name) {
			filteredWeapons = append(filteredWeapons, weapon)
		}
	}
	filteredClasses := make([]rclib.Class, 0, len(classes))
	for _, class := range classes {
		if access.CanRead("class", class.Name) {
			filteredClasses = append(filteredClasses, class)
		}
	}
	filteredNPCs := make([]rclib.NPC, 0, len(npcs))
	for _, npc := range npcs {
		if access.CanRead("npc", npc.Name) {
			filteredNPCs = append(filteredNPCs, npc)
		}
	}
	return ScriptLists{Weapons: filteredWeapons, Classes: filteredClasses, NPCs: filteredNPCs}, nil
}

// IsNCConnected reports whether the NC (script) socket is up. Used by the sync
// engine to gate server I/O.
func (s *Service) IsNCConnected() bool {
	h, err := s.requireHandle()
	if err != nil || h == 0 {
		return false
	}
	return rclib.IsNCConnected(h)
}

// IsNCAuthenticated reports whether the NC handshake completed. A connected
// socket can still be warming its weapon/class/NPC caches for a short moment.
func (s *Service) IsNCAuthenticated() bool {
	h, err := s.requireHandle()
	if err != nil || h == 0 {
		return false
	}
	return rclib.IsNCAuthenticated(h)
}

// FetchAllScripts pulls every weapon/class/npc script body from the server.
// Requests are PIPELINED with bounded concurrency (ncFetchConcurrency): each
// OpenScript sends its NC packet (serialized on dllMu for the brief send) then
// waits on its own pending reply channel, so many requests are in flight at
// once rather than strictly sequential — a large server that took a minute
// serially now takes seconds. A single hung script times out (scriptTimeout,
// 15s) and is skipped+logged; it never aborts the fetch. progress (optional)
// reports done/total so the UI can show a background bar.
func (s *Service) FetchAllScripts(ctx context.Context, allowed func(scriptType, name string) bool, progress func(done, total int)) ([]rclib.ScriptReply, error) {
	if _, err := s.requireNC(); err != nil {
		return nil, err
	}
	weapons, err := s.GetWeapons()
	if err != nil {
		return nil, err
	}
	classes, err := s.GetClasses()
	if err != nil {
		return nil, err
	}
	npcs, err := s.GetNPCs()
	if err != nil {
		return nil, err
	}
	log.Printf("[sync fetch] script lists received weapons=%d classes=%d npcs=%d", len(weapons), len(classes), len(npcs))

	type job struct {
		stype, key, name string
	}
	var jobs []job
	skipped := 0
	for _, w := range weapons {
		if allowed == nil || allowed("weapon", w.Name) {
			jobs = append(jobs, job{"weapon", w.Name, w.Name})
		} else {
			skipped++
		}
	}
	for _, c := range classes {
		if allowed == nil || allowed("class", c.Name) {
			jobs = append(jobs, job{"class", c.Name, c.Name})
		} else {
			skipped++
		}
	}
	for _, n := range npcs {
		if allowed == nil || allowed("npc", n.Name) {
			jobs = append(jobs, job{"npc", strconv.Itoa(n.ID), n.Name})
		} else {
			skipped++
		}
	}
	total := len(jobs)
	log.Printf("[sync fetch] permission filter kept=%d skipped=%d", total, skipped)

	out := make([]rclib.ScriptReply, 0, total)
	var outMu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, ncFetchConcurrency)
	var done int32
	for _, j := range jobs {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			if r, err := s.openScript(j.stype, j.key, j.name); err == nil {
				// Some NC callbacks return only the NPC id. Keep the display
				// name from the cached NPC list so local sync never falls back
				// to an ID-based filename.
				if r.Type == "npc" && r.Name == "" {
					r.Name = j.name
				}
				outMu.Lock()
				out = append(out, r)
				outMu.Unlock()
			} else {
				log.Printf("sync fetch %s:%s failed: %v", j.stype, j.key, err)
			}
			if progress != nil {
				progress(int(atomic.AddInt32(&done, 1)), total)
			}
		}(j)
	}
	wg.Wait()
	return out, ctx.Err()
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
	if err := s.requireScriptPermission("weapon", name, 'w'); err != nil {
		return err
	}
	s.ncRequestMu.Lock()
	defer s.ncRequestMu.Unlock()
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.UpdateWeapon(h, name, script)
}

// SaveClass writes a class's script back to the server.
func (s *Service) SaveClass(name, script string) error {
	if err := s.requireScriptPermission("class", name, 'w'); err != nil {
		return err
	}
	s.ncRequestMu.Lock()
	defer s.ncRequestMu.Unlock()
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.UpdateClass(h, name, script)
}

// SaveNPC writes an NPC's script back to the server.
func (s *Service) SaveNPC(id int, script string) error {
	name, err := s.npcNameByID(id)
	if err != nil {
		return err
	}
	if err := s.requireScriptPermission("npc", name, 'w'); err != nil {
		return err
	}
	s.ncRequestMu.Lock()
	defer s.ncRequestMu.Unlock()
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.UpdateNPC(h, id, script)
}

func (s *Service) npcNameByID(id int) (string, error) {
	npcs, err := s.GetNPCs()
	if err != nil {
		return "", err
	}
	for _, npc := range npcs {
		if npc.ID == id {
			return npc.Name, nil
		}
	}
	return "", fmt.Errorf("NPC %d is not present in the current script list", id)
}

func (s *Service) requireScriptPermission(scriptType, name string, right rune) error {
	if err := s.ensureSelfFolderRights(); err != nil {
		return fmt.Errorf("script permissions unavailable: %w", err)
	}
	allowed := false
	if right == 'r' {
		allowed = s.CanReadScript(scriptType, name)
	} else if right == 'w' {
		allowed = s.CanWriteScript(scriptType, name)
	}
	if !allowed {
		return fmt.Errorf("no %c permission for %s %q", right, scriptType, name)
	}
	return nil
}

// OpenScript requests a script from the server and waits for the reply. For
// weapon/class, key is the name; for npc, key is the stringified id.
func (s *Service) OpenScript(scriptType, key string) (rclib.ScriptReply, error) {
	name := key
	if scriptType == "npc" {
		id, err := strconv.Atoi(key)
		if err != nil {
			return rclib.ScriptReply{}, err
		}
		name, err = s.npcNameByID(id)
		if err != nil {
			return rclib.ScriptReply{}, err
		}
	}
	return s.openScript(scriptType, key, name)
}

func (s *Service) openScript(scriptType, key, name string) (rclib.ScriptReply, error) {
	if scriptType != "weapon" && scriptType != "class" && scriptType != "npc" {
		return rclib.ScriptReply{}, errors.New("unknown script type: " + scriptType)
	}
	if err := s.requireScriptPermission(scriptType, name, 'r'); err != nil {
		return rclib.ScriptReply{}, err
	}
	h, err := s.requireHandle()
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	pending := pendingKey(scriptType, key)
	ch := s.registerPending(pending)

	// Serialize only the native send. The reply is delivered asynchronously by
	// the pump and is correlated by its own pending key, so another script can
	// be sent while this one is still waiting for the server.
	s.ncRequestMu.Lock()
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
		s.ncRequestMu.Unlock()
		s.dropPending(pending, ch)
		return rclib.ScriptReply{}, errors.New("unknown script type: " + scriptType)
	}
	s.ncRequestMu.Unlock()
	if err != nil {
		s.dropPending(pending, ch)
		return rclib.ScriptReply{}, err
	}
	select {
	case reply := <-ch:
		return reply, nil
	case <-time.After(scriptTimeout):
		s.dropPending(pending, ch)
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

// RefreshWeapons re-requests the weapon list via grclib's dedicated
// rc_request_weapon_list primitive (the same call the reference C++ RC makes),
// forcing the server to repopulate the cache and re-emit add events. Note:
// grclib exposes NO equivalent for class/npc lists — those are maintained by
// the server's live add/delete push packets (rc_on_class_added/deleted,
// rc_on_npc_added/deleted), wired into the sync engine via HandleListChanged.
func (s *Service) RefreshWeapons() error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}
	return rclib.RequestWeaponList(h)
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
	s.clearSelfFolderRights()
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
	s.rightsMu.RLock()
	st.RealAccount = s.selfRightsAccount
	st.CommunityName = s.selfRightsCommunityName
	s.rightsMu.RUnlock()
	return st
}
