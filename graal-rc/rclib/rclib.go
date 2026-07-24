// Package rclib binds the grclib native library (C ABI) without cgo: on Windows
// it loads the DLL via the syscall package; on Linux/macOS it dlopen's the .so /
// .dylib via github.com/ebitengine/purego (pure-Go) and invokes resolved
// symbols through purego.SyscallN. Either way the project builds with
// CGO_ENABLED=0.
//
// The native file is selected by GOOS: grclib64.dll (Windows), grclib.so
// (Linux), grclib.dylib (macOS) — one amd64 lib per desktop target, shipped
// committed under rclib/.
//
// Struct and signatures mirror how the reference C++ client consumes the
// library: rc_connect -> rc_get_servers -> rc_connect_to_server.
package rclib

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
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
//	struct { int id; char* name; char* type; char* image; char* script; char* level; }
//
// x64 layout: id@0 (4 bytes), pad@4, name@8, type@16, image@24, script@32,
// level@40 -> 48 bytes. The int32 pads to the next 8-byte boundary before name.
// The trailing level field MUST stay in lockstep with the C struct or the array
// stride drifts and pointer reads segfault.
type RCNPC struct {
	ID    int32
	_     int32
	Name  *byte
	Type  *byte
	Image *byte
	_     *byte // script (unused in list view; fetched via RequestNPCScript)
	Level *byte
}

// NPC is the Go-friendly copy of an RCNPC entry.
type NPC struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Type  string `json:"type"`
	Level string `json:"level"`
}

// RCFileBrowserFolder mirrors grclib's RCFileBrowserFolder struct (include/grclib.h):
//
//	struct { char* rights; char* pattern; }
//
// x64 layout: rights@0, pattern@8 -> 16 bytes. Note the field order is
// rights-then-pattern (the header declares them in that order).
type RCFileBrowserFolder struct {
	Rights  *byte
	Pattern *byte
}

// FileBrowserFolder is the Go-friendly copy of an RCFileBrowserFolder entry.
type FileBrowserFolder struct {
	Pattern string `json:"pattern"`
	Rights  string `json:"rights"`
}

// RCFileBrowserEntry mirrors grclib's RCFileBrowserEntry struct (include/grclib.h):
//
//	struct { char* path; char* rights; int size; int modified; int is_directory; }
//
// x64 layout: path@0, rights@8, size@16, modified@20, is_directory@24,
// pad@28 -> 32 bytes. The trailing int32 pad aligns the array stride (each
// entry is 32 bytes) so iterating the C array does not drift.
type RCFileBrowserEntry struct {
	Path        *byte
	Rights      *byte
	Size        int32
	Modified    int32
	IsDirectory int32
	_           int32 // pad to 32 bytes (array stride)
}

// FileBrowserEntry is the Go-friendly copy of an RCFileBrowserEntry entry.
type FileBrowserEntry struct {
	Path        string `json:"path"`
	Rights      string `json:"rights"`
	Size        int    `json:"size"`
	Modified    int    `json:"modified"`
	IsDirectory bool   `json:"isDirectory"`
}

// ScriptReply carries a fetched script / flags / attributes payload back to the
// caller. Type is "weapon" | "class" | "npc" | "npcflags" | "npcattr".
type ScriptReply struct {
	Type   string `json:"type"`
	Name   string `json:"name"`
	ID     int    `json:"id"`
	Script string `json:"script"`
}

// dllMu serializes every grclib call. The reference C++ client is single-threaded
// (GTK main loop + one 50ms timer), so only one external caller is ever inside
// grclib at a time. The Go port fans calls across the event-pump goroutine plus N
// Wails goroutines; without serialization two threads re-enter grclib and corrupt
// shared socket/buffer state (observed as the NC socket dropping right after the
// first weapon-script open). The mutex collapses all Go callers back to one.
// (grclib's internal nc_recv_thread is managed by the DLL itself and is unaffected.)
var dllMu sync.Mutex

var (
	once    sync.Once
	loadErr error

	procConnect         *proc
	procGetServers      *proc
	procConnectToServer *proc
	procDisconnect      *proc
	procLastError       *proc
	procIsConnected     *proc
	procIsAuthenticated *proc
	procSetNewProtocol  *proc
	procIsNewProtocol   *proc
	procFree            *proc
	procProcessEvents   *proc
	procOnConnected     *proc
	procOnDisconnected  *proc

	procConnectToNcServer *proc
	procDisconnectNc      *proc
	procIsNcConnected     *proc
	procIsNcAuthenticated *proc
	procHasNcServer       *proc
	procIrcLogin          *proc
	procSendIrcText       *proc
	procExecute           *proc
	procSetNickname       *proc
	procGetPlayers         *proc
	procOnMessage          *proc
	procOnIrcMessage       *proc
	procOnServerData       *proc
	procOnPrivateMessage   *proc
	procSendPrivateMessage *proc
	procSendMassPM         *proc
	procSendAdminMessage   *proc
	procSendAdminMessageAll *proc

	// Player admin editors (rights / attributes / bans) on the main server.
	procRequestPlayerRights       *proc
	procSetPlayerRights           *proc
	procRequestPlayerAttrs        *proc
	procSetPlayerAttributes       *proc
	procParsePlayerAttributesText *proc
	procRequestPlayerBan          *proc
	procRequestPlayerBanByAccount *proc
	procRequestBanTypes           *proc
	procRequestBanHistory         *proc
	procRequestStaffActivity      *proc
	procSetBan                    *proc
	procOnPlayerRights            *proc
	procOnPlayerAttributes        *proc
	procOnBanData                 *proc
	procOnBanListData             *proc
	procRequestPlayerComments     *proc
	procSetPlayerComments         *proc
	procOnPlayerTextData          *proc

	// Script management (NC server).
	procGetWeapons           *proc
	procGetClasses           *proc
	procGetNPCs              *proc
	procAddWeapon            *proc
	procDeleteWeapon         *proc
	procUpdateWeapon         *proc
	procAddClass             *proc
	procDeleteClass          *proc
	procUpdateClass          *proc
	procDeleteNPC            *proc
	procUpdateNPC            *proc
	procCreateNPCOnServer    *proc
	procRequestWeaponScript  *proc
	procRequestClassScript   *proc
	procRequestNPCScript     *proc
	procResetNPC             *proc
	procRequestNPCAttributes *proc
	procGetNPCFlags          *proc
	procSetNPCFlags          *proc
	procSendNCPacket         *proc
	procWarpNPC              *proc

	// Server-side text configs (server options / folder config / server flags).
	// Content arrives via on_server_data ("options"/"folder_config"/"flags").
	procRequestServerOptions *proc
	procRequestFolderConfig  *proc
	procRequestServerFlags   *proc
	procUploadServerOptions  *proc
	procUploadFolderConfig   *proc
	procUploadServerFlags    *proc

	// Script/NC event callbacks.
	procOnScriptReceived *proc
	procOnWeaponAdded    *proc
	procOnWeaponDeleted  *proc
	procOnClassAdded     *proc
	procOnClassDeleted   *proc
	procOnNPCAdded       *proc
	procOnNPCDeleted     *proc
	procOnNPCFlags       *proc
	procOnNPCAttributes  *proc

	// File browser (main server socket).
	procFileBrowserStart        *proc
	procFileBrowserCd           *proc
	procFileBrowserDownload     *proc
	procFileBrowserDelete       *proc
	procFileBrowserRename       *proc
	procFileBrowserMove         *proc
	procUploadFile              *proc
	procGetMaxUploadFileSize    *proc
	procCopyFileBrowserFolders  *proc
	procFreeFileBrowserFolders  *proc
	procCopyFileBrowserFiles    *proc
	procFreeFileBrowserFiles    *proc
	procOnFileBrowserFolders    *proc
	procOnFileBrowserFiles      *proc
	procOnFileBrowserMessage    *proc
	procOnFileReceived          *proc
	procOnMaxUploadFileSize     *proc
)

// Default listserver endpoint used by the reference client.
const (
	DefaultListserverHost = "listserver.graalonline.com"
	DefaultListserverPort = 14922
)

// proc wraps a resolved grclib symbol so call sites stay identical across OSes.
// The concrete fields and the Call method are defined per-OS in
// rclib_windows.go (syscall.Proc) and rclib_unix.go (purego.Dlsym address), so
// this shared file never references Windows-only syscall types.

// arg returns a[i] or 0 when out of range, so the unix SyscallN calls always
// get a full argument slot regardless of how many were passed.
func arg(a []uintptr, i int) uintptr {
	if i >= 0 && i < len(a) {
		return a[i]
	}
	return 0
}

// libFileName returns the committed native library filename for the current
// GOOS. All desktop targets are amd64; the lib bitness must match the process
// bitness, so each platform ships exactly one lib (selected here). macOS uses
// the .dylib extension (purego.Dlopen/dlsym load it the same as a .so).
func libFileName() string {
	switch runtime.GOOS {
	case "windows":
		return "grclib64.dll"
	case "darwin":
		return "grclib.dylib"
	default: // linux and other ELF targets
		return "grclib.so"
	}
}

// libSearchPaths returns candidate locations for the native library. It checks
// the cwd and executable directory, then walks every parent of each looking for
// a "rclib/<lib>" sibling. This finds the lib during `wails dev` (cwd at the
// project root) and when running the built binary from build/bin (lib several
// levels up at <repo>/rclib/<lib>).
func libSearchPaths() []string {
	name := libFileName()

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
		add(filepath.Join(root, name))
		add(filepath.Join(root, "rclib", name))
		// Walk parents: <root>/.., <root>/../.., ... looking for rclib/<lib>.
		dir := root
		for i := 0; i < 8; i++ {
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			add(filepath.Join(parent, "rclib", name))
			add(filepath.Join(parent, name))
			dir = parent
		}
	}
	return paths
}

// load resolves and loads the native library exactly once. Subsequent calls
// return the cached error (nil on success). The OS-specific loadProcs (see
// rclib_windows.go / rclib_unix.go) open the library and register every proc.
func load() error {
	once.Do(func() {
		var libPath string
		for _, p := range libSearchPaths() {
			if _, err := os.Stat(p); err == nil {
				libPath = p
				break
			}
		}
		if libPath == "" {
			loadErr = fmt.Errorf("%s not found; searched %v", libFileName(), libSearchPaths())
			return
		}
		loadErr = loadProcs(libPath)
	})
	return loadErr
}

// registerAll resolves every grclib proc via resolve and assigns the package
// vars, returning the first missing-symbol error. Shared by both OS loaders so
// the proc list lives in one place.
func registerAll(resolve func(name string) (*proc, error)) error {
	var firstErr error
	get := func(name string) *proc {
		p, err := resolve(name)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return p
	}
	procConnect = get("rc_connect")
	procGetServers = get("rc_get_servers")
	procConnectToServer = get("rc_connect_to_server")
	procDisconnect = get("rc_disconnect")
	procLastError = get("rc_last_error")
	procIsConnected = get("rc_is_connected")
	procIsAuthenticated = get("rc_is_authenticated")
	procSetNewProtocol = get("rc_set_new_protocol")
	procIsNewProtocol = get("rc_is_new_protocol")
	procFree = get("rc_free")
	procProcessEvents = get("rc_process_events")
	procOnConnected = get("rc_on_connected")
	procOnDisconnected = get("rc_on_disconnected")
	procConnectToNcServer = get("rc_connect_to_nc_server")
	procDisconnectNc = get("rc_disconnect_nc")
	procIsNcConnected = get("rc_is_nc_connected")
	procIsNcAuthenticated = get("rc_is_nc_authenticated")
	procHasNcServer = get("rc_has_nc_server")
	procIrcLogin = get("rc_irc_login")
	procSendIrcText = get("rc_send_irc_text")
	procExecute = get("rc_execute")
	procSetNickname = get("rc_set_nickname")
	procGetPlayers = get("rc_get_players")
	procOnMessage = get("rc_on_message")
	procOnIrcMessage = get("rc_on_irc_message")
	procOnServerData = get("rc_on_server_data")
	procOnPrivateMessage = get("rc_on_private_message")
	procSendPrivateMessage = get("rc_send_private_message")
	procSendMassPM = get("rc_send_mass_pm")
	procSendAdminMessage = get("rc_send_admin_message")
	procSendAdminMessageAll = get("rc_send_admin_message_all")
	procRequestPlayerRights = get("rc_request_player_rights")
	procSetPlayerRights = get("rc_set_player_rights")
	procRequestPlayerAttrs = get("rc_request_player_attrs")
	procSetPlayerAttributes = get("rc_set_player_attributes")
	procParsePlayerAttributesText = get("rc_parse_player_attributes_text")
	procRequestPlayerBan = get("rc_request_player_ban")
	procRequestPlayerBanByAccount = get("rc_request_player_ban_by_account")
	procRequestBanTypes = get("rc_request_ban_types")
	procRequestBanHistory = get("rc_request_ban_history")
	procRequestStaffActivity = get("rc_request_staff_activity")
	procSetBan = get("rc_set_ban")
	procOnPlayerRights = get("rc_on_player_rights")
	procOnPlayerAttributes = get("rc_on_player_attributes")
	procOnBanData = get("rc_on_ban_data")
	procOnBanListData = get("rc_on_ban_list_data")
	procRequestPlayerComments = get("rc_request_player_comments")
	procSetPlayerComments = get("rc_set_player_comments")
	procOnPlayerTextData = get("rc_on_player_text_data")
	procGetWeapons = get("rc_get_weapons")
	procGetClasses = get("rc_get_classes")
	procGetNPCs = get("rc_get_npcs")
	procAddWeapon = get("rc_add_weapon")
	procDeleteWeapon = get("rc_delete_weapon")
	procUpdateWeapon = get("rc_update_weapon")
	procAddClass = get("rc_add_class")
	procDeleteClass = get("rc_delete_class")
	procUpdateClass = get("rc_update_class")
	procDeleteNPC = get("rc_delete_npc")
	procUpdateNPC = get("rc_update_npc")
	procCreateNPCOnServer = get("rc_create_npc_on_server")
	procRequestWeaponScript = get("rc_request_weapon_script")
	procRequestClassScript = get("rc_request_class_script")
	procRequestNPCScript = get("rc_request_npc_script")
	procResetNPC = get("rc_reset_npc")
	procRequestNPCAttributes = get("rc_request_npc_attributes")
	procGetNPCFlags = get("rc_get_npc_flags")
	procSetNPCFlags = get("rc_set_npc_flags")
	procSendNCPacket = get("rc_send_nc_packet")
	procWarpNPC = get("rc_warp_npc")
	procRequestServerOptions = get("rc_request_server_options")
	procRequestFolderConfig = get("rc_request_folder_config")
	procRequestServerFlags = get("rc_request_server_flags")
	procUploadServerOptions = get("rc_upload_server_options")
	procUploadFolderConfig = get("rc_upload_folder_config")
	procUploadServerFlags = get("rc_upload_server_flags")
	procOnScriptReceived = get("rc_on_script_received")
	procOnWeaponAdded = get("rc_on_weapon_added")
	procOnWeaponDeleted = get("rc_on_weapon_deleted")
	procOnClassAdded = get("rc_on_class_added")
	procOnClassDeleted = get("rc_on_class_deleted")
	procOnNPCAdded = get("rc_on_npc_added")
	procOnNPCDeleted = get("rc_on_npc_deleted")
	procOnNPCFlags = get("rc_on_npc_flags")
	procOnNPCAttributes = get("rc_on_npc_attributes")
	procFileBrowserStart = get("rc_filebrowser_start")
	procFileBrowserCd = get("rc_filebrowser_cd")
	procFileBrowserDownload = get("rc_filebrowser_download")
	procFileBrowserDelete = get("rc_filebrowser_delete")
	procFileBrowserRename = get("rc_filebrowser_rename")
	procFileBrowserMove = get("rc_filebrowser_move")
	procUploadFile = get("rc_upload_file")
	procGetMaxUploadFileSize = get("rc_get_max_upload_file_size")
	procCopyFileBrowserFolders = get("rc_copy_filebrowser_folders")
	procFreeFileBrowserFolders = get("rc_free_filebrowser_folders")
	procCopyFileBrowserFiles = get("rc_copy_filebrowser_files")
	procFreeFileBrowserFiles = get("rc_free_filebrowser_files")
	procOnFileBrowserFolders = get("rc_on_filebrowser_folders")
	procOnFileBrowserFiles = get("rc_on_filebrowser_files")
	procOnFileBrowserMessage = get("rc_on_filebrowser_message")
	procOnFileReceived = get("rc_on_file_received")
	procOnMaxUploadFileSize = get("rc_on_max_upload_file_size")
	return firstErr
}

// DLLPath returns the native library path that will be (or was) loaded, if
// found. Name retained for callers (app.go/service.go use rclib.DLLPath); it is
// no longer Windows-specific.
func DLLPath() (string, error) {
	for _, p := range libSearchPaths() {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New(libFileName() + " not found")
}

// maxString caps bptrToString's NUL scan. A non-terminated or corrupt C pointer
// would otherwise walk unmapped memory until it segfaults (which, in a
// windowsgui release build, takes the whole process down with no trace). 16 MiB
// is far above any legitimate grclib string (scripts/configs are KiB-range).
const maxString = 16 << 20

// safeRecover logs a recovered panic from a grclib callback shim instead of
// letting it kill the event-pump goroutine (which would silently drop the NC
// socket and all chat). Mirrors the try/catch around each event in grclib's
// rc_process_events (grclib.cpp ~5042).
func safeRecover(label string) {
	if r := recover(); r != nil {
		log.Printf("[rclib] panic in %s: %v\n%s", label, r, debug.Stack())
	}
}

// bptrToString reads a NUL-terminated C string and returns an owned copy
// (decoupled from the DLL's memory, safe to keep after the buffer is released).
// The scan is capped at maxString so a corrupt/unterminated pointer cannot walk
// unmapped memory into a segfault.
func bptrToString(p *byte) string {
	if p == nil {
		return ""
	}
	n := 0
	for ptr := unsafe.Pointer(p); *(*byte)(ptr) != 0; ptr = unsafe.Pointer(uintptr(ptr) + 1) {
		n++
		if n >= maxString {
			log.Printf("[rclib] bptrToString hit %d-byte cap (possible corrupt C string)", maxString)
			break
		}
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
	PrivateMessage func(playerID int, account, nick, message string)

	// Script/NC callbacks (fired on the pump goroutine).
	ScriptReceived func(scriptType, name string, id int, script string)
	WeaponChanged  func(name string) // weapon added or deleted
	ClassChanged   func(name string) // class added or deleted
	NPCChanged     func(id int)      // npc added or deleted
	NPCFlags       func(id int, flags string)
	NPCAttributes  func(id int, attrs string)

	// File browser callbacks (main server socket). The folders/files callbacks
	// only signal readiness with a count; the app snapshots via CopyFileBrowser*.
	FileBrowserFolders func(count int)
	FileBrowserFiles   func(folder string, count int)
	FileBrowserMessage func(message string)
	FileReceived       func(path string, content []byte)
	MaxUploadSize      func(maxSize int64)

	// Player admin editor callbacks (fired on the pump goroutine).
	PlayerRights     func(account string, rights int, ipRange, folderAccess string)
	PlayerAttributes func(account, propertiesJSON, editorText string)
	BanData          func(account, computerID, details string)
	BanListData      func(dataType, account, content string)
	PlayerTextData   func(dataType, account, content string)
}

var (
	cbConnected    = newCallback(connectedEntry)
	cbDisconnected = newCallback(disconnectedEntry)
	cbMessage      = newCallback(messageEntry)
	cbIrcMessage   = newCallback(ircMessageEntry)
	cbServerData   = newCallback(serverDataEntry)
	cbPrivateMessage = newCallback(privateMessageEntry)

	cbScriptReceived = newCallback(scriptReceivedEntry)
	cbWeaponAdded    = newCallback(weaponCacheChangedEntry)
	cbWeaponDeleted  = newCallback(weaponCacheChangedEntry)
	cbClassAdded     = newCallback(classCacheChangedEntry)
	cbClassDeleted   = newCallback(classCacheChangedEntry)
	cbNPCAdded       = newCallback(npcAddedEntry)
	cbNPCDeleted     = newCallback(npcDeletedEntry)
	cbNPCFlags       = newCallback(npcFlagsEntry)
	cbNPCAttributes  = newCallback(npcAttributesEntry)

	cbFileBrowserFolders = newCallback(fileBrowserFoldersEntry)
	cbFileBrowserFiles   = newCallback(fileBrowserFilesEntry)
	cbFileBrowserMessage = newCallback(fileBrowserMessageEntry)
	cbFileReceived       = newCallback(fileReceivedEntry)
	cbMaxUploadSize      = newCallback(maxUploadSizeEntry)

	cbPlayerRights     = newCallback(playerRightsEntry)
	cbPlayerAttributes = newCallback(playerAttributesEntry)
	cbBanData          = newCallback(banDataEntry)
	cbBanListData      = newCallback(banListDataEntry)
	cbPlayerTextData   = newCallback(playerTextDataEntry)

	routeMu sync.Mutex
	routes  = map[Handle]*EventCallbacks{}
)

// connectedEntry is the C-callable shim for RC_OnConnected(user_data).
func connectedEntry(userData uintptr) uintptr {
	defer safeRecover("on_connected")
	fire(userData, func(c *EventCallbacks) {
		if c.Connected != nil {
			c.Connected()
		}
	})
	return 0
}

// disconnectedEntry is the C-callable shim for RC_OnDisconnected(reason, user_data).
func disconnectedEntry(reason, userData uintptr) uintptr {
	defer safeRecover("on_disconnected")
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
	defer safeRecover("on_message")
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
	defer safeRecover("on_irc_message")
	ch := bptrToString((*byte)(unsafe.Pointer(channel)))
	ln := bptrToString((*byte)(unsafe.Pointer(line)))
	fire(userData, func(c *EventCallbacks) {
		if c.IrcMessage != nil {
			c.IrcMessage(ch, ln)
		}
	})
	return 0
}

// privateMessageEntry is the shim for RC_OnPrivateMessage(player_id, account, nick, message, user_data).
func privateMessageEntry(playerID, account, nick, message, userData uintptr) uintptr {
	defer safeRecover("on_private_message")
	acct := bptrToString((*byte)(unsafe.Pointer(account)))
	nm := bptrToString((*byte)(unsafe.Pointer(nick)))
	msg := bptrToString((*byte)(unsafe.Pointer(message)))
	fire(userData, func(c *EventCallbacks) {
		if c.PrivateMessage != nil {
			c.PrivateMessage(int(int32(playerID)), acct, nm, msg)
		}
	})
	return 0
}

// serverDataEntry is the C-callable shim for RC_OnServerData(data_type, content, user_data).
func serverDataEntry(dataType, content, userData uintptr) uintptr {
	defer safeRecover("on_server_data")
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
	defer safeRecover("on_script_received")
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
	defer safeRecover("on_weapon_changed")
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
	defer safeRecover("on_class_changed")
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
	defer safeRecover("on_npc_added")
	fire(userData, func(c *EventCallbacks) {
		if c.NPCChanged != nil {
			c.NPCChanged(int(int32(id)))
		}
	})
	return 0
}

// npcDeletedEntry is the shim for RC_OnNPCDeleted(id, user_data).
func npcDeletedEntry(id, userData uintptr) uintptr {
	defer safeRecover("on_npc_deleted")
	fire(userData, func(c *EventCallbacks) {
		if c.NPCChanged != nil {
			c.NPCChanged(int(int32(id)))
		}
	})
	return 0
}

// npcFlagsEntry is the shim for RC_OnNPCFlags(npc_id, flags, user_data).
func npcFlagsEntry(npcID, flags, userData uintptr) uintptr {
	defer safeRecover("on_npc_flags")
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
	defer safeRecover("on_npc_attributes")
	at := bptrToString((*byte)(unsafe.Pointer(attrs)))
	fire(userData, func(c *EventCallbacks) {
		if c.NPCAttributes != nil {
			c.NPCAttributes(int(int32(npcID)), at)
		}
	})
	return 0
}

// fileBrowserFoldersEntry is the shim for RC_OnFileBrowserFolders(count, user_data).
// The callback only signals that folder data is ready; the app snapshots via
// CopyFileBrowserFolders.
func fileBrowserFoldersEntry(count, userData uintptr) uintptr {
	defer safeRecover("on_filebrowser_folders")
	fire(userData, func(c *EventCallbacks) {
		if c.FileBrowserFolders != nil {
			c.FileBrowserFolders(int(int32(count)))
		}
	})
	return 0
}

// fileBrowserFilesEntry is the shim for RC_OnFileBrowserFiles(folder, count, user_data).
func fileBrowserFilesEntry(folder, count, userData uintptr) uintptr {
	defer safeRecover("on_filebrowser_files")
	f := bptrToString((*byte)(unsafe.Pointer(folder)))
	fire(userData, func(c *EventCallbacks) {
		if c.FileBrowserFiles != nil {
			c.FileBrowserFiles(f, int(int32(count)))
		}
	})
	return 0
}

// fileBrowserMessageEntry is the shim for RC_OnFileBrowserMessage(message, user_data).
func fileBrowserMessageEntry(message, userData uintptr) uintptr {
	defer safeRecover("on_filebrowser_message")
	msg := bptrToString((*byte)(unsafe.Pointer(message)))
	fire(userData, func(c *EventCallbacks) {
		if c.FileBrowserMessage != nil {
			c.FileBrowserMessage(msg)
		}
	})
	return 0
}

// fileReceivedEntry is the shim for RC_OnFileReceived(path, content, length, user_data).
// The content buffer is owned by grclib and may be freed on return, so it is
// copied into a fresh Go allocation before dispatch.
func fileReceivedEntry(path, content, length, userData uintptr) uintptr {
	defer safeRecover("on_file_received")
	p := bptrToString((*byte)(unsafe.Pointer(path)))
	var data []byte
	if content != 0 && length != 0 {
		// Copy the C buffer into a Go-owned slice so it survives the call.
		data = append([]byte(nil), unsafe.Slice((*byte)(unsafe.Pointer(content)), int(int32(length)))...)
	}
	fire(userData, func(c *EventCallbacks) {
		if c.FileReceived != nil {
			c.FileReceived(p, data)
		}
	})
	return 0
}

// maxUploadSizeEntry is the shim for RC_OnMaxUploadFileSize(max_size, user_data).
func maxUploadSizeEntry(maxSize, userData uintptr) uintptr {
	defer safeRecover("on_max_upload_file_size")
	fire(userData, func(c *EventCallbacks) {
		if c.MaxUploadSize != nil {
			c.MaxUploadSize(int64(maxSize))
		}
	})
	return 0
}

// playerRightsEntry is the shim for RC_OnPlayerRights(account, rights, ip_range, folder_access, user_data).
func playerRightsEntry(account, rights, ipRange, folderAccess, userData uintptr) uintptr {
	defer safeRecover("on_player_rights")
	acct := bptrToString((*byte)(unsafe.Pointer(account)))
	ip := bptrToString((*byte)(unsafe.Pointer(ipRange)))
	fa := bptrToString((*byte)(unsafe.Pointer(folderAccess)))
	fire(userData, func(c *EventCallbacks) {
		if c.PlayerRights != nil {
			c.PlayerRights(acct, int(int32(rights)), ip, fa)
		}
	})
	return 0
}

// playerAttributesEntry is the shim for RC_OnPlayerAttributes(account, properties_json, editor_text, user_data).
func playerAttributesEntry(account, properties, editorText, userData uintptr) uintptr {
	defer safeRecover("on_player_attributes")
	acct := bptrToString((*byte)(unsafe.Pointer(account)))
	prop := bptrToString((*byte)(unsafe.Pointer(properties)))
	ed := bptrToString((*byte)(unsafe.Pointer(editorText)))
	fire(userData, func(c *EventCallbacks) {
		if c.PlayerAttributes != nil {
			c.PlayerAttributes(acct, prop, ed)
		}
	})
	return 0
}

// banDataEntry is the shim for RC_OnBanData(account, computer_id, details, user_data).
func banDataEntry(account, computerID, details, userData uintptr) uintptr {
	defer safeRecover("on_ban_data")
	acct := bptrToString((*byte)(unsafe.Pointer(account)))
	cid := bptrToString((*byte)(unsafe.Pointer(computerID)))
	det := bptrToString((*byte)(unsafe.Pointer(details)))
	fire(userData, func(c *EventCallbacks) {
		if c.BanData != nil {
			c.BanData(acct, cid, det)
		}
	})
	return 0
}

// banListDataEntry is the shim for RC_OnBanListData(data_type, account, content, user_data).
func banListDataEntry(dataType, account, content, userData uintptr) uintptr {
	defer safeRecover("on_ban_list_data")
	dt := bptrToString((*byte)(unsafe.Pointer(dataType)))
	acct := bptrToString((*byte)(unsafe.Pointer(account)))
	cnt := bptrToString((*byte)(unsafe.Pointer(content)))
	fire(userData, func(c *EventCallbacks) {
		if c.BanListData != nil {
			c.BanListData(dt, acct, cnt)
		}
	})
	return 0
}

// playerTextDataEntry is the shim for RC_OnPlayerTextData(data_type, account, content, user_data).
// Carries comments / profile / account text replies.
func playerTextDataEntry(dataType, account, content, userData uintptr) uintptr {
	defer safeRecover("on_player_text_data")
	dt := bptrToString((*byte)(unsafe.Pointer(dataType)))
	acct := bptrToString((*byte)(unsafe.Pointer(account)))
	cnt := bptrToString((*byte)(unsafe.Pointer(content)))
	fire(userData, func(c *EventCallbacks) {
		if c.PlayerTextData != nil {
			c.PlayerTextData(dt, acct, cnt)
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
	procOnPrivateMessage.Call(uintptr(h), cbPrivateMessage, uintptr(h))
	procOnScriptReceived.Call(uintptr(h), cbScriptReceived, uintptr(h))
	procOnWeaponAdded.Call(uintptr(h), cbWeaponAdded, uintptr(h))
	procOnWeaponDeleted.Call(uintptr(h), cbWeaponDeleted, uintptr(h))
	procOnClassAdded.Call(uintptr(h), cbClassAdded, uintptr(h))
	procOnClassDeleted.Call(uintptr(h), cbClassDeleted, uintptr(h))
	procOnNPCAdded.Call(uintptr(h), cbNPCAdded, uintptr(h))
	procOnNPCDeleted.Call(uintptr(h), cbNPCDeleted, uintptr(h))
	procOnNPCFlags.Call(uintptr(h), cbNPCFlags, uintptr(h))
	procOnNPCAttributes.Call(uintptr(h), cbNPCAttributes, uintptr(h))
	procOnFileBrowserFolders.Call(uintptr(h), cbFileBrowserFolders, uintptr(h))
	procOnFileBrowserFiles.Call(uintptr(h), cbFileBrowserFiles, uintptr(h))
	procOnFileBrowserMessage.Call(uintptr(h), cbFileBrowserMessage, uintptr(h))
	procOnFileReceived.Call(uintptr(h), cbFileReceived, uintptr(h))
	procOnMaxUploadFileSize.Call(uintptr(h), cbMaxUploadSize, uintptr(h))
	procOnPlayerRights.Call(uintptr(h), cbPlayerRights, uintptr(h))
	procOnPlayerAttributes.Call(uintptr(h), cbPlayerAttributes, uintptr(h))
	procOnBanData.Call(uintptr(h), cbBanData, uintptr(h))
	procOnBanListData.Call(uintptr(h), cbBanListData, uintptr(h))
	procOnPlayerTextData.Call(uintptr(h), cbPlayerTextData, uintptr(h))
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
	procOnPrivateMessage.Call(uintptr(h), 0, 0)
	procOnScriptReceived.Call(uintptr(h), 0, 0)
	procOnWeaponAdded.Call(uintptr(h), 0, 0)
	procOnWeaponDeleted.Call(uintptr(h), 0, 0)
	procOnClassAdded.Call(uintptr(h), 0, 0)
	procOnClassDeleted.Call(uintptr(h), 0, 0)
	procOnNPCAdded.Call(uintptr(h), 0, 0)
	procOnNPCDeleted.Call(uintptr(h), 0, 0)
	procOnNPCFlags.Call(uintptr(h), 0, 0)
	procOnNPCAttributes.Call(uintptr(h), 0, 0)
	procOnFileBrowserFolders.Call(uintptr(h), 0, 0)
	procOnFileBrowserFiles.Call(uintptr(h), 0, 0)
	procOnFileBrowserMessage.Call(uintptr(h), 0, 0)
	procOnFileReceived.Call(uintptr(h), 0, 0)
	procOnMaxUploadFileSize.Call(uintptr(h), 0, 0)
	procOnPlayerRights.Call(uintptr(h), 0, 0)
	procOnPlayerAttributes.Call(uintptr(h), 0, 0)
	procOnBanData.Call(uintptr(h), 0, 0)
	procOnBanListData.Call(uintptr(h), 0, 0)
	procOnPlayerTextData.Call(uintptr(h), 0, 0)
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

// surfaceError returns the DLL's last_error for the handle when one is set,
// otherwise the supplied default. This is how the reference C++ client reports
// every failure (it calls rc_last_error right after a 0/null return and pipes
// the result to chat). Callers that today invent a generic message should use
// this so the real server/DLL reason reaches the UI (toasts come for free via
// rcService.ts' try/catch). rc_last_error(NULL) safely returns "Invalid handle".
func surfaceError(h Handle, defaultMsg string) error {
	if e := LastError(h); e != "" {
		return errors.New(e)
	}
	return errors.New(defaultMsg)
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
		// Handle is null. rc_last_error safely returns "Invalid handle" for a
		// null arg (grclib.cpp ~4249), and a real reason if grclib set a global
		// last_error during the failed connect — surface either before falling
		// back to the generic hint.
		return 0, surfaceError(0, "rc_connect returned null (check host/port/account/password)")
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
		// Empty list is surfaced at a higher layer (Login maps it to "not staff
		// anywhere" or the DLL last_error). Log the DLL reason here in case the
		// emptiness is actually a silent failure (auth rejected, network).
		if e := LastError(h); e != "" {
			log.Printf("[rclib] get_servers empty list; last_error=%q", e)
		}
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
		if e := LastError(h); e != "" {
			log.Printf("[rclib] get_players empty list; last_error=%q", e)
		}
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

// SendPrivateMessage sends a private message to a single player id.
func SendPrivateMessage(h Handle, playerID int, message string) error {
	return callHandleIDStr(h, procSendPrivateMessage, playerID, message)
}

// SendMassPM sends one bulk PM packet to many player ids (single server round-trip).
func SendMassPM(h Handle, playerIDs []int, message string) error {
	if err := load(); err != nil {
		return err
	}
	msg, _ := syscall.BytePtrFromString(message)
	var idsPtr unsafe.Pointer
	if len(playerIDs) > 0 {
		// Build a contiguous C int array (4 bytes each) the DLL can read.
		buf := make([]int32, len(playerIDs))
		for i, id := range playerIDs {
			buf[i] = int32(id)
		}
		idsPtr = unsafe.Pointer(&buf[0])
	}
	r1, _, _ := procSendMassPM.Call(
		uintptr(h),
		uintptr(idsPtr),
		uintptr(len(playerIDs)),
		uintptr(unsafe.Pointer(msg)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// SendAdminMessage sends an admin message to a single player id.
func SendAdminMessage(h Handle, playerID int, message string) error {
	return callHandleIDStr(h, procSendAdminMessage, playerID, message)
}

// SendAdminMessageAll sends an admin message to every player on the server.
func SendAdminMessageAll(h Handle, message string) error {
	return callHandleStr(h, procSendAdminMessageAll, message)
}

// RequestPlayerRights asks the server for the current rights of an account.
// Reply arrives via the PlayerRights callback.
func RequestPlayerRights(h Handle, account string) error {
	return callHandleStr(h, procRequestPlayerRights, account)
}

// SetPlayerRights writes rights flags + ip range + folder access for an account.
func SetPlayerRights(h Handle, account string, rights int, ipRange, folderAccess string) error {
	if err := load(); err != nil {
		return err
	}
	acct, _ := syscall.BytePtrFromString(account)
	ip, _ := syscall.BytePtrFromString(ipRange)
	fa, _ := syscall.BytePtrFromString(folderAccess)
	r1, _, _ := procSetPlayerRights.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(acct)),
		uintptr(rights),
		uintptr(unsafe.Pointer(ip)),
		uintptr(unsafe.Pointer(fa)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// RequestPlayerAttrs asks the server for the attributes of an account.
// Reply arrives via the PlayerAttributes callback (properties JSON + editor text).
func RequestPlayerAttrs(h Handle, account string) error {
	return callHandleStr(h, procRequestPlayerAttrs, account)
}

// SetPlayerAttributes writes the properties JSON blob for an account.
func SetPlayerAttributes(h Handle, account, propertiesJSON string) error {
	if err := load(); err != nil {
		return err
	}
	acct, _ := syscall.BytePtrFromString(account)
	prop, _ := syscall.BytePtrFromString(propertiesJSON)
	r1, _, _ := procSetPlayerAttributes.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(acct)),
		uintptr(unsafe.Pointer(prop)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// ParsePlayerAttributesText converts an INI-style attribute editor document
// into the properties JSON blob the protocol expects. The returned C string is
// allocated by grclib and freed here; the Go string is a fresh copy.
func ParsePlayerAttributesText(text string) (string, error) {
	if err := load(); err != nil {
		return "", err
	}
	txt, _ := syscall.BytePtrFromString(text)
	r1, _, _ := procParsePlayerAttributesText.Call(uintptr(unsafe.Pointer(txt)))
	if r1 == 0 {
		return "", errors.New("rc_parse_player_attributes_text returned null")
	}
	out := bptrToString((*byte)(unsafe.Pointer(r1)))
	Free(r1)
	return out, nil
}

// RequestPlayerBan asks the server for the ban data of an online player.
func RequestPlayerBan(h Handle, account string, playerID int) error {
	if err := load(); err != nil {
		return err
	}
	acct, _ := syscall.BytePtrFromString(account)
	r1, _, _ := procRequestPlayerBan.Call(uintptr(h), uintptr(unsafe.Pointer(acct)), uintptr(playerID))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// RequestPlayerBanByAccount asks for ban data of an (possibly offline) account.
func RequestPlayerBanByAccount(h Handle, account string) error {
	return callHandleStr(h, procRequestPlayerBanByAccount, account)
}

// RequestBanTypes asks the server for the available ban types/durations.
// Reply arrives via the BanListData callback with data_type == "bantypes".
func RequestBanTypes(h Handle) error {
	return callHandle(h, procRequestBanTypes)
}

// RequestBanHistory asks for the ban history of an account.
func RequestBanHistory(h Handle, account string) error {
	return callHandleStr(h, procRequestBanHistory, account)
}

// RequestStaffActivity asks for the staff activity log of an account.
func RequestStaffActivity(h Handle, account string) error {
	return callHandleStr(h, procRequestStaffActivity, account)
}

// SetBan writes ban data for a target. world is "local" or "all"; target is the
// account or "pc:<computerID>"; banned toggles the ban; releaseTime "" resets.
func SetBan(h Handle, target, world string, banned bool, banType, releaseTime, reason string) error {
	if err := load(); err != nil {
		return err
	}
	tgt, _ := syscall.BytePtrFromString(target)
	wld, _ := syscall.BytePtrFromString(world)
	bt, _ := syscall.BytePtrFromString(banType)
	rt, _ := syscall.BytePtrFromString(releaseTime)
	rsn, _ := syscall.BytePtrFromString(reason)
	var bannedInt int
	if banned {
		bannedInt = 1
	}
	r1, _, _ := procSetBan.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(tgt)),
		uintptr(unsafe.Pointer(wld)),
		uintptr(bannedInt),
		uintptr(unsafe.Pointer(bt)),
		uintptr(unsafe.Pointer(rt)),
		uintptr(unsafe.Pointer(rsn)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// RequestPlayerComments asks the server for an account's comments text.
// Reply arrives via the PlayerTextData callback with data_type == "comments".
func RequestPlayerComments(h Handle, account string) error {
	return callHandleStr(h, procRequestPlayerComments, account)
}

// SetPlayerComments writes the comments text for an account.
func SetPlayerComments(h Handle, account, comments string) error {
	if err := load(); err != nil {
		return err
	}
	acct, _ := syscall.BytePtrFromString(account)
	cmt, _ := syscall.BytePtrFromString(comments)
	r1, _, _ := procSetPlayerComments.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(acct)),
		uintptr(unsafe.Pointer(cmt)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}
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
		if e := LastError(h); e != "" {
			log.Printf("[rclib] get_weapons empty list; last_error=%q", e)
		}
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
		if e := LastError(h); e != "" {
			log.Printf("[rclib] get_classes empty list; last_error=%q", e)
		}
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
		if e := LastError(h); e != "" {
			log.Printf("[rclib] get_npcs empty list; last_error=%q", e)
		}
		return nil, nil
	}
	arr := (*[1 << 20]RCNPC)(unsafe.Pointer(ptr))[:count:count]
	out := make([]NPC, count)
	for i := 0; i < count; i++ {
		n := arr[i]
		out[i] = NPC{ID: int(n.ID), Name: bptrToString(n.Name), Type: bptrToString(n.Type), Level: bptrToString(n.Level)}
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
	r1 := createNPCCall(
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
func RequestWeaponScript(h Handle, name string) error {
	return callStr1(h, procRequestWeaponScript, name)
}

// RequestClassScript asks the server for a class's script.
func RequestClassScript(h Handle, name string) error {
	return callStr1(h, procRequestClassScript, name)
}

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

// RequestServerOptions asks the server for its options text; the reply arrives
// asynchronously via on_server_data (data_type "options").
func RequestServerOptions(h Handle) error { return callHandle(h, procRequestServerOptions) }

// RequestFolderConfig asks the server for its folder-config text; the reply
// arrives via on_server_data (data_type "folder_config").
func RequestFolderConfig(h Handle) error { return callHandle(h, procRequestFolderConfig) }

// RequestServerFlags asks the server for its flags text; the reply arrives via
// on_server_data (data_type "flags").
func RequestServerFlags(h Handle) error { return callHandle(h, procRequestServerFlags) }

// UploadServerOptions writes the server options text back to the server.
func UploadServerOptions(h Handle, content string) error {
	return callHandleStr(h, procUploadServerOptions, content)
}

// UploadFolderConfig writes the folder-config text back to the server.
func UploadFolderConfig(h Handle, content string) error {
	return callHandleStr(h, procUploadFolderConfig, content)
}

// UploadServerFlags writes the server flags text back to the server.
func UploadServerFlags(h Handle, content string) error {
	return callHandleStr(h, procUploadServerFlags, content)
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

// FileBrowserStart begins a file-browser session (requests the root folder list
// + current files). Folder/file data arrives asynchronously via the
// FileBrowserFolders / FileBrowserFiles callbacks.
func FileBrowserStart(h Handle) error { return callHandle(h, procFileBrowserStart) }

// FileBrowserCd changes the current browser folder. The server replies with an
// updated folder/file set (delivered via the callbacks).
func FileBrowserCd(h Handle, folder string) error {
	return callStr1(h, procFileBrowserCd, folder)
}

// FileBrowserDownload requests a file; its content arrives asynchronously via
// the FileReceived callback. Callers correlate by path.
func FileBrowserDownload(h Handle, path string) error {
	return callStr1(h, procFileBrowserDownload, path)
}

// FileBrowserDelete deletes a remote file.
func FileBrowserDelete(h Handle, path string) error {
	return callStr1(h, procFileBrowserDelete, path)
}

// FileBrowserRename renames a remote file (old path -> new path).
func FileBrowserRename(h Handle, oldPath, newPath string) error {
	return callStr2(h, procFileBrowserRename, oldPath, newPath)
}

// FileBrowserMove moves a file into a destination folder. NOTE the argument
// order: (destination_folder, file_path), matching grclib's declaration.
func FileBrowserMove(h Handle, destFolder, filePath string) error {
	return callStr2(h, procFileBrowserMove, destFolder, filePath)
}

// UploadFile uploads raw bytes (content may contain NULs) to a remote path.
// The length is passed explicitly — content is NOT treated as a C string.
func UploadFile(h Handle, path string, content []byte) error {
	if err := load(); err != nil {
		return err
	}
	pathPtr, _ := syscall.BytePtrFromString(path)
	var contentPtr unsafe.Pointer
	if len(content) > 0 {
		contentPtr = unsafe.Pointer(&content[0])
	}
	r1, _, _ := procUploadFile.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(contentPtr),
		uintptr(len(content)),
	)
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// GetMaxUploadFileSize returns the server's max upload size in bytes (0 if
// unknown). The value is also pushed via the MaxUploadSize callback.
func GetMaxUploadFileSize(h Handle) int64 {
	if err := load(); err != nil {
		return 0
	}
	r1, _, _ := procGetMaxUploadFileSize.Call(uintptr(h))
	return int64(r1)
}

// CopyFileBrowserFolders snapshots the current browser folders into Go-owned
// copies. Call after a FileBrowserFolders callback signals data is ready.
func CopyFileBrowserFolders(h Handle) ([]FileBrowserFolder, error) {
	if err := load(); err != nil {
		return nil, err
	}
	var ptr uintptr
	r1, _, _ := procCopyFileBrowserFolders.Call(uintptr(h), uintptr(unsafe.Pointer(&ptr)))
	count := int(int32(r1))
	if count <= 0 || ptr == 0 {
		return nil, nil
	}
	arr := (*[1 << 20]RCFileBrowserFolder)(unsafe.Pointer(ptr))[:count:count]
	out := make([]FileBrowserFolder, count)
	for i := 0; i < count; i++ {
		f := arr[i]
		out[i] = FileBrowserFolder{
			Pattern: bptrToString(f.Pattern),
			Rights:  bptrToString(f.Rights),
		}
	}
	procFreeFileBrowserFolders.Call(ptr, uintptr(count))
	return out, nil
}

// CopyFileBrowserFiles snapshots the current browser files into Go-owned copies.
// Call after a FileBrowserFiles callback signals data is ready.
func CopyFileBrowserFiles(h Handle) ([]FileBrowserEntry, error) {
	if err := load(); err != nil {
		return nil, err
	}
	var ptr uintptr
	r1, _, _ := procCopyFileBrowserFiles.Call(uintptr(h), uintptr(unsafe.Pointer(&ptr)))
	count := int(int32(r1))
	if count <= 0 || ptr == 0 {
		return nil, nil
	}
	arr := (*[1 << 20]RCFileBrowserEntry)(unsafe.Pointer(ptr))[:count:count]
	out := make([]FileBrowserEntry, count)
	for i := 0; i < count; i++ {
		e := arr[i]
		out[i] = FileBrowserEntry{
			Path:        bptrToString(e.Path),
			Rights:      bptrToString(e.Rights),
			Size:        int(e.Size),
			Modified:    int(e.Modified),
			IsDirectory: e.IsDirectory != 0,
		}
	}
	procFreeFileBrowserFiles.Call(ptr, uintptr(count))
	return out, nil
}

// callStr1 calls a (handle, const char*) DLL function returning int (0 = error).
func callStr1(h Handle, p *proc, a string) error {
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
func callStr2(h Handle, p *proc, a, b string) error {
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
func callStr3(h Handle, p *proc, a, b, c string) error {
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

// callHandle calls a (handle)-only DLL function returning int (e.g. the server
// text-config requestors).
func callHandle(h Handle, p *proc) error {
	if err := load(); err != nil {
		return err
	}
	r1, _, _ := p.Call(uintptr(h))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// callHandleIDStr calls a (handle, int player_id, const char*) DLL function
// returning int (e.g. rc_send_private_message / rc_send_admin_message).
func callHandleIDStr(h Handle, p *proc, playerID int, a string) error {
	if err := load(); err != nil {
		return err
	}
	ptr, _ := syscall.BytePtrFromString(a)
	r1, _, _ := p.Call(uintptr(h), uintptr(playerID), uintptr(unsafe.Pointer(ptr)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}

// callHandleStr calls a (handle, const char*) DLL function returning int (e.g.
// the server text-config uploaders).
func callHandleStr(h Handle, p *proc, content string) error {
	if err := load(); err != nil {
		return err
	}
	c, _ := syscall.BytePtrFromString(content)
	r1, _, _ := p.Call(uintptr(h), uintptr(unsafe.Pointer(c)))
	if r1 == 0 {
		return errors.New(LastError(h))
	}
	return nil
}
