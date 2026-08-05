package plugins

// Package plugins owns plugin discovery, validation, permissions and the small
// host-side persistence surface used by the TypeScript runtime. Plugin code is
// never given direct access to Wails bindings or the filesystem.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"graal-rc/internal/fileutil"
)

const APIVersion = 1
const maxResponseBytes int64 = 4 << 20
const maxRequestBodyBytes = 1 << 20
const maxHTTPRequestsPerMinute = 60
const maxPluginFailures = 3

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)+$`)

var (
	ErrPluginNotFound   = errors.New("plugin not found")
	ErrPermissionDenied = errors.New("plugin permission denied")
)

type Permissions struct {
	Events  []string   `json:"events,omitempty"`
	APIs    []string   `json:"apis,omitempty"`
	Network []string   `json:"network,omitempty"`
	Plugins []string   `json:"plugins,omitempty"`
	Files   FileScopes `json:"files,omitempty"`
}

// FileScopes limits plugin access to remote File Browser paths. Patterns are
// slash-separated globs and are always evaluated against the server-side path;
// they never grant access to the user's local filesystem.
type FileScopes struct {
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
}

type Manifest struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	APIVersion  int         `json:"apiVersion"`
	Main        string      `json:"main"`
	Description string      `json:"description,omitempty"`
	Permissions Permissions `json:"permissions,omitempty"`
}

type PluginInfo struct {
	Manifest          Manifest `json:"manifest"`
	Directory         string   `json:"directory"`
	Enabled           bool     `json:"enabled"`
	Status            string   `json:"status"`
	Error             string   `json:"error,omitempty"`
	ApprovedEvents    []string `json:"approvedEvents,omitempty"`
	ApprovedAPIs      []string `json:"approvedApis,omitempty"`
	ApprovedHosts     []string `json:"approvedHosts,omitempty"`
	ApprovedPlugins   []string `json:"approvedPlugins,omitempty"`
	ApprovedFileRead  []string `json:"approvedFileRead,omitempty"`
	ApprovedFileWrite []string `json:"approvedFileWrite,omitempty"`
	FailureCount      int      `json:"failureCount"`
}

type persistedState struct {
	Enabled           bool     `json:"enabled"`
	ApprovedEvents    []string `json:"approvedEvents,omitempty"`
	ApprovedAPIs      []string `json:"approvedApis,omitempty"`
	ApprovedHosts     []string `json:"approvedHosts,omitempty"`
	ApprovedPlugins   []string `json:"approvedPlugins,omitempty"`
	ApprovedFileRead  []string `json:"approvedFileRead,omitempty"`
	ApprovedFileWrite []string `json:"approvedFileWrite,omitempty"`
	FailureCount      int      `json:"failureCount,omitempty"`
}

type persistedFile struct {
	Plugins map[string]persistedState `json:"plugins"`
}

type HTTPRequest struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

type HTTPResponse struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

type Manager struct {
	mu       sync.RWMutex
	root     string
	state    string
	plugins  map[string]PluginInfo
	settings map[string]persistedState

	runtimeMu      sync.RWMutex
	runtimeEmitter func(name string, data any)

	socketsMu     sync.Mutex
	sockets       map[string]*pluginSocket
	socketOpening map[string]int

	httpMu     sync.Mutex
	httpServer *pluginHTTPServer
	requestMu  sync.Mutex
	requestLog map[string][]time.Time

	fileEditorsMu sync.RWMutex
	fileEditors   map[string]FileEditorRegistration
}

// FileEditorRegistration describes a plugin-owned editor for one or more
// remote file extensions. The handler itself remains inside the plugin iframe;
// the host stores only this metadata for routing an open request.
type FileEditorRegistration struct {
	ID         string   `json:"id"`
	PluginID   string   `json:"pluginId"`
	Label      string   `json:"label"`
	Extensions []string `json:"extensions"`
	Priority   int      `json:"priority,omitempty"`
}

// SetRuntimeEmitter connects host capabilities to the Wails event bus without
// exposing the application object to plugin code. It is intentionally a
// callback so the manager remains usable in unit tests and headless contexts.
func (m *Manager) SetRuntimeEmitter(emitter func(name string, data any)) {
	m.runtimeMu.Lock()
	m.runtimeEmitter = emitter
	m.runtimeMu.Unlock()
}

func (m *Manager) emitRuntime(name string, data any) {
	m.runtimeMu.RLock()
	emitter := m.runtimeEmitter
	m.runtimeMu.RUnlock()
	if emitter != nil {
		emitter(name, data)
	}
}

func (m *Manager) CreateTemplate(name string) (PluginInfo, error) {
	slug := slugify(name)
	if slug == "" {
		return PluginInfo{}, errors.New("plugin name must contain letters or numbers")
	}
	id := "com.gorc." + slug
	directory := filepath.Join(m.root, slug)
	if _, err := os.Stat(directory); err == nil {
		return PluginInfo{}, errors.New("a plugin with this folder already exists")
	} else if !os.IsNotExist(err) {
		return PluginInfo{}, err
	}
	manifest := Manifest{ID: id, Name: strings.TrimSpace(name), Version: "0.1.0", APIVersion: APIVersion, Main: "dist/index.js", Description: "Plugin created by GoRC.", Permissions: Permissions{}}
	if err := os.MkdirAll(filepath.Join(directory, "dist"), 0o700); err != nil {
		return PluginInfo{}, err
	}
	b, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return PluginInfo{}, err
	}
	if err := fileutil.AtomicWriteFile(filepath.Join(directory, "manifest.json"), append(b, '\n'), 0o600); err != nil {
		return PluginInfo{}, err
	}
	label, err := json.Marshal(manifest.Name)
	if err != nil {
		return PluginInfo{}, err
	}
	className := pluginClassName(manifest.Name)
	source := []byte(fmt.Sprintf("export default class %s extends Plugin {\n  onLoad() {\n    this.commands.register({ id: \"about\", label: %s }, args => {\n      console.log(\"Command arguments:\", args)\n    })\n  }\n}\n", className, label))
	bundle := []byte(fmt.Sprintf("class %s extends Plugin {\n  onLoad() {\n    this.commands.register({ id: \"about\", label: %s }, args => {\n      console.log(\"Command arguments:\", args)\n    })\n  }\n}\n\nnew %s()\n", className, label, className))
	if err := fileutil.AtomicWriteFile(filepath.Join(directory, manifest.Main), bundle, 0o600); err != nil {
		return PluginInfo{}, err
	}
	if err := os.MkdirAll(filepath.Join(directory, "src"), 0o700); err != nil {
		return PluginInfo{}, err
	}
	if err := fileutil.AtomicWriteFile(filepath.Join(directory, "src", "index.ts"), source, 0o600); err != nil {
		return PluginInfo{}, err
	}
	if err := fileutil.AtomicWriteFile(filepath.Join(directory, "src", "plugin.d.ts"), []byte(pluginTypeDeclarations), 0o600); err != nil {
		return PluginInfo{}, err
	}
	if err := m.Discover(); err != nil {
		return PluginInfo{}, err
	}
	m.mu.RLock()
	info := cloneInfo(m.plugins[id])
	m.mu.RUnlock()
	return info, nil
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func pluginClassName(value string) string {
	slug := slugify(value)
	var b strings.Builder
	b.WriteString("Gorc")
	upper := true
	for _, r := range slug {
		if r == '-' {
			upper = true
			continue
		}
		if upper {
			if r >= 'a' && r <= 'z' {
				r -= 'a' - 'A'
			}
			upper = false
		}
		b.WriteRune(r)
	}
	if b.Len() == len("Gorc") {
		return "GorcPlugin"
	}
	return b.String()
}

const pluginTypeDeclarations = `type PluginEventName =
  | "rc.connected" | "rc.disconnected" | "rc.message" | "irc.message" | "irc.channels"
  | "pm.received" | "pm.sent" | "script.received" | "script.opened" | "script.saved" | "script.conflict"
  | "script.identity.changed" | "script.permissions.changed" | "weapon.changed" | "class.changed" | "npc.changed"
  | "npc.flags" | "npc.attributes" | "filebrowser.file.opening" | "filebrowser.file.opened"
  | "filebrowser.file.read" | "filebrowser.file.saved" | "filebrowser.selection.changed"
  | "filebrowser.folders" | "filebrowser.files" | "filebrowser.message" | "filebrowser.maxUpload"
  | "filebrowser.changed" | "filebrowser.started" | "filebrowser.directory.changed"
  | "player.rights" | "player.attributes" | "player.ban" | "player.banList"
  | "player.text" | "nc.serverdata" | "nc.connected" | "nc.disconnected" | "sync.progress" | "sync.status"

interface PluginConsole {
  log(...data: unknown[]): void
  info(...data: unknown[]): void
  warn(...data: unknown[]): void
  error(...data: unknown[]): void
}

declare const console: PluginConsole

interface PluginCommand {
  id: string
  label: string
  description?: string
}

type PluginCommandHandler = (args: string[]) => void | Promise<void>
type PluginHttpBody = string | Record<string, unknown> | unknown[] | number | boolean | null

interface PluginSocketMessage {
  data: string
  binary: boolean
}

interface PluginSocketClose {
  code: number
  reason: string
}

interface PluginSocket {
  readonly id: string
  readonly url: string
  readonly readyState: string
  on(event: "open" | "message" | "error" | "close", listener: (value?: PluginSocketMessage | PluginSocketClose | Error) => void): () => void
  send(data: string): Promise<void>
  close(): Promise<void>
}

interface PluginPeer {
  id: string
  name: string
  version: string
  enabled: boolean
  status: string
}

interface PluginExpressRequest {
  method: string
  path: string
  query: Record<string, string[]>
  headers: Record<string, string>
  body: string
  json<T = unknown>(): T | null
}

interface PluginExpressResponse {
  status(code: number): PluginExpressResponse
  set(headers: Record<string, string>): PluginExpressResponse
  json(value: unknown): PluginExpressResponse
  send(value: unknown): PluginExpressResponse
  text(value: string): PluginExpressResponse
  end(): PluginExpressResponse
}

type PluginExpressHandler = (request: PluginExpressRequest, response: PluginExpressResponse) => void | Promise<void>

interface PluginExpressServer {
  readonly baseUrl: string
  readonly token: string
  get(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
  post(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
  put(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
  patch(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
  delete(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
}

interface PluginPanel {
  id: string
  title: string
  html?: string
}

interface PluginRemoteFile {
  path: string
  name: string
  extension: string
  size: number
  revision: string
  content: string
}

interface PluginScriptDocument {
  type: string
  name: string
  id: number
  script: string
}

interface PluginScriptIndex {
  weapons: Array<{name: string}>
  classes: Array<{name: string}>
  npcs: Array<{id: number; name: string; type: string; level: string}>
}

interface PluginFileEditor {
  id: string
  label: string
  extensions: string[]
  priority?: number
}

type PluginFileEditorHandler = (file: Pick<PluginRemoteFile, "path" | "name" | "extension">) => boolean | void | Promise<boolean | void | {handled: boolean}>

type PluginUIPrimitive = string | number | boolean | null
type PluginUIView =
  | {type: "stack" | "row"; children: PluginUIView[]; gap?: number}
  | {type: "text" | "heading"; text: string; tone?: "default" | "muted" | "danger" | "success"}
  | {type: "divider"}
  | {type: "button"; id: string; label: string; action: string; disabled?: boolean; variant?: "default" | "secondary" | "danger"}
  | {type: "input"; id: string; label: string; value?: string; placeholder?: string; action?: string}
  | {type: "select"; id: string; label: string; value?: string; options: Array<{label: string; value: string}>; action?: string}
  | {type: "code"; language?: string; value: string}
  | {type: "table"; columns: Array<{key: string; label: string}>; rows: Array<Record<string, PluginUIPrimitive>>}

interface PluginUIWindowOptions {
  id: string
  title: string
  width?: number
  height?: number
  view: PluginUIView
}

interface PluginUIWindow extends PluginUIWindowOptions {
  pluginId: string
}

interface PluginUIAction {
  windowId: string
  action: string
  value?: PluginUIPrimitive
}

type PluginNotificationLevel = "info" | "success" | "warning" | "error"

interface PluginNotification {
  title?: string
  message: string
  level?: PluginNotificationLevel
  durationMs?: number
}

interface PluginMonacoDiagnostic {
  message: string
  severity?: 1 | 2 | 3 | 4
  startLine: number
  startColumn: number
  endLine: number
  endColumn: number
  source?: string
}

interface PluginMonacoCompletion {
  label: string
  insertText: string
  kind?: "text" | "method" | "function" | "field" | "property" | "keyword"
  detail?: string
  documentation?: string
}

interface PluginMonacoCompletionContext {
  language: string
  uri: string
  text: string
  position: {line: number; column: number}
}

interface PluginMonacoDocumentContext {
  language: string
  uri: string
  text: string
}

interface PluginContext {
  readonly id: string
  readonly events: {
    on<T = unknown>(name: PluginEventName | (string & {}), listener: (...data: T[]) => void): () => void
  }
  readonly commands: {
    register(command: PluginCommand, handler?: PluginCommandHandler): () => void
  }
  readonly panels: {
    register(panel: PluginPanel): () => void
  }
  readonly storage: {
    get(key: string): Promise<string | null>
    set(key: string, value: string): Promise<void>
    delete(key: string): Promise<void>
  }
  readonly secrets: {
    get(key: string): Promise<string | null>
    set(key: string, value: string): Promise<void>
    delete(key: string): Promise<void>
  }
  readonly network: {
    request(request: {url: string; method?: string; headers?: Record<string, string>; body?: PluginHttpBody}): Promise<{status: number; headers: Record<string, string>; body: string}>
  }
  readonly sockets: {
    connect(request: {url: string; protocols?: string[]}): Promise<PluginSocket>
  }
  readonly plugins: {
    list(): Promise<PluginPeer[]>
    on(channel: string, listener: (message: {from: string; channel: string; data: unknown}) => void): () => void
    send(targetId: string, channel: string, data: unknown): Promise<void>
    call<T = unknown>(targetId: string, method: string, data: unknown): Promise<T>
    expose<T = unknown>(method: string, handler: (data: unknown) => T | Promise<T>): () => void
  }
  readonly express: {
    listen(): Promise<PluginExpressServer>
  }
  readonly fileBrowser: {
    readText(path: string): Promise<PluginRemoteFile>
    writeText(path: string, content: string, options?: {expectedRevision?: string}): Promise<PluginRemoteFile>
    editors: {register(editor: PluginFileEditor, handler: PluginFileEditorHandler): () => Promise<void>}
  }
  readonly ui: {
    windows: {open(options: PluginUIWindowOptions): Promise<PluginUIWindow>}
    onAction(listener: (action: PluginUIAction) => void | Promise<void>): () => void
    onClosed(listener: (windowId: string) => void): () => void
  }
  readonly notifications: {
    show(notification: PluginNotification): void
    info(message: string, title?: string): void
    success(message: string, title?: string): void
    warning(message: string, title?: string): void
    error(message: string, title?: string): void
  }
  readonly monaco: {
    languages: {register(language: {id: string; extensions?: string[]; aliases?: string[]}): () => Promise<void>}
    diagnostics: {register(language: string, handler: (document: PluginMonacoDocumentContext) => PluginMonacoDiagnostic[] | Promise<PluginMonacoDiagnostic[]>): () => Promise<void>}
    completions: {register(language: string, handler: (context: PluginMonacoCompletionContext) => PluginMonacoCompletion[] | Promise<PluginMonacoCompletion[]>): () => Promise<void>}
  }
  readonly nc: {
    readWeapon(name: string): Promise<PluginScriptDocument>
    readClass(name: string): Promise<PluginScriptDocument>
    readNPC(id: number): Promise<PluginScriptDocument>
    readNPCFlags(id: number): Promise<PluginScriptDocument>
    readNPCAttributes(id: number): Promise<PluginScriptDocument>
    list(): Promise<PluginScriptIndex>
    saveWeapon(name: string, script: string): Promise<void>
    saveClass(name: string, script: string): Promise<void>
    saveNPC(id: number, script: string): Promise<void>
    saveNPCFlags(id: number, flags: string): Promise<void>
    createWeapon(name: string): Promise<void>
    deleteWeapon(name: string): Promise<void>
    createClass(name: string): Promise<void>
    deleteClass(name: string): Promise<void>
    createNPC(options: {name: string; id: number; type: string; scripter: string; level: string; x: string; y: string}): Promise<void>
    deleteNPC(id: number): Promise<void>
    resetNPC(id: number): Promise<void>
  }
  readonly automation: {
    timeout(handler: () => void | Promise<void>, delayMs: number): () => void
    interval(handler: () => void | Promise<void>, intervalMs: number): () => void
    schedule(handler: () => void | Promise<void>, options?: {delayMs?: number; intervalMs?: number; immediate?: boolean}): {cancel(): void; pause(): void; resume(): void}
    debounce<T extends unknown[]>(handler: (...args: T) => void | Promise<void>, waitMs: number): (...args: T) => void
    retry<T>(operation: () => Promise<T>, options?: {attempts?: number; delayMs?: number; backoff?: number}): Promise<T>
  }
  readonly actions: {
    pm: {send(playerId: number, message: string): Promise<void>}
    admin: {send(playerId: number, message: string): Promise<void>}
    rc: {execute(message: string): Promise<void>}
    nc: {
      saveWeapon(name: string, script: string): Promise<void>
      saveClass(name: string, script: string): Promise<void>
      saveNPC(id: number, script: string): Promise<void>
    }
  }
}

declare class Plugin {
  readonly api: PluginContext
  readonly events: PluginContext["events"]
  readonly commands: PluginContext["commands"]
  readonly panels: PluginContext["panels"]
  readonly storage: PluginContext["storage"]
  readonly secrets: PluginContext["secrets"]
  readonly network: PluginContext["network"]
  readonly sockets: PluginContext["sockets"]
  readonly plugins: PluginContext["plugins"]
  readonly express: PluginContext["express"]
  readonly fileBrowser: PluginContext["fileBrowser"]
  readonly ui: PluginContext["ui"]
  readonly notifications: PluginContext["notifications"]
  readonly monaco: PluginContext["monaco"]
  readonly nc: PluginContext["nc"]
  readonly automation: PluginContext["automation"]
  readonly actions: PluginContext["actions"]
  onLoad?(api?: PluginContext): void | Promise<void>
  onStart?(): void | Promise<void>
  onStop?(): void | Promise<void>
  onUnload?(): void | Promise<void>
}`

func NewManager() (*Manager, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(dir, "graal-rc", "plugins")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{root: root, state: filepath.Join(filepath.Dir(root), "plugins.json"), plugins: map[string]PluginInfo{}, settings: map[string]persistedState{}, sockets: map[string]*pluginSocket{}, socketOpening: map[string]int{}, fileEditors: map[string]FileEditorRegistration{}, requestLog: map[string][]time.Time{}}
	if err := m.loadState(); err != nil {
		return nil, err
	}
	return m, m.Discover()
}

func (m *Manager) Root() string { return m.root }

func (m *Manager) loadState() error {
	b, found, err := fileutil.ReadAndRecover(m.state, 0o600, func(payload []byte) error {
		var f persistedFile
		return json.Unmarshal(payload, &f)
	})
	if err != nil {
		return fmt.Errorf("plugins state: %w", err)
	}
	if !found {
		return nil
	}
	var f persistedFile
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("plugins state: %w", err)
	}
	if f.Plugins != nil {
		m.settings = f.Plugins
	}
	return nil
}

func (m *Manager) persistLocked() error {
	b, err := json.MarshalIndent(persistedFile{Plugins: m.settings}, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileWithValidator(m.state, b, 0o600, func(payload []byte) error {
		var f persistedFile
		return json.Unmarshal(payload, &f)
	})
}

func (m *Manager) Discover() error {
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	found := map[string]PluginInfo{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		manifestPath := filepath.Join(m.root, id, "manifest.json")
		b, readErr := os.ReadFile(manifestPath)
		if readErr != nil {
			continue
		}
		var manifest Manifest
		info := PluginInfo{Directory: filepath.Join(m.root, id), Status: "invalid"}
		if jsonErr := json.Unmarshal(b, &manifest); jsonErr != nil {
			info.Error = "invalid manifest: " + jsonErr.Error()
			found[id] = info
			continue
		}
		info.Manifest = manifest
		if validationErr := ValidateManifest(manifest); validationErr != nil {
			info.Error = validationErr.Error()
			found[id] = info
			continue
		}
		if _, err := safePluginPath(info.Directory, manifest.Main); err != nil {
			info.Error = err.Error()
			found[id] = info
			continue
		}
		state := m.settings[manifest.ID]
		info.Enabled = state.Enabled
		info.FailureCount = state.FailureCount
		info.ApprovedEvents = intersect(state.ApprovedEvents, manifest.Permissions.Events)
		info.ApprovedAPIs = intersect(state.ApprovedAPIs, manifest.Permissions.APIs)
		info.ApprovedHosts = intersect(state.ApprovedHosts, manifest.Permissions.Network)
		info.ApprovedPlugins = intersect(state.ApprovedPlugins, manifest.Permissions.Plugins)
		info.ApprovedFileRead = intersect(state.ApprovedFileRead, manifest.Permissions.Files.Read)
		info.ApprovedFileWrite = intersect(state.ApprovedFileWrite, manifest.Permissions.Files.Write)
		info.Status = "disabled"
		if info.Enabled {
			info.Status = "ready"
		}
		found[manifest.ID] = info
	}
	m.mu.Lock()
	m.plugins = found
	m.mu.Unlock()
	return nil
}

func ValidateManifest(m Manifest) error {
	if !pluginIDPattern.MatchString(m.ID) {
		return errors.New("plugin id must be a reverse-domain identifier")
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("plugin name is required")
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("plugin version is required")
	}
	if m.APIVersion != APIVersion {
		return fmt.Errorf("unsupported plugin api version %d (supported: %d)", m.APIVersion, APIVersion)
	}
	if m.Main == "" || filepath.IsAbs(m.Main) || strings.Contains(m.Main, "..") {
		return errors.New("plugin main must be a relative path")
	}
	if filepath.Ext(m.Main) != ".js" {
		return errors.New("plugin main must be a JavaScript bundle")
	}
	for _, target := range m.Permissions.Plugins {
		if !pluginIDPattern.MatchString(target) {
			return fmt.Errorf("plugin permission target %q is not a reverse-domain identifier", target)
		}
	}
	for _, pattern := range append(append([]string{}, m.Permissions.Files.Read...), m.Permissions.Files.Write...) {
		if err := validateFileScope(pattern); err != nil {
			return err
		}
	}
	return nil
}

func safePluginPath(root, relative string) (string, error) {
	path := filepath.Clean(filepath.Join(root, relative))
	base, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return "", errors.New("plugin main escapes plugin directory")
	}
	return abs, nil
}

func (m *Manager) List() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]PluginInfo, 0, len(m.plugins))
	for _, p := range m.plugins {
		result = append(result, cloneInfo(p))
	}
	return result
}

func (m *Manager) SetEnabled(id string, enabled bool) error {
	if !enabled {
		m.ClosePluginSockets(id)
		m.ClosePluginHTTP(id)
		m.ClosePluginEditors(id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[id]
	if !ok {
		return ErrPluginNotFound
	}
	state := m.settings[id]
	state.Enabled = enabled
	if enabled {
		state.FailureCount = 0
	}
	m.settings[id] = state
	if err := m.persistLocked(); err != nil {
		return err
	}
	p.Enabled = enabled
	if enabled {
		p.FailureCount = 0
		p.Error = ""
	}
	p.Status = "disabled"
	if enabled {
		p.Status = "ready"
	}
	m.plugins[id] = p
	return nil
}

// RecordPluginFailure persists a failed sandbox start/lifecycle execution.
// Keeping this in the host makes the circuit breaker effective across reloads
// and prevents a broken plugin from repeatedly consuming runtime resources.
func (m *Manager) RecordPluginFailure(id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[id]
	if !ok {
		return false, ErrPluginNotFound
	}
	state := m.settings[id]
	state.FailureCount++
	p.FailureCount = state.FailureCount
	disabled := state.FailureCount >= maxPluginFailures
	if disabled {
		state.Enabled = false
		p.Enabled = false
		p.Status = "disabled"
		p.Error = fmt.Sprintf("disabled after %d consecutive runtime failures", state.FailureCount)
	}
	m.settings[id] = state
	if err := m.persistLocked(); err != nil {
		return false, err
	}
	m.plugins[id] = p
	return disabled, nil
}

// RecordPluginSuccess clears the circuit-breaker counter after a successful
// sandbox initialization. It does not change the user's enabled preference.
func (m *Manager) RecordPluginSuccess(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[id]
	if !ok {
		return ErrPluginNotFound
	}
	if p.FailureCount == 0 && m.settings[id].FailureCount == 0 {
		return nil
	}
	state := m.settings[id]
	state.FailureCount = 0
	m.settings[id] = state
	p.FailureCount = 0
	m.plugins[id] = p
	return m.persistLocked()
}

func (m *Manager) Approve(id string, permissions Permissions) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[id]
	if !ok {
		return ErrPluginNotFound
	}
	state := m.settings[id]
	state.ApprovedEvents = intersect(permissions.Events, p.Manifest.Permissions.Events)
	state.ApprovedAPIs = intersect(permissions.APIs, p.Manifest.Permissions.APIs)
	state.ApprovedHosts = intersect(permissions.Network, p.Manifest.Permissions.Network)
	state.ApprovedPlugins = intersect(permissions.Plugins, p.Manifest.Permissions.Plugins)
	state.ApprovedFileRead = intersect(permissions.Files.Read, p.Manifest.Permissions.Files.Read)
	state.ApprovedFileWrite = intersect(permissions.Files.Write, p.Manifest.Permissions.Files.Write)
	m.settings[id] = state
	if err := m.persistLocked(); err != nil {
		return err
	}
	p.ApprovedEvents, p.ApprovedAPIs, p.ApprovedHosts, p.ApprovedPlugins = state.ApprovedEvents, state.ApprovedAPIs, state.ApprovedHosts, state.ApprovedPlugins
	p.ApprovedFileRead, p.ApprovedFileWrite = state.ApprovedFileRead, state.ApprovedFileWrite
	m.plugins[id] = p
	return nil
}

func (m *Manager) Remove(id string) error {
	m.ClosePluginSockets(id)
	m.ClosePluginHTTP(id)
	m.ClosePluginEditors(id)
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plugins[id]
	if !ok {
		return ErrPluginNotFound
	}
	if err := os.RemoveAll(p.Directory); err != nil {
		return err
	}
	delete(m.plugins, id)
	delete(m.settings, id)
	return m.persistLocked()
}

func (m *Manager) Bundle(id string) (string, error) {
	m.mu.RLock()
	p, ok := m.plugins[id]
	m.mu.RUnlock()
	if !ok {
		return "", ErrPluginNotFound
	}
	path, err := safePluginPath(p.Directory, p.Manifest.Main)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(b) > 8<<20 {
		return "", errors.New("plugin bundle exceeds 8 MiB")
	}
	return string(b), nil
}

func (m *Manager) KVPath(id string) (string, error) {
	m.mu.RLock()
	_, ok := m.plugins[id]
	m.mu.RUnlock()
	if !ok {
		return "", ErrPluginNotFound
	}
	return filepath.Join(m.root, ".data", id+".json"), nil
}

func (m *Manager) Request(id string, req HTTPRequest) (HTTPResponse, error) {
	m.mu.RLock()
	p, ok := m.plugins[id]
	m.mu.RUnlock()
	if !ok {
		return HTTPResponse{}, ErrPluginNotFound
	}
	u, err := url.Parse(req.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return HTTPResponse{}, errors.New("only HTTPS plugin requests are allowed")
	}
	if !contains(p.ApprovedHosts, origin(u)) {
		return HTTPResponse{}, ErrPermissionDenied
	}
	if !contains(p.ApprovedAPIs, "network.http") {
		return HTTPResponse{}, ErrPermissionDenied
	}
	if len(req.Body) > maxRequestBodyBytes {
		return HTTPResponse{}, errors.New("plugin request body exceeds 1 MiB")
	}
	if err := m.allowHTTPRequest(id); err != nil {
		return HTTPResponse{}, err
	}
	method := strings.ToUpper(req.Method)
	if method == "" {
		method = http.MethodGet
	}
	request, err := http.NewRequest(method, u.String(), strings.NewReader(req.Body))
	if err != nil {
		return HTTPResponse{}, err
	}
	for key, value := range req.Headers {
		if strings.EqualFold(key, "authorization") || strings.EqualFold(key, "cookie") {
			continue
		}
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return HTTPResponse{}, err
	}
	defer resp.Body.Close()
	limited := ioLimitReader(resp.Body, maxResponseBytes)
	b, err := limited()
	if err != nil {
		return HTTPResponse{}, err
	}
	headers := map[string]string{}
	for key := range resp.Header {
		headers[key] = resp.Header.Get(key)
	}
	return HTTPResponse{Status: resp.StatusCode, Headers: headers, Body: string(b)}, nil
}

func (m *Manager) allowHTTPRequest(id string) error {
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	m.requestMu.Lock()
	defer m.requestMu.Unlock()
	if m.requestLog == nil {
		m.requestLog = map[string][]time.Time{}
	}
	entries := m.requestLog[id]
	first := 0
	for first < len(entries) && entries[first].Before(cutoff) {
		first++
	}
	entries = entries[first:]
	if len(entries) >= maxHTTPRequestsPerMinute {
		m.requestLog[id] = entries
		return errors.New("plugin network request rate limit exceeded")
	}
	m.requestLog[id] = append(entries, now)
	return nil
}

func (m *Manager) RequireAPI(id, api string) error {
	m.mu.RLock()
	p, ok := m.plugins[id]
	m.mu.RUnlock()
	if !ok {
		return ErrPluginNotFound
	}
	if !p.Enabled || !contains(p.ApprovedAPIs, api) {
		return ErrPermissionDenied
	}
	return nil
}

// PluginPeer is the intentionally small, non-sensitive view exposed to other
// plugins. It never includes a directory, token, secret, or permission list.
type PluginPeer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

func (m *Manager) ListPeers(id string) ([]PluginPeer, error) {
	if err := m.RequireAPI(id, "plugins.messaging"); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	peers := make([]PluginPeer, 0, len(m.plugins))
	for _, plugin := range m.plugins {
		if plugin.Manifest.ID == id {
			continue
		}
		peers = append(peers, PluginPeer{ID: plugin.Manifest.ID, Name: plugin.Manifest.Name, Version: plugin.Manifest.Version, Enabled: plugin.Enabled, Status: plugin.Status})
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].ID < peers[j].ID })
	return peers, nil
}

func (m *Manager) AuthorizePluginMessage(senderID, targetID string) error {
	if senderID == targetID || targetID == "" {
		return ErrPermissionDenied
	}
	m.mu.RLock()
	sender, senderOK := m.plugins[senderID]
	target, targetOK := m.plugins[targetID]
	m.mu.RUnlock()
	if !senderOK || !targetOK || !sender.Enabled || !target.Enabled || sender.Status != "ready" || target.Status != "ready" {
		return ErrPermissionDenied
	}
	if !contains(sender.ApprovedAPIs, "plugins.messaging") || !contains(target.ApprovedAPIs, "plugins.messaging") || !contains(sender.ApprovedPlugins, targetID) {
		return ErrPermissionDenied
	}
	return nil
}

func ioLimitReader(body io.Reader, limit int64) func() ([]byte, error) {
	return func() ([]byte, error) {
		b := make([]byte, 0, 4096)
		buf := make([]byte, 32*1024)
		var total int64
		for {
			n, err := body.Read(buf)
			if n > 0 {
				total += int64(n)
				if total > limit {
					return nil, errors.New("plugin response exceeds 4 MiB")
				}
				b = append(b, buf[:n]...)
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					return b, nil
				}
				return nil, err
			}
		}
	}
}

func origin(u *url.URL) string { return u.Scheme + "://" + u.Host }
func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
func intersect(requested, allowed []string) []string {
	result := []string{}
	for _, value := range requested {
		if contains(allowed, value) && !contains(result, value) {
			result = append(result, value)
		}
	}
	return result
}
func cloneInfo(p PluginInfo) PluginInfo {
	p.ApprovedEvents = append([]string(nil), p.ApprovedEvents...)
	p.ApprovedAPIs = append([]string(nil), p.ApprovedAPIs...)
	p.ApprovedHosts = append([]string(nil), p.ApprovedHosts...)
	p.ApprovedPlugins = append([]string(nil), p.ApprovedPlugins...)
	p.ApprovedFileRead = append([]string(nil), p.ApprovedFileRead...)
	p.ApprovedFileWrite = append([]string(nil), p.ApprovedFileWrite...)
	return p
}
