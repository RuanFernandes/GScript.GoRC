// Package rclib binds grclib.dll (C ABI) via the syscall package, so the
// project builds without cgo / a C toolchain on Windows.
//
// Struct and signatures mirror how the reference C++ client
// (rclib/example/GScript.RemoteControl/src/TServerList.cpp) consumes the
// library: rc_connect -> rc_get_servers -> rc_connect_to_server.
package rclib

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"
)

// Handle is an opaque grclib connection handle (rc_connect return value).
type Handle uintptr

// RCServer mirrors the in-memory layout of grclib's RCServer struct exactly as
// declared in include/grclib.h:
//
//	struct { char* name; char* ip; int port; int players;
//	         char* language; char* description; char* version; char* homepage; }
//
// x64 layout: name@0, ip@8, port@16, players@20, language@24, description@32,
// version@40, homepage@48 -> 56 bytes. Go reproduces this with the same field
// order (int32 after two pointers pads naturally to the next 8-aligned field).
type RCServer struct {
	Name        *byte
	IP          *byte
	Port        int32
	Players     int32
	Language    *byte
	Description *byte
	Version     *byte
	Homepage    *byte
}

// Server is the Go-friendly copy of an RCServer entry.
type Server struct {
	Name        string `json:"name"`
	IP          string `json:"ip"`
	Port        int    `json:"port"`
	Players     int    `json:"players"`
	Language    string `json:"language"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Homepage    string `json:"homepage"`
}

// RCPlayer mirrors grclib's RCPlayer struct (include/grclib.h):
//
//	struct { char* account; int id; char* nick; char* level; }
//
// x64 layout: account@0, id@8, (pad@12), nick@16, level@24 -> 32 bytes.
// The int32 pad aligns nick to the next 8-byte boundary.
type RCPlayer struct {
	Account *byte
	ID      int32
	_       int32
	Nick    *byte
	Level   *byte
}

// Player is the Go-friendly copy of an RCPlayer entry.
type Player struct {
	Account string `json:"account"`
	ID      int    `json:"id"`
	Nick    string `json:"nick"`
	Level   string `json:"level"`
}

var (
	once    sync.Once
	loadErr error

	procConnect         *syscall.Proc
	procGetServers      *syscall.Proc
	procConnectToServer *syscall.Proc
	procDisconnect      *syscall.Proc
	procLastError       *syscall.Proc
	procIsConnected     *syscall.Proc
	procIsAuthenticated *syscall.Proc
	procSetNewProtocol  *syscall.Proc
	procIsNewProtocol   *syscall.Proc
	procFree            *syscall.Proc
	procProcessEvents   *syscall.Proc
	procOnConnected     *syscall.Proc
	procOnDisconnected  *syscall.Proc

	procConnectToNcServer *syscall.Proc
	procDisconnectNc      *syscall.Proc
	procIsNcConnected     *syscall.Proc
	procIsNcAuthenticated *syscall.Proc
	procHasNcServer       *syscall.Proc
	procIrcLogin          *syscall.Proc
	procSendIrcText       *syscall.Proc
	procExecute           *syscall.Proc
	procSetNickname       *syscall.Proc
	procGetPlayers        *syscall.Proc
	procOnMessage         *syscall.Proc
	procOnIrcMessage      *syscall.Proc
	procOnServerData      *syscall.Proc
)

// Default listserver endpoint used by the reference client.
const (
	DefaultListserverHost = "listserver.graalonline.com"
	DefaultListserverPort = 14922
)

// dllSearchPaths returns candidate locations for grclib.dll. It checks the cwd
// and executable directory, then walks every parent of each looking for a
// "rclib/grclib.dll" sibling. This finds the DLL during `wails dev` (cwd at the
// project root) and when running the built binary from build/bin (DLL several
// levels up at <repo>/rclib/grclib.dll).
func dllSearchPaths() []string {
	var roots []string
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	if exe, err := os.Executable(); err == nil {
		roots = append(roots, filepath.Dir(exe))
	}

	var paths []string
	seen := map[string]struct{}{}
	add := func(p string) {
		if p == "" {
			return
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if _, ok := seen[abs]; ok {
			return
		}
		seen[abs] = struct{}{}
		paths = append(paths, abs)
	}

	for _, root := range roots {
		add(filepath.Join(root, "grclib.dll"))
		add(filepath.Join(root, "rclib", "grclib.dll"))
		// Walk parents: <root>/.., <root>/../.., ... looking for rclib/grclib.dll.
		dir := root
		for i := 0; i < 8; i++ {
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			add(filepath.Join(parent, "rclib", "grclib.dll"))
			add(filepath.Join(parent, "grclib.dll"))
			dir = parent
		}
	}
	return paths
}

// load resolves and loads the DLL exactly once. Subsequent calls return the
// cached error (nil on success).
func load() error {
	once.Do(func() {
		var dllPath string
		for _, p := range dllSearchPaths() {
			if _, err := os.Stat(p); err == nil {
				dllPath = p
				break
			}
		}
		if dllPath == "" {
			loadErr = fmt.Errorf("grclib.dll not found; searched %v", dllSearchPaths())
			return
		}

		// syscall.LoadLibrary converts the Go string to UTF-16 for Windows.
		h, err := syscall.LoadLibrary(dllPath)
		if err != nil {
			loadErr = fmt.Errorf("LoadLibrary(%s): %w", dllPath, err)
			return
		}
		dll := &syscall.DLL{Handle: h}

		find := func(name string) *syscall.Proc {
			p, e := dll.FindProc(name)
			if e != nil {
				loadErr = fmt.Errorf("FindProc(%s): %w", name, e)
				return nil
			}
			return p
		}

		procConnect = find("rc_connect")
		if loadErr != nil {
			return
		}
		procGetServers = find("rc_get_servers")
		procConnectToServer = find("rc_connect_to_server")
		procDisconnect = find("rc_disconnect")
		procLastError = find("rc_last_error")
		procIsConnected = find("rc_is_connected")
		procIsAuthenticated = find("rc_is_authenticated")
		procSetNewProtocol = find("rc_set_new_protocol")
		procIsNewProtocol = find("rc_is_new_protocol")
		procFree = find("rc_free")
		procProcessEvents = find("rc_process_events")
		procOnConnected = find("rc_on_connected")
		procOnDisconnected = find("rc_on_disconnected")
		procConnectToNcServer = find("rc_connect_to_nc_server")
		procDisconnectNc = find("rc_disconnect_nc")
		procIsNcConnected = find("rc_is_nc_connected")
		procIsNcAuthenticated = find("rc_is_nc_authenticated")
		procHasNcServer = find("rc_has_nc_server")
		procIrcLogin = find("rc_irc_login")
		procSendIrcText = find("rc_send_irc_text")
		procExecute = find("rc_execute")
		procSetNickname = find("rc_set_nickname")
		procGetPlayers = find("rc_get_players")
		procOnMessage = find("rc_on_message")
		procOnIrcMessage = find("rc_on_irc_message")
		procOnServerData = find("rc_on_server_data")
	})
	return loadErr
}

// DLLPath returns the DLL path that will be (or was) loaded, if found.
func DLLPath() (string, error) {
	for _, p := range dllSearchPaths() {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("grclib.dll not found")
}

// bptrToString reads a NUL-terminated C string and returns an owned copy
// (decoupled from the DLL's memory, safe to keep after the buffer is released).
func bptrToString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*byte)(ptr) != 0; ptr = unsafe.Pointer(uintptr(ptr) + 1) {
		n++
	}
	// unsafe.Slice + string() copies the bytes into a fresh Go allocation.
	return string(unsafe.Slice(p, n))
}

// EventCallbacks lets callers subscribe to connection + chat/IRC/data events.
// Methods are invoked from the event-pump goroutine (during rc_process_events),
// so they must be non-blocking.
type EventCallbacks struct {
	Connected    func()
	Disconnected func(reason string)
	Message      func(text string)
	IrcMessage   func(channel, line string)
	ServerData   func(dataType, content string)
}

var (
	cbConnected    = syscall.NewCallback(connectedEntry)
	cbDisconnected = syscall.NewCallback(disconnectedEntry)
	cbMessage      = syscall.NewCallback(messageEntry)
	cbIrcMessage   = syscall.NewCallback(ircMessageEntry)
	cbServerData   = syscall.NewCallback(serverDataEntry)

	routeMu sync.Mutex
	routes  = map[Handle]*EventCallbacks{}
)

// connectedEntry is the C-callable shim for RC_OnConnected(user_data).
func connectedEntry(userData uintptr) uintptr {
	fire(userData, func(c *EventCallbacks) {
		if c.Connected != nil {
			c.Connected()
		}
	})
	return 0
}

// disconnectedEntry is the C-callable shim for RC_OnDisconnected(reason, user_data).
func disconnectedEntry(reason, userData uintptr) uintptr {
	msg := bptrToString((*byte)(unsafe.Pointer(reason)))
	fire(userData, func(c *EventCallbacks) {
		if c.Disconnected != nil {
			c.Disconnected(msg)
		}
	})
	return 0
}

// messageEntry is the C-callable shim for RC_OnMessage(message, user_data).
func messageEntry(message, userData uintptr) uintptr {
	msg := bptrToString((*byte)(unsafe.Pointer(message)))
	fire(userData, func(c *EventCallbacks) {
		if c.Message != nil {
			c.Message(msg)
		}
	})
	return 0
}

// ircMessageEntry is the C-callable shim for RC_OnIrcMessage(channel, line, user_data).
func ircMessageEntry(channel, line, userData uintptr) uintptr {
	ch := bptrToString((*byte)(unsafe.Pointer(channel)))
	ln := bptrToString((*byte)(unsafe.Pointer(line)))
	fire(userData, func(c *EventCallbacks) {
		if c.IrcMessage != nil {
			c.IrcMessage(ch, ln)
		}
	})
	return 0
}

// serverDataEntry is the C-callable shim for RC_OnServerData(data_type, content, user_data).
func serverDataEntry(dataType, content, userData uintptr) uintptr {
	dt := bptrToString((*byte)(unsafe.Pointer(dataType)))
	ct := bptrToString((*byte)(unsafe.Pointer(content)))
	fire(userData, func(c *EventCallbacks) {
		if c.ServerData != nil {
			c.ServerData(dt, ct)
		}
	})
	return 0
}

func fire(userData uintptr, dispatch func(*EventCallbacks)) {
	if cb := routeFor(Handle(userData)); cb != nil {
		dispatch(cb)
	}
}

func routeFor(h Handle) *EventCallbacks {
	routeMu.Lock()
	defer routeMu.Unlock()
	return routes[h]
}

// RegisterCallbacks subscribes the given callbacks to the handle's connection
// events. The handle is passed back as the C user_data so events route to the
// right callbacks (supports multiple concurrent handles).
func RegisterCallbacks(h Handle, cbs *EventCallbacks) {
	if err := load(); err != nil {
		return
	}
	routeMu.Lock()
	routes[h] = cbs
	routeMu.Unlock()
	procOnConnected.Call(uintptr(h), cbConnected, uintptr(h))
	procOnDisconnected.Call(uintptr(h), cbDisconnected, uintptr(h))
	procOnMessage.Call(uintptr(h), cbMessage, uintptr(h))
	procOnIrcMessage.Call(uintptr(h), cbIrcMessage, uintptr(h))
	procOnServerData.Call(uintptr(h), cbServerData, uintptr(h))
}

// UnregisterCallbacks detaches event callbacks for the handle.
func UnregisterCallbacks(h Handle) {
	if err := load(); err != nil {
		return
	}
	routeMu.Lock()
	delete(routes, h)
	routeMu.Unlock()
	procOnConnected.Call(uintptr(h), 0, 0)
	procOnDisconnected.Call(uintptr(h), 0, 0)
	procOnMessage.Call(uintptr(h), 0, 0)
	procOnIrcMessage.Call(uintptr(h), 0, 0)
	procOnServerData.Call(uintptr(h), 0, 0)
}

// ProcessEvents pumps queued connection callbacks once. Call regularly from a
// goroutine while a handle is active so events (on_connected/on_disconnected,
// etc.) are delivered.
func ProcessEvents(h Handle) {
	if err := load(); err != nil {
		return
	}
	procProcessEvents.Call(uintptr(h))
}

// LastError returns the last connection/API error string for a handle.
func LastError(h Handle) string {
	if err := load(); err != nil {
		return err.Error()
	}
	r1, _, _ := procLastError.Call(uintptr(h))
	if r1 == 0 {
		return ""
	}
	return bptrToString((*byte)(unsafe.Pointer(r1)))
}

// Connect connects to the listserver and stores the returned server list on the
// handle. Pass account/password as plain text; PCID is derived inside grclib.
func Connect(host string, port int, account, password string) (Handle, error) {
	if err := load(); err != nil {
		return 0, err
	}
	hostPtr, _ := syscall.BytePtrFromString(host)
	acctPtr, _ := syscall.BytePtrFromString(account)
	passPtr, _ := syscall.BytePtrFromString(password)

	r1, _, _ := procConnect.Call(
		uintptr(unsafe.Pointer(hostPtr)),
		uintptr(port),
		uintptr(unsafe.Pointer(acctPtr)),
		uintptr(unsafe.Pointer(passPtr)),
	)
	if r1 == 0 {
		// Handle is null, so rc_last_error is not usable; surface a generic
		// reason. (Account/password rejected or network unreachable.)
		return 0, errors.New("rc_connect returned null (check host/port/account/password)")
	}
	return Handle(r1), nil
}

// GetServers copies the cached server list from a connected handle into Go.
// Returns the parsed error string when the list is empty.
func GetServers(h Handle) ([]Server, error) {
	if err := load(); err != nil {
		return nil, err
	}
	var serversPtr uintptr
	r1, _, _ := procGetServers.Call(uintptr(h), uintptr(unsafe.Pointer(&serversPtr)))
	count := int(int32(r1))
	if count <= 0 || serversPtr == 0 {
		// No servers cached for this handle. This is not an error at this
		// layer (the listserver authenticated fine); callers decide what an
		// empty list means (e.g. the account is not staff anywhere).
		return nil, nil
	}
	arr := (*[1 << 20]RCServer)(unsafe.Pointer(serversPtr))[:count:count]

	out := make([]Server, count)
	for i := 0; i < count; i++ {
		s := arr[i]
		out[i] = Server{
			Name:        bptrToString(s.Name),
			IP:          bptrToString(s.IP),
			Port:        int(s.Port),
			Players:     int(s.Players),
			Language:    bptrToString(s.Language),
			Description: bptrToString(s.Description),
			Version:     bptrToString(s.Version),
			Homepage:    bptrToString(s.Homepage),
		}
	}
	return out, nil
}

// ConnectToServer authenticates to the server at the given list index.
func ConnectToServer(h Handle, index int) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procConnectToServer.Call(uintptr(h), uintptr(index))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// IsConnected reports whether the game socket is connected.
func IsConnected(h Handle) bool {
	if err := load(); err != nil {
		return false
	}
	r1, _, _ := procIsConnected.Call(uintptr(h))
	return r1 != 0
}

// IsAuthenticated reports whether the game socket finished login.
func IsAuthenticated(h Handle) bool {
	if err := load(); err != nil {
		return false
	}
	r1, _, _ := procIsAuthenticated.Call(uintptr(h))
	return r1 != 0
}

// SetNewProtocol toggles newer-protocol compatibility. Must be called after
// Connect and before ConnectToServer. enable=false targets older servers.
func SetNewProtocol(h Handle, enable bool) error {
	if err := load(); err != nil {
		return err
	}
	val := uintptr(0)
	if enable {
		val = 1
	}
	procSetNewProtocol.Call(uintptr(h), val)
	return nil
}

// IsNewProtocol reads the current newer-protocol compatibility flag.
func IsNewProtocol(h Handle) bool {
	if err := load(); err != nil {
		return true
	}
	r1, _, _ := procIsNewProtocol.Call(uintptr(h))
	return r1 != 0
}

// HasNCServer reports whether the connected server exposes an NC (script) socket.
func HasNCServer(h Handle) bool {
	if err := load(); err != nil {
		return false
	}
	r1, _, _ := procHasNcServer.Call(uintptr(h))
	return r1 != 0
}

// IsNCConnected reports whether the NC socket is connected.
func IsNCConnected(h Handle) bool {
	if err := load(); err != nil {
		return false
	}
	r1, _, _ := procIsNcConnected.Call(uintptr(h))
	return r1 != 0
}

// IsNCAuthenticated reports whether the NC socket finished login.
func IsNCAuthenticated(h Handle) bool {
	if err := load(); err != nil {
		return false
	}
	r1, _, _ := procIsNcAuthenticated.Call(uintptr(h))
	return r1 != 0
}

// ConnectToNCServer opens the NC (script) socket for the connected server.
func ConnectToNCServer(h Handle) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procConnectToNcServer.Call(uintptr(h))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// DisconnectNC closes the NC socket.
func DisconnectNC(h Handle) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procDisconnectNc.Call(uintptr(h))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// IrcLogin starts the IRC session for the handle (host is derived inside grclib).
func IrcLogin(h Handle) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procIrcLogin.Call(uintptr(h))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// SendIrcText sends a raw IRC command (command + up to 3 params) on the IRC socket.
func SendIrcText(h Handle, command, p1, p2, p3 string) error {
	if err := load(); err != nil {
		return err
	}
	cmd, _ := syscall.BytePtrFromString(command)
	a1, _ := syscall.BytePtrFromString(p1)
	a2, _ := syscall.BytePtrFromString(p2)
	a3, _ := syscall.BytePtrFromString(p3)
	r1, _, _ := procSendIrcText.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(cmd)),
		uintptr(unsafe.Pointer(a1)),
		uintptr(unsafe.Pointer(a2)),
		uintptr(unsafe.Pointer(a3)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// Execute sends a chat line or slash command to the game server.
func Execute(h Handle, message string) error {
	if err := load(); err != nil {
		return err
	}
	msg, _ := syscall.BytePtrFromString(message)
	r1, _, _ := procExecute.Call(uintptr(h), uintptr(unsafe.Pointer(msg)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// SetNickname changes the RC nickname shown on the server.
func SetNickname(h Handle, nickname string) error {
	if err := load(); err != nil {
		return err
	}
	nick, _ := syscall.BytePtrFromString(nickname)
	r1, _, _ := procSetNickname.Call(uintptr(h), uintptr(unsafe.Pointer(nick)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// GetPlayers copies the cached player list for the connected server into Go.
func GetPlayers(h Handle) ([]Player, error) {
	if err := load(); err != nil {
		return nil, err
	}
	var playersPtr uintptr
	r1, _, _ := procGetPlayers.Call(uintptr(h), uintptr(unsafe.Pointer(&playersPtr)))
	count := int(int32(r1))
	if count <= 0 || playersPtr == 0 {
		return nil, nil
	}
	arr := (*[1 << 20]RCPlayer)(unsafe.Pointer(playersPtr))[:count:count]

	out := make([]Player, count)
	for i := 0; i < count; i++ {
		p := arr[i]
		out[i] = Player{
			Account: bptrToString(p.Account),
			ID:      int(p.ID),
			Nick:    bptrToString(p.Nick),
			Level:   bptrToString(p.Level),
		}
	}
	return out, nil
}

// Disconnect closes all sockets and frees the handle.
func Disconnect(h Handle) {
	if err := load(); err != nil {
		return
	}
	procDisconnect.Call(uintptr(h))
}

// Free releases memory returned by grclib.
func Free(p uintptr) {
	if err := load(); err != nil {
		return
	}
	procFree.Call(p)
}
