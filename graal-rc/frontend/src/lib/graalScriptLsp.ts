import {rcService} from "@/services/rcService"

interface Disposable {
  dispose(): void
}

interface MonacoPosition {
  lineNumber: number
  column: number
}

interface MonacoRange {
  startLineNumber: number
  startColumn: number
  endLineNumber: number
  endColumn: number
}

interface MonacoModel {
  uri: {toString(): string}
  getValue(): string
}

interface MonacoLanguageAPI {
  languages: {
    registerCompletionItemProvider(languageId: string, provider: unknown): Disposable
    registerHoverProvider(languageId: string, provider: unknown): Disposable
    registerSignatureHelpProvider(languageId: string, provider: unknown): Disposable
  }
}

interface LspPosition {
  line: number
  character: number
}

interface LspRange {
  start: LspPosition
  end: LspPosition
}

export interface GraalScriptDiagnostic {
  range: LspRange
  severity?: number
  source?: string
  message: string
}

interface LspCompletionItem {
  label: string
  kind?: number
  detail?: string
  documentation?: string | {kind: string; value: string}
  sortText?: string
  insertText?: string
  textEdit?: {range: LspRange; newText: string}
}

interface LspCompletionList {
  isIncomplete: boolean
  items: LspCompletionItem[]
}

interface LspHover {
  contents: string | {kind: string; value: string}
  range?: LspRange
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

interface LspSignatureHelp {
  signatures: LspSignatureInformation[]
  activeSignature?: number
  activeParameter?: number
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

export interface GraalScriptLspRegistration {
  dispose(): void
}

// GraalScriptLspClient keeps the official LSP message shape while using the
// Wails service as the desktop transport. The semantic server remains unaware
// of Monaco and can later be exposed over stdio for VS Code.
export class GraalScriptLspClient {
  private nextId = 1
  private initialized = false
  private version = 0
  private uri = ""
  private sendQueue: Promise<unknown> = Promise.resolve()
  private initializePromise: Promise<unknown> | null = null
  private documentPromise: Promise<unknown> | null = null
  private disposed = false

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
        clientInfo: {name: "graal-rc", version: "0.1.0"},
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
      await this.notify("textDocument/didOpen", {
        textDocument: {
          uri: this.uri,
          languageId: "graalscript",
          version: this.version,
          text: model.getValue(),
        },
      })
      return this.requestDiagnostics()
    })()
    this.documentPromise = task
    return task
  }

  async change(text: string): Promise<GraalScriptDiagnostic[]> {
    if (!await this.waitForDocument()) return []
    this.version++
    await this.notify("textDocument/didChange", {
      textDocument: {uri: this.uri, version: this.version},
      contentChanges: [{text}],
    })
    return this.requestDiagnostics()
  }

  async close(): Promise<void> {
    this.disposed = true
    if (!this.initialized || !this.uri) return
    await this.notify("textDocument/didClose", {textDocument: {uri: this.uri}})
    this.initialized = false
    this.uri = ""
  }

  async completion(position: LspPosition): Promise<LspCompletionList | null> {
    if (!await this.waitForDocument()) return null
    return this.request<LspCompletionList>("textDocument/completion", {
      textDocument: {uri: this.uri},
      position,
      context: {triggerKind: 1},
    })
  }

  async hover(position: LspPosition): Promise<LspHover | null> {
    if (!await this.waitForDocument()) return null
    return this.request<LspHover>("textDocument/hover", {
      textDocument: {uri: this.uri},
      position,
    })
  }

  async signatureHelp(position: LspPosition): Promise<LspSignatureHelp | null> {
    if (!await this.waitForDocument()) return null
    return this.request<LspSignatureHelp>("textDocument/signatureHelp", {
      textDocument: {uri: this.uri},
      position,
    })
  }

  async diagnostics(): Promise<GraalScriptDiagnostic[]> {
    if (!await this.waitForDocument()) return []
    return this.requestDiagnostics()
  }

  private async requestDiagnostics(): Promise<GraalScriptDiagnostic[]> {
    const result = await this.request<LspDocumentDiagnosticReport>("textDocument/diagnostic", {
      textDocument: {uri: this.uri},
    })
    return result?.kind === "full" ? result.items ?? [] : []
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
    const next = this.sendQueue.then(() => rcService.graalScriptLspRequest(JSON.stringify(message)))
    this.sendQueue = next.catch(() => undefined)
    return next
  }
}

export function registerGraalScriptLsp(monaco: unknown, client: GraalScriptLspClient): GraalScriptLspRegistration {
  const m = monaco as MonacoLanguageAPI
  const disposables: Disposable[] = []

  disposables.push(m.languages.registerCompletionItemProvider("graalscript", {
    triggerCharacters: [".", "(", ",", "@"],
    provideCompletionItems: async (model: MonacoModel, position: MonacoPosition) => {
      try {
        const result = await client.completion(toLspPosition(position))
        if (!result) return {suggestions: []}
        return {
          incomplete: result.isIncomplete,
          suggestions: result.items.map((item) => ({
            label: item.label,
            kind: completionKind(item.kind),
            detail: item.detail,
            documentation: documentationMarkdown(item.documentation),
            filterText: item.label,
            sortText: item.sortText,
            insertText: item.textEdit?.newText ?? item.insertText ?? item.label,
            range: item.textEdit ? toMonacoRange(item.textEdit.range) : undefined,
          })),
        }
      } catch {
        return {suggestions: []}
      }
    },
  }))

  disposables.push(m.languages.registerHoverProvider("graalscript", {
    provideHover: async (_model: MonacoModel, position: MonacoPosition) => {
      try {
        const result = await client.hover(toLspPosition(position))
        if (!result) return null
        return {
          contents: [documentationMarkdown(result.contents)],
          range: result.range ? toMonacoRange(result.range) : undefined,
        }
      } catch {
        return null
      }
    },
  }))

  disposables.push(m.languages.registerSignatureHelpProvider("graalscript", {
    signatureHelpTriggerCharacters: ["(", ","],
    signatureHelpRetriggerCharacters: [",", ")"],
    provideSignatureHelp: async (_model: MonacoModel, position: MonacoPosition) => {
      try {
        const result = await client.signatureHelp(toLspPosition(position))
        if (!result) return null
        return {
          value: {
            signatures: result.signatures.map((signature) => ({
              label: signature.label,
              documentation: documentationMarkdown(signature.documentation),
              parameters: (signature.parameters ?? []).map((parameter) => ({
                label: parameter.label,
                documentation: documentationMarkdown(parameter.documentation),
              })),
            })),
            activeSignature: result.activeSignature ?? 0,
            activeParameter: result.activeParameter ?? 0,
          },
          dispose: () => {},
        }
      } catch {
        return null
      }
    },
  }))

  return {
    dispose: () => {
      for (const disposable of disposables) disposable.dispose()
    },
  }
}

export function graalScriptDocumentUri(kind: string, key: string): string {
  return `graalscript://editor/${encodeURIComponent(kind)}/${encodeURIComponent(key)}`
}

function toLspPosition(position: MonacoPosition): LspPosition {
  return {line: position.lineNumber - 1, character: position.column - 1}
}

function toMonacoRange(range: LspRange): MonacoRange {
  return {
    startLineNumber: range.start.line + 1,
    startColumn: range.start.character + 1,
    endLineNumber: range.end.line + 1,
    endColumn: range.end.character + 1,
  }
}

function documentationValue(value: string | {kind: string; value: string} | undefined): string {
  return typeof value === "string" ? value : value?.value ?? ""
}

function documentationMarkdown(value: string | {kind: string; value: string} | undefined): {value: string} {
  return {value: documentationValue(value).replace(/```gs2\b/gi, "```graalscript")}
}

function completionKind(kind: number | undefined): number {
  // Monaco's CompletionItemKind values are numerically compatible with LSP for
  // the common kinds used by this server. Keep a safe fallback for unknown API
  // entries rather than depending on Monaco's enum at module load time.
  switch (kind) {
    case 3: return 1 // Function
    case 6: return 4 // Variable
    case 7: return 5 // Class
    case 10: return 9 // Property
    default: return 18 // Text
  }
}

function filePathToUri(path: string): string {
  const normalized = path.replace(/\\/g, "/")
  const parts = normalized.split("/").filter((part, index) => part !== "" || index === 0)
  const encoded = parts.map((part) => encodeURIComponent(part)).join("/")
  return normalized.startsWith("/") ? `file://${encoded}` : `file:///${encoded}`
}
