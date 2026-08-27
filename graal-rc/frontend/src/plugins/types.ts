export type PluginEventName =
  | "rc.connected"
  | "rc.disconnected"
  | "rc.message"
  | "irc.message"
  | "irc.channels"
  | "pm.received"
  | "pm.sent"
  | "script.received"
  | "script.opened"
  | "script.saved"
  | "script.conflict"
  | "script.identity.changed"
  | "script.permissions.changed"
  | "sync.progress"
  | "sync.status"
  | "weapon.changed"
  | "class.changed"
  | "npc.changed"
  | "npc.flags"
  | "npc.attributes"
  | "filebrowser.file.opening"
  | "filebrowser.file.opened"
  | "filebrowser.file.read"
  | "filebrowser.file.saved"
  | "filebrowser.selection.changed"
  | "filebrowser.folders"
  | "filebrowser.files"
  | "filebrowser.message"
  | "filebrowser.maxUpload"
  | "filebrowser.changed"
  | "filebrowser.started"
  | "filebrowser.directory.changed"
  | "player.rights"
  | "player.attributes"
  | "player.ban"
  | "player.banList"
  | "player.text"
  | "nc.serverdata"
  | "nc.connected"
  | "nc.disconnected"

export interface PluginManifest {
  id: string
  name: string
  version: string
  apiVersion: number
  main: string
  description?: string
  permissions?: {events?: string[]; apis?: string[]; network?: string[]; plugins?: string[]; files?: {read?: string[]; write?: string[]}}
}

export interface PluginInfo {
  manifest: PluginManifest
  directory: string
  enabled: boolean
  status: "ready" | "disabled" | "invalid" | string
  error?: string
  approvedEvents?: string[]
  approvedApis?: string[]
  approvedHosts?: string[]
  approvedPlugins?: string[]
  approvedFileRead?: string[]
  approvedFileWrite?: string[]
  failureCount: number
}

export interface PluginEvent<T = unknown> {
  name: PluginEventName | string
  data: T[]
}

export interface PluginCommand {
  id: string
  label: string
  description?: string
}

export interface PluginPanel {
  id: string
  title: string
  html?: string
}

export type PluginUITabIcon = "dashboard" | "terminal" | "settings" | "puzzle"

export interface PluginUITabOptions {
  id: string
  title: string
  icon?: PluginUITabIcon
  order?: number
  open?: boolean
  view: PluginUIView
}

export interface PluginUITabInfo extends PluginUITabOptions {
  pluginId: string
}

export interface PluginRemoteFile {
  path: string
  name: string
  extension: string
  size: number
  revision: string
  content: string
}

export interface PluginScriptDocument {
  type: string
  name: string
  id: number
  script: string
}

export interface PluginScriptIndex {
  weapons: Array<{name: string}>
  classes: Array<{name: string}>
  npcs: Array<{id: number; name: string; type: string; level: string}>
}

export interface PluginFileEditor {
  id: string
  label: string
  extensions: string[]
  priority?: number
}

export type PluginFileEditorHandler = (file: Pick<PluginRemoteFile, "path" | "name" | "extension">) => boolean | void | Promise<boolean | void | {handled: boolean}>

export type PluginUIPrimitive = string | number | boolean | null
export type PluginUIView =
  | {type: "stack" | "row"; children: PluginUIView[]; gap?: number}
  | {type: "card"; title?: string; description?: string; children: PluginUIView[]}
  | {type: "text" | "heading"; text: string; tone?: "default" | "muted" | "danger" | "success"}
  | {type: "badge"; text: string; tone?: "default" | "muted" | "danger" | "success"}
  | {type: "divider"}
  | {type: "button"; id: string; label: string; action: string; disabled?: boolean; variant?: "default" | "secondary" | "danger"}
  | {type: "input"; id: string; label: string; value?: string; placeholder?: string; action?: string}
  | {type: "textarea"; id: string; label: string; value?: string; placeholder?: string; rows?: number; action?: string}
  | {type: "checkbox"; id: string; label: string; value?: boolean; action?: string}
  | {type: "select"; id: string; label: string; value?: string; options: Array<{label: string; value: string}>; action?: string}
  | {type: "progress"; value: number; max?: number; label?: string}
  | {type: "empty"; title: string; description?: string}
  | {type: "code"; language?: string; value: string}
  | {type: "table"; columns: Array<{key: string; label: string}>; rows: Array<Record<string, PluginUIPrimitive>>}

export interface PluginUIWindowOptions {
  id: string
  title: string
  width?: number
  height?: number
  view: PluginUIView
}

export interface PluginUIWindowInfo extends PluginUIWindowOptions {
  pluginId: string
}

export interface PluginUIWindow {
  readonly id: string
  update(view: PluginUIView): Promise<void>
  close(): Promise<void>
}

export interface PluginUITab {
  readonly id: string
  update(view: PluginUIView): void
  open(): void
  close(): void
}

export interface PluginUIAction {
  windowId: string
  action: string
  value?: PluginUIPrimitive
}

export interface PluginUITabAction {
  tabId: string
  action: string
  value?: PluginUIPrimitive
}

export type PluginNotificationLevel = "info" | "success" | "warning" | "error"

export interface PluginNotification {
  title?: string
  message: string
  level?: PluginNotificationLevel
  durationMs?: number
}

export interface PluginMonacoDiagnostic {
  message: string
  severity?: 1 | 2 | 3 | 4
  startLine: number
  startColumn: number
  endLine: number
  endColumn: number
  source?: string
}

export interface PluginMonacoLanguage {
  id: string
  extensions?: string[]
  aliases?: string[]
}

export interface PluginMonacoCompletion {
  label: string
  insertText: string
  kind?: "text" | "method" | "function" | "field" | "property" | "keyword"
  detail?: string
  documentation?: string
}

export interface PluginMonacoCompletionContext {
  language: string
  uri: string
  text: string
  position: {line: number; column: number}
}

export interface PluginMonacoDocumentContext {
  language: string
  uri: string
  text: string
}

export interface PluginSocketMessage {
  data: string
  binary: boolean
}

export interface PluginSocketClose {
  code: number
  reason: string
}

export interface PluginPeer {
  id: string
  name: string
  version: string
  enabled: boolean
  status: string
}

export interface PluginHttpRequestEvent {
  pluginId: string
  routeId: string
  requestId: string
  method: string
  path: string
  query?: Record<string, string[]>
  headers?: Record<string, string>
  body?: string
}

export interface PluginFile {path: string; content: string}
export interface PluginBuildResult {plugin: PluginInfo; success: boolean; message: string}
export interface PluginLogEntry {timestamp: number; level: string; message: string}
