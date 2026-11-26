package rclib

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// fakeLibrary stands in for grclib: every export is recorded and, unless a
// handler says otherwise, returns 0.
type fakeLibrary struct {
	mu       sync.Mutex
	calls    []string
	args     [][]uintptr
	handlers map[string]func([]uintptr) uintptr
}

// fakeConnection is the address every fake rc_connect returns, which models the
// allocator handing a deleted RCConnection's address to the next one.
var fakeConnection = new([64]byte)

func fakeConnectionWord() uintptr { return uintptr(unsafe.Pointer(&fakeConnection[0])) }

func installFakeLibrary(t *testing.T) *fakeLibrary {
	t.Helper()
	fake := &fakeLibrary{handlers: map[string]func([]uintptr) uintptr{}}
	fake.handlers["rc_connect"] = func([]uintptr) uintptr { return fakeConnectionWord() }
	originalLoad := load
	load = func() error { return nil }
	if err := registerAll(func(name string) (*proc, error) {
		return &proc{name: name, native: func(args []uintptr) uintptr {
			fake.mu.Lock()
			fake.calls = append(fake.calls, name)
			fake.args = append(fake.args, append([]uintptr(nil), args...))
			handler := fake.handlers[name]
			fake.mu.Unlock()
			if handler == nil {
				return 0
			}
			return handler(args)
		}}, nil
	}); err != nil {
		t.Fatalf("register fake library: %v", err)
	}
	t.Cleanup(func() {
		load = originalLoad
		// Let a later test that wants the real library load it again.
		once, loadErr = sync.Once{}, nil
		_ = registerAll(func(string) (*proc, error) { return nil, nil })
	})
	return fake
}

func (f *fakeLibrary) handle(name string, handler func([]uintptr) uintptr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[name] = handler
}

func (f *fakeLibrary) takeCalls() ([]string, [][]uintptr) {
	f.mu.Lock()
	defer f.mu.Unlock()
	calls, args := f.calls, f.args
	f.calls, f.args = nil, nil
	return calls, args
}

func connectFake(t *testing.T) Handle {
	t.Helper()
	h, err := Connect("host", 1, "account", "password")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { Disconnect(h) })
	return h
}

type handleResult struct {
	value any // zero value expected from a destroyed handle; nil when none
	err   error
}

func errorOnly(err error) handleResult { return handleResult{err: err} }

// handleCalls invokes every exported function that takes a Handle.
// TestEveryHandleFunctionIsExercised keeps this table complete.
var handleCalls = map[string]func(Handle) handleResult{
	"RegisterCallbacks":   func(h Handle) handleResult { return errorOnly(RegisterCallbacks(h, &EventCallbacks{})) },
	"UnregisterCallbacks": func(h Handle) handleResult { return errorOnly(UnregisterCallbacks(h)) },
	"ProcessEvents":       func(h Handle) handleResult { return errorOnly(ProcessEvents(h)) },
	"LastError": func(h Handle) handleResult {
		_ = LastError(h)
		return handleResult{}
	},
	"GetServers": func(h Handle) handleResult {
		v, err := GetServers(h)
		return handleResult{len(v), err}
	},
	"ConnectToServer":   func(h Handle) handleResult { return errorOnly(ConnectToServer(h, 0)) },
	"IsConnected":       func(h Handle) handleResult { return handleResult{value: IsConnected(h)} },
	"IsAuthenticated":   func(h Handle) handleResult { return handleResult{value: IsAuthenticated(h)} },
	"SetNewProtocol":    func(h Handle) handleResult { return errorOnly(SetNewProtocol(h, true)) },
	"IsNewProtocol":     func(h Handle) handleResult { return handleResult{value: IsNewProtocol(h)} },
	"HasNCServer":       func(h Handle) handleResult { return handleResult{value: HasNCServer(h)} },
	"IsNCConnected":     func(h Handle) handleResult { return handleResult{value: IsNCConnected(h)} },
	"IsNCAuthenticated": func(h Handle) handleResult { return handleResult{value: IsNCAuthenticated(h)} },
	"ConnectToNCServer": func(h Handle) handleResult { return errorOnly(ConnectToNCServer(h)) },
	"DisconnectNC":      func(h Handle) handleResult { return errorOnly(DisconnectNC(h)) },
	"IrcLogin":          func(h Handle) handleResult { return errorOnly(IrcLogin(h)) },
	"SendIrcText":       func(h Handle) handleResult { return errorOnly(SendIrcText(h, "c", "1", "2", "3")) },
	"Execute":           func(h Handle) handleResult { return errorOnly(Execute(h, "m")) },
	"SetNickname":       func(h Handle) handleResult { return errorOnly(SetNickname(h, "n")) },
	"GetPlayers": func(h Handle) handleResult {
		v, err := GetPlayers(h)
		return handleResult{len(v), err}
	},
	"SendPrivateMessage":  func(h Handle) handleResult { return errorOnly(SendPrivateMessage(h, 1, "m")) },
	"SendMassPM":          func(h Handle) handleResult { return errorOnly(SendMassPM(h, []int{1}, "m")) },
	"SendAdminMessage":    func(h Handle) handleResult { return errorOnly(SendAdminMessage(h, 1, "m")) },
	"SendAdminMessageAll": func(h Handle) handleResult { return errorOnly(SendAdminMessageAll(h, "m")) },
	"RequestPlayerRights": func(h Handle) handleResult { return errorOnly(RequestPlayerRights(h, "a")) },
	"SetPlayerRights":     func(h Handle) handleResult { return errorOnly(SetPlayerRights(h, "a", 1, "ip", "f")) },
	"RequestPlayerAttrs":  func(h Handle) handleResult { return errorOnly(RequestPlayerAttrs(h, "a")) },
	"SetPlayerAttributes": func(h Handle) handleResult { return errorOnly(SetPlayerAttributes(h, "a", "{}")) },
	"RequestPlayerBan":    func(h Handle) handleResult { return errorOnly(RequestPlayerBan(h, "a", 1)) },
	"RequestPlayerBanByAccount": func(h Handle) handleResult {
		return errorOnly(RequestPlayerBanByAccount(h, "a"))
	},
	"RequestBanTypes":      func(h Handle) handleResult { return errorOnly(RequestBanTypes(h)) },
	"RequestBanHistory":    func(h Handle) handleResult { return errorOnly(RequestBanHistory(h, "a")) },
	"RequestStaffActivity": func(h Handle) handleResult { return errorOnly(RequestStaffActivity(h, "a")) },
	"SetLegacyPlayerBan": func(h Handle) handleResult { return errorOnly(SetLegacyPlayerBan(h, "a", true, "r")) },
	"SetBan": func(h Handle) handleResult {
		return errorOnly(SetBan(h, "a", "local", true, "t", "r", "why"))
	},
	"RequestPlayerComments": func(h Handle) handleResult { return errorOnly(RequestPlayerComments(h, "a")) },
	"SetPlayerComments":     func(h Handle) handleResult { return errorOnly(SetPlayerComments(h, "a", "c")) },
	"Disconnect": func(h Handle) handleResult {
		Disconnect(h)
		return handleResult{}
	},
	"GetWeapons": func(h Handle) handleResult {
		v, err := GetWeapons(h)
		return handleResult{len(v), err}
	},
	"GetClasses": func(h Handle) handleResult {
		v, err := GetClasses(h)
		return handleResult{len(v), err}
	},
	"GetNPCs": func(h Handle) handleResult {
		v, err := GetNPCs(h)
		return handleResult{len(v), err}
	},
	"AddWeapon":    func(h Handle) handleResult { return errorOnly(AddWeapon(h, "n")) },
	"DeleteWeapon": func(h Handle) handleResult { return errorOnly(DeleteWeapon(h, "n")) },
	"UpdateWeapon": func(h Handle) handleResult { return errorOnly(UpdateWeapon(h, "n", "s")) },
	"AddClass":     func(h Handle) handleResult { return errorOnly(AddClass(h, "n")) },
	"DeleteClass":  func(h Handle) handleResult { return errorOnly(DeleteClass(h, "n")) },
	"UpdateClass":  func(h Handle) handleResult { return errorOnly(UpdateClass(h, "n", "s")) },
	"DeleteNPC":    func(h Handle) handleResult { return errorOnly(DeleteNPC(h, 1)) },
	"UpdateNPC":    func(h Handle) handleResult { return errorOnly(UpdateNPC(h, 1, "s")) },
	"CreateNPC": func(h Handle) handleResult {
		return errorOnly(CreateNPC(h, "n", 1, "t", "s", "l", "1", "2"))
	},
	"RequestWeaponScript":  func(h Handle) handleResult { return errorOnly(RequestWeaponScript(h, "n")) },
	"RequestClassScript":   func(h Handle) handleResult { return errorOnly(RequestClassScript(h, "n")) },
	"RequestNPCScript":     func(h Handle) handleResult { return errorOnly(RequestNPCScript(h, 1)) },
	"ResetNPC":             func(h Handle) handleResult { return errorOnly(ResetNPC(h, 1)) },
	"RequestNPCAttributes": func(h Handle) handleResult { return errorOnly(RequestNPCAttributes(h, 1)) },
	"GetNPCFlags":          func(h Handle) handleResult { return errorOnly(GetNPCFlags(h, 1)) },
	"SetNPCFlags":          func(h Handle) handleResult { return errorOnly(SetNPCFlags(h, 1, "f")) },
	"WarpNPC":              func(h Handle) handleResult { return errorOnly(WarpNPC(h, 1, 1, 2, "l")) },
	"RequestServerOptions": func(h Handle) handleResult { return errorOnly(RequestServerOptions(h)) },
	"RequestFolderConfig":  func(h Handle) handleResult { return errorOnly(RequestFolderConfig(h)) },
	"RequestServerFlags":   func(h Handle) handleResult { return errorOnly(RequestServerFlags(h)) },
	"UploadServerOptions":  func(h Handle) handleResult { return errorOnly(UploadServerOptions(h, "c")) },
	"UploadFolderConfig":   func(h Handle) handleResult { return errorOnly(UploadFolderConfig(h, "c")) },
	"UploadServerFlags":    func(h Handle) handleResult { return errorOnly(UploadServerFlags(h, "c")) },
	"SendNCPacket":         func(h Handle) handleResult { return errorOnly(SendNCPacket(h, 1)) },
	"SendRawPacket":        func(h Handle) handleResult { return errorOnly(SendRawPacket(h, 1, []byte("d"))) },
	"RequestWeaponList":    func(h Handle) handleResult { return errorOnly(RequestWeaponList(h)) },
	"FileBrowserStart":     func(h Handle) handleResult { return errorOnly(FileBrowserStart(h)) },
	"FileBrowserCd":        func(h Handle) handleResult { return errorOnly(FileBrowserCd(h, "f")) },
	"FileBrowserDownload":  func(h Handle) handleResult { return errorOnly(FileBrowserDownload(h, "p")) },
	"FileBrowserDelete":    func(h Handle) handleResult { return errorOnly(FileBrowserDelete(h, "p")) },
	"FileBrowserRename":    func(h Handle) handleResult { return errorOnly(FileBrowserRename(h, "a", "b")) },
	"FileBrowserMove":      func(h Handle) handleResult { return errorOnly(FileBrowserMove(h, "a", "b")) },
	"UploadFile":           func(h Handle) handleResult { return errorOnly(UploadFile(h, "p", []byte("d"))) },
	"GetMaxUploadFileSize": func(h Handle) handleResult { return handleResult{value: GetMaxUploadFileSize(h)} },
	"CopyFileBrowserFolders": func(h Handle) handleResult {
		v, err := CopyFileBrowserFolders(h)
		return handleResult{len(v), err}
	},
	"CopyFileBrowserFiles": func(h Handle) handleResult {
		v, err := CopyFileBrowserFiles(h)
		return handleResult{len(v), err}
	},
}

// noErrorResult lists the functions whose signature cannot report a failure.
var noErrorResult = map[string]bool{
	"LastError": true, "IsConnected": true, "IsAuthenticated": true, "IsNewProtocol": true,
	"HasNCServer": true, "IsNCConnected": true, "IsNCAuthenticated": true, "Disconnect": true,
	"GetMaxUploadFileSize": true,
}

func sortedHandleCalls() []string {
	names := make([]string, 0, len(handleCalls))
	for name := range handleCalls {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func packageSources(t *testing.T) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	fileSet := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = file
	}
	return files
}

func TestEveryHandleFunctionIsExercised(t *testing.T) {
	declared := map[string]bool{}
	for _, file := range packageSources(t) {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || !function.Name.IsExported() || function.Recv != nil {
				continue
			}
			for _, field := range function.Type.Params.List {
				if ident, ok := field.Type.(*ast.Ident); ok && ident.Name == "Handle" {
					declared[function.Name.Name] = true
				}
			}
		}
	}
	for name := range declared {
		if handleCalls[name] == nil {
			t.Errorf("%s takes a Handle but is not covered by the destroyed-handle tests", name)
		}
	}
	for name := range handleCalls {
		if !declared[name] {
			t.Errorf("handleCalls names unknown function %s", name)
		}
	}
}

func TestDestroyedHandleNeverEntersTheLibrary(t *testing.T) {
	fake := installFakeLibrary(t)
	stale := connectFake(t)
	Disconnect(stale)
	// The next session reuses the same native address.
	live := connectFake(t)
	if live == stale {
		t.Errorf("a new connection revived the destroyed handle %#x", uintptr(stale))
	}

	for label, h := range map[string]Handle{"destroyed": stale, "zero": 0} {
		for _, name := range sortedHandleCalls() {
			t.Run(label+"/"+name, func(t *testing.T) {
				fake.takeCalls()
				result := handleCalls[name](h)
				if calls, _ := fake.takeCalls(); len(calls) != 0 {
					t.Fatalf("entered the library: %v", calls)
				}
				if !noErrorResult[name] && result.err == nil {
					t.Fatal("reported success")
				}
				switch result.value {
				case nil, false, 0, int64(0):
				default:
					t.Fatalf("returned %v, want the zero value", result.value)
				}
			})
		}
	}
}

// Every function must pass the connection's native pointer to the library and
// finish its failure path without taking dllMu a second time.
func TestLiveHandleCallsPassNativePointerAndDoNotRelock(t *testing.T) {
	for _, name := range sortedHandleCalls() {
		t.Run(name, func(t *testing.T) {
			fake := installFakeLibrary(t)
			h := connectFake(t)
			fake.takeCalls()
			done := make(chan struct{})
			go func() {
				defer close(done)
				handleCalls[name](h)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("deadlocked")
			}
			calls, args := fake.takeCalls()
			if len(calls) == 0 {
				t.Fatal("never entered the library")
			}
			for i, call := range calls {
				if len(args[i]) == 0 || args[i][0] != fakeConnectionWord() {
					t.Fatalf("%s did not receive the native connection pointer", call)
				}
			}
		})
	}
}

func TestNoNativeCallRunsAfterDisconnect(t *testing.T) {
	fake := installFakeLibrary(t)
	for round := 0; round < 50; round++ {
		h := connectFake(t)
		var destroyed atomic.Bool
		var afterDestroy atomic.Int32
		fake.handle("rc_disconnect", func([]uintptr) uintptr { destroyed.Store(true); return 0 })
		fake.handle("rc_execute", func([]uintptr) uintptr {
			if destroyed.Load() {
				afterDestroy.Add(1)
			}
			return 1
		})
		var wait sync.WaitGroup
		started := make(chan struct{}, 8)
		for worker := 0; worker < 8; worker++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				started <- struct{}{}
				for i := 0; i < 50; i++ {
					_ = Execute(h, "m")
				}
			}()
		}
		<-started
		Disconnect(h)
		wait.Wait()
		fake.takeCalls()
		if n := afterDestroy.Load(); n != 0 {
			t.Fatalf("round %d: %d calls ran on a destroyed connection", round, n)
		}
	}
}

// nativeBuffers imitates grclib's caches: each query rewrites the memory that
// the previous one returned. A copy made outside the native lock observes a
// mixture of generations.
type nativeBuffers struct {
	generation int
	strings    [][]byte
}

const (
	bufferEntries = 300
	bufferText    = 200
)

func newNativeBuffers() *nativeBuffers {
	b := &nativeBuffers{strings: make([][]byte, bufferEntries)}
	for i := range b.strings {
		b.strings[i] = make([]byte, bufferText+1)
	}
	return b
}

func (b *nativeBuffers) rewrite() {
	b.generation++
	letter := byte('a' + b.generation%26)
	for _, text := range b.strings {
		for i := 0; i < bufferText; i++ {
			text[i] = letter
		}
	}
}

// uniform reports whether every copied string belongs to one generation.
func uniform(values []string) error {
	if len(values) == 0 || values[0] == "" {
		return fmt.Errorf("no values copied")
	}
	want := strings.Repeat(values[0][:1], bufferText)
	for _, value := range values {
		if value != want {
			return fmt.Errorf("copy observed a later call rewriting the buffer")
		}
	}
	return nil
}

func TestQueriesCopyNativeBuffersUnderTheLock(t *testing.T) {
	buffers := newNativeBuffers()
	text := func(i int) *byte { return &buffers.strings[i][0] }

	servers := make([]RCServer, bufferEntries)
	players := make([]RCPlayer, bufferEntries)
	weapons := make([]RCWeapon, bufferEntries)
	classes := make([]RCClass, bufferEntries)
	npcs := make([]RCNPC, bufferEntries)
	folders := make([]RCFileBrowserFolder, bufferEntries)
	files := make([]RCFileBrowserEntry, bufferEntries)
	for i := 0; i < bufferEntries; i++ {
		servers[i] = RCServer{Name: text(i)}
		players[i] = RCPlayer{Account: text(i)}
		weapons[i] = RCWeapon{Name: text(i)}
		classes[i] = RCClass{Name: text(i)}
		npcs[i] = RCNPC{Name: text(i)}
		folders[i] = RCFileBrowserFolder{Pattern: text(i)}
		files[i] = RCFileBrowserEntry{Path: text(i)}
	}
	list := func(first unsafe.Pointer) func([]uintptr) uintptr {
		return func(args []uintptr) uintptr {
			buffers.rewrite()
			*(*uintptr)(nativePointer(args[1])) = uintptr(first)
			return bufferEntries
		}
	}
	pick := func(n int, value func(int) string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = value(i)
		}
		return out
	}

	tests := []struct {
		export  string
		handler func([]uintptr) uintptr
		query   func(Handle) ([]string, error)
	}{
		{"rc_get_servers", list(unsafe.Pointer(&servers[0])), func(h Handle) ([]string, error) {
			v, err := GetServers(h)
			return pick(len(v), func(i int) string { return v[i].Name }), err
		}},
		{"rc_get_players", list(unsafe.Pointer(&players[0])), func(h Handle) ([]string, error) {
			v, err := GetPlayers(h)
			return pick(len(v), func(i int) string { return v[i].Account }), err
		}},
		{"rc_get_weapons", list(unsafe.Pointer(&weapons[0])), func(h Handle) ([]string, error) {
			v, err := GetWeapons(h)
			return pick(len(v), func(i int) string { return v[i].Name }), err
		}},
		{"rc_get_classes", list(unsafe.Pointer(&classes[0])), func(h Handle) ([]string, error) {
			v, err := GetClasses(h)
			return pick(len(v), func(i int) string { return v[i].Name }), err
		}},
		{"rc_get_npcs", list(unsafe.Pointer(&npcs[0])), func(h Handle) ([]string, error) {
			v, err := GetNPCs(h)
			return pick(len(v), func(i int) string { return v[i].Name }), err
		}},
		{"rc_copy_filebrowser_folders", list(unsafe.Pointer(&folders[0])), func(h Handle) ([]string, error) {
			v, err := CopyFileBrowserFolders(h)
			return pick(len(v), func(i int) string { return v[i].Pattern }), err
		}},
		{"rc_copy_filebrowser_files", list(unsafe.Pointer(&files[0])), func(h Handle) ([]string, error) {
			v, err := CopyFileBrowserFiles(h)
			return pick(len(v), func(i int) string { return v[i].Path }), err
		}},
		{"rc_last_error", func([]uintptr) uintptr {
			buffers.rewrite()
			return uintptr(unsafe.Pointer(text(0)))
		}, func(h Handle) ([]string, error) { return []string{LastError(h)}, nil }},
	}
	for _, test := range tests {
		t.Run(test.export, func(t *testing.T) {
			fake := installFakeLibrary(t)
			fake.handle(test.export, test.handler)
			h := connectFake(t)
			var wait sync.WaitGroup
			var failure atomic.Value
			for worker := 0; worker < 8; worker++ {
				wait.Add(1)
				go func() {
					defer wait.Done()
					for i := 0; i < 150 && failure.Load() == nil; i++ {
						values, err := test.query(h)
						if err == nil {
							err = uniform(values)
						}
						if err != nil {
							failure.Store(err.Error())
						}
						fake.takeCalls()
					}
				}()
			}
			wait.Wait()
			if message := failure.Load(); message != nil {
				t.Fatal(message)
			}
		})
	}
}

// A new export that takes the connection must not be declared as a raw proc,
// and only handle.go may reach a connectionProc's export or call native code.
func TestOnlyHandleFreeExportsAreRawProcs(t *testing.T) {
	handleFree := map[string]bool{
		"procConnect": true, "procFree": true, "procParsePlayerAttributesText": true,
		"procFreeFileBrowserFolders": true, "procFreeFileBrowserFiles": true,
	}
	raw := 0
	for name, file := range packageSources(t) {
		for _, declaration := range file.Decls {
			if general, ok := declaration.(*ast.GenDecl); ok && general.Tok == token.VAR {
				for _, spec := range general.Specs {
					value := spec.(*ast.ValueSpec)
					star, ok := value.Type.(*ast.StarExpr)
					if !ok {
						continue
					}
					if ident, ok := star.X.(*ast.Ident); !ok || ident.Name != "proc" {
						continue
					}
					for _, variable := range value.Names {
						raw++
						if !handleFree[variable.Name] {
							t.Errorf("%s is a raw *proc; an export that takes a handle must be a *connectionProc", variable.Name)
						}
					}
				}
			}
			if name == "handle.go" {
				continue
			}
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv != nil && function.Name.Name == "Call" {
				continue
			}
			ast.Inspect(declaration, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.SelectorExpr:
					if n.Sel.Name == "native" || n.Sel.Name == "export" {
						// registerAll and the loaders build these with literals;
						// reading them back bypasses the liveness check.
						t.Errorf("%s reads .%s outside handle.go", name, n.Sel.Name)
					}
				case *ast.Ident:
					if n.Name == "liveConnections" {
						t.Errorf("%s uses liveConnections outside handle.go", name)
					}
				}
				return true
			})
		}
	}
	if raw != len(handleFree) {
		t.Errorf("found %d raw procs, want %d", raw, len(handleFree))
	}
}

func TestExtendedDisconnectReportsTheHandle(t *testing.T) {
	fake := installFakeLibrary(t)
	var token uintptr
	fake.handle("rc_on_disconnected_ex", func(args []uintptr) uintptr {
		if args[1] != 0 {
			token = args[2]
		}
		return 0
	})
	h := connectFake(t)
	var received []Handle
	if err := RegisterCallbacks(h, &EventCallbacks{
		DisconnectedEx: func(source Handle, _ string) { received = append(received, source) },
	}); err != nil {
		t.Fatal(err)
	}
	reason := []byte("Connection closed\x00")
	disconnectedExEntry(unsafe.Pointer(&fakeConnection[0]), unsafe.Pointer(&reason[0]), token)
	if len(received) != 1 || received[0] != h {
		t.Fatalf("disconnect reported %v, want handle %#x", received, uintptr(h))
	}
}
