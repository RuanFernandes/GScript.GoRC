import {rcService} from "@/services/rcService"
import {GraalScriptLspClient as GraalScriptLspProtocolClient} from "./graalScriptLspClient"
import type {
  LspCompletionItem,
  LspPosition,
  LspRange,
  MonacoModel,
} from "./graalScriptLspClient"

export type {GraalScriptDiagnostic} from "./graalScriptLspClient"

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

interface MonacoLanguageAPI {
  editor?: {
    registerLinkOpener?(opener: {open(resource: {toString(): string}): boolean | Promise<boolean>}): Disposable
  }
  languages: {
    registerCompletionItemProvider(languageId: string, provider: unknown): Disposable
    registerHoverProvider(languageId: string, provider: unknown): Disposable
    registerSignatureHelpProvider(languageId: string, provider: unknown): Disposable
  }
}

export interface GraalScriptLspRegistration {
  dispose(): void
}

interface MonacoMarkdownDocumentation {
  value: string
  supportThemeIcons?: boolean
}

const GRAALSCRIPT_WIKI_SEARCH_URL = "https://wiki.gscript.dev/index.php"

// Keep the app's Wails transport adapter separate from the testable LSP client.
export class GraalScriptLspClient extends GraalScriptLspProtocolClient {
  constructor() {
    super((message) => rcService.graalScriptLspRequest(message))
  }
}

export function registerGraalScriptLsp(monaco: unknown, client: GraalScriptLspClient): GraalScriptLspRegistration {
  const m = monaco as MonacoLanguageAPI
  const disposables: Disposable[] = []

  const wikiLinkOpener = m.editor?.registerLinkOpener?.({
    open: (resource) => {
      const url = resource.toString()
      if (!url.startsWith(`${GRAALSCRIPT_WIKI_SEARCH_URL}?`)) return false
      void rcService.openChatLink(url).catch(() => undefined)
      return true
    },
  })
  if (wikiLinkOpener) disposables.push(wikiLinkOpener)

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
            documentation: completionDocumentation(item),
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

function documentationMarkdown(value: string | {kind: string; value: string} | undefined): MonacoMarkdownDocumentation {
  return {value: documentationValue(value).replace(/```gs2\b/gi, "```graalscript")}
}

function completionDocumentation(item: LspCompletionItem): MonacoMarkdownDocumentation {
  const documentation = documentationMarkdown(item.documentation)
  if (item.kind !== 2 && item.kind !== 3) return documentation

  const wikiURL = wikiSearchURL(item.label)
  if (!wikiURL) return documentation

  const link = `[$(book) Abrir na Wiki](${wikiURL})`
  return {
    value: documentation.value ? `${documentation.value}\n\n---\n\n${link}` : link,
    supportThemeIcons: true,
  }
}

function wikiSearchURL(name: string): string | null {
  const sanitized = name.replace(/[^\p{L}\p{N}_]/gu, "")
  if (!sanitized || sanitized.length > 100) return null

  const params = new URLSearchParams({title: "Special:Search", search: sanitized, go: "Go"})
  return `${GRAALSCRIPT_WIKI_SEARCH_URL}?${params.toString()}`
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
