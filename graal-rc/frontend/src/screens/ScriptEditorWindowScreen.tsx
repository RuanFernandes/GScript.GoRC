// ScriptEditorWindowScreen is the content of a per-script editor window (opened
// via App.OpenScriptEditor, URL "/#editor?t=<kind>&k=<key>"). Each open script
// is its own window so several can be edited at once. Monaco provides the text
// editing (Ctrl+Z/Y undo/redo are native); Ctrl+S writes the script back to the
// server. npcattr is read-only; npcflags edits flags; weapon/class/npc edit the
// script body.
import {useCallback, useEffect, useRef, useState} from "react"
import Editor, {type BeforeMount, type OnMount} from "@monaco-editor/react"
import {Events} from "@wailsio/runtime"
import {toast} from "sonner"

import {Loader2} from "lucide-react"
import {AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogHeader, AlertDialogTitle} from "@/components/ui/alert-dialog"
import {Button} from "@/components/ui/button"
import {useCodingSettings} from "@/hooks/useCodingSettings"
import {ensureTheme, toMonacoThemeName} from "@/lib/monacoThemes"
import {registerGraalScript} from "@/lib/monacoGraalScript"
import {GraalScriptLspClient, graalScriptDocumentUri, registerGraalScriptLsp, type GraalScriptDiagnostic} from "@/lib/graalScriptLsp"
import {registerServerConfig} from "@/lib/monacoServerConfig"
import {adaptMonacoTheme} from "@/lib/adaptTheme"
import {rcService} from "@/services/rcService"
import type {EditorKind} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

// Minimal monaco surface used for keybindings (kept loose; monaco-editor is a
// transitive dep of @monaco-editor/react).
interface MonacoInstance {
  KeyMod: {CtrlCmd: number; chord: (a: number) => unknown}
  KeyCode: {KeyS: number}
  editor: {
    defineTheme(name: string, data: unknown): void
    setTheme(name: string): void
    setModelMarkers(model: unknown, owner: string, markers: MonacoMarker[]): void
  }
  languages: {
    register(language: {id: string}): void
    setMonarchTokensProvider(languageId: string, provider: unknown): void
    setLanguageConfiguration(languageId: string, config: unknown): void
    registerCompletionItemProvider(languageId: string, provider: unknown): {dispose(): void}
    registerHoverProvider(languageId: string, provider: unknown): {dispose(): void}
    registerSignatureHelpProvider(languageId: string, provider: unknown): {dispose(): void}
  }
}

interface MonacoMarker {
  startLineNumber: number
  startColumn: number
  endLineNumber: number
  endColumn: number
  severity: number
  message: string
  source?: string
}
interface EditorInstance {
  addCommand(keybinding: number, handler: () => void): void
  updateOptions(opts: {fontFamily?: string; fontSize?: number; readOnly?: boolean}): void
  getValue(): string
  getModel(): {uri: {toString(): string}; getValue(): string} | null
}

type SaveAction = "save" | "saveAndClose"

function parseEditorParams(): {kind: EditorKind; key: string} | null {
  const hash = typeof window !== "undefined" ? window.location.hash : ""
  const q = hash.indexOf("?")
  if (q < 0) return null
  const params = new URLSearchParams(hash.slice(q + 1))
  const kind = (params.get("t") ?? "") as EditorKind
  const key = params.get("k") ?? ""
  if (!kind || !key) return null
  return {kind, key}
}

export function ScriptEditorWindowScreen() {
  const {t} = useLanguage()
  const parsed = useRef(parseEditorParams())
  const {settings} = useCodingSettings()
  const [content, setContent] = useState<string>("")
  const [scriptName, setScriptName] = useState("")
  const [original, setOriginal] = useState<string>("")
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const editorRef = useRef<EditorInstance | null>(null)
  const monacoRef = useRef<MonacoInstance | null>(null)
  const lspClientRef = useRef<GraalScriptLspClient | null>(null)
  const lspRegistrationRef = useRef<{dispose(): void} | null>(null)
  const applyDiagnosticsRef = useRef<(diagnostics: GraalScriptDiagnostic[]) => void>(() => {})
  const diagnosticsRef = useRef<GraalScriptDiagnostic[]>([])
  const pendingLspUpdateRef = useRef<Promise<void>>(Promise.resolve())
  const lspReadyRef = useRef<Promise<void>>(Promise.resolve())
  const pendingSaveActionRef = useRef<SaveAction | null>(null)
  // contentRef mirrors `content` so the Ctrl+S handler (registered once at mount
  // with a stale closure) always saves the LATEST text — without this, the mount-
  // time doSave closure captures an empty/stale content and saves nothing.
  const contentRef = useRef("")
  const [editorReady, setEditorReady] = useState(false)
  const [remoteDef, setRemoteDef] = useState<unknown>(null)
  const [customDefs, setCustomDefs] = useState<Record<string, unknown>>({})

  const {kind, key} = parsed.current ?? {kind: "weapon" as EditorKind, key: ""}
  const readOnly = kind === "npcattr"
  const [confirmClose, setConfirmClose] = useState(false)
  const [confirmSaveWithErrors, setConfirmSaveWithErrors] = useState(false)
  const [saveDiagnostics, setSaveDiagnostics] = useState<GraalScriptDiagnostic[]>([])
  const [closingAfterSave, setClosingAfterSave] = useState(false)

  // The script payload was already fetched by OpenScriptEditor before this
  // window was created (so a no-permission/no-response script never opens a
  // window). Read it from the backend cache.
  useEffect(() => {
    let cancelled = false
    rcService
      .getLoadedScript(kind, key)
      .then((reply) => {
        if (cancelled) return
        const text = reply?.script ?? ""
        setScriptName(reply?.name ?? "")
        setContent(text)
        contentRef.current = text
        setOriginal(text)
      })
      .catch((err: unknown) => {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [kind, key])

  // Push dirty state to the backend so the close-interception hook knows whether
  // to prompt (the hook lives in Go because that's where window close is
  // observed).
  useEffect(() => {
    rcService.setEditorDirty(kind, key, dirty).catch(() => {})
  }, [kind, key, dirty])

  // When the backend cancels a close (window has unsaved changes) it emits this
  // event; show the Save / Discard / Cancel prompt. Only react to our own
  // script (broadcasts reach every editor window).
  useEffect(() => {
    const myKey = `${kind}:${key}`
    const off = Events.On("rc:editorConfirmClose", (e: {data: string}) => {
      if (e.data === myKey) setConfirmClose(true)
    })
    return () => {
      off()
    }
  }, [kind, key])

  // Close after a successful save: dirty is already false, so the hook won't
  // re-prompt — just ask the backend to close the window.
  useEffect(() => {
    if (closingAfterSave && !dirty) {
      rcService.closeScriptEditor(kind, key).catch(() => {})
      setClosingAfterSave(false)
    }
  }, [closingAfterSave, dirty, kind, key])

  const diagnosticsBeforeSave = useCallback(async (): Promise<GraalScriptDiagnostic[]> => {
    const client = lspClientRef.current
    if (!client) return diagnosticsRef.current

    await lspReadyRef.current
    const pendingUpdate = pendingLspUpdateRef.current
    await pendingUpdate
    if (pendingUpdate !== pendingLspUpdateRef.current) {
      await pendingLspUpdateRef.current
    }

    if (lspClientRef.current !== client) return diagnosticsRef.current
    try {
      const diagnostics = await client.diagnostics()
      if (lspClientRef.current === client) {
        diagnosticsRef.current = diagnostics
        applyDiagnosticsRef.current(diagnostics)
      }
      return diagnostics
    } catch {
      return diagnosticsRef.current
    }
  }, [])

  const doSave = useCallback(async (action: SaveAction = "save", allowErrors = false): Promise<boolean> => {
    if (readOnly) return false
    if (!allowErrors) {
      const diagnostics = await diagnosticsBeforeSave()
      const blockingDiagnostics = diagnostics.filter(isSaveBlockingDiagnostic)
      if (blockingDiagnostics.length > 0) {
        pendingSaveActionRef.current = action
        setSaveDiagnostics(blockingDiagnostics)
        setConfirmSaveWithErrors(true)
        return false
      }
    }

    const text = contentRef.current
    setSaving(true)
    try {
      if (kind === "weapon") {
        await rcService.saveWeapon(key, text)
      } else if (kind === "class") {
        await rcService.saveClass(key, text)
      } else if (kind === "npc") {
        await rcService.saveNPC(Number(key), text)
      } else if (kind === "npcflags") {
        await rcService.saveNPCFlags(Number(key), text)
      } else if (kind === "options" || kind === "folder_config" || kind === "flags") {
        await rcService.uploadServerText(kind, text)
      }
      setOriginal(text)
      setDirty(false)
      toast.success(t("editor.saved"))
      return true
    } catch (err) {
      toast.error(t("editor.saveFailed"), {description: String(err)})
      return false
    } finally {
      setSaving(false)
    }
  }, [diagnosticsBeforeSave, kind, key, readOnly, t])

  const handleBeforeMount: BeforeMount = useCallback(
    (monaco) => {
      const m = monaco as unknown as MonacoInstance
      registerGraalScript(m)
      registerServerConfig(m)
      ensureTheme(m, settings.theme)
    },
    [settings.theme],
  )

  const handleMount: OnMount = useCallback(
    (editor, monaco) => {
      editorRef.current = editor as unknown as EditorInstance
      monacoRef.current = monaco as unknown as MonacoInstance
      setEditorReady(true)
      // Ctrl/Cmd+S writes the script back; preventDefault stops the browser save.
      editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
        void doSave()
      })

      const m = monaco as unknown as MonacoInstance
      const model = (editor as unknown as EditorInstance).getModel()
      const supportsGraalScriptLsp = kind === "weapon" || kind === "class" || kind === "npc"
      if (model && supportsGraalScriptLsp) {
        const client = new GraalScriptLspClient()
        lspClientRef.current = client
        const applyDiagnostics = (diagnostics: GraalScriptDiagnostic[]) => {
          diagnosticsRef.current = diagnostics
          m.editor.setModelMarkers(model, "graalscript-lsp", diagnostics.map(toMonacoMarker))
        }
        applyDiagnosticsRef.current = applyDiagnostics
        lspReadyRef.current = (async () => {
          try {
            const syncConfig = await rcService.getSyncConfig()
            if (!syncConfig?.enabled || !syncConfig.outputDir?.trim()) {
              throw new Error("GraalScript LSP requires Local Sync to be enabled with an output folder")
            }
            await client.initialize(syncConfig.outputDir)
            const diagnostics = await client.open(model, graalScriptDocumentUri(kind, key))
            if (lspClientRef.current === client) {
              // Do not register a provider that can only answer with an empty
              // list while the workspace index is still being built. Monaco
              // may keep that first empty response and fall back to word-only
              // completion until the user invokes suggestions again.
              lspRegistrationRef.current?.dispose()
              lspRegistrationRef.current = registerGraalScriptLsp(m, client)
              pendingLspUpdateRef.current = Promise.resolve()
              applyDiagnostics(diagnostics)
            }
          } catch (err) {
            // LSP assistance is optional. A missing sync workspace or a
            // malformed local definitions file must never prevent editing.
            console.warn("GraalScript LSP unavailable", err)
          }
        })()
      }
    },
    [doSave, key, kind],
  )

  useEffect(() => {
    return () => {
      applyDiagnosticsRef.current([])
      applyDiagnosticsRef.current = () => {}
      lspRegistrationRef.current?.dispose()
      lspRegistrationRef.current = null
      const client = lspClientRef.current
      lspClientRef.current = null
      diagnosticsRef.current = []
      pendingLspUpdateRef.current = Promise.resolve()
      void client?.close()
    }
  }, [])

  // Keep font options in sync as coding settings change.
  useEffect(() => {
    editorRef.current?.updateOptions({fontFamily: settings.fontFamily, fontSize: settings.fontSize})
  }, [settings.fontFamily, settings.fontSize])

  // Cached remote theme definition (from the gallery), loaded on mount + on the
  // rc:remoteTheme broadcast so a newly picked theme applies live.
  useEffect(() => {
    rcService
      .getRemoteTheme()
      .then((rt) => {
        if (rt?.definition) {
          try {
            setRemoteDef(JSON.parse(rt.definition))
          } catch {
            // ignore malformed
          }
        }
      })
      .catch(() => {})
    const off = Events.On("rc:remoteTheme", (e: {data: string}) => {
      try {
        const rt = JSON.parse(e.data) as {name: string; definition: string}
        setRemoteDef(JSON.parse(rt.definition))
      } catch {
        // ignore
      }
    })
    return () => {
      off()
    }
  }, [])

  useEffect(() => {
    rcService.getCustomThemes().then((themes) => {
      const next: Record<string, unknown> = {}
      for (const theme of themes ?? []) {
        try { next[theme.key] = JSON.parse(theme.definition) } catch { /* ignore invalid saved theme */ }
      }
      setCustomDefs(next)
    }).catch(() => {})
    const off = Events.On("rc:customTheme", (e: {data: string}) => {
      try {
        const theme = JSON.parse(e.data) as {key: string; definition: string}
        setCustomDefs((current) => ({...current, [theme.key]: JSON.parse(theme.definition)}))
      } catch { /* ignore malformed custom theme */ }
    })
    return () => off()
  }, [])

  // Apply theme on change: custom (monokai/darcula) and remote themes must be
  // defineTheme'd on the live monaco instance before setTheme, otherwise Monaco
  // silently ignores the unknown name. editorReady is in deps so the apply runs
  // once Monaco has actually mounted (it mounts after loading clears, which is
  // after this effect's first run — without editorReady the remote defineTheme
  // would never happen and Monaco would fall back to the light default).
  useEffect(() => {
    const m = monacoRef.current
    if (!m || !editorReady) return
    const customDef = customDefs[settings.theme]
    if (customDef) {
      try {
        const themeName = toMonacoThemeName(settings.theme)
        m.editor.defineTheme(themeName, adaptMonacoTheme(customDef as never))
        m.editor.setTheme(themeName)
      } catch { m.editor.setTheme("vs-dark") }
    } else if (settings.theme === "remoteTheme") {
      if (remoteDef) {
        try {
          m.editor.defineTheme("remoteTheme", adaptMonacoTheme(remoteDef as never))
        } catch {
          // ignore malformed remote definition
        }
        m.editor.setTheme("remoteTheme")
      } else {
        m.editor.setTheme("vs-dark") // fallback until the remote def is loaded
      }
    } else {
      ensureTheme(m, settings.theme)
      m.editor.setTheme(settings.theme)
    }
  }, [settings.theme, remoteDef, customDefs, editorReady])

  // Save then close: the closingAfterSave effect waits for dirty=false (set by
  // doSave on success) before asking the backend to close, so the hook won't
  // re-prompt. If the save fails, dirty stays true and the window stays open.
  const saveAndClose = useCallback(async () => {
    setConfirmClose(false)
    if (await doSave("saveAndClose")) setClosingAfterSave(true)
  }, [doSave])

  const saveWithErrors = useCallback(async () => {
    const action = pendingSaveActionRef.current ?? "save"
    pendingSaveActionRef.current = null
    setConfirmSaveWithErrors(false)
    if (await doSave(action, true) && action === "saveAndClose") {
      setClosingAfterSave(true)
    }
  }, [doSave])

  const cancelSaveWithErrors = useCallback(() => {
    pendingSaveActionRef.current = null
    setSaveDiagnostics([])
    setConfirmSaveWithErrors(false)
  }, [])

  const discardAndClose = useCallback(() => {
    setConfirmClose(false)
    setDirty(false)
    rcService.setEditorDirty(kind, key, false).catch(() => {})
    rcService.closeScriptEditor(kind, key).catch(() => {})
  }, [kind, key])

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2">
        <h1 className="text-sm font-semibold">
          {kind}: {(kind === "npc" || kind === "npcflags" || kind === "npcattr") ? (scriptName || key) : key}
        </h1>
        {readOnly && <span className="text-muted-foreground text-xs">(read-only)</span>}
        {dirty && !readOnly && <span className="text-amber-500 text-xs">• unsaved</span>}
        {saving && <Loader2 className="text-muted-foreground size-3.5 animate-spin" />}
      </header>
      <div className="min-h-0 flex-1">
        {loading ? (
          <div className="text-muted-foreground flex h-full items-center justify-center gap-2 text-sm">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </div>
        ) : loadError ? (
          <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-3 p-6 text-center text-sm">
            <p className="text-destructive font-medium">{t("editor.couldNotOpen")} {kind}</p>
            <p className="max-w-md">{loadError}</p>
            <p className="text-xs">{t("editor.canClose")}</p>
          </div>
        ) : (
          <Editor
            theme={settings.theme === "remoteTheme" && !remoteDef ? "vs-dark" : settings.theme}
            language={
              kind === "options" || kind === "folder_config" || kind === "flags" || kind === "npcflags"
                ? "serverconfig"
                : kind === "npcattr"
                  ? "ini"
                  : "graalscript"
            }
            value={content}
            beforeMount={handleBeforeMount}
            onMount={handleMount}
            onChange={(value) => {
              const v = value ?? ""
              setContent(v)
              contentRef.current = v
              setDirty(v !== original)
              const lspClient = lspClientRef.current
              if (lspClient) {
                pendingLspUpdateRef.current = lspClient
                  .change(v)
                  .then((diagnostics) => {
                    if (lspClientRef.current === lspClient) applyDiagnosticsRef.current(diagnostics)
                  })
                  .catch(() => {})
              }
            }}
            options={{
              fontFamily: settings.fontFamily,
              fontSize: settings.fontSize,
              fontLigatures: true,
              readOnly,
              minimap: {enabled: false},
              scrollBeyondLastLine: false,
              automaticLayout: true,
              // Keep marker hover cards inside the visible editor window. In
              // particular, diagnostics on the first line must open below
              // the marker instead of being clipped by the window header.
              fixedOverflowWidgets: true,
              hover: {above: false},
            }}
          />
        )}
      </div>

      <AlertDialog open={confirmClose} onOpenChange={(v) => !v && setConfirmClose(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("editor.saveBeforeClosing")}</AlertDialogTitle>
            <AlertDialogDescription>
              This script has unsaved changes. Save them before the window closes, or discard them.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmClose(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="destructive" onClick={discardAndClose}>
              {t("editor.closeWithoutSaving")}
            </Button>
            <Button onClick={saveAndClose}>{t("common.save")}</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>

      <AlertDialog open={confirmSaveWithErrors} onOpenChange={(open) => !open && cancelSaveWithErrors()}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("editor.saveWithErrors")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("editor.saveWithErrorsDescription", {count: saveDiagnostics.length})}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="max-h-32 overflow-y-auto rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
            {saveDiagnostics.slice(0, 5).map((diagnostic, index) => (
              <div key={`${diagnostic.range.start.line}:${diagnostic.range.start.character}:${index}`}>
                {diagnostic.message}
              </div>
            ))}
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={cancelSaveWithErrors}>
              {t("common.cancel")}
            </Button>
            <Button variant="destructive" onClick={saveWithErrors}>
              {t("editor.saveAnyway")}
            </Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function isSaveBlockingDiagnostic(diagnostic: GraalScriptDiagnostic): boolean {
  return diagnostic.severity === undefined || diagnostic.severity === 1
}

function toMonacoMarker(diagnostic: GraalScriptDiagnostic): MonacoMarker {
  return {
    startLineNumber: diagnostic.range.start.line + 1,
    startColumn: diagnostic.range.start.character + 1,
    endLineNumber: diagnostic.range.end.line + 1,
    endColumn: diagnostic.range.end.character + 1,
    severity: monacoMarkerSeverity(diagnostic.severity),
    message: diagnostic.message,
    source: diagnostic.source ?? "graalscript",
  }
}

function monacoMarkerSeverity(severity: number | undefined): number {
  switch (severity) {
    case 2: return 4 // Warning
    case 3: return 2 // Info
    case 4: return 1 // Hint
    default: return 8 // Error
  }
}
