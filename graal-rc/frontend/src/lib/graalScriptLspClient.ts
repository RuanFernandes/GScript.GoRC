import {APP_VERSION} from "./appVersion.ts"

export interface MonacoModel {
  uri: {toString(): string}
  getValue(): string
}

export interface LspPosition {
  line: number
  character: number
}

export interface LspRange {
  start: LspPosition
  end: LspPosition
}

export interface GraalScriptDiagnostic {
  range: LspRange
  severity?: number
  source?: string
  message: string
}

export interface LspCompletionItem {
  label: string
  kind?: number
  detail?: string
  documentation?: string | {kind: string; value: string}
  sortText?: string
  insertText?: string
  textEdit?: {range: LspRange; newText: string}
}

export interface LspCompletionList {
  isIncomplete: boolean
  items: LspCompletionItem[]
}

export interface LspHover {
  contents: string | {kind: string; value: string}
  range?: LspRange
}

export interface LspSignatureHelp {
  signatures: LspSignatureInformation[]
  activeSignature?: number
  activeParameter?: number
}

interface LspParameterInformation {
  label: string
  documentation?: string | {kind: string; value: string}
}

interface LspSignatureInformation {
  label: string
  documentation?: string | {kind: string; value: string}
  parameters?: LspParameterInformation[]
}

interface LspDocumentDiagnosticReport {
  kind: "full" | "unchanged"
  resultId?: string
  items?: GraalScriptDiagnostic[]
}

interface LspResponse<T> {
  jsonrpc: string
  id?: number
  result?: T | null
  error?: {code: number; message: string}
}

interface LspInitializeResult {
  capabilities?: {
    completionProvider?: {triggerCharacters?: string[]}
    hoverProvider?: boolean
    signatureHelpProvider?: {triggerCharacters?: string[]}
    diagnosticProvider?: Record<string, unknown>
  }
}

interface PendingChangeWaiter {
  resolve(diagnostics: GraalScriptDiagnostic[]): void
  reject(error: unknown): void
}

export type GraalScriptLspTransport = (message: string) => Promise<string>

export class GraalScriptLspClient {
  private readonly transport: GraalScriptLspTransport
  private readonly changeDebounceMs: number
  private nextId = 1
  private initialized = false
  private version = 0
  private uri = ""
  private latestText: string | null = null
  private diagnosedText: string | null = null
  private cachedDiagnostics: GraalScriptDiagnostic[] = []
  private pendingText: string | null = null
  private pendingChangeWaiters: PendingChangeWaiter[] = []
  private changeTimer: ReturnType<typeof setTimeout> | null = null
  private inFlightChange: Promise<GraalScriptDiagnostic[]> | null = null
  private sendQueue: Promise<unknown> = Promise.resolve()
  private initializePromise: Promise<unknown> | null = null
  private documentPromise: Promise<unknown> | null = null
  private disposed = false

  constructor(transport: GraalScriptLspTransport, changeDebounceMs = 150) {
    this.transport = transport
    this.changeDebounceMs = changeDebounceMs
  }

  async initialize(workspaceRoot: string): Promise<LspInitializeResult> {
    if (this.disposed) return {}
    const rootUri = workspaceRoot ? filePathToUri(workspaceRoot) : null
    const task = (async () => {
      const result = await this.request<LspInitializeResult>("initialize", {
        processId: null,
        rootUri,
        capabilities: {
          textDocument: {
            completion: {completionItem: {snippetSupport: false}},
            hover: {contentFormat: ["markdown", "plaintext"]},
            signatureHelp: {signatureInformation: {documentationFormat: ["markdown", "plaintext"]}},
          },
        },
        clientInfo: {name: "graal-rc", version: APP_VERSION},
      })
      await this.notify("initialized", {})
      if (this.disposed) return result ?? {}
      this.initialized = true
      return result ?? {}
    })()
    this.initializePromise = task
    return task
  }

  async open(model: MonacoModel, documentUri: string): Promise<GraalScriptDiagnostic[]> {
    if (this.disposed) return []
    const task = (async () => {
      if (this.initializePromise) await this.initializePromise
      if (this.disposed || !this.initialized) return []
      this.uri = documentUri
      this.version = 1
      const text = model.getValue()
      this.latestText = text
      await this.notify("textDocument/didOpen", {
        textDocument: {
          uri: this.uri,
          languageId: "graalscript",
          version: this.version,
          text,
        },
      })
      return this.requestDiagnostics(text)
    })()
    this.documentPromise = task
    return task
  }

  change(text: string): Promise<GraalScriptDiagnostic[]> {
    if (this.disposed) return Promise.resolve([])

    this.latestText = text
    this.pendingText = text
    if (this.changeTimer !== null) clearTimeout(this.changeTimer)

    const result = new Promise<GraalScriptDiagnostic[]>((resolve, reject) => {
      this.pendingChangeWaiters.push({resolve, reject})
    })
    this.changeTimer = setTimeout(() => {
      this.changeTimer = null
      void this.flushPendingChange().catch(() => undefined)
    }, this.changeDebounceMs)
    return result
  }

  async close(): Promise<void> {
    this.disposed = true
    if (this.changeTimer !== null) {
      clearTimeout(this.changeTimer)
      this.changeTimer = null
    }
    this.pendingText = null
    for (const waiter of this.pendingChangeWaiters) waiter.reject(new Error("GraalScript LSP client is closed"))
    this.pendingChangeWaiters = []

    if (!this.initialized || !this.uri) return
    await this.notify("textDocument/didClose", {textDocument: {uri: this.uri}})
    this.initialized = false
    this.uri = ""
    this.latestText = null
    this.diagnosedText = null
    this.cachedDiagnostics = []
  }

  async completion(position: LspPosition): Promise<LspCompletionList | null> {
    if (!await this.waitForLatestDocument()) return null
    return this.request<LspCompletionList>("textDocument/completion", {
      textDocument: {uri: this.uri},
      position,
      context: {triggerKind: 1},
    })
  }

  async hover(position: LspPosition): Promise<LspHover | null> {
    if (!await this.waitForLatestDocument()) return null
    return this.request<LspHover>("textDocument/hover", {
      textDocument: {uri: this.uri},
      position,
    })
  }

  async signatureHelp(position: LspPosition): Promise<LspSignatureHelp | null> {
    if (!await this.waitForLatestDocument()) return null
    return this.request<LspSignatureHelp>("textDocument/signatureHelp", {
      textDocument: {uri: this.uri},
      position,
    })
  }

  async diagnostics(): Promise<GraalScriptDiagnostic[]> {
    if (!await this.waitForDocument()) return []
    if (this.pendingText !== null) {
      await this.flushPendingChange()
      return this.diagnostics()
    }
    if (this.inFlightChange) {
      await this.inFlightChange
      return this.diagnostics()
    }
    if (this.latestText !== null && this.diagnosedText === this.latestText) return this.cachedDiagnostics

    const expectedText = this.latestText
    const diagnostics = await this.requestDiagnostics(expectedText)
    if (this.pendingText !== null || this.inFlightChange || this.latestText !== expectedText) {
      return this.diagnostics()
    }
    return diagnostics
  }

  private async flushPendingChange(): Promise<GraalScriptDiagnostic[]> {
    if (this.changeTimer !== null) {
      clearTimeout(this.changeTimer)
      this.changeTimer = null
    }
    if (this.pendingText === null) {
      if (this.inFlightChange) return this.inFlightChange
      if (this.latestText !== null && this.diagnosedText === this.latestText) return this.cachedDiagnostics
      return this.requestDiagnostics(this.latestText)
    }

    const text = this.pendingText
    this.pendingText = null
    const waiters = this.pendingChangeWaiters
    this.pendingChangeWaiters = []
    const task = this.publishChange(text)
    this.inFlightChange = task
    try {
      const diagnostics = await task
      for (const waiter of waiters) waiter.resolve(diagnostics)
      return diagnostics
    } catch (error) {
      for (const waiter of waiters) waiter.reject(error)
      throw error
    } finally {
      if (this.inFlightChange === task) this.inFlightChange = null
    }
  }

  private async publishChange(text: string): Promise<GraalScriptDiagnostic[]> {
    if (!await this.waitForDocument()) return []
    this.version++
    await this.notify("textDocument/didChange", {
      textDocument: {uri: this.uri, version: this.version},
      contentChanges: [{text}],
    })
    return this.requestDiagnostics(text)
  }

  private async requestDiagnostics(expectedText: string | null): Promise<GraalScriptDiagnostic[]> {
    const result = await this.request<LspDocumentDiagnosticReport>("textDocument/diagnostic", {
      textDocument: {uri: this.uri},
    })
    const diagnostics = result?.kind === "full" ? result.items ?? [] : []
    if (expectedText === this.latestText) {
      this.diagnosedText = expectedText
      this.cachedDiagnostics = diagnostics
    }
    return diagnostics
  }

  private async waitForDocument(): Promise<boolean> {
    try {
      if (this.initializePromise) await this.initializePromise
      if (this.documentPromise) await this.documentPromise
    } catch {
      return false
    }
    return !this.disposed && this.initialized && Boolean(this.uri)
  }

  private async waitForLatestDocument(): Promise<boolean> {
    if (!await this.waitForDocument()) return false
    await this.diagnostics()
    return !this.disposed
  }

  private async notify(method: string, params: unknown): Promise<void> {
    await this.send({jsonrpc: "2.0", method, params})
  }

  private async request<T>(method: string, params: unknown): Promise<T | null> {
    const id = this.nextId++
    const response = await this.send({jsonrpc: "2.0", id, method, params})
    const parsed = JSON.parse(response) as LspResponse<T>
    if (parsed.error) throw new Error(parsed.error.message)
    return parsed.result ?? null
  }

  private async send(message: unknown): Promise<string> {
    const next = this.sendQueue.then(() => this.transport(JSON.stringify(message)))
    this.sendQueue = next.catch(() => undefined)
    return next
  }
}

function filePathToUri(path: string): string {
  const normalized = path.replace(/\\/g, "/")
  const parts = normalized.split("/").filter((part, index) => part !== "" || index === 0)
  const encoded = parts.map((part) => encodeURIComponent(part)).join("/")
  return normalized.startsWith("/") ? `file://${encoded}` : `file:///${encoded}`
}
