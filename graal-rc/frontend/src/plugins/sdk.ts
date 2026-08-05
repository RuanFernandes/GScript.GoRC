import type {PluginCommand, PluginEventName, PluginFileEditor, PluginFileEditorHandler, PluginMonacoCompletion, PluginMonacoCompletionContext, PluginMonacoDiagnostic, PluginMonacoDocumentContext, PluginNotification, PluginPanel, PluginPeer, PluginRemoteFile, PluginScriptDocument, PluginScriptIndex, PluginSocketClose, PluginSocketMessage, PluginUIAction, PluginUIView, PluginUIWindow, PluginUIWindowOptions} from "./types"

export type PluginCommandHandler = (args: string[]) => void | Promise<void>
export type PluginHttpBody = string | Record<string, unknown> | unknown[] | number | boolean | null

export interface PluginSocket {
  readonly id: string
  readonly url: string
  readonly readyState: string
  on(event: "open" | "message" | "error" | "close", listener: (value?: PluginSocketMessage | PluginSocketClose | Error) => void): () => void
  send(data: string): Promise<void>
  close(): Promise<void>
}

export interface PluginMessage {
  from: string
  channel: string
  data: unknown
}

export interface PluginExpressRequest {
  method: string
  path: string
  query: Record<string, string[]>
  headers: Record<string, string>
  body: string
  json<T = unknown>(): T | null
}

export interface PluginExpressResponse {
  status(code: number): PluginExpressResponse
  set(headers: Record<string, string>): PluginExpressResponse
  json(value: unknown): PluginExpressResponse
  send(value: unknown): PluginExpressResponse
  text(value: string): PluginExpressResponse
  end(): PluginExpressResponse
}

export type PluginExpressHandler = (request: PluginExpressRequest, response: PluginExpressResponse) => void | Promise<void>

export interface PluginFileBrowser {
  readText(path: string): Promise<PluginRemoteFile>
  writeText(path: string, content: string, options?: {expectedRevision?: string}): Promise<PluginRemoteFile>
  editors: {
    register(editor: PluginFileEditor, handler: PluginFileEditorHandler): () => Promise<void>
  }
}

export interface PluginUI {
  windows: {
    open(options: PluginUIWindowOptions): Promise<PluginUIWindow>
  }
  onAction(listener: (action: PluginUIAction) => void | Promise<void>): () => void
  onClosed(listener: (windowId: string) => void): () => void
}

export interface PluginNotifications {
  show(notification: PluginNotification): void
  info(message: string, title?: string): void
  success(message: string, title?: string): void
  warning(message: string, title?: string): void
  error(message: string, title?: string): void
}

export interface PluginMonaco {
  languages: {
    register(language: {id: string; extensions?: string[]; aliases?: string[]}): () => Promise<void>
  }
  diagnostics: {
    register(language: string, handler: (document: PluginMonacoDocumentContext) => PluginMonacoDiagnostic[] | Promise<PluginMonacoDiagnostic[]>): () => Promise<void>
  }
  completions: {
    register(language: string, handler: (context: PluginMonacoCompletionContext) => PluginMonacoCompletion[] | Promise<PluginMonacoCompletion[]>): () => Promise<void>
  }
}

export interface PluginNC {
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

export interface PluginAutomation {
  timeout(handler: () => void | Promise<void>, delayMs: number): () => void
  interval(handler: () => void | Promise<void>, intervalMs: number): () => void
  schedule(handler: () => void | Promise<void>, options?: {delayMs?: number; intervalMs?: number; immediate?: boolean}): PluginScheduledJob
  debounce<T extends unknown[]>(handler: (...args: T) => void | Promise<void>, waitMs: number): (...args: T) => void
  retry<T>(operation: () => Promise<T>, options?: {attempts?: number; delayMs?: number; backoff?: number}): Promise<T>
}

export interface PluginScheduledJob {
  cancel(): void
  pause(): void
  resume(): void
}

export interface PluginExpressServer {
  readonly baseUrl: string
  readonly token: string
  get(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
  post(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
  put(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
  patch(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
  delete(path: string, handler: PluginExpressHandler): Promise<() => Promise<void>>
}

export interface PluginContext {
  readonly id: string
  readonly events: {
    on<T = unknown>(name: PluginEventName | (string & {}), listener: (...data: T[]) => void): () => void
  }
  readonly commands: {register(command: PluginCommand, handler?: PluginCommandHandler): () => void}
  readonly panels: {register(panel: PluginPanel): () => void}
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
    on(channel: string, listener: (message: PluginMessage) => void): () => void
    send(targetId: string, channel: string, data: unknown): Promise<void>
    call<T = unknown>(targetId: string, method: string, data: unknown): Promise<T>
    expose<T = unknown>(method: string, handler: (data: unknown) => T | Promise<T>): () => void
  }
  readonly express: {
    listen(): Promise<PluginExpressServer>
  }
  readonly fileBrowser: PluginFileBrowser
  readonly ui: PluginUI
  readonly notifications: PluginNotifications
  readonly monaco: PluginMonaco
  readonly nc: PluginNC
  readonly automation: PluginAutomation
  readonly actions: {
    pm: {send(playerId: number, message: string): Promise<void>}
    admin: {send(playerId: number, message: string): Promise<void>}
    rc: {execute(message: string): Promise<void>}
    nc: {saveWeapon(name: string, script: string): Promise<void>; saveClass(name: string, script: string): Promise<void>; saveNPC(id: number, script: string): Promise<void>}
  }
}

export type PluginLifecycle = {
  onLoad?(api?: PluginContext): void | Promise<void>
  onStart?(): void | Promise<void>
  onStop?(): void | Promise<void>
  onUnload?(): void | Promise<void>
}

// Base class used by plugins created in the GoRC workspace. The host injects
// the concrete runtime context when it instantiates the bundle; plugin authors
// only need to extend this class and instantiate their implementation.
export class Plugin implements PluginLifecycle {
  api!: PluginContext
  events!: PluginContext["events"]
  commands!: PluginContext["commands"]
  panels!: PluginContext["panels"]
  storage!: PluginContext["storage"]
  secrets!: PluginContext["secrets"]
  network!: PluginContext["network"]
  sockets!: PluginContext["sockets"]
  plugins!: PluginContext["plugins"]
  express!: PluginContext["express"]
  fileBrowser!: PluginContext["fileBrowser"]
  ui!: PluginContext["ui"]
  notifications!: PluginContext["notifications"]
  monaco!: PluginContext["monaco"]
  nc!: PluginContext["nc"]
  automation!: PluginContext["automation"]
  actions!: PluginContext["actions"]

  onLoad?(api?: PluginContext): void | Promise<void>
  onStart?(): void | Promise<void>
  onStop?(): void | Promise<void>
  onUnload?(): void | Promise<void>
}

export interface PluginDefinition {
  onLoad?(context: PluginContext): void | Promise<void>
  onStart?(): void | Promise<void>
  onStop?(): void | Promise<void>
  onUnload?(): void | Promise<void>
}

export function definePlugin(definition: PluginDefinition): PluginDefinition {
  return definition
}
