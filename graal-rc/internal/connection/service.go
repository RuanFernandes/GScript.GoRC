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
	Loaded            bool   `json:"loaded"`
	DLLPath           string `json:"dllPath"`
	Connected         bool   `json:"connected"`
	Authenticated     bool   `json:"authenticated"`
	Account           string `json:"account"`
	Nickname          string `json:"nickname"`
	ServerName        string `json:"serverName"`
	RealAccount       string `json:"realAccount"`
	CommunityName     string `json:"communityName"`
	Rights            int    `json:"rights"`
	RightsReady       bool   `json:"rightsReady"`
	CanBanPlayers     bool   `json:"canBanPlayers"`
	ScriptWriteAccess bool   `json:"scriptWriteAccess"`
}

// Service manages the grclib connection handle and the credentials in use.
// Methods are safe to call from Wails-bound goroutines.
type Service struct {
	mu                            sync.Mutex
	handle                        rclib.Handle
	creds                         Credentials
	serverName                    string // name of the server selected in ConnectToServer; "" when none
	serverEpoch                   uint64 // increments whenever the active server/session changes
	lifecycleMu                   sync.Mutex
	lifecycleOp                   *connectionOperation
	pumpMu                        sync.Mutex
	pumpCancel                    context.CancelFunc
	pumpDone                      chan struct{}
	pumpErr                       error
	ncConnectMu                   sync.Mutex // serializes native NC connect/disconnect operations
	ncConnectionAttempted         bool       // initial automatic attempt for this main-server session
	ncManuallyDisconnected        bool       // disabled by an explicit DisconnectNC call
	nextNCConnectAttempt          time.Time  // scheduled after refreshing the NPC-server location
	ncConnectionWasUp             bool       // NC was observed connected at least once
	ncAutomaticReconnectAttempted bool       // one retry consumed while this NC drop remains down
	ncRequestMu                   sync.Mutex // serializes brief NC sends and synchronous mutations
	ncHeartbeatMu                 sync.Mutex
	ncHeartbeatCancel             context.CancelFunc
	ncHeartbeatDone               chan struct{}
	// scriptListsMu coalesces concurrent reads of the native NC script caches.
	// GetScriptLists can be called from more than one Wails window and from
	// several cache-change events at once, while grclib exposes one shared
	// native call path. Only one base snapshot may be in flight at a time.
	scriptListsMu       sync.Mutex
	scriptListsInFlight *scriptListsRequest
	// weaponListGeneration advances only after grclib has rebuilt its complete
	// weapon cache from a list response. weaponListReady is scoped to the
	// current NC session so the sync bootstrap can wait for the automatic list
	// without requesting another one.
	weaponListMu         sync.Mutex
	weaponListGeneration uint64
	weaponListReady      bool
	weaponListSignal     chan struct{}
	emitMu               sync.RWMutex
	emit                 func(name string, data ...any)
	chatMu               sync.RWMutex
	chatHistory          []ChatLine

	// maxUpload is the latest server-reported max upload size (bytes), pushed via
	// the MaxUploadSize callback. 0 means unknown. Guarded by mu.
	maxUpload int64

	// selfRights is the cached openrights response for the logged-in account on
	// the current server. It is deliberately fail-closed: until a fresh
	// response is cached, script reads/writes and privileged player actions are
	// rejected.
	rightsMu         sync.RWMutex
	rightsRefreshMu  sync.Mutex
	rightsRequestSeq uint64
	selfRights       folderrights.Access
	selfStaffRights  int
	selfRightsLoaded bool
	selfRightsError  string
	// selfRightsBaseline is the last successful server snapshot retained across
	// disconnect/reconnect transitions. The active cache above is cleared while
	// disconnected so callers remain fail-closed, but permission comparisons must
	// still use the last server snapshot instead of treating every rejoin as a
	// first load.
	selfRightsBaseline        folderrights.Access
	selfStaffRightsBaseline   int
	selfRightsBaselineLoaded  bool
	selfRightsBaselineAccount string
	selfRightsBaselineServer  string
	selfRightsAccount         string
	// selfRightsCommunityName is the optional community name associated with
	// selfRightsAccount. The account is learned from the authenticated rights
	// response, while the community name comes from the player property stream
	// when the server exposes it.
	selfRightsCommunityName string
	selfRightsServer        string
	selfRightsUpdated       time.Time

	// playerIdentityProps mirrors the account/community properties decoded by
	// grclib for each player. The initial PLO_ADDPLAYER callback emits property
	// changes before the player-joined callback, so retaining both values lets
	// us correlate the local player's community name when the authenticated
	// account becomes available a few events later.
	playerIdentityMu  sync.Mutex
	playerAccounts    map[int]string
	playerCommunities map[int]string
	selfPlayerID      int

	// pendingFiles correlates a file download request (by remote path) to its
	// content bytes, delivered asynchronously via the FileReceived callback.
	// grclib has one active File Browser transfer slot: starting another request
	// clears the native transfer state for the previous one. fileDownloadMu is
	// held from request dispatch until the matching callback arrives so normal
	// downloads and thumbnail previews cannot overwrite each other.
	fileDownloadMu sync.Mutex
	pendingFilesMu sync.Mutex
	pendingFiles   map[string]*fileWait
	// channels is the authoritative set of joined IRC channels, derived from the
	// join/left marker lines. It is the single source of truth for which IRC
	// tabs the frontend should show; the frontend reconciles its tabs against a
	// snapshot of this set, so React batching/event ordering cannot desync them.
	// Removals are debounced (channelLeaveCooldown): the server always sends a
	// PART before the matching JOIN (to avoid duplicates), so a leave is only
	// committed after the cooldown elapses with no rejoin — a JOIN in the window
	// cancels the pending leave and the tab survives.
	channels map[string]*channelState

	// pending maps a script/flags/attributes request key to its reply waiter.
	// A request (OpenScript/OpenNPCFlags/OpenNPCAttributes) registers under a key
	// like "weapon:name" / "npc:<id>" / "npcflags:<id>" / "npcattr:<id>" and the
	// matching pump-goroutine callback resolves it. Replies arrive asynchronously
	// from the NC server.
	pendingMu sync.Mutex
	pending   map[string]*pendingWait

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

	// serverText contains the latest server options/flags snapshot. These are
	// fetched after server login so the embedded GraalScript LSP can resolve
	// server., serverr. and serveroptions. without opening a config editor first.
	serverTextMu        sync.RWMutex
	serverTextRequestMu sync.Mutex
	serverOptions       string
	serverFlags         string
	serverOptionsLoaded bool
	serverFlagsLoaded   bool
	serverTextServer    string
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

type scriptListsRequest struct {
	done  chan struct{}
	lists ScriptLists
	err   error
}

type scriptFetchJob struct {
	stype string
	key   string
	name  string
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

// ServerScriptContext is the server-side configuration snapshot consumed by
// the embedded GraalScript language server. The raw text stays here so the
// parser remains the single source of truth for the options/flags formats.
type ServerScriptContext struct {
	ServerName         string `json:"serverName"`
	ServerOptions      string `json:"serverOptions"`
	ServerFlags        string `json:"serverFlags"`
	ServerOptionsReady bool   `json:"serverOptionsReady"`
	ServerFlagsReady   bool   `json:"serverFlagsReady"`
}

// scriptTimeout is how long OpenScript/OpenNPC* waits for the NC server reply.
const scriptTimeout = 15 * time.Second

var errNCUnavailable = errors.New("NC is not connected")

const (
	rightsChangedMessage = "has set rights of"
	rightsLoadedMessage  = "loaded the rights of"
	banPlayersRightBit   = 11
)

// downloadTimeout is how long a file download waits for the full content. File
// transfers can be large and the server slow (30 MB over a sluggish link can
// take minutes; grclib streams "Received chunk" progress meanwhile), so this is
// far longer than scriptTimeout.
const downloadTimeout = 10 * time.Minute

// pendingKey builds the correlation key for a script/flags/attributes request.
func pendingKey(kind, idOrName string) string { return kind + ":" + idOrName }

// editorKey is the correlation key used by the player-editor callbacks. The
// server may change the account casing (or return the canonical account for a
// community alias), so the ordinary account key is case-insensitive.
func editorKey(kind, account string) string {
	return pendingKey(kind, strings.ToLower(strings.TrimSpace(account)))
}

func editorSelfKey(kind string) string { return kind + ":self" }

// selfRightsPendingKey is deliberately independent of the listserver login
// account. Some servers return the canonical in-game account from /openrights
// (for example, "Graal5766947") even when the RC/listserver credential is a
// different alias (for example, "Repinho").
const selfRightsPendingKey = "rights:self"

var errConnectionSessionChanged = fmt.Errorf("connection session changed: %w", context.Canceled)

type connectionOperation struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type pendingWait struct {
	done   chan struct{}
	reply  rclib.ScriptReply
	err    error
	closed bool
}

type fileWait struct {
	done    chan struct{}
	content []byte
	err     error
	closed  bool
}

// beginLifecycleOperation serializes Login, server selection and Logout. A
// newer lifecycle operation cancels the previous one and waits for it to stop
// before touching the native handle, so an old operation cannot disconnect or
// reconfigure a handle that a newer operation has already adopted.
func (s *Service) beginLifecycleOperation() *connectionOperation {
	for {
		s.lifecycleMu.Lock()
		previous := s.lifecycleOp
		if previous == nil {
			ctx, cancel := context.WithCancel(context.Background())
			operation := &connectionOperation{ctx: ctx, cancel: cancel, done: make(chan struct{})}
			s.lifecycleOp = operation
			s.lifecycleMu.Unlock()
			return operation
		}
		previous.cancel()
		done := previous.done
		s.lifecycleMu.Unlock()
		<-done
	}
}

func (s *Service) endLifecycleOperation(operation *connectionOperation) {
	s.lifecycleMu.Lock()
	if s.lifecycleOp == operation {
		s.lifecycleOp = nil
		close(operation.done)
	}
	s.lifecycleMu.Unlock()
}

func operationErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (w *pendingWait) finish(reply rclib.ScriptReply, err error) {
	if w.closed {
		return
	}
	w.reply = reply
	w.err = err
	w.closed = true
	close(w.done)
}

func (w *fileWait) finish(content []byte, err error) {
	if w.closed {
		return
	}
	w.content = content
	w.err = err
	w.closed = true
	close(w.done)
}

// registerPending installs a reply waiter before dispatching a request. A
// duplicate key joins the existing request instead of issuing a second packet:
// this protocol has no request ID, so a late reply for the first packet could
// otherwise resolve the second caller's waiter incorrectly.
func (s *Service) registerPending(key string) (*pendingWait, bool) {
	w := &pendingWait{done: make(chan struct{})}
	s.pendingMu.Lock()
	if s.pending == nil {
		s.pending = map[string]*pendingWait{}
	}
	if previous := s.pending[key]; previous != nil && !previous.closed {
		s.pendingMu.Unlock()
		return previous, false
	}
	s.pending[key] = w
	s.pendingMu.Unlock()
	return w, true
}

// editorWait is a fan-out reply slot: done is closed when reply lands, so every
// caller sharing the in-flight request observes it. closed guards against a
// double close when the same key is resolved more than once.
type editorWait struct {
	done   chan struct{}
	reply  any
	err    error
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
	resolved := false
	if ok {
		resolved = completeEditorWait(w, reply)
		delete(s.editor, key)
	}
	s.editorMu.Unlock()
	return resolved
}

func completeEditorWait(waiter *editorWait, reply any) bool {
	if waiter == nil || waiter.closed {
		return false
	}
	waiter.reply = reply
	waiter.err = nil
	waiter.closed = true
	close(waiter.done)
	return true
}

// resolveEditorForAccount mirrors the reference client's callback handling:
// the callback is authoritative and is accepted even when the returned
// account differs from the requested spelling. Exact normalized matches are
// preferred; a self request has a dedicated key because /open* commands let
// the server resolve the current account. If the server still returns an
// unexpected account, a single in-flight request of that editor kind is an
// unambiguous fallback. Multiple unrelated requests remain fail-closed.
func (s *Service) resolveEditorForAccount(kind, account string, reply any) bool {
	exactKey := editorKey(kind, account)
	selfKey := editorSelfKey(kind)
	prefix := kind + ":"

	s.editorMu.Lock()
	defer s.editorMu.Unlock()

	for _, key := range []string{exactKey, selfKey} {
		if waiter, ok := s.editor[key]; ok {
			if completeEditorWait(waiter, reply) {
				delete(s.editor, key)
				return true
			}
			delete(s.editor, key)
		}
	}

	var fallbackKey string
	for key, waiter := range s.editor {
		if !strings.HasPrefix(key, prefix) || key == exactKey || key == selfKey {
			continue
		}
		if waiter.closed {
			delete(s.editor, key)
			continue
		}
		if fallbackKey != "" {
			return false
		}
		fallbackKey = key
	}
	if fallbackKey == "" {
		return false
	}
	waiter := s.editor[fallbackKey]
	if !completeEditorWait(waiter, reply) {
		delete(s.editor, fallbackKey)
		return false
	}
	delete(s.editor, fallbackKey)
	return true
}

// cancelEditor removes a waiter only if it is still the request that registered
// it. This identity check prevents a late timeout from deleting a newer request
// reusing the same key.
func (s *Service) cancelEditor(key string, waiter *editorWait, err error) {
	s.editorMu.Lock()
	if current, ok := s.editor[key]; ok && current == waiter {
		if !waiter.closed {
			waiter.err = err
			waiter.closed = true
			close(waiter.done)
		}
		delete(s.editor, key)
	}
	s.editorMu.Unlock()
}

// dropEditor is kept for call sites that fail before they can await a waiter.
// The identity check is not needed there because the caller owns the key
// before dispatching a new request.
func (s *Service) dropEditor(key string) {
	s.editorMu.Lock()
	if waiter, ok := s.editor[key]; ok {
		delete(s.editor, key)
		if !waiter.closed {
			waiter.err = context.Canceled
			waiter.closed = true
			close(waiter.done)
		}
	}
	s.editorMu.Unlock()
}

// awaitEditor blocks on the wait's reply or the timeout, returning the typed
// reply. Only the owner (isNew) drops the key on timeout.
func (s *Service) awaitEditor(w *editorWait, key string, isNew bool, kind string) (any, error) {
	select {
	case <-w.done:
		if w.err != nil {
			return nil, w.err
		}
		return w.reply, nil
	case <-time.After(scriptTimeout):
		if isNew {
			s.cancelEditor(key, w, fmt.Errorf("%s request timed out", kind))
		}
		return nil, errors.New(kind + " request timed out")
	}
}

// resolvePending delivers a reply to the waiter for key (if any) and drops it.
// Called from pump-goroutine callbacks; closing the waiter is non-blocking.
func (s *Service) resolvePending(key string, reply rclib.ScriptReply) {
	s.pendingMu.Lock()
	w, ok := s.pending[key]
	if ok {
		delete(s.pending, key)
		w.finish(reply, nil)
	}
	s.pendingMu.Unlock()
}

// cancelPending removes a waiter only if it is still the request that
// registered it. A late timeout must not delete a newer request that reused the
// same key.
func (s *Service) cancelPending(key string, waiter *pendingWait, err error) {
	s.pendingMu.Lock()
	if current, ok := s.pending[key]; ok && current == waiter {
		delete(s.pending, key)
		waiter.finish(rclib.ScriptReply{}, err)
	}
	s.pendingMu.Unlock()
}

// registerFile installs a content waiter for a download keyed by remote path.
// A duplicate joins the existing request because the file callback carries no
// request ID with which to distinguish two packets for the same path. Called
// before FileBrowserDownload so the matching FileReceived callback resolves it.
func (s *Service) registerFile(path string) (*fileWait, bool) {
	w := &fileWait{done: make(chan struct{})}
	s.pendingFilesMu.Lock()
	if s.pendingFiles == nil {
		s.pendingFiles = map[string]*fileWait{}
	}
	if previous := s.pendingFiles[path]; previous != nil && !previous.closed {
		s.pendingFilesMu.Unlock()
		return previous, false
	}
	s.pendingFiles[path] = w
	s.pendingFilesMu.Unlock()
	return w, true
}

// resolveFile delivers downloaded content to the waiter for path (if any) and
// drops it. Called from the pump-goroutine FileReceived callback; non-blocking.
func (s *Service) resolveFile(path string, content []byte) {
	s.pendingFilesMu.Lock()
	matchedPath := path
	w, ok := s.pendingFiles[path]
	if !ok {
		for requestedPath, waiter := range s.pendingFiles {
			if fileBrowserPathsMatch(requestedPath, path) {
				matchedPath = requestedPath
				w = waiter
				ok = true
				break
			}
		}
	}
	if ok {
		delete(s.pendingFiles, matchedPath)
		w.finish(content, nil)
	}
	s.pendingFilesMu.Unlock()
}

// fileBrowserPathsMatch mirrors the native client's transfer correlation. The
// server may report a completed path with or without the current-folder
// prefix, while the request uses the path returned by the file listing.
func fileBrowserPathsMatch(requested, received string) bool {
	requested = normalizeFileBrowserTransferPath(requested)
	received = normalizeFileBrowserTransferPath(received)
	if requested == "" || received == "" {
		return requested == received
	}
	return requested == received ||
		strings.HasSuffix(received, "/"+requested) ||
		strings.HasSuffix(requested, "/"+received)
}

func normalizeFileBrowserTransferPath(path string) string {
	path = strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	return strings.Trim(path, "/")
}

func (s *Service) cancelFile(path string, waiter *fileWait, err error) {
	s.pendingFilesMu.Lock()
	if current, ok := s.pendingFiles[path]; ok && current == waiter {
		delete(s.pendingFiles, path)
		waiter.finish(nil, err)
	}
	s.pendingFilesMu.Unlock()
}

// cancelWaiters wakes every asynchronous request when the native session or
// its pump is no longer usable. This is deliberately separate from timeouts so
// Logout/server switching never leaves callers waiting for 15 minutes or ten
// minutes on a dead callback route.
func (s *Service) cancelWaiters(err error) {
	s.pendingMu.Lock()
	for key, waiter := range s.pending {
		delete(s.pending, key)
		waiter.finish(rclib.ScriptReply{}, err)
	}
	s.pendingMu.Unlock()

	s.editorMu.Lock()
	for key, waiter := range s.editor {
		delete(s.editor, key)
		if !waiter.closed {
			waiter.err = err
			waiter.closed = true
			close(waiter.done)
		}
	}
	s.editorMu.Unlock()

	s.pendingFilesMu.Lock()
	for path, waiter := range s.pendingFiles {
		delete(s.pendingFiles, path)
		waiter.finish(nil, err)
	}
	s.pendingFilesMu.Unlock()
}

// channelState tracks one IRC channel's join state plus a pending (debounced)
// leave deadline. leaveAt is the zero time when no leave is pending.
type channelState struct {
	joined  bool
	leaveAt time.Time
}

// ChatLine is an RC Chat message captured by the active session.
type ChatLine struct {
	Text      string
	Timestamp time.Time
}

// channelLeaveCooldown is how long a PART waits before it actually removes the
// channel, giving a rejoin (JOIN) time to cancel it.
const channelLeaveCooldown = 600 * time.Millisecond

// NewService returns an empty service.
func NewService() *Service { return &Service{} }

// ChatHistory returns the most recent RC chat lines with capture timestamps.
// The returned slice is a copy and is safe for callers to retain.
func (s *Service) ChatHistory(limit int) []ChatLine {
	if limit <= 0 || limit > 50 {
		limit = 50
	}
	s.chatMu.RLock()
	defer s.chatMu.RUnlock()
	start := len(s.chatHistory) - limit
	if start < 0 {
		start = 0
	}
	return append([]ChatLine(nil), s.chatHistory[start:]...)
}

func (s *Service) appendChatHistory(text string) {
	s.chatMu.Lock()
	s.chatHistory = append(s.chatHistory, ChatLine{Text: text, Timestamp: time.Now().UTC()})
	if len(s.chatHistory) > 1000 {
		s.chatHistory = s.chatHistory[len(s.chatHistory)-1000:]
	}
	s.chatMu.Unlock()
}

func (s *Service) clearChatHistory() {
	s.chatMu.Lock()
	s.chatHistory = nil
	s.chatMu.Unlock()
}

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
func (s *Service) SetEmitter(fn func(name string, data ...any)) {
	s.emitMu.Lock()
	s.emit = fn
	s.emitMu.Unlock()
}

// emitEvent is a nil-safe helper for the pump-goroutine callbacks.
func (s *Service) emitEvent(name string, data ...any) {
	s.emitMu.RLock()
	emit := s.emit
	s.emitMu.RUnlock()
	if emit != nil {
		emit(name, data...)
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
// the canonical account is learned from the authenticated rights response and
// the optional community name from player properties.
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

// editorTarget identifies requests that should use the server-side /open*
// command for the current session. The command is preferable for self because
// the server knows the canonical account even when the login credential is an
// alias or has different casing.
func (s *Service) editorTarget(account string) (target string, selfRequest bool, err error) {
	account = strings.TrimSpace(account)
	selfRequest = account == "" || s.isSelfRightsAlias(account)
	target = account
	if selfRequest {
		target = strings.TrimSpace(s.SelfAccount())
	}
	if target == "" {
		return "", selfRequest, errors.New("account is required")
	}
	return target, selfRequest, nil
}

// captureServerCommunityName stores the community name from the native player
// property stream. An empty value is meaningful when a server clears the
// property, so the caller may intentionally pass an empty string.
func (s *Service) captureServerCommunityName(community string) {
	community = strings.TrimSpace(community)
	s.rightsMu.Lock()
	changed := s.selfRightsCommunityName != community
	if changed {
		s.selfRightsCommunityName = community
	}
	s.rightsMu.Unlock()
	if changed {
		log.Printf("[identity] community name received from player properties community=%q", community)
		s.emitEvent("rc:scriptIdentityChanged")
	}
}

// rememberPlayerProperty keeps the account/community values decoded by
// grclib. PLO_ADDPLAYER emits the individual property callbacks before its
// player-joined callback, so the small per-session map lets the authenticated
// account resolve the local player id and its community name later.
func (s *Service) rememberPlayerProperty(playerID int, prop, value string) {
	if playerID < 0 {
		return
	}
	prop = strings.ToLower(strings.TrimSpace(prop))
	value = strings.TrimSpace(value)
	if prop != "account" && prop != "accountname" && prop != "community" && prop != "communityname" {
		return
	}

	s.playerIdentityMu.Lock()
	if s.playerAccounts == nil {
		s.playerAccounts = make(map[int]string)
	}
	if s.playerCommunities == nil {
		s.playerCommunities = make(map[int]string)
	}
	switch prop {
	case "account", "accountname":
		if value == "" {
			delete(s.playerAccounts, playerID)
		} else {
			s.playerAccounts[playerID] = value
		}
	case "community", "communityname":
		s.playerCommunities[playerID] = value
	}
	s.playerIdentityMu.Unlock()

	s.resolveSelfPlayerIdentity()
}

func (s *Service) rememberPlayerJoined(account string, playerID int) {
	s.rememberPlayerProperty(playerID, "account", account)
}

func (s *Service) forgetPlayer(playerID int) {
	s.playerIdentityMu.Lock()
	delete(s.playerAccounts, playerID)
	delete(s.playerCommunities, playerID)
	if s.selfPlayerID == playerID {
		s.selfPlayerID = 0
	}
	s.playerIdentityMu.Unlock()
}

// resolveSelfPlayerIdentity associates the authenticated account with the
// native player-property stream and applies its community name if present.
func (s *Service) resolveSelfPlayerIdentity() {
	canonical := strings.TrimSpace(s.SelfAccount())
	aliases := s.selfRightsAliases()
	if canonical == "" && len(aliases) == 0 {
		return
	}

	s.playerIdentityMu.Lock()
	if s.selfPlayerID == 0 {
		for playerID, account := range s.playerAccounts {
			if equalFoldAny(account, canonical) || equalFoldAny(account, aliases...) {
				s.selfPlayerID = playerID
				break
			}
		}
	}
	playerID := s.selfPlayerID
	community, hasCommunity := s.playerCommunities[playerID]
	s.playerIdentityMu.Unlock()

	if playerID != 0 && hasCommunity {
		s.captureServerCommunityName(community)
	}
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
		s.resolveSelfPlayerIdentity()
		s.emitEvent("rc:scriptIdentityChanged")
	}
}

func (s *Service) emitScriptPermissionsEvent(permissionChanged bool) {
	s.emitEvent("rc:scriptPermissionsChanged", permissionChanged)
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
// reports one (mirroring the reference client's pump loop), and starts the
// lightweight NC heartbeat for the lifetime of this session.
func (s *Service) startPump(h rclib.Handle) {
	s.stopPump()
	s.resetNCConnectionState()
	s.resetWeaponListState()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.pumpMu.Lock()
	s.pumpCancel = cancel
	s.pumpDone = done
	s.pumpErr = nil
	s.pumpMu.Unlock()
	s.startNCHeartbeat(h)
	go func() {
		defer func() {
			s.finishPump(done)
			close(done)
		}()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.pumpTick(h); err != nil {
					s.failPump(h, done, err)
					return
				}
			}
		}
	}()
}

// pumpTick contains one complete tick behind a recovery boundary. A panic or
// native error is terminal for this handle: continuing to call into a possibly
// corrupted native connection is less safe than stopping it. The failure is
// surfaced to the UI and all waiters are canceled; recovery is explicit through
// a later Login/ConnectToServer operation, never an implicit reconnect.
func (s *Service) pumpTick(h rclib.Handle) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("event pump panicked: %v", recovered)
			log.Printf("[pump] %v\n%s", err, debug.Stack())
		}
	}()
	if err := rclib.ProcessEvents(h); err != nil {
		return err
	}
	s.maybeConnectNC(h)
	s.settleChannelLeaves()
	return nil
}

func (s *Service) finishPump(done chan struct{}) {
	s.pumpMu.Lock()
	if s.pumpDone == done {
		s.pumpCancel = nil
		s.pumpDone = nil
	}
	s.pumpMu.Unlock()
}

func (s *Service) failPump(h rclib.Handle, done chan struct{}, err error) {
	s.pumpMu.Lock()
	active := s.pumpDone == done
	s.pumpMu.Unlock()
	if !active {
		return
	}

	s.stopNCHeartbeat()
	pumpErr := fmt.Errorf("event pump stopped: %w", err)
	s.mu.Lock()
	if s.handle == h {
		s.serverName = ""
		s.serverEpoch++
		s.ncConnectionAttempted = true
		s.nextNCConnectAttempt = time.Time{}
		s.ncConnectionWasUp = false
		s.ncAutomaticReconnectAttempted = false
		s.channels = nil
		s.maxUpload = 0
	}
	s.mu.Unlock()
	s.clearSelfFolderRights()
	s.clearServerTextCache()
	s.resetWeaponListState()
	s.pumpMu.Lock()
	if s.pumpDone == done {
		s.pumpErr = pumpErr
	}
	s.pumpMu.Unlock()
	log.Printf("[pump] handle=%#x %v", uintptr(h), pumpErr)
	s.cancelWaiters(pumpErr)
	// Do not reconnect or disconnect here. The native handle may be in the
	// middle of a fault path; the user-visible error lets the next explicit
	// lifecycle operation perform orderly cleanup.
	func() {
		defer func() { _ = recover() }()
		s.emitEvent("rc:pumpError", pumpErr.Error())
	}()
}

// ncConnectionTimeout bounds the wait for the asynchronous native NC
// handshake and the location-refresh round trip used by an explicit
// reconnect.
const ncConnectionTimeout = 15 * time.Second

const (
	// Control-NPC is present on every server and GetNPCFlags is a small,
	// read-only NC round trip. It keeps the NC socket exercised without
	// rebuilding any of the large weapon/class/NPC lists.
	ncHeartbeatNPCID    = 10000
	ncHeartbeatInterval = 3 * time.Minute
	ncHeartbeatTimeout  = 15 * time.Second
)

const ncStatusPollInterval = 100 * time.Millisecond

const ncReconnectLocationDelay = 500 * time.Millisecond

// ncFetchConcurrency bounds the number of in-flight OpenScript requests during
// a bulk fetch. Sixteen requests is the highest-throughput setting validated
// against the reference NC server; lower values can interact badly with its
// asynchronous response queue and take several minutes for the same snapshot.
// The NC send path remains serialized by ncRequestMu/dllMu.
const ncFetchConcurrency = 16

// maybeConnectNC opens the NC socket when the server exposes one to this
// account (HasNCServer). It performs the initial attempt once, and permits one
// automatic retry after observing a connected NC socket go down. Both the
// automatic retry and the explicit reconnect refresh the NPC-server location
// before the native connection attempt.
func (s *Service) maybeConnectNC(h rclib.Handle) {
	s.ncConnectMu.Lock()
	defer s.ncConnectMu.Unlock()

	if rclib.IsNCConnected(h) {
		s.markNCConnectionObserved()
		return
	}
	if s.claimNCAutomaticReconnect() {
		log.Printf("[nc] automatic reconnect requested after disconnect")
		if err := s.tryConnectNCLocked(h, true); err != nil {
			log.Printf("[nc] automatic reconnect failed: %v", err)
		}
		return
	}
	if err := s.tryConnectNCLocked(h, false); err != nil {
		log.Printf("[nc] connect failed: %v", err)
	}
}

func (s *Service) markNCConnectionObserved() {
	s.mu.Lock()
	if s.ncAutomaticReconnectAttempted && !s.ncConnectionWasUp {
		// A reconnect completed after the previous drop. The next drop gets its
		// own single retry, while a failed retry remains latched below.
		s.ncAutomaticReconnectAttempted = false
	}
	s.ncConnectionWasUp = true
	s.mu.Unlock()
}

// claimNCAutomaticReconnect allows exactly one reconnect after the pump has
// observed a live NC socket and then seen it go down. A pending manual/scheduled
// reconnect always wins over this background retry.
func (s *Service) claimNCAutomaticReconnect() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ncManuallyDisconnected || !s.nextNCConnectAttempt.IsZero() ||
		!s.ncConnectionAttempted || !s.ncConnectionWasUp || s.ncAutomaticReconnectAttempted {
		return false
	}
	s.ncAutomaticReconnectAttempted = true
	return true
}

// claimNCAttempt applies the reference client's initial-attempt latch and its
// scheduled reconnect state. force is used by an explicit reconnect or by the
// single automatic retry after an observed NC drop.
func (s *Service) claimNCAttempt(force bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if force {
		s.ncManuallyDisconnected = false
		s.ncConnectionAttempted = true
		s.nextNCConnectAttempt = time.Time{}
		return true
	}
	if s.ncManuallyDisconnected {
		return false
	}
	now := time.Now()
	automaticAttempt := s.nextNCConnectAttempt.IsZero() && !s.ncConnectionAttempted
	scheduledAttempt := !s.nextNCConnectAttempt.IsZero() && !now.Before(s.nextNCConnectAttempt)
	if !automaticAttempt && !scheduledAttempt {
		return false
	}
	s.ncConnectionAttempted = true
	s.nextNCConnectAttempt = time.Time{}
	return true
}

// resetNCConnectionState starts the NC lifecycle for a new main-server
// session, matching TRemoteFrame::setConnection in the reference client.
func (s *Service) resetNCConnectionState() {
	s.ncConnectMu.Lock()
	defer s.ncConnectMu.Unlock()
	s.mu.Lock()
	s.ncConnectionAttempted = false
	s.ncManuallyDisconnected = false
	s.nextNCConnectAttempt = time.Time{}
	s.ncConnectionWasUp = false
	s.ncAutomaticReconnectAttempted = false
	s.mu.Unlock()
}

// tryConnectNC performs one serialized NC lifecycle action. The pump uses the
// one-shot/scheduled path; force refreshes the NPC-server endpoint before
// scheduling the native connection.
func (s *Service) tryConnectNC(h rclib.Handle, force bool) error {
	s.ncConnectMu.Lock()
	defer s.ncConnectMu.Unlock()
	return s.tryConnectNCLocked(h, force)
}

func (s *Service) tryConnectNCLocked(h rclib.Handle, force bool) error {
	if !rclib.IsConnected(h) || !rclib.IsAuthenticated(h) {
		return nil
	}
	if !rclib.HasNCServer(h) {
		return nil
	}
	if !force && rclib.IsNCConnected(h) {
		return nil
	}
	if !s.claimNCAttempt(force) {
		return nil
	}
	if force {
		// The current NC session is being intentionally replaced. Do not count
		// the old connection as the new session's observation if this reconnect
		// fails; otherwise the same pump tick could schedule a second retry.
		s.mu.Lock()
		s.ncConnectionWasUp = false
		s.mu.Unlock()
	}
	if force && rclib.IsNCConnected(h) {
		// The C++ client's explicit /nc rc path always closes an existing NC
		// socket before opening the replacement one.
		if err := rclib.DisconnectNC(h); err != nil {
			log.Printf("[nc] disconnect before reconnect failed: %v", err)
		}
	}
	if force {
		// The native library updates its cached NC endpoint when the main socket
		// receives the NPCSERVERADDR reply. Give that request a short head start
		// before rc_connect_to_nc_server reads the endpoint.
		s.resetWeaponListState()
		s.scheduleNCReconnectLocked(h)
		return nil
	}

	// A reconnect starts a new native list-cache lifecycle. The old borrowed
	// weapon pointers must never be used as the snapshot for this socket.
	s.resetWeaponListState()
	err := rclib.ConnectToNCServer(h)
	if err == nil {
		log.Printf("[nc] connect requested")
	}
	return err
}

// scheduleNCReconnectLocked mirrors TRemoteFrame::reconnectNPCServer. The
// caller holds ncConnectMu. It first schedules a fallback connection using
// the endpoint already cached by grclib, then asks the main RC socket for the
// live (npcserver) location. When that request succeeds, the native endpoint
// refresh is given 500 ms to arrive before the pump calls
// rc_connect_to_nc_server.
func (s *Service) scheduleNCReconnectLocked(h rclib.Handle) {
	s.mu.Lock()
	epoch := s.serverEpoch
	s.nextNCConnectAttempt = time.Now()
	s.mu.Unlock()

	players, err := rclib.GetPlayers(h)
	if err != nil {
		log.Printf("[nc] get players for endpoint refresh failed: %v", err)
		return
	}
	npcServerID := -1
	for _, player := range players {
		if strings.EqualFold(player.Account, "(npcserver)") {
			npcServerID = player.ID
			break
		}
	}
	if npcServerID < 0 {
		return
	}

	query, err := npcServerLocationQuery(npcServerID)
	if err != nil {
		log.Printf("[nc] invalid NPC-server player ID %d: %v", npcServerID, err)
		return
	}
	if err := rclib.SendRawPacket(h, rclib.PacketNPCServerQuery, query); err != nil {
		log.Printf("[nc] NPC-server location query failed: %v", err)
		return
	}

	s.mu.Lock()
	if s.handle == h && s.serverEpoch == epoch && !s.ncManuallyDisconnected {
		s.nextNCConnectAttempt = time.Now().Add(ncReconnectLocationDelay)
	}
	s.mu.Unlock()
}

// npcServerLocationQuery uses the same two 7-bit player-id bytes and literal
// command as the reference C++ client.
func npcServerLocationQuery(playerID int) ([]byte, error) {
	if playerID < 0 || playerID > 0x3fff {
		return nil, fmt.Errorf("player ID %d is outside the 14-bit protocol range", playerID)
	}
	return []byte{
		byte((playerID >> 7) + 32),
		byte((playerID & 127) + 32),
		'l', 'o', 'c', 'a', 't', 'i', 'o', 'n',
	}, nil
}

// startNCHeartbeat starts one cancellable heartbeat loop for the current
// main-server session. It is deliberately independent from the event pump:
// the pump must remain free to process the NPC flags callback that completes
// each heartbeat request.
func (s *Service) startNCHeartbeat(h rclib.Handle) {
	s.stopNCHeartbeat()

	s.mu.Lock()
	epoch := s.serverEpoch
	s.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.ncHeartbeatMu.Lock()
	s.ncHeartbeatCancel = cancel
	s.ncHeartbeatDone = done
	s.ncHeartbeatMu.Unlock()

	go func() {
		defer close(done)
		s.runNCHeartbeat(ctx, h, epoch)
	}()
}

// stopNCHeartbeat cancels the heartbeat and waits for its goroutine to leave
// before the handle can be reused by another server/session.
func (s *Service) stopNCHeartbeat() {
	s.ncHeartbeatMu.Lock()
	cancel := s.ncHeartbeatCancel
	done := s.ncHeartbeatDone
	s.ncHeartbeatCancel = nil
	s.ncHeartbeatDone = nil
	s.ncHeartbeatMu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *Service) runNCHeartbeat(ctx context.Context, h rclib.Handle, epoch uint64) {
	ticker := time.NewTicker(ncHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.isCurrentNCHeartbeatSession(h, epoch) {
				return
			}
			if err := s.sendNCHeartbeat(ctx, h, epoch); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("[nc heartbeat] Control-NPC flags failed: %v", err)
			} else if err == nil {
				log.Printf("[nc heartbeat] Control-NPC flags ok")
			}
		}
	}
}

func (s *Service) isCurrentNCHeartbeatSession(h rclib.Handle, epoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handle == h && s.serverEpoch == epoch
}

// sendNCHeartbeat sends one deduplicated GetNPCFlags request and waits for the
// matching callback. It never reconnects NC; reconnect policy remains owned by
// the pump, while this loop only prevents an otherwise idle socket from going
// stale and provides a bounded failure signal.
func (s *Service) sendNCHeartbeat(ctx context.Context, h rclib.Handle, epoch uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.isCurrentNCHeartbeatSession(h, epoch) {
		return context.Canceled
	}
	if !rclib.IsConnected(h) || !rclib.IsAuthenticated(h) || !rclib.HasNCServer(h) {
		return nil
	}
	if !rclib.IsNCConnected(h) || !rclib.IsNCAuthenticated(h) {
		return nil
	}

	key := pendingKey("npcflags", strconv.Itoa(ncHeartbeatNPCID))
	waiter, isNew := s.registerPending(key)
	if isNew {
		if err := ctx.Err(); err != nil {
			s.cancelPending(key, waiter, err)
			return err
		}
		s.ncRequestMu.Lock()
		err := rclib.GetNPCFlags(h, ncHeartbeatNPCID)
		s.ncRequestMu.Unlock()
		if err != nil {
			s.cancelPending(key, waiter, err)
			return err
		}
	}

	timer := time.NewTimer(ncHeartbeatTimeout)
	defer timer.Stop()
	select {
	case <-waiter.done:
		return waiter.err
	case <-ctx.Done():
		if isNew {
			s.cancelPending(key, waiter, ctx.Err())
		}
		return ctx.Err()
	case <-timer.C:
		if isNew {
			s.cancelPending(key, waiter, fmt.Errorf("timeout after %s", ncHeartbeatTimeout))
		}
		return fmt.Errorf("timeout after %s", ncHeartbeatTimeout)
	}
}

// stopPump stops the active event pump, if any.
func (s *Service) stopPump() {
	s.stopNCHeartbeat()
	s.pumpMu.Lock()
	cancel := s.pumpCancel
	done := s.pumpDone
	s.pumpCancel = nil
	s.pumpDone = nil
	s.pumpMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// hasHandle reports whether a listserver connection is currently held.
func (s *Service) hasHandle() bool {
	s.mu.Lock()
	hasHandle := s.handle != 0
	s.mu.Unlock()
	return hasHandle
}

func (s *Service) clearPumpError() {
	s.pumpMu.Lock()
	s.pumpErr = nil
	s.pumpMu.Unlock()
}

// unregisterCallbacks is cleanup: preserve the primary operation error, but
// make a failed native detach visible in logs instead of leaving that failure
// indistinguishable from a clean session transition.
func unregisterCallbacks(h rclib.Handle) {
	if h == 0 {
		return
	}
	if err := rclib.UnregisterCallbacks(h); err != nil {
		log.Printf("[connection] unregister callbacks for handle %#x: %v", uintptr(h), err)
	}
}

// Login connects to the listserver with the given credentials and keeps the
// resulting handle. A previous handle is dropped first. Returns the server
// list produced by the listserver login.
func (s *Service) Login(creds Credentials) ([]rclib.Server, error) {
	operation := s.beginLifecycleOperation()
	defer s.endLifecycleOperation(operation)
	return s.login(operation.ctx, creds)
}

func (s *Service) login(ctx context.Context, creds Credentials) ([]rclib.Server, error) {
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
	if err := operationErr(ctx); err != nil {
		rclib.Disconnect(h)
		return nil, err
	}

	servers, err := rclib.GetServers(h)
	lastErr := rclib.LastError(h)
	if operationErr(ctx) != nil {
		rclib.Disconnect(h)
		return nil, ctx.Err()
	}
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

	if err := operationErr(ctx); err != nil {
		rclib.Disconnect(h)
		return nil, err
	}
	s.stopPump()
	s.mu.Lock()
	previous := s.handle
	s.handle = h
	s.creds = creds
	s.serverEpoch++
	s.channels = nil
	s.maxUpload = 0
	s.mu.Unlock()
	s.cancelWaiters(errConnectionSessionChanged)
	s.clearPumpError()
	if previous != 0 {
		unregisterCallbacks(previous)
		rclib.Disconnect(previous)
	}
	s.clearSelfFolderRights()
	s.clearServerTextCache()

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
	operation := s.beginLifecycleOperation()
	defer s.endLifecycleOperation(operation)
	return s.connectToServer(operation.ctx, index)
}

func (s *Service) connectToServer(ctx context.Context, index int) error {
	if err := operationErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	h := s.handle
	nickname := s.creds.Nickname
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	s.stopPump()
	s.cancelWaiters(errConnectionSessionChanged)
	s.clearSelfFolderRights()
	s.clearServerTextCache()
	s.mu.Lock()
	s.serverEpoch++
	epoch := s.serverEpoch
	s.serverName = ""
	s.maxUpload = 0
	s.channels = nil
	s.mu.Unlock()
	// The native handle is reused when switching servers, so invalidate the
	// frontend's file-browser context before any new server data can arrive.
	s.emitEvent("rc:fbReset")
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
	s.resetWeaponListState()
	if err := operationErr(ctx); err != nil {
		return err
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
	handleDisconnected := func(callbackHandle rclib.Handle, reason string) {
		s.mu.Lock()
		current := s.handle == h && s.serverEpoch == epoch
		s.mu.Unlock()
		if callbackHandle != h || !current {
			log.Printf("[connection] ignoring stale disconnect callback handle=%#x expected=%#x epoch=%d", uintptr(callbackHandle), uintptr(h), epoch)
			return
		}
		select {
		case disconnected <- reason:
		default:
		}
		s.mu.Lock()
		s.serverName = ""
		s.ncConnectionAttempted = true
		s.nextNCConnectAttempt = time.Time{}
		s.ncConnectionWasUp = false
		s.ncAutomaticReconnectAttempted = false
		s.mu.Unlock()
		s.clearSelfFolderRights()
		s.resetWeaponListState()
		s.clearChatHistory()
		s.clearServerTextCacheIfCurrent(epoch)
		s.emitEvent("rc:disconnected", reason)
		s.emitEvent("rc:fbReset")
		s.emitEvent("rc:channels", s.resetChannels())
	}
	if err := rclib.RegisterCallbacks(h, &rclib.EventCallbacks{
		Connected: func() {
			select {
			case connected <- struct{}{}:
			default:
			}
			s.emitEvent("rc:connected")
		},
		Disconnected: func(reason string) {
			// Legacy native fallback does not include the source handle.
			handleDisconnected(h, reason)
		},
		DisconnectedEx: func(callbackHandle rclib.Handle, reason string) {
			handleDisconnected(callbackHandle, reason)
		},
		PlayerJoined: func(account string, playerID int) {
			s.rememberPlayerJoined(account, playerID)
		},
		PlayerLeft: func(_ string, playerID int) {
			s.forgetPlayer(playerID)
		},
		PlayerPropChanged: func(playerID int, prop, value string) {
			s.rememberPlayerProperty(playerID, prop, value)
		},
		Message: func(text string) {
			s.appendChatHistory(text)
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
				s.cacheServerText(epoch, serverName, dataType, content)
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
			s.emitEvent("rc:scriptReceived", scriptType, name, id, script)
		},
		WeaponChanged: func(name string) { s.emitEvent("rc:weaponsChanged", name) },
		WeaponListReceived: func(count int) {
			s.markWeaponListReceived(count)
			s.emitEvent("rc:weaponsChanged", count)
		},
		ClassChanged: func(name string) { s.emitEvent("rc:classesChanged", name) },
		NPCChanged:   func(id int) { s.emitEvent("rc:npcsChanged", id) },
		NPCFlags: func(id int, flags string) {
			s.resolvePending(pendingKey("npcflags", strconv.Itoa(id)), rclib.ScriptReply{Type: "npcflags", ID: id, Script: flags})
			s.emitEvent("rc:npcFlags", id, flags)
		},
		NPCAttributes: func(id int, attrs string) {
			s.resolvePending(pendingKey("npcattr", strconv.Itoa(id)), rclib.ScriptReply{Type: "npcattr", ID: id, Script: attrs})
			s.emitEvent("rc:npcAttributes", id, attrs)
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
			resolved := s.resolveEditorForAccount("rights", account, data)
			if resolved {
				log.Printf("[rights callback] resolved pending request account=%q", account)
			} else {
				log.Printf("[rights callback] no pending waiter for account=%q", account)
			}
			s.emitEvent("rc:playerRights", account, rights, ipRange, folderAccess)
		},
		PlayerAttributes: func(account, propertiesJSON, editorText string) {
			log.Printf("[editor-cb] player_attributes account=%q editorLen=%d", account, len(editorText))
			resolved := s.resolveEditorForAccount("attrs", account, AttrsData{
				Account: account, PropertiesJSON: propertiesJSON, EditorText: editorText,
			})
			if !resolved {
				log.Printf("[editor-cb] no pending attrs waiter for account=%q", account)
			}
			s.emitEvent("rc:playerAttributes", account, propertiesJSON, editorText)
		},
		BanData: func(account, computerID, details string) {
			log.Printf("[editor-cb] ban_data account=%q", account)
			resolved := s.resolveEditorForAccount("ban", account, BanData{
				Account: account, ComputerID: computerID, Details: details,
			})
			if !resolved {
				log.Printf("[editor-cb] no pending ban waiter for account=%q", account)
			}
			s.emitEvent("rc:banData", account, computerID, details)
		},
		BanListData: func(dataType, account, content string) {
			log.Printf("[editor-cb] ban_list_data type=%q account=%q len=%d", dataType, account, len(content))
			switch dataType {
			case "bantypes":
				// Global list (no account correlation): cache for GetBanTypes.
				s.editorMu.Lock()
				s.banTypes = content
				s.editorMu.Unlock()
				s.resolveEditor(editorKey("bantypes", ""), content)
			case "banhistory":
				if !s.resolveEditorForAccount("banhistory", account, content) {
					log.Printf("[editor-cb] no pending banhistory waiter for account=%q", account)
				}
			case "staffactivity":
				if !s.resolveEditorForAccount("staffactivity", account, content) {
					log.Printf("[editor-cb] no pending staffactivity waiter for account=%q", account)
				}
			default:
				log.Printf("[banlistdata] %s %s: %q", dataType, account, content)
			}
			s.emitEvent("rc:banListData", dataType, account, content)
		},
		PlayerTextData: func(dataType, account, content string) {
			log.Printf("[editor-cb] player_text_data type=%q account=%q len=%d", dataType, account, len(content))
			switch dataType {
			case "comments":
				// Empty comments are a valid response. The native C++ client
				// still opens its editor with an empty buffer, so correlation must
				// never depend on content being non-empty.
				if !s.resolveEditorForAccount("comments", account, CommentsData{Account: account, Content: content}) {
					log.Printf("[editor-cb] no pending comments waiter for account=%q", account)
				}
			default:
				log.Printf("[playertextdata] %s %s: %q", dataType, account, content)
			}
			s.emitEvent("rc:playerTextData", dataType, account, content)
		},
	}); err != nil {
		s.cancelWaiters(errConnectionSessionChanged)
		unregisterCallbacks(h)
		return fmt.Errorf("register connection callbacks: %w", err)
	}
	s.startPump(h)

	// Kick off the server login; the result arrives asynchronously via events.
	if err := operationErr(ctx); err != nil {
		s.stopPump()
		unregisterCallbacks(h)
		return err
	}
	if err := rclib.ConnectToServer(h, index); err != nil {
		s.stopPump()
		unregisterCallbacks(h)
		s.cancelWaiters(errConnectionSessionChanged)
		return err
	}

	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	select {
	case <-connected:
		if err := operationErr(ctx); err != nil {
			s.stopPump()
			unregisterCallbacks(h)
			s.cancelWaiters(errConnectionSessionChanged)
			return err
		}
		// The server does not adopt our nickname until we send it (mirrors the
		// reference client, which calls rc_set_nickname in its onConnected).
		if nickname != "" {
			if err := rclib.SetNickname(h, nickname); err != nil {
				log.Printf("set nickname %q: %v", nickname, err)
			}
		}
		if err := operationErr(ctx); err != nil {
			s.stopPump()
			unregisterCallbacks(h)
			s.cancelWaiters(errConnectionSessionChanged)
			return err
		}
		// Match the reference Remote Control client: announce the client build
		// date through the RC chat packet immediately after authentication.
		if err := rclib.Execute(h, "/npc newrc,"+remoteControlBuildDate); err != nil {
			log.Printf("announce Remote Control build date %q: %v", remoteControlBuildDate, err)
		}
		if err := operationErr(ctx); err != nil {
			s.stopPump()
			unregisterCallbacks(h)
			s.cancelWaiters(errConnectionSessionChanged)
			return err
		}
		s.mu.Lock()
		s.serverName = serverName
		s.mu.Unlock()
		// Warm the current server's openrights snapshot after the authenticated
		// connection has been handed back to the caller. The request is a
		// callback-based round trip and can take up to scriptTimeout; keeping it
		// on this path leaves the frontend in "Connecting" even though the RC
		// socket is already authenticated. Sync and permission-gated operations
		// remain fail-closed until this background refresh completes.
		go func(expectedEpoch uint64) {
			if err := s.ensureSelfFolderRightsForEpoch(expectedEpoch); err != nil && !errors.Is(err, context.Canceled) {
				log.Printf("[rights] login refresh failed: %v", err)
			}
		}(epoch)
		go s.refreshServerTextCache(h, epoch, serverName)
		return nil
	case reason := <-disconnected:
		s.stopPump()
		unregisterCallbacks(h)
		s.cancelWaiters(errConnectionSessionChanged)
		s.mu.Lock()
		s.serverName = ""
		s.mu.Unlock()
		if reason == "" {
			reason = "disconnected by server"
		}
		return errors.New(reason)
	case <-timeout.C:
		s.stopPump()
		unregisterCallbacks(h)
		s.cancelWaiters(errConnectionSessionChanged)
		s.mu.Lock()
		s.serverName = ""
		s.mu.Unlock()
		return errors.New("server connection timed out")
	case <-ctx.Done():
		s.stopPump()
		unregisterCallbacks(h)
		s.cancelWaiters(errConnectionSessionChanged)
		return ctx.Err()
	}
}

// SetNewProtocol toggles newer-protocol compatibility before server login.
func (s *Service) SetNewProtocol(enable bool) error {
	operation := s.beginLifecycleOperation()
	defer s.endLifecycleOperation(operation)
	if err := operationErr(operation.ctx); err != nil {
		return err
	}
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	if err := rclib.SetNewProtocol(h, enable); err != nil {
		return err
	}
	return operationErr(operation.ctx)
}

// ConnectToNCServer explicitly refreshes the NPC-server location and schedules
// the NC (script) socket connection.
func (s *Service) ConnectToNCServer() error {
	operation := s.beginLifecycleOperation()
	defer s.endLifecycleOperation(operation)
	return s.connectToNC(operation.ctx)
}

func (s *Service) connectToNC(ctx context.Context) error {
	if err := operationErr(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	if !rclib.IsConnected(h) || !rclib.IsAuthenticated(h) {
		return errors.New("main server is not authenticated")
	}
	if !rclib.HasNCServer(h) {
		return errors.New("this account has no NC rights on this server")
	}
	if err := s.tryConnectNC(h, true); err != nil {
		return err
	}
	if err := operationErr(ctx); err != nil {
		// A newer lifecycle operation canceled this attempt after the native
		// scheduling completed. Do not leave a delayed reconnect from the
		// canceled operation attached to the session it no longer owns.
		s.ncConnectMu.Lock()
		s.mu.Lock()
		if s.handle == h {
			s.nextNCConnectAttempt = time.Time{}
			s.ncConnectionWasUp = false
		}
		s.mu.Unlock()
		disconnectErr := rclib.DisconnectNC(h)
		s.ncConnectMu.Unlock()
		if disconnectErr != nil {
			log.Printf("[connection] cleanup canceled NC connect: %v", disconnectErr)
		}
		return err
	}
	return nil
}

// EnsureNCConnected waits for the NC socket to be connected and authenticated.
// It may trigger the initial automatic attempt when the pump has not done so
// yet; the pump owns the separate one-shot retry after an observed NC drop.
func (s *Service) EnsureNCConnected(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	waitCtx, cancel := context.WithTimeout(ctx, ncConnectionTimeout)
	defer cancel()

	attempted := false
	started := time.Now()
	for {
		h, err := s.requireHandle()
		if err != nil {
			return err
		}
		if !rclib.IsConnected(h) || !rclib.IsAuthenticated(h) {
			return errors.New("main server is not authenticated")
		}
		if !rclib.HasNCServer(h) {
			return errors.New("this account has no NC rights on this server")
		}
		if rclib.IsNCConnected(h) && rclib.IsNCAuthenticated(h) {
			if attempted {
				log.Printf("[nc] connection restored after %s", time.Since(started))
			}
			return nil
		}

		s.mu.Lock()
		manuallyDisconnected := s.ncManuallyDisconnected
		s.mu.Unlock()
		if manuallyDisconnected {
			return errors.New("NC server is manually disconnected")
		}
		if !attempted {
			attempted = true
			if err := s.tryConnectNC(h, false); err != nil {
				return fmt.Errorf("NC connect failed: %w", err)
			}
		}

		timer := time.NewTimer(ncStatusPollInterval)
		select {
		case <-waitCtx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("NC connection timed out: %w", waitCtx.Err())
		case <-timer.C:
		}
	}
}

// DisconnectNC closes the NC socket and prevents the pump from issuing the
// reference client's automatic connection attempt for the current session.
// An explicit ConnectToNCServer or a new main-server session re-enables it.
func (s *Service) DisconnectNC() error {
	operation := s.beginLifecycleOperation()
	defer s.endLifecycleOperation(operation)
	if err := operationErr(operation.ctx); err != nil {
		return err
	}
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	s.ncConnectMu.Lock()
	s.mu.Lock()
	s.ncManuallyDisconnected = true
	s.ncConnectionAttempted = true
	s.nextNCConnectAttempt = time.Time{}
	s.ncConnectionWasUp = false
	s.mu.Unlock()
	err := rclib.DisconnectNC(h)
	s.ncConnectMu.Unlock()
	if err != nil {
		return err
	}
	s.resetWeaponListState()
	return operationErr(operation.ctx)
}

// NCStatus returns the NC socket snapshot for the active handle.
func (s *Service) NCStatus() NCStatus {
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return NCStatus{}
	}
	mainConnected := rclib.IsConnected(h)
	mainAuthenticated := mainConnected && rclib.IsAuthenticated(h)
	ncConnected := mainAuthenticated && rclib.IsNCConnected(h)
	return NCStatus{
		HasNc:         mainAuthenticated && rclib.HasNCServer(h),
		Connected:     ncConnected,
		Authenticated: ncConnected && rclib.IsNCAuthenticated(h),
	}
}

// IrcLogin starts the IRC session for the active handle.
func (s *Service) IrcLogin() error {
	operation := s.beginLifecycleOperation()
	defer s.endLifecycleOperation(operation)
	if err := operationErr(operation.ctx); err != nil {
		return err
	}
	s.mu.Lock()
	h := s.handle
	s.mu.Unlock()
	if h == 0 {
		return errors.New("not connected: log in first")
	}
	if err := rclib.IrcLogin(h); err != nil {
		return err
	}
	return operationErr(operation.ctx)
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
	h, err := s.requireHandle()
	if err != nil {
		return err
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
	if err := validatePrivateMessage(message); err != nil {
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
	if err := validatePrivateMessage(message); err != nil {
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

// SelfAccount returns the account name selected by the active server for the
// logged-in session. The authenticated rights response may provide the
// canonical account; the listserver credential remains the startup fallback.
func (s *Service) SelfAccount() string {
	s.mu.Lock()
	account := s.creds.Account
	s.mu.Unlock()

	s.rightsMu.RLock()
	canonical := strings.TrimSpace(s.selfRightsAccount)
	s.rightsMu.RUnlock()
	if canonical != "" {
		return canonical
	}
	return account
}

// clearSelfFolderRights invalidates the permission snapshot when the active
// account or server changes. Clearing instead of reusing the previous value is
// important because the same account can have different folder rules per
// server.
func (s *Service) clearSelfFolderRights() {
	s.rightsMu.Lock()
	stateChanged := s.selfRightsLoaded || s.selfRightsError != "" || s.selfRightsAccount != "" || s.selfRightsCommunityName != "" || s.selfRightsServer != ""
	s.selfRights = folderrights.Access{}
	s.selfStaffRights = 0
	s.selfRightsLoaded = false
	s.selfRightsError = ""
	s.selfRightsAccount = ""
	s.selfRightsCommunityName = ""
	s.selfRightsServer = ""
	s.selfRightsUpdated = time.Time{}
	s.rightsMu.Unlock()
	s.playerIdentityMu.Lock()
	s.playerAccounts = nil
	s.playerCommunities = nil
	s.selfPlayerID = 0
	s.playerIdentityMu.Unlock()
	if stateChanged {
		s.emitScriptPermissionsEvent(false)
	}
}

// clearServerTextCache invalidates the options/flags snapshot when the active
// account or server changes. Server flags and options are server-specific and
// must never leak into a later connection.
func (s *Service) clearServerTextCache() {
	s.serverTextMu.Lock()
	s.serverOptions = ""
	s.serverFlags = ""
	s.serverOptionsLoaded = false
	s.serverFlagsLoaded = false
	s.serverTextServer = ""
	s.serverTextMu.Unlock()
}

func (s *Service) clearServerTextCacheIfCurrent(epoch uint64) {
	s.mu.Lock()
	current := s.serverEpoch == epoch
	s.mu.Unlock()
	if current {
		s.clearServerTextCache()
	}
}

func (s *Service) cacheServerText(epoch uint64, serverName, dataType, content string) {
	s.mu.Lock()
	current := s.serverEpoch == epoch && s.serverName == serverName
	s.mu.Unlock()
	if !current {
		log.Printf("[serverdata] ignoring stale %s response for server=%q", dataType, serverName)
		return
	}

	s.serverTextMu.Lock()
	s.serverTextServer = serverName
	switch dataType {
	case "options":
		s.serverOptions = content
		s.serverOptionsLoaded = true
	case "flags":
		s.serverFlags = content
		s.serverFlagsLoaded = true
	default:
		s.serverTextMu.Unlock()
		return
	}
	s.serverTextMu.Unlock()
	log.Printf("[serverdata] cached %s for server=%q len=%d", dataType, serverName, len(content))
}

func (s *Service) cacheCurrentServerText(dataType, content string) {
	s.mu.Lock()
	epoch, serverName := s.serverEpoch, s.serverName
	s.mu.Unlock()
	s.cacheServerText(epoch, serverName, dataType, content)
}

// GetServerScriptContext returns a copy of the current server options/flags
// snapshot for the embedded GraalScript language server.
func (s *Service) GetServerScriptContext() ServerScriptContext {
	s.mu.Lock()
	serverName := s.serverName
	s.mu.Unlock()
	s.serverTextMu.RLock()
	context := ServerScriptContext{
		ServerName:         serverName,
		ServerOptions:      s.serverOptions,
		ServerFlags:        s.serverFlags,
		ServerOptionsReady: s.serverOptionsLoaded,
		ServerFlagsReady:   s.serverFlagsLoaded,
	}
	s.serverTextMu.RUnlock()
	return context
}

// RefreshServerScriptContext requests fresh server options and flags for the
// active server. The callbacks update the same cache used by the login warm-up
// and by the embedded GraalScript language server.
func (s *Service) RefreshServerScriptContext() error {
	s.mu.Lock()
	h := s.handle
	serverName := s.serverName
	s.mu.Unlock()
	if h == 0 || strings.TrimSpace(serverName) == "" {
		return errors.New("not connected to a server")
	}

	for _, kind := range []string{"options", "flags"} {
		if _, err := s.requestServerText(h, kind); err != nil {
			return fmt.Errorf("refresh server %s: %w", kind, err)
		}
	}
	return nil
}

func (s *Service) invalidateSelfFolderRights(err error) {
	nextError := ""
	if err != nil {
		nextError = err.Error()
	}
	s.rightsMu.Lock()
	previousError := s.selfRightsError
	stateChanged := s.selfRightsLoaded || previousError != nextError
	s.selfRights = folderrights.Access{}
	s.selfStaffRights = 0
	s.selfRightsLoaded = false
	s.selfRightsError = ""
	if nextError != "" {
		s.selfRightsError = nextError
	}
	s.selfRightsUpdated = time.Time{}
	s.rightsMu.Unlock()
	if stateChanged {
		s.emitScriptPermissionsEvent(false)
	}
}

// replaceSelfFolderRights stores one server snapshot and reports whether the
// observable rights state changed. The comparison uses the last successful
// server snapshot, not the active cache: the latter is deliberately cleared on
// disconnect/reconnect to keep access fail-closed. The boolean returned to the
// frontend is intentionally narrower: a first load or a cache reset needs
// subscribers to refresh, but it is not a server-side permission change worth
// notifying the operator about.
func (s *Service) replaceSelfFolderRights(access folderrights.Access, staffRights int, account, server string) (stateChanged, permissionChanged bool) {
	s.rightsMu.Lock()
	wasLoaded := s.selfRightsLoaded
	sameBaseline := s.selfRightsBaselineLoaded &&
		equalFoldAny(s.selfRightsBaselineAccount, account) &&
		s.selfRightsBaselineServer == server
	permissionChanged = sameBaseline && (!s.selfRightsBaseline.Equal(access) || s.selfStaffRightsBaseline != staffRights)
	stateChanged = !wasLoaded || permissionChanged || s.selfRightsError != "" || !equalFoldAny(s.selfRightsAccount, account) || s.selfRightsServer != server
	s.selfRights = access
	s.selfStaffRights = staffRights
	s.selfRightsLoaded = true
	s.selfRightsError = ""
	s.selfRightsAccount = account
	s.selfRightsServer = server
	s.selfRightsUpdated = time.Now()
	// A successful snapshot becomes the new comparison point. If the server
	// changed the rights, updating the baseline prevents the same poll result
	// from generating the same warning repeatedly.
	s.selfRightsBaseline = access
	s.selfStaffRightsBaseline = staffRights
	s.selfRightsBaselineLoaded = true
	s.selfRightsBaselineAccount = account
	s.selfRightsBaselineServer = server
	s.rightsMu.Unlock()
	return stateChanged, permissionChanged
}

func (s *Service) isCurrentServerEpoch(expectedEpoch uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.serverEpoch == expectedEpoch && strings.TrimSpace(s.serverName) != ""
}

// replaceSelfFolderRightsIfCurrent atomically checks the active server epoch
// before committing a background login response. Without this guard, a late
// /openrights callback from the previous server could populate the permission
// cache of the newly selected server.
func (s *Service) replaceSelfFolderRightsIfCurrent(expectedEpoch uint64, access folderrights.Access, staffRights int, account string) (stateChanged, permissionChanged, current bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serverEpoch != expectedEpoch || strings.TrimSpace(s.serverName) == "" {
		return false, false, false
	}
	stateChanged, permissionChanged = s.replaceSelfFolderRights(access, staffRights, account, s.serverName)
	return stateChanged, permissionChanged, true
}

// RefreshSelfFolderRights explicitly asks the current server for this
// account's folder access and replaces the local snapshot. Callers should use
// EnsureSelfFolderRights when they only need the current session snapshot.
func (s *Service) RefreshSelfFolderRights() error {
	return s.refreshSelfFolderRights(nil)
}

func (s *Service) refreshSelfFolderRights(expectedEpoch *uint64) error {
	s.rightsRefreshMu.Lock()
	defer s.rightsRefreshMu.Unlock()
	return s.refreshSelfFolderRightsLocked(expectedEpoch)
}

// ensureSelfFolderRightsForEpoch shares the login refresh with concurrent
// permission-aware callers. The loaded check must happen after acquiring the
// refresh mutex: a caller that arrived while login was loading rights must
// observe that completed snapshot instead of issuing a second /openrights.
func (s *Service) ensureSelfFolderRightsForEpoch(expectedEpoch uint64) error {
	s.rightsRefreshMu.Lock()
	defer s.rightsRefreshMu.Unlock()
	if !s.isCurrentServerEpoch(expectedEpoch) {
		return errConnectionSessionChanged
	}
	if s.selfFolderRightsLoaded() {
		return nil
	}
	return s.refreshSelfFolderRightsLocked(&expectedEpoch)
}

// refreshSelfFolderRightsLocked performs the callback-based rights request.
// The caller must hold rightsRefreshMu so only one OpenRights round trip can
// be active at a time.
func (s *Service) refreshSelfFolderRightsLocked(expectedEpoch *uint64) error {
	if expectedEpoch != nil && !s.isCurrentServerEpoch(*expectedEpoch) {
		return errConnectionSessionChanged
	}
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
		if expectedEpoch != nil && !s.isCurrentServerEpoch(*expectedEpoch) {
			return errConnectionSessionChanged
		}
		err = fmt.Errorf("openrights for %q: %w", account, err)
		log.Printf("[rights] refresh failed after %s: %v", time.Since(started), err)
		s.invalidateSelfFolderRights(err)
		return err
	}
	log.Printf("[rights] openrights completed account=%q folderAccessLen=%d", data.Account, len(data.FolderAccess))
	returnedAccount := strings.TrimSpace(data.Account)
	if returnedAccount == "" {
		if expectedEpoch != nil && !s.isCurrentServerEpoch(*expectedEpoch) {
			return errConnectionSessionChanged
		}
		err = fmt.Errorf("openrights returned an empty account, expected the current server account")
		log.Printf("[rights] refresh failed after %s: %v", time.Since(started), err)
		s.invalidateSelfFolderRights(err)
		return err
	}
	access, err := folderrights.Parse(data.FolderAccess)
	if err != nil {
		if expectedEpoch != nil && !s.isCurrentServerEpoch(*expectedEpoch) {
			return errConnectionSessionChanged
		}
		err = fmt.Errorf("parse folder access for %q: %w", account, err)
		log.Printf("[rights] refresh failed after %s: %v", time.Since(started), err)
		s.invalidateSelfFolderRights(err)
		return err
	}

	var stateChanged, permissionChanged bool
	if expectedEpoch != nil {
		var current bool
		stateChanged, permissionChanged, current = s.replaceSelfFolderRightsIfCurrent(*expectedEpoch, access, data.Rights, returnedAccount)
		if !current {
			return errConnectionSessionChanged
		}
	} else {
		s.mu.Lock()
		server := s.serverName
		s.mu.Unlock()
		stateChanged, permissionChanged = s.replaceSelfFolderRights(access, data.Rights, returnedAccount, server)
	}
	log.Printf("[rights] refresh success localAccount=%q serverAccount=%q folderAccessLen=%d elapsed=%s", account, returnedAccount, len(data.FolderAccess), time.Since(started))
	if stateChanged {
		s.emitScriptPermissionsEvent(permissionChanged)
	}
	return nil
}

// EnsureSelfFolderRights uses the current session snapshot and requests it
// only when it has not been loaded yet. It is the cache-aware operation used by
// sync bootstrap and permission-gated reads.
func (s *Service) EnsureSelfFolderRights() error {
	return s.ensureSelfFolderRights()
}

func (s *Service) ensureSelfFolderRights() error {
	s.rightsRefreshMu.Lock()
	defer s.rightsRefreshMu.Unlock()
	if s.selfFolderRightsLoaded() {
		return nil
	}
	return s.refreshSelfFolderRightsLocked(nil)
}

func (s *Service) selfFolderRightsLoaded() bool {
	s.rightsMu.RLock()
	loaded := s.selfRightsLoaded
	s.rightsMu.RUnlock()
	return loaded
}

func (s *Service) selfFolderRights() (folderrights.Access, bool) {
	s.rightsMu.RLock()
	access, loaded := s.selfRights, s.selfRightsLoaded
	s.rightsMu.RUnlock()
	return access, loaded
}

// RequireBanPlayersRight authorizes actions that expose or mutate another
// player's ban state. The cache is fail-closed while the login refresh is
// still pending or has failed.
func (s *Service) RequireBanPlayersRight() error {
	s.rightsMu.RLock()
	loaded := s.selfRightsLoaded
	rights := s.selfStaffRights
	errText := s.selfRightsError
	s.rightsMu.RUnlock()
	if !loaded {
		if errText != "" {
			return fmt.Errorf("staff rights are unavailable: %s", errText)
		}
		return errors.New("staff rights are still loading")
	}
	if rights&(1<<banPlayersRightBit) == 0 {
		return errors.New("Ban players right is required for this action")
	}
	return nil
}

// CanBanPlayers reports the current cached moderation permission without
// turning an unavailable snapshot into an accidental allow.
func (s *Service) CanBanPlayers() bool {
	return s.RequireBanPlayersRight() == nil
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

// HasWritableScriptAccess reports whether the current account can write at
// least one Weapon, Class, or NPC script. It is deliberately based on the
// complete folder-rights snapshot rather than the currently warmed NC lists,
// so a write rule cannot be missed during the first login.
func (s *Service) HasWritableScriptAccess() bool {
	access, loaded := s.selfFolderRights()
	return loaded && access.HasWriteAccessForScriptTypes("weapon", "class", "npc")
}

// OpenRights requests an account's staff rights and waits for the reply. An
// empty account sends /openrights without an argument so the server resolves
// the current RC session to itself; explicit accounts use the direct rights
// request packet.
func (s *Service) OpenRights(account string) (RightsData, error) {
	account = strings.TrimSpace(account)
	selfAccount := strings.TrimSpace(s.SelfAccount())
	selfRequest := account == "" || s.isSelfRightsAlias(account)
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
	key := editorKey("rights", expectedAccount)
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
	target, selfRequest, err := s.editorTarget(account)
	if err != nil {
		return AttrsData{}, err
	}
	h, err := s.requireHandle()
	if err != nil {
		return AttrsData{}, err
	}
	key := editorKey("attrs", target)
	if selfRequest {
		key = editorSelfKey("attrs")
	}
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request attrs account=%q self=%v (self creds.Account=%q nickname=%q)", target, selfRequest, s.creds.Account, s.creds.Nickname)
		var requestErr error
		if selfRequest {
			requestErr = rclib.Execute(h, "/open")
		} else {
			requestErr = rclib.RequestPlayerAttrs(h, target)
		}
		if requestErr != nil {
			s.dropEditor(key)
			return AttrsData{}, requestErr
		}
	} else {
		log.Printf("[editor] dedup attrs account=%q self=%v (awaiting in-flight request)", target, selfRequest)
	}
	reply, err := s.awaitEditor(w, key, isNew, "player attributes")
	if err != nil {
		return AttrsData{}, err
	}
	data, ok := reply.(AttrsData)
	if !ok {
		return AttrsData{}, fmt.Errorf("unexpected player attributes callback type %T", reply)
	}
	return data, nil
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
	if err := s.RequireBanPlayersRight(); err != nil {
		return BanData{}, err
	}
	target, selfRequest, err := s.editorTarget(account)
	if err != nil {
		return BanData{}, err
	}
	h, err := s.requireHandle()
	if err != nil {
		return BanData{}, err
	}
	key := editorKey("ban", target)
	if selfRequest {
		key = editorSelfKey("ban")
	}
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request ban account=%q self=%v (self creds.Account=%q nickname=%q)", target, selfRequest, s.creds.Account, s.creds.Nickname)
		var requestErr error
		if selfRequest {
			requestErr = rclib.Execute(h, "/openaccess")
		} else {
			requestErr = rclib.RequestPlayerBanByAccount(h, target)
		}
		if requestErr != nil {
			s.dropEditor(key)
			return BanData{}, requestErr
		}
	} else {
		log.Printf("[editor] dedup ban account=%q self=%v (awaiting in-flight request)", target, selfRequest)
	}
	reply, err := s.awaitEditor(w, key, isNew, "player ban")
	if err != nil {
		return BanData{}, err
	}
	data, ok := reply.(BanData)
	if !ok {
		return BanData{}, fmt.Errorf("unexpected player ban callback type %T", reply)
	}
	return data, nil
}

// SetBan writes ban data for a target. world is "local" or "all"; target is the
// account or "pc:<computerID>"; releaseTime "" resets the ban timer.
func (s *Service) SetBan(target, world string, banned bool, banType, releaseTime, reason string) error {
	if err := s.RequireBanPlayersRight(); err != nil {
		return err
	}
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	return rclib.SetBan(h, target, world, banned, banType, releaseTime, reason)
}

// OpenComments requests an account's comments text and waits for the reply.
func (s *Service) OpenComments(account string) (CommentsData, error) {
	target, selfRequest, err := s.editorTarget(account)
	if err != nil {
		return CommentsData{}, err
	}
	h, err := s.requireHandle()
	if err != nil {
		return CommentsData{}, err
	}
	key := editorKey("comments", target)
	if selfRequest {
		key = editorSelfKey("comments")
	}
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request comments account=%q self=%v (self creds.Account=%q nickname=%q)", target, selfRequest, s.creds.Account, s.creds.Nickname)
		var requestErr error
		if selfRequest {
			requestErr = rclib.Execute(h, "/opencomments")
		} else {
			requestErr = rclib.RequestPlayerComments(h, target)
		}
		if requestErr != nil {
			s.dropEditor(key)
			return CommentsData{}, requestErr
		}
	} else {
		log.Printf("[editor] dedup comments account=%q self=%v (awaiting in-flight request)", target, selfRequest)
	}
	reply, err := s.awaitEditor(w, key, isNew, "player comments")
	if err != nil {
		return CommentsData{}, err
	}
	data, ok := reply.(CommentsData)
	if !ok {
		return CommentsData{}, fmt.Errorf("unexpected player comments callback type %T", reply)
	}
	return data, nil
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
	key := editorKey("bantypes", "")
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
	if err := s.RequireBanPlayersRight(); err != nil {
		return "", err
	}
	target, _, err := s.editorTarget(account)
	if err != nil {
		return "", err
	}
	h, err := s.requireHandle()
	if err != nil {
		return "", err
	}
	key := editorKey("banhistory", target)
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request banhistory account=%q", target)
		if err := rclib.RequestBanHistory(h, target); err != nil {
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
	target, _, err := s.editorTarget(account)
	if err != nil {
		return "", err
	}
	h, err := s.requireHandle()
	if err != nil {
		return "", err
	}
	key := editorKey("staffactivity", target)
	w, isNew := s.registerEditor(key)
	if isNew {
		log.Printf("[editor] request staffactivity account=%q", target)
		if err := rclib.RequestStaffActivity(h, target); err != nil {
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
	s.pumpMu.Lock()
	pumpErr := s.pumpErr
	s.pumpMu.Unlock()
	if pumpErr != nil {
		return 0, fmt.Errorf("connection event pump unavailable: %w", pumpErr)
	}
	if !rclib.IsConnected(h) || !rclib.IsAuthenticated(h) {
		return 0, errors.New("server connection is no longer authenticated")
	}
	return h, nil
}

// requireNC returns the active handle after checking that the NC (script)
// socket is connected. If the reference client's initial automatic attempt has
// not happened yet, it is allowed to start it; the pump handles the one-shot
// endpoint-refresh retry after a previously observed NC connection drops.
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
	if err := s.tryConnectNC(h, false); err != nil {
		return 0, fmt.Errorf("NC server not connected: %w", err)
	}
	if !rclib.IsNCConnected(h) {
		return 0, errors.New("NC server is not connected")
	}
	return h, nil
}

// requireNCContext is the read path used by script opens. The script manager
// can be populated from the native cache while NC is offline, so opening one
// of those cached entries must wait for the automatic reconnect instead of
// immediately surfacing a transient "NC is not connected" error.
func (s *Service) requireNCContext(ctx context.Context) (rclib.Handle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	h, err := s.requireHandle()
	if err != nil {
		return 0, err
	}
	if !rclib.HasNCServer(h) {
		return 0, errors.New("this account has no NC rights on this server")
	}
	if rclib.IsNCConnected(h) && rclib.IsNCAuthenticated(h) {
		return h, nil
	}
	if err := s.EnsureNCConnected(ctx); err != nil {
		return 0, err
	}

	// Re-read the handle after waiting: a lifecycle operation may have changed
	// the active session while the reconnect was in progress.
	h, err = s.requireHandle()
	if err != nil {
		return 0, err
	}
	if !rclib.IsNCConnected(h) || !rclib.IsNCAuthenticated(h) {
		return 0, errNCUnavailable
	}
	return h, nil
}

// GetWeapons returns the cached weapon list for the NC server.
func (s *Service) GetWeapons() ([]rclib.Weapon, error) {
	h, err := s.requireNC()
	if err != nil {
		return nil, err
	}
	if err := s.ensureWeaponListReady(context.Background()); err != nil {
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

// beginScriptListsRequest joins an existing native cache read or becomes its
// owner. The request is intentionally shared between readable and unfiltered
// callers: permission filtering is cheap and happens after the base snapshot,
// while the native cache read is the expensive, globally serialized operation.
func (s *Service) beginScriptListsRequest() (*scriptListsRequest, bool) {
	s.scriptListsMu.Lock()
	defer s.scriptListsMu.Unlock()
	if s.scriptListsInFlight != nil {
		return s.scriptListsInFlight, false
	}
	request := &scriptListsRequest{done: make(chan struct{})}
	s.scriptListsInFlight = request
	return request, true
}

func (s *Service) completeScriptListsRequest(request *scriptListsRequest, lists ScriptLists, err error) {
	s.scriptListsMu.Lock()
	defer s.scriptListsMu.Unlock()
	if s.scriptListsInFlight != request {
		return
	}
	request.lists = lists
	request.err = err
	s.scriptListsInFlight = nil
	close(request.done)
}

// getScriptListsSnapshot reads the native NC caches once and shares that
// result with all callers that arrive while the read is in progress. This
// prevents Wails request storms from piling up behind rclib's global DLL
// mutex when the main window and Script Manager refresh together.
func (s *Service) getScriptListsSnapshot() (lists ScriptLists, err error) {
	request, owner := s.beginScriptListsRequest()
	if !owner {
		<-request.done
		return cloneScriptLists(request.lists), request.err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			s.completeScriptListsRequest(request, ScriptLists{}, fmt.Errorf("get script lists panic: %v", recovered))
			panic(recovered)
		}
		s.completeScriptListsRequest(request, lists, err)
	}()

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
	return ScriptLists{Weapons: weapons, Classes: classes, NPCs: npcs}, nil
}

func cloneScriptLists(lists ScriptLists) ScriptLists {
	clone := ScriptLists{}
	if lists.Weapons != nil {
		clone.Weapons = append([]rclib.Weapon(nil), lists.Weapons...)
	}
	if lists.Classes != nil {
		clone.Classes = append([]rclib.Class(nil), lists.Classes...)
	}
	if lists.NPCs != nil {
		clone.NPCs = append([]rclib.NPC(nil), lists.NPCs...)
	}
	return clone
}

func filterReadableScriptLists(lists ScriptLists, access folderrights.Access) ScriptLists {
	filteredWeapons := make([]rclib.Weapon, 0, len(lists.Weapons))
	for _, weapon := range lists.Weapons {
		if access.CanRead("weapon", weapon.Name) {
			filteredWeapons = append(filteredWeapons, weapon)
		}
	}
	filteredClasses := make([]rclib.Class, 0, len(lists.Classes))
	for _, class := range lists.Classes {
		if access.CanRead("class", class.Name) {
			filteredClasses = append(filteredClasses, class)
		}
	}
	filteredNPCs := make([]rclib.NPC, 0, len(lists.NPCs))
	for _, npc := range lists.NPCs {
		if access.CanRead("npc", npc.Name) {
			filteredNPCs = append(filteredNPCs, npc)
		}
	}
	return ScriptLists{Weapons: filteredWeapons, Classes: filteredClasses, NPCs: filteredNPCs}
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

	lists, err := s.getScriptListsSnapshot()
	if err != nil {
		return ScriptLists{}, err
	}
	if !onlyReadable {
		return lists, nil
	}
	return filterReadableScriptLists(lists, access), nil
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
	if err := s.EnsureNCConnected(ctx); err != nil {
		return nil, err
	}
	if err := s.ensureWeaponListReady(ctx); err != nil {
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

	var jobs []scriptFetchJob
	skipped := 0
	for _, w := range weapons {
		if !rclib.IsUsableScriptName(w.Name) {
			skipped++
			log.Printf("[sync fetch] skipping weapon with invalid name %q", w.Name)
			continue
		}
		if allowed == nil || allowed("weapon", w.Name) {
			jobs = append(jobs, scriptFetchJob{stype: "weapon", key: w.Name, name: w.Name})
		} else {
			skipped++
		}
	}
	for _, c := range classes {
		if !rclib.IsUsableScriptName(c.Name) {
			skipped++
			log.Printf("[sync fetch] skipping class with invalid name %q", c.Name)
			continue
		}
		if allowed == nil || allowed("class", c.Name) {
			jobs = append(jobs, scriptFetchJob{stype: "class", key: c.Name, name: c.Name})
		} else {
			skipped++
		}
	}
	for _, n := range npcs {
		if !rclib.IsUsableScriptName(n.Name) {
			skipped++
			log.Printf("[sync fetch] skipping NPC id=%d with invalid name %q", n.ID, n.Name)
			continue
		}
		if allowed == nil || allowed("npc", n.Name) {
			jobs = append(jobs, scriptFetchJob{stype: "npc", key: strconv.Itoa(n.ID), name: n.Name})
		} else {
			skipped++
		}
	}
	total := len(jobs)
	log.Printf("[sync fetch] permission filter kept=%d skipped=%d", total, skipped)

	replies, fetchErr := s.fetchScriptJobs(ctx, jobs, progress)
	// A dropped NC can leave a valid-looking partial reply slice while the
	// workers are unwinding. Never let that slice become a reconciliation
	// snapshot: the caller must retry after the socket is restored.
	if errors.Is(fetchErr, errNCUnavailable) {
		return nil, errNCUnavailable
	}
	if !s.IsNCConnected() || !s.IsNCAuthenticated() {
		return nil, errNCUnavailable
	}
	return replies, fetchErr
}

// fetchScriptJobs processes a bulk script fetch with a fixed number of
// workers. Keeping the job queue bounded is important here: a server can
// legitimately expose thousands of scripts, and creating one goroutine per
// script would retain a large amount of stack and scheduler state while the
// workers wait on the NC request limit.
func (s *Service) fetchScriptJobs(ctx context.Context, jobs []scriptFetchJob, progress func(done, total int)) ([]rclib.ScriptReply, error) {
	return runScriptFetchJobs(ctx, jobs, ncFetchConcurrency, func(ctx context.Context, job scriptFetchJob) (rclib.ScriptReply, error) {
		return s.openScriptContext(ctx, job.stype, job.key, job.name)
	}, progress)
}

func runScriptFetchJobs(ctx context.Context, jobs []scriptFetchJob, workerCount int, fetch func(context.Context, scriptFetchJob) (rclib.ScriptReply, error), progress func(done, total int)) ([]rclib.ScriptReply, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	total := len(jobs)
	out := make([]rclib.ScriptReply, 0, total)
	if total == 0 {
		return out, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > total {
		workerCount = total
	}
	jobCh := make(chan scriptFetchJob)
	var outMu sync.Mutex
	var fetchErrMu sync.Mutex
	var terminalFetchErr error
	var wg sync.WaitGroup
	var done int32
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-workCtx.Done():
					return
				case j, ok := <-jobCh:
					if !ok {
						return
					}
					r, err := fetch(workCtx, j)
					if err == nil {
						// Some NC callbacks return only the NPC id. Keep the
						// display name from the cached NPC list so local sync
						// never falls back to an ID-based filename.
						if r.Type == "npc" && r.Name == "" {
							r.Name = j.name
						}
						outMu.Lock()
						out = append(out, r)
						outMu.Unlock()
					} else {
						if errors.Is(err, errNCUnavailable) {
							fetchErrMu.Lock()
							if terminalFetchErr == nil {
								terminalFetchErr = err
							}
							fetchErrMu.Unlock()
							cancel()
						}
						log.Printf("sync fetch %s:%s failed: %v", j.stype, j.key, err)
					}
					if progress != nil {
						progress(int(atomic.AddInt32(&done, 1)), total)
					}
				}
			}
		}()
	}

sendJobs:
	for _, j := range jobs {
		select {
		case <-workCtx.Done():
			break sendJobs
		case jobCh <- j:
		}
		if workCtx.Err() != nil {
			break sendJobs
		}
	}
	close(jobCh)
	wg.Wait()
	fetchErrMu.Lock()
	deferredErr := terminalFetchErr
	fetchErrMu.Unlock()
	if deferredErr != nil {
		return out, deferredErr
	}
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
	return s.openScriptContext(context.Background(), scriptType, key, name)
}

func (s *Service) openScriptContext(ctx context.Context, scriptType, key, name string) (rclib.ScriptReply, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if scriptType != "weapon" && scriptType != "class" && scriptType != "npc" {
		return rclib.ScriptReply{}, errors.New("unknown script type: " + scriptType)
	}
	if !rclib.IsUsableScriptName(name) {
		return rclib.ScriptReply{}, fmt.Errorf("%s script name is empty or invalid", scriptType)
	}
	if err := s.requireScriptPermission(scriptType, name, 'r'); err != nil {
		return rclib.ScriptReply{}, err
	}
	h, err := s.requireNCContext(ctx)
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	pending := pendingKey(scriptType, key)
	waiter, isNew := s.registerPending(pending)
	if err := operationErr(ctx); err != nil {
		if isNew {
			s.cancelPending(pending, waiter, err)
		}
		return rclib.ScriptReply{}, err
	}

	// Serialize only the native send. The reply is delivered asynchronously by
	// the pump and is correlated by its pending key, so requests with different
	// keys can be sent while this one is still waiting for the server. A caller
	// sharing this key joins the existing waiter and does not send a duplicate.
	if isNew {
		s.ncRequestMu.Lock()
		switch scriptType {
		case "weapon":
			err = rclib.RequestWeaponScript(h, key)
		case "class":
			err = rclib.RequestClassScript(h, key)
		case "npc":
			id, convErr := strconv.Atoi(key)
			if convErr != nil {
				s.ncRequestMu.Unlock()
				s.cancelPending(pending, waiter, convErr)
				return rclib.ScriptReply{}, convErr
			}
			err = rclib.RequestNPCScript(h, id)
		default:
			s.ncRequestMu.Unlock()
			s.cancelPending(pending, waiter, errors.New("unknown script type: "+scriptType))
			return rclib.ScriptReply{}, errors.New("unknown script type: " + scriptType)
		}
		s.ncRequestMu.Unlock()
		if err != nil {
			s.cancelPending(pending, waiter, err)
			return rclib.ScriptReply{}, err
		}
	}
	if err := operationErr(ctx); err != nil {
		if isNew {
			s.cancelPending(pending, waiter, err)
		}
		return rclib.ScriptReply{}, err
	}
	timer := time.NewTimer(scriptTimeout)
	defer timer.Stop()
	ncPoll := time.NewTicker(ncStatusPollInterval)
	defer ncPoll.Stop()
	for {
		select {
		case <-waiter.done:
			return waiter.reply, waiter.err
		case <-ctx.Done():
			if isNew {
				s.cancelPending(pending, waiter, ctx.Err())
			}
			return rclib.ScriptReply{}, ctx.Err()
		case <-ncPoll.C:
			if !s.IsNCConnected() || !s.IsNCAuthenticated() {
				if isNew {
					s.cancelPending(pending, waiter, errNCUnavailable)
				}
				return rclib.ScriptReply{}, errNCUnavailable
			}
		case <-timer.C:
			if isNew {
				s.cancelPending(pending, waiter, errors.New("script request timed out"))
			}
			return rclib.ScriptReply{}, errors.New("script request timed out")
		}
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
	return s.requestServerText(h, kind)
}

// requestServerText registers the waiter before dispatching the request. The
// server-data callback is asynchronous, but a fast local response can still
// arrive before a waiter registered after the request, which would otherwise
// make the cache/editor wait forever. Serializing these requests also lets the
// login warm-up and a manually opened config share the same callback safely.
func (s *Service) requestServerText(h rclib.Handle, kind string) (rclib.ScriptReply, error) {
	switch kind {
	case "options", "folder_config", "flags":
	default:
		return rclib.ScriptReply{}, errors.New("unknown server text kind: " + kind)
	}

	s.serverTextRequestMu.Lock()
	defer s.serverTextRequestMu.Unlock()

	key := pendingKey("serverdata", kind)
	waiter, isNew := s.registerPending(key)
	if isNew {
		var err error
		switch kind {
		case "options":
			err = rclib.RequestServerOptions(h)
		case "folder_config":
			err = rclib.RequestFolderConfig(h)
		case "flags":
			err = rclib.RequestServerFlags(h)
		}
		if err != nil {
			s.cancelPending(key, waiter, err)
			return rclib.ScriptReply{}, err
		}
	}
	select {
	case <-waiter.done:
		return waiter.reply, waiter.err
	case <-time.After(scriptTimeout):
		if isNew {
			s.cancelPending(key, waiter, errors.New("server text request timed out"))
		}
		return rclib.ScriptReply{}, errors.New("server text request timed out")
	}
}

func (s *Service) refreshServerTextCache(h rclib.Handle, epoch uint64, serverName string) {
	for _, kind := range []string{"options", "flags"} {
		s.mu.Lock()
		current := s.handle == h && s.serverEpoch == epoch && s.serverName == serverName
		s.mu.Unlock()
		if !current {
			return
		}
		if _, err := s.requestServerText(h, kind); err != nil {
			log.Printf("[serverdata] login cache %s failed for server=%q: %v", kind, serverName, err)
		}
	}
}

// UploadServerText writes a server-side text config (options/folder_config/
// flags) back to the server.
func (s *Service) UploadServerText(kind, content string) error {
	h, err := s.requireHandle()
	if err != nil {
		return err
	}
	var uploadErr error
	switch kind {
	case "options":
		uploadErr = rclib.UploadServerOptions(h, content)
	case "folder_config":
		uploadErr = rclib.UploadFolderConfig(h, content)
	case "flags":
		uploadErr = rclib.UploadServerFlags(h, content)
	default:
		return errors.New("unknown server text kind: " + kind)
	}
	if uploadErr == nil && (kind == "options" || kind == "flags") {
		s.cacheCurrentServerText(kind, content)
	}
	return uploadErr
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
	key := strconv.Itoa(id)
	pending := pendingKey("npcflags", key)
	waiter, isNew := s.registerPending(pending)
	if isNew {
		if err := rclib.GetNPCFlags(h, id); err != nil {
			s.cancelPending(pending, waiter, err)
			return rclib.ScriptReply{}, err
		}
	}
	select {
	case <-waiter.done:
		return waiter.reply, waiter.err
	case <-time.After(scriptTimeout):
		if isNew {
			s.cancelPending(pending, waiter, errors.New("npc flags request timed out"))
		}
		return rclib.ScriptReply{}, errors.New("npc flags request timed out")
	}
}

// OpenNPCAttributes requests an NPC's attributes and waits for the reply.
func (s *Service) OpenNPCAttributes(id int) (rclib.ScriptReply, error) {
	h, err := s.requireHandle()
	if err != nil {
		return rclib.ScriptReply{}, err
	}
	key := strconv.Itoa(id)
	pending := pendingKey("npcattr", key)
	waiter, isNew := s.registerPending(pending)
	if isNew {
		if err := rclib.RequestNPCAttributes(h, id); err != nil {
			s.cancelPending(pending, waiter, err)
			return rclib.ScriptReply{}, err
		}
	}
	select {
	case <-waiter.done:
		return waiter.reply, waiter.err
	case <-time.After(scriptTimeout):
		if isNew {
			s.cancelPending(pending, waiter, errors.New("npc attributes request timed out"))
		}
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

const weaponListRefreshTimeout = 15 * time.Second

// resetWeaponListState starts a new NC weapon-cache lifecycle. The native
// cache is borrowed by rclib.GetWeapons, so a snapshot from the previous NC
// session must not be considered valid after a reconnect.
func (s *Service) resetWeaponListState() {
	s.weaponListMu.Lock()
	previousSignal := s.weaponListSignal
	s.weaponListSignal = make(chan struct{})
	s.weaponListReady = false
	s.weaponListMu.Unlock()

	if previousSignal != nil {
		close(previousSignal)
	}
}

// markWeaponListReceived records that the native weapon cache was rebuilt from
// a complete list response and wakes callers waiting for that response.
func (s *Service) markWeaponListReceived(count int) {
	s.weaponListMu.Lock()
	s.weaponListGeneration++
	s.weaponListReady = true
	previousSignal := s.weaponListSignal
	s.weaponListSignal = make(chan struct{})
	generation := s.weaponListGeneration
	s.weaponListMu.Unlock()

	if previousSignal != nil {
		close(previousSignal)
	}
	log.Printf("[connection] weapon list received count=%d generation=%d", count, generation)
}

// ensureWeaponListReady waits for the automatic list sent by the NC server
// after authentication. This is the C++ client's normal pull path: it reads
// the cache populated by that response and does not send PLI_NC_WEAPONLISTGET
// as part of sync.
func (s *Service) ensureWeaponListReady(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	waitCtx, cancel := context.WithTimeout(ctx, weaponListRefreshTimeout)
	defer cancel()
	ncPoll := time.NewTicker(ncStatusPollInterval)
	defer ncPoll.Stop()

	for {
		s.weaponListMu.Lock()
		if s.weaponListReady {
			s.weaponListMu.Unlock()
			return nil
		}
		signal := s.weaponListSignal
		if signal == nil {
			signal = make(chan struct{})
			s.weaponListSignal = signal
		}
		s.weaponListMu.Unlock()

		select {
		case <-signal:
		case <-waitCtx.Done():
			return fmt.Errorf("waiting for initial weapon list: %w", waitCtx.Err())
		case <-ncPoll.C:
			if !s.IsNCConnected() || !s.IsNCAuthenticated() {
				return errNCUnavailable
			}
		}
	}
}

// waitForWeaponList waits until a list response newer than generation has
// rebuilt the native cache. The generation check avoids treating an older
// callback as the response to a refresh that has not been issued yet.
func (s *Service) waitForWeaponList(ctx context.Context, generation uint64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	waitCtx, cancel := context.WithTimeout(ctx, weaponListRefreshTimeout)
	defer cancel()
	ncPoll := time.NewTicker(ncStatusPollInterval)
	defer ncPoll.Stop()

	for {
		s.weaponListMu.Lock()
		if s.weaponListGeneration > generation {
			s.weaponListMu.Unlock()
			return nil
		}
		signal := s.weaponListSignal
		if signal == nil {
			signal = make(chan struct{})
			s.weaponListSignal = signal
		}
		s.weaponListMu.Unlock()

		select {
		case <-signal:
		case <-waitCtx.Done():
			return fmt.Errorf("waiting for weapon list: %w", waitCtx.Err())
		case <-ncPoll.C:
			if !s.IsNCConnected() || !s.IsNCAuthenticated() {
				return errNCUnavailable
			}
		}
	}
}

// RefreshWeapons re-requests the weapon list via grclib's dedicated
// rc_request_weapon_list primitive (the same call the reference C++ RC makes)
// and waits until rc_on_weapon_list_received confirms that the native cache is
// complete. Reading GetWeapons before that callback can produce a partial
// snapshot containing only classes and NPCs. Note: grclib exposes NO equivalent
// for class/npc lists — those are maintained by the server's live add/delete
// push packets (rc_on_class_added/deleted, rc_on_npc_added/deleted).
func (s *Service) RefreshWeapons() error {
	h, err := s.requireNC()
	if err != nil {
		return err
	}

	s.weaponListMu.Lock()
	generation := s.weaponListGeneration
	if s.weaponListSignal == nil {
		s.weaponListSignal = make(chan struct{})
	}
	s.weaponListMu.Unlock()

	if err := rclib.RequestWeaponList(h); err != nil {
		return err
	}
	if err := s.waitForWeaponList(context.Background(), generation); err != nil {
		return err
	}
	return nil
}

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
	// The native reference client is single-transfer. Keep this lock across the
	// asynchronous wait, not only around FileBrowserDownload, because the native
	// request resets its pending path whenever a new transfer starts.
	s.fileDownloadMu.Lock()
	defer s.fileDownloadMu.Unlock()

	h, err := s.requireHandle()
	if err != nil {
		return nil, err
	}
	waiter, isNew := s.registerFile(path)
	if isNew {
		if err := rclib.FileBrowserDownload(h, path); err != nil {
			s.cancelFile(path, waiter, err)
			return nil, err
		}
	}
	select {
	case <-waiter.done:
		if waiter.err != nil {
			return nil, waiter.err
		}
		if waiter.content == nil {
			return nil, errors.New("server returned no file content")
		}
		return waiter.content, nil
	case <-time.After(downloadTimeout):
		if isNew {
			s.cancelFile(path, waiter, errors.New("file download timed out (no response from server)"))
		}
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
	operation := s.beginLifecycleOperation()
	defer s.endLifecycleOperation(operation)
	s.logout()
}

func (s *Service) logout() {
	s.stopPump()
	s.mu.Lock()
	h := s.handle
	s.handle = 0
	s.creds = Credentials{}
	s.serverName = ""
	s.serverEpoch++
	s.ncConnectionAttempted = false
	s.ncManuallyDisconnected = false
	s.nextNCConnectAttempt = time.Time{}
	s.ncConnectionWasUp = false
	s.ncAutomaticReconnectAttempted = false
	s.channels = nil
	s.maxUpload = 0
	s.mu.Unlock()
	s.cancelWaiters(errConnectionSessionChanged)
	s.clearPumpError()
	s.emitEvent("rc:fbReset")
	if h != 0 {
		unregisterCallbacks(h)
		rclib.Disconnect(h)
	}
	s.clearSelfFolderRights()
	s.clearServerTextCache()
	s.resetWeaponListState()
}

// Status returns a snapshot of the current session state.
func (s *Service) Status() Status {
	s.pumpMu.Lock()
	pumpFailed := s.pumpErr != nil
	s.pumpMu.Unlock()

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
	if s.handle != 0 && !pumpFailed {
		st.Connected = rclib.IsConnected(s.handle)
		st.Authenticated = st.Connected && rclib.IsAuthenticated(s.handle)
	}
	s.rightsMu.RLock()
	st.RealAccount = s.selfRightsAccount
	st.CommunityName = s.selfRightsCommunityName
	st.Rights = s.selfStaffRights
	st.RightsReady = s.selfRightsLoaded
	st.CanBanPlayers = s.selfRightsLoaded && s.selfStaffRights&(1<<banPlayersRightBit) != 0
	st.ScriptWriteAccess = s.selfRightsLoaded && s.selfRights.HasWriteAccessForScriptTypes("weapon", "class", "npc")
	s.rightsMu.RUnlock()
	return st
}
