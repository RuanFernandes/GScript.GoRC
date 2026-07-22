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
	"math"
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

// RCWeapon mirrors grclib's RCWeapon struct (include/grclib.h):
//
//	struct { char* name; char* image; char* script; }
//
// x64 layout: name@0, image@8, script@16 -> 24 bytes.
type RCWeapon struct {
	Name   *byte
	Image  *byte
	Script *byte
}

// Weapon is the Go-friendly copy of an RCWeapon entry (list view: image is no
// longer used by modern servers, so only the name is exposed; the script is
// fetched on demand via RequestWeaponScript).
type Weapon struct {
	Name string `json:"name"`
}

// RCClass mirrors grclib's RCClass struct (include/grclib.h):
//
//	struct { char* name; char* script; }
//
// x64 layout: name@0, script@8 -> 16 bytes.
type RCClass struct {
	Name   *byte
	Script *byte
}

// Class is the Go-friendly copy of an RCClass entry.
type Class struct {
	Name string `json:"name"`
}

// RCNPC mirrors grclib's RCNPC struct (include/grclib.h):
//
//	struct { int id; char* name; char* type; char* image; char* script; }
//
// x64 layout: id@0 (4 bytes), pad@4, name@8, type@16, image@24, script@32
// -> 40 bytes. The int32 pads to the next 8-byte boundary before name.
type RCNPC struct {
	ID    int32
	_     int32
	Name  *byte
	Type  *byte
	Image *byte
	_     *byte // script (unused in list view; fetched via RequestNPCScript)
}

// NPC is the Go-friendly copy of an RCNPC entry.
type NPC struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ScriptReply carries a fetched script / flags / attributes payload back to the
// caller. Type is "weapon" | "class" | "npc" | "npcflags" | "npcattr".
type ScriptReply struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	ID     int    `json:"id"`
	Script string `json:"script"`
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

	// Script management (NC server).
	procGetWeapons           *syscall.Proc
	procGetClasses           *syscall.Proc
	procGetNPCs              *syscall.Proc
	procAddWeapon            *syscall.Proc
	procDeleteWeapon         *syscall.Proc
	procUpdateWeapon         *syscall.Proc
	procAddClass             *syscall.Proc
	procDeleteClass          *syscall.Proc
	procUpdateClass          *syscall.Proc
	procDeleteNPC            *syscall.Proc
	procUpdateNPC            *syscall.Proc
	procCreateNPCOnServer    *syscall.Proc
	procRequestWeaponScript  *syscall.Proc
	procRequestClassScript   *syscall.Proc
	procRequestNPCScript     *syscall.Proc
	procResetNPC             *syscall.Proc
	procRequestNPCAttributes *syscall.Proc
	procGetNPCFlags          *syscall.Proc
	procSetNPCFlags          *syscall.Proc
	procSendNCPacket         *syscall.Proc
	procWarpNPC              *syscall.Proc

	// Script/NC event callbacks.
	procOnScriptReceived  *syscall.Proc
	procOnWeaponAdded     *syscall.Proc
	procOnWeaponDeleted   *syscall.Proc
	procOnClassAdded      *syscall.Proc
	procOnClassDeleted    *syscall.Proc
	procOnNPCAdded        *syscall.Proc
	procOnNPCDeleted      *syscall.Proc
	procOnNPCFlags        *syscall.Proc
	procOnNPCAttributes   *syscall.Proc
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
		procGetWeapons = find("rc_get_weapons")
		procGetClasses = find("rc_get_classes")
		procGetNPCs = find("rc_get_npcs")
		procAddWeapon = find("rc_add_weapon")
		procDeleteWeapon = find("rc_delete_weapon")
		procUpdateWeapon = find("rc_update_weapon")
		procAddClass = find("rc_add_class")
		procDeleteClass = find("rc_delete_class")
		procUpdateClass = find("rc_update_class")
		procDeleteNPC = find("rc_delete_npc")
		procUpdateNPC = find("rc_update_npc")
		procCreateNPCOnServer = find("rc_create_npc_on_server")
		procRequestWeaponScript = find("rc_request_weapon_script")
		procRequestClassScript = find("rc_request_class_script")
		procRequestNPCScript = find("rc_request_npc_script")
		procResetNPC = find("rc_reset_npc")
		procRequestNPCAttributes = find("rc_request_npc_attributes")
		procGetNPCFlags = find("rc_get_npc_flags")
		procSetNPCFlags = find("rc_set_npc_flags")
		procSendNCPacket = find("rc_send_nc_packet")
		procWarpNPC = find("rc_warp_npc")
		procOnScriptReceived = find("rc_on_script_received")
		procOnWeaponAdded = find("rc_on_weapon_added")
		procOnWeaponDeleted = find("rc_on_weapon_deleted")
		procOnClassAdded = find("rc_on_class_added")
		procOnClassDeleted = find("rc_on_class_deleted")
		procOnNPCAdded = find("rc_on_npc_added")
		procOnNPCDeleted = find("rc_on_npc_deleted")
		procOnNPCFlags = find("rc_on_npc_flags")
		procOnNPCAttributes = find("rc_on_npc_attributes")
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

	// Script/NC callbacks (fired on the pump goroutine).
	ScriptReceived func(scriptType, name string, id int, script string)
	WeaponChanged  func(name string) // weapon added or deleted
	ClassChanged   func(name string) // class added or deleted
	NPCChanged     func(id int)      // npc added or deleted
	NPCFlags       func(id int, flags string)
	NPCAttributes  func(id int, attrs string)
}

var (
	cbConnected    = syscall.NewCallback(connectedEntry)
	cbDisconnected = syscall.NewCallback(disconnectedEntry)
	cbMessage      = syscall.NewCallback(messageEntry)
	cbIrcMessage   = syscall.NewCallback(ircMessageEntry)
	cbServerData   = syscall.NewCallback(serverDataEntry)

	cbScriptReceived = syscall.NewCallback(scriptReceivedEntry)
	cbWeaponAdded    = syscall.NewCallback(weaponCacheChangedEntry)
	cbWeaponDeleted  = syscall.NewCallback(weaponCacheChangedEntry)
	cbClassAdded     = syscall.NewCallback(classCacheChangedEntry)
	cbClassDeleted   = syscall.NewCallback(classCacheChangedEntry)
	cbNPCAdded       = syscall.NewCallback(npcAddedEntry)
	cbNPCDeleted     = syscall.NewCallback(npcDeletedEntry)
	cbNPCFlags       = syscall.NewCallback(npcFlagsEntry)
	cbNPCAttributes  = syscall.NewCallback(npcAttributesEntry)

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

// scriptReceivedEntry is the shim for RC_OnScriptReceived(script_type, name, id, script, user_data).
func scriptReceivedEntry(scriptType, name, id, script, userData uintptr) uintptr {
	st := bptrToString((*byte)(unsafe.Pointer(scriptType)))
	nm := bptrToString((*byte)(unsafe.Pointer(name)))
	sc := bptrToString((*byte)(unsafe.Pointer(script)))
	fire(userData, func(c *EventCallbacks) {
		if c.ScriptReceived != nil {
			c.ScriptReceived(st, nm, int(int32(id)), sc)
		}
	})
	return 0
}

// weaponCacheChangedEntry is the shim for RC_OnWeaponAdded/RC_OnWeaponDeleted(name, user_data).
func weaponCacheChangedEntry(name, userData uintptr) uintptr {
	nm := bptrToString((*byte)(unsafe.Pointer(name)))
	fire(userData, func(c *EventCallbacks) {
		if c.WeaponChanged != nil {
			c.WeaponChanged(nm)
		}
	})
	return 0
}

// classCacheChangedEntry is the shim for RC_OnClassAdded/RC_OnClassDeleted(name, user_data).
func classCacheChangedEntry(name, userData uintptr) uintptr {
	nm := bptrToString((*byte)(unsafe.Pointer(name)))
	fire(userData, func(c *EventCallbacks) {
		if c.ClassChanged != nil {
			c.ClassChanged(nm)
		}
	})
	return 0
}

// npcAddedEntry is the shim for RC_OnNPCAdded(id, name, user_data).
func npcAddedEntry(id, _, userData uintptr) uintptr {
	fire(userData, func(c *EventCallbacks) {
		if c.NPCChanged != nil {
			c.NPCChanged(int(int32(id)))
		}
	})
	return 0
}

// npcDeletedEntry is the shim for RC_OnNPCDeleted(id, user_data).
func npcDeletedEntry(id, userData uintptr) uintptr {
	fire(userData, func(c *EventCallbacks) {
		if c.NPCChanged != nil {
			c.NPCChanged(int(int32(id)))
		}
	})
	return 0
}

// npcFlagsEntry is the shim for RC_OnNPCFlags(npc_id, flags, user_data).
func npcFlagsEntry(npcID, flags, userData uintptr) uintptr {
	fl := bptrToString((*byte)(unsafe.Pointer(flags)))
	fire(userData, func(c *EventCallbacks) {
		if c.NPCFlags != nil {
			c.NPCFlags(int(int32(npcID)), fl)
		}
	})
	return 0
}

// npcAttributesEntry is the shim for RC_OnNPCAttributes(npc_id, attributes, user_data).
func npcAttributesEntry(npcID, attrs, userData uintptr) uintptr {
	at := bptrToString((*byte)(unsafe.Pointer(attrs)))
	fire(userData, func(c *EventCallbacks) {
		if c.NPCAttributes != nil {
			c.NPCAttributes(int(int32(npcID)), at)
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
	procOnScriptReceived.Call(uintptr(h), cbScriptReceived, uintptr(h))
	procOnWeaponAdded.Call(uintptr(h), cbWeaponAdded, uintptr(h))
	procOnWeaponDeleted.Call(uintptr(h), cbWeaponDeleted, uintptr(h))
	procOnClassAdded.Call(uintptr(h), cbClassAdded, uintptr(h))
	procOnClassDeleted.Call(uintptr(h), cbClassDeleted, uintptr(h))
	procOnNPCAdded.Call(uintptr(h), cbNPCAdded, uintptr(h))
	procOnNPCDeleted.Call(uintptr(h), cbNPCDeleted, uintptr(h))
	procOnNPCFlags.Call(uintptr(h), cbNPCFlags, uintptr(h))
	procOnNPCAttributes.Call(uintptr(h), cbNPCAttributes, uintptr(h))
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
	procOnScriptReceived.Call(uintptr(h), 0, 0)
	procOnWeaponAdded.Call(uintptr(h), 0, 0)
	procOnWeaponDeleted.Call(uintptr(h), 0, 0)
	procOnClassAdded.Call(uintptr(h), 0, 0)
	procOnClassDeleted.Call(uintptr(h), 0, 0)
	procOnNPCAdded.Call(uintptr(h), 0, 0)
	procOnNPCDeleted.Call(uintptr(h), 0, 0)
	procOnNPCFlags.Call(uintptr(h), 0, 0)
	procOnNPCAttributes.Call(uintptr(h), 0, 0)
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

// GetWeapons copies the cached weapon list for the NC server into Go.
func GetWeapons(h Handle) ([]Weapon, error) {
	if err := load(); err != nil {
		return nil, err
	}
	var ptr uintptr
	r1, _, _ := procGetWeapons.Call(uintptr(h), uintptr(unsafe.Pointer(&ptr)))
	count := int(int32(r1))
	if count <= 0 || ptr == 0 {
		return nil, nil
	}
	arr := (*[1 << 20]RCWeapon)(unsafe.Pointer(ptr))[:count:count]
	out := make([]Weapon, count)
	for i := 0; i < count; i++ {
		out[i] = Weapon{Name: bptrToString(arr[i].Name)}
	}
	return out, nil
}

// GetClasses copies the cached class list for the NC server into Go.
func GetClasses(h Handle) ([]Class, error) {
	if err := load(); err != nil {
		return nil, err
	}
	var ptr uintptr
	r1, _, _ := procGetClasses.Call(uintptr(h), uintptr(unsafe.Pointer(&ptr)))
	count := int(int32(r1))
	if count <= 0 || ptr == 0 {
		return nil, nil
	}
	arr := (*[1 << 20]RCClass)(unsafe.Pointer(ptr))[:count:count]
	out := make([]Class, count)
	for i := 0; i < count; i++ {
		out[i] = Class{Name: bptrToString(arr[i].Name)}
	}
	return out, nil
}

// GetNPCs copies the cached NPC list for the NC server into Go.
func GetNPCs(h Handle) ([]NPC, error) {
	if err := load(); err != nil {
		return nil, err
	}
	var ptr uintptr
	r1, _, _ := procGetNPCs.Call(uintptr(h), uintptr(unsafe.Pointer(&ptr)))
	count := int(int32(r1))
	if count <= 0 || ptr == 0 {
		return nil, nil
	}
	arr := (*[1 << 20]RCNPC)(unsafe.Pointer(ptr))[:count:count]
	out := make([]NPC, count)
	for i := 0; i < count; i++ {
		n := arr[i]
		out[i] = NPC{ID: int(n.ID), Name: bptrToString(n.Name), Type: bptrToString(n.Type)}
	}
	return out, nil
}

// AddWeapon creates a weapon by name (empty image + script).
func AddWeapon(h Handle, name string) error {
	return callStr3(h, procAddWeapon, name, "", "")
}

// DeleteWeapon deletes a weapon by name.
func DeleteWeapon(h Handle, name string) error {
	return callStr3(h, procDeleteWeapon, name, "", "")
}

// UpdateWeapon writes a weapon's script back (upsert, same as add). Image is no
// longer used by modern servers, so it is sent empty.
func UpdateWeapon(h Handle, name, script string) error {
	return callStr3(h, procUpdateWeapon, name, "", script)
}

// AddClass creates a class by name (empty script).
func AddClass(h Handle, name string) error {
	return callStr2(h, procAddClass, name, "")
}

// DeleteClass deletes a class by name.
func DeleteClass(h Handle, name string) error {
	return callStr2(h, procDeleteClass, name, "")
}

// UpdateClass writes a class's script back (upsert, same as add).
func UpdateClass(h Handle, name, script string) error {
	return callStr2(h, procUpdateClass, name, script)
}

// DeleteNPC deletes an NPC by id.
func DeleteNPC(h Handle, id int) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procDeleteNPC.Call(uintptr(h), uintptr(id))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// UpdateNPC writes an NPC's script back.
func UpdateNPC(h Handle, id int, script string) error {
	if err := load(); err != nil {
		return err
	}
	s, _ := syscall.BytePtrFromString(script)
	r1, _, _ := procUpdateNPC.Call(uintptr(h), uintptr(id), uintptr(unsafe.Pointer(s)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// CreateNPC creates a new DB NPC on the server (7 fields, mirrors
// rc_create_npc_on_server).
func CreateNPC(h Handle, name string, id int, npcType, scripter, level, x, y string) error {
	if err := load(); err != nil {
		return err
	}
	n, _ := syscall.BytePtrFromString(name)
	t, _ := syscall.BytePtrFromString(npcType)
	sc, _ := syscall.BytePtrFromString(scripter)
	lv, _ := syscall.BytePtrFromString(level)
	xp, _ := syscall.BytePtrFromString(x)
	yp, _ := syscall.BytePtrFromString(y)
	r1, _, _ := procCreateNPCOnServer.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(n)),
		uintptr(id),
		uintptr(unsafe.Pointer(t)),
		uintptr(unsafe.Pointer(sc)),
		uintptr(unsafe.Pointer(lv)),
		uintptr(unsafe.Pointer(xp)),
		uintptr(unsafe.Pointer(yp)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// RequestWeaponScript asks the server for a weapon's script; the reply arrives
// asynchronously via the ScriptReceived callback.
func RequestWeaponScript(h Handle, name string) error { return callStr1(h, procRequestWeaponScript, name) }

// RequestClassScript asks the server for a class's script.
func RequestClassScript(h Handle, name string) error { return callStr1(h, procRequestClassScript, name) }

// RequestNPCScript asks the server for an NPC's script.
func RequestNPCScript(h Handle, id int) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procRequestNPCScript.Call(uintptr(h), uintptr(id))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// ResetNPC resets an NPC by id.
func ResetNPC(h Handle, id int) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procResetNPC.Call(uintptr(h), uintptr(id))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// RequestNPCAttributes asks the server for an NPC's attributes; the reply arrives
// via the NPCAttributes callback.
func RequestNPCAttributes(h Handle, id int) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procRequestNPCAttributes.Call(uintptr(h), uintptr(id))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// GetNPCFlags asks the server for an NPC's flags; the reply arrives via the
// NPCFlags callback. (Despite the "get" name, the result is delivered
// asynchronously.)
func GetNPCFlags(h Handle, id int) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procGetNPCFlags.Call(uintptr(h), uintptr(id))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// SetNPCFlags writes an NPC's flags.
func SetNPCFlags(h Handle, id int, flags string) error {
	if err := load(); err != nil {
		return err
	}
	f, _ := syscall.BytePtrFromString(flags)
	r1, _, _ := procSetNPCFlags.Call(uintptr(h), uintptr(id), uintptr(unsafe.Pointer(f)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// WarpNPC warps an NPC to (x, y) on the given level. x/y are server tile coords
// (float, since rc_warp_npc takes C float).
func WarpNPC(h Handle, id int, x, y float64, level string) error {
	if err := load(); err != nil {
		return err
	}
	lvl, _ := syscall.BytePtrFromString(level)
	r1, _, _ := procWarpNPC.Call(
		uintptr(h),
		uintptr(id),
		uintptr(math.Float32bits(float32(x))),
		uintptr(math.Float32bits(float32(y))),
		uintptr(unsafe.Pointer(lvl)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// SendNCPacket sends a raw NC packet (used to re-request the weapon list with
// packet id 115, PLI_NC_WEAPONLISTGET, since grclib only sends it once at auth).
func SendNCPacket(h Handle, packetID int) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := procSendNCPacket.Call(uintptr(h), uintptr(packetID), 0, 0)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// callStr1 calls a (handle, const char*) DLL function returning int (0 = error).
func callStr1(h Handle, p *syscall.Proc, a string) error {
	if err := load(); err != nil {
		return err
	}
	ptr, _ := syscall.BytePtrFromString(a)
	r1, _, _ := p.Call(uintptr(h), uintptr(unsafe.Pointer(ptr)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// callStr2 calls a (handle, const char*, const char*) DLL function returning int.
func callStr2(h Handle, p *syscall.Proc, a, b string) error {
	if err := load(); err != nil {
		return err
	}
	pa, _ := syscall.BytePtrFromString(a)
	pb, _ := syscall.BytePtrFromString(b)
	r1, _, _ := p.Call(uintptr(h), uintptr(unsafe.Pointer(pa)), uintptr(unsafe.Pointer(pb)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// callStr3 calls a (handle, const char*, const char*, const char*) DLL function returning int.
func callStr3(h Handle, p *syscall.Proc, a, b, c string) error {
	if err := load(); err != nil {
		return err
	}
	pa, _ := syscall.BytePtrFromString(a)
	pb, _ := syscall.BytePtrFromString(b)
	pc, _ := syscall.BytePtrFromString(c)
	r1, _, _ := p.Call(uintptr(h), uintptr(unsafe.Pointer(pa)), uintptr(unsafe.Pointer(pb)), uintptr(unsafe.Pointer(pc)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}
