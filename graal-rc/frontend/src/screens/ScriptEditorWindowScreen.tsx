// ScriptEditorWindowScreen is the content of a per-script editor window (opened
// via App.OpenScriptEditor, URL "/#editor?t=<kind>&k=<key>"). Each open script
// is its own window so several can be edited at once. Monaco provides the text
// editing (Ctrl+Z/Y undo/redo are native); Ctrl+S writes the script back to the
// server. npcattr is read-only; npcflags edits flags; weapon/class/npc edit the
// script body.
import {useCallback, useEffect, useRef, useState} from "react"
import Editor, {DiffEditor, type BeforeMount, type OnMount} from "@monaco-editor/react"
import {Events} from "@wailsio/runtime"
import {toast} from "sonner"

import {BookOpen, GitCompare, Loader2, X} from "lucide-react"
import {AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogHeader, AlertDialogTitle} from "@/components/ui/alert-dialog"
import {Button} from "@/components/ui/button"
import {ScriptGalleryDialog, type ScriptGalleryType} from "@/components/ScriptGalleryDialog"
import {useCodingSettings} from "@/hooks/useCodingSettings"
import {useEditorDraft} from "@/hooks/useEditorDraft"
import {ensureTheme, toMonacoThemeName} from "@/lib/monacoThemes"
import {registerGraalScript} from "@/lib/monacoGraalScript"
import {GraalScriptLspClient, graalScriptDocumentUri, registerGraalScriptLsp, type GraalScriptDiagnostic} from "@/lib/graalScriptLsp"
import {registerServerConfig} from "@/lib/monacoServerConfig"
import {adaptMonacoTheme} from "@/lib/adaptTheme"
import {rcService} from "@/services/rcService"
import type {EditorKind, SyncReviewItem} from "@/types"
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
    register(language: {id: string; extensions?: string[]; aliases?: string[]}): {dispose(): void}
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
  updateOptions(opts: {fontFamily?: string; fontSize?: number; tabSize?: number; readOnly?: boolean}): void
	getValue(): string
	setValue(value: string): void
  getModel(): {uri: {toString(): string}; getValue(): string} | null
}

type SaveAction = "save" | "saveAndClose"

interface SyncLocalUploaded {
  kind: string
  key: string
  content: string
}

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
  const {draft, draftError} = useEditorDraft()
  const [draftRestored, setDraftRestored] = useState(false)
  const [draftRemoteChanged, setDraftRemoteChanged] = useState(false)
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
  const pluginCompletionRegistrationRef = useRef<{dispose(): void} | null>(null)
  const pluginDiagnosticsTimerRef = useRef<number | null>(null)
  const pluginDiagnosticsGenerationRef = useRef(0)
  const applyDiagnosticsRef = useRef<(diagnostics: GraalScriptDiagnostic[]) => void>(() => {})
  const diagnosticsRef = useRef<GraalScriptDiagnostic[]>([])
  const lspReadyRef = useRef<Promise<void>>(Promise.resolve())
  const pendingSaveActionRef = useRef<SaveAction | null>(null)
  const pendingExternalContentRef = useRef<string | null>(null)
  const latestSyncedContentRef = useRef<string | null>(null)
  // contentRef mirrors `content` so the Ctrl+S handler (registered once at mount
  // with a stale closure) always saves the LATEST text — without this, the mount-
  // time doSave closure captures an empty/stale content and saves nothing.
  const contentRef = useRef("")
  const originalRef = useRef("")
  const savingRef = useRef(false)
  const [editorReady, setEditorReady] = useState(false)
  const [remoteDef, setRemoteDef] = useState<unknown>(null)
  const [customDefs, setCustomDefs] = useState<Record<string, unknown>>({})
  const [conflict, setConflict] = useState<SyncReviewItem | null>(null)
  const [mergeContent, setMergeContent] = useState("")

  const {kind, key} = parsed.current ?? {kind: "weapon" as EditorKind, key: ""}
  const readOnly = kind === "npcattr"
  const [confirmClose, setConfirmClose] = useState(false)
  const [confirmSaveWithErrors, setConfirmSaveWithErrors] = useState(false)
  const [saveDiagnostics, setSaveDiagnostics] = useState<GraalScriptDiagnostic[]>([])
  const [closingAfterSave, setClosingAfterSave] = useState(false)
  const [showChanges, setShowChanges] = useState(false)
  const [galleryOpen, setGalleryOpen] = useState(false)
  const pluginLanguage = kind === "options" || kind === "folder_config" || kind === "flags" || kind === "npcflags"
    ? "serverconfig"
    : kind === "npcattr" ? "ini" : "graalscript"
  const galleryType: ScriptGalleryType | null = kind === "weapon" || kind === "class" || kind === "npc" ? kind : null

  const requestPluginDiagnostics = useCallback(async () => {
    const model = editorRef.current?.getModel()
    if (!model || !monacoRef.current) return
    const generation = ++pluginDiagnosticsGenerationRef.current
    try {
      const response = await rcService.pluginMonacoRequest("diagnostics", pluginLanguage, {
        language: pluginLanguage,
        uri: model.uri.toString(),
        text: model.getValue(),
      })
      if (generation !== pluginDiagnosticsGenerationRef.current || !monacoRef.current) return
      const diagnostics = Array.isArray(response) ? response : []
      monacoRef.current.editor.setModelMarkers(model, "gorc-plugin", diagnostics.map(toPluginMarker).filter((marker): marker is MonacoMarker => marker !== null))
    } catch {
      // Plugin diagnostics are optional; the built-in LSP remains authoritative.
    }
  }, [pluginLanguage])

  const schedulePluginDiagnostics = useCallback(() => {
    if (pluginDiagnosticsTimerRef.current !== null) window.clearTimeout(pluginDiagnosticsTimerRef.current)
    pluginDiagnosticsTimerRef.current = window.setTimeout(() => {
      pluginDiagnosticsTimerRef.current = null
      void requestPluginDiagnostics()
    }, 220)
  }, [requestPluginDiagnostics])

  // The script payload was already fetched by OpenScriptEditor before this
  // window was created (so a no-permission/no-response script never opens a
  // window). Read it from the backend cache.
  useEffect(() => {
    let cancelled = false
    draft
      .readContent()
      .then(async (reply) => {
        const text = latestSyncedContentRef.current ?? reply?.text ?? ""
        const recovered = readOnly ? {text, recovered: false, remoteChanged: false} : await draft.open(text)
        if (cancelled) return
        const currentServer = latestSyncedContentRef.current ?? text
        const nextContent = recovered.recovered ? recovered.text : currentServer
        const remoteChanged = recovered.remoteChanged || recovered.recovered && currentServer !== text
        if (currentServer !== text) draft.rebase(currentServer)
        setScriptName(reply?.name ?? "")
        setContent(nextContent)
        contentRef.current = nextContent
        originalRef.current = currentServer
        setOriginal(currentServer)
        setDirty(nextContent !== currentServer)
        setDraftRestored(recovered.recovered)
        setDraftRemoteChanged(remoteChanged)
        setShowChanges(remoteChanged)
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
  }, [kind, key, readOnly, draft])

  // Sync updates the baseline. An editor with unsaved changes keeps its text
  // and opens a comparison instead of replacing the Monaco model.
  useEffect(() => {
    const off = Events.On("rc:syncLocalUploaded", (e: {data: string}) => {
      try {
        const update = JSON.parse(e.data) as Partial<SyncLocalUploaded>
        if (update.kind !== kind || update.key !== key || typeof update.content !== "string") return

        const next = update.content
        latestSyncedContentRef.current = next
        if (contentRef.current !== originalRef.current) {
          // A sync upload must never replace work still being edited here.
          originalRef.current = next
          setOriginal(next)
          draft.rebase(next)
          setDirty(contentRef.current !== next)
          setDraftRestored(true)
          setDraftRemoteChanged(true)
          setShowChanges(true)
          return
        }
        const editor = editorRef.current
        if (editor && editor.getValue() !== next) {
          pendingExternalContentRef.current = next
          try {
            editor.setValue(next)
          } catch {
            pendingExternalContentRef.current = null
          }
        } else {
          pendingExternalContentRef.current = null
        }
        contentRef.current = next
        originalRef.current = next
        void draft.saved(next, draft.revision).catch(() => {})
        setContent(next)
        setOriginal(next)
        setDirty(false)
      } catch {
        // Ignore malformed broadcasts; the local file and server remain intact.
      }
    })
    return () => off()
  }, [kind, key, draft])

  // Sync conflicts are broadcast to every window. Only the editor matching
  // the conflicted script claims the inline review surface.
  useEffect(() => {
    const off = Events.On("rc:syncConflict", (e: {data: string}) => {
      try {
        const item = JSON.parse(e.data) as SyncReviewItem
        if (item.kind !== kind || item.key !== key) return
        setConflict(item)
        setMergeContent(item.local ?? "")
      } catch {
        // Ignore malformed sync events; the global review window remains available.
      }
    })
    return () => off()
  }, [kind, key])

  // Push dirty state to the backend so the close-interception hook knows whether
  // to prompt (the hook lives in Go because that's where window close is
  // observed).
  useEffect(() => {
    draft.setDirty(dirty).catch(() => {})
  }, [draft, dirty])

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
      draft.setDirty(false).then(() => draft.close()).catch(() => {})
      setClosingAfterSave(false)
    }
  }, [closingAfterSave, dirty, draft])

  const diagnosticsBeforeSave = useCallback(async (): Promise<GraalScriptDiagnostic[]> => {
    const client = lspClientRef.current
    if (!client) return diagnosticsRef.current

    await lspReadyRef.current
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
    if (readOnly || savingRef.current) return false
    savingRef.current = true
    setSaving(true)
    try {
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
      const revision = draft.revision
      await draft.upload(text)
      originalRef.current = text
      setOriginal(text)
      setDirty(contentRef.current !== text)
      setDraftRestored(false)
      setDraftRemoteChanged(false)
      await draft.saved(text, revision).catch(() => {})
      toast.success(t("editor.saved"))
      return contentRef.current === text
    } catch (err) {
      toast.error(t("editor.saveFailed"), {description: String(err)})
      return false
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }, [diagnosticsBeforeSave, kind, key, readOnly, t, draft])

  const resolveInlineConflict = useCallback(async (choice: "local" | "server" | "merge") => {
    if (!conflict) return
    const nextContent = choice === "local" ? conflict.local : choice === "server" ? conflict.server : mergeContent
    if (choice === "merge" && !nextContent) {
      toast.error(t("editor.conflictMergeRequired"))
      return
    }
    try {
      const revision = draft.revision
      await draft.resolveConflict(choice, choice === "merge" ? nextContent : "")
      if (draft.revision !== revision) {
        originalRef.current = nextContent
        setOriginal(nextContent)
        draft.rebase(nextContent)
        setDirty(contentRef.current !== nextContent)
        setConflict(null)
        return
      }
      editorRef.current?.setValue(nextContent)
      contentRef.current = nextContent
      setContent(nextContent)
      originalRef.current = nextContent
      setOriginal(nextContent)
      setDirty(false)
      await draft.saved(nextContent, draft.revision).catch(() => {})
      setConflict(null)
      toast.success(t("editor.conflictResolved"))
    } catch (err) {
      toast.error(t("editor.conflictResolveFailed"), {description: String(err)})
    }
  }, [conflict, kind, key, mergeContent, t, draft])

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
      if (model) {
        pluginCompletionRegistrationRef.current?.dispose()
        pluginCompletionRegistrationRef.current = m.languages.registerCompletionItemProvider(pluginLanguage, {
          triggerCharacters: [".", ":", "@"],
          provideCompletionItems: async (_currentModel: unknown, position: {lineNumber: number; column: number}) => {
            try {
              const response = await rcService.pluginMonacoRequest("completions", pluginLanguage, {
                language: pluginLanguage,
                uri: model.uri.toString(),
                text: model.getValue(),
                position: {line: position.lineNumber, column: position.column},
              })
              const suggestions = Array.isArray(response) ? response.filter(value => value && typeof value === "object") : []
              return {suggestions: suggestions.map(value => {
                const item = value as {label?: unknown; insertText?: unknown; detail?: unknown; documentation?: unknown}
                return {
                  label: typeof item.label === "string" ? item.label : "plugin completion",
                  insertText: typeof item.insertText === "string" ? item.insertText : String(item.label ?? ""),
                  detail: typeof item.detail === "string" ? item.detail : undefined,
                  documentation: typeof item.documentation === "string" ? item.documentation : undefined,
                }
              })}
            } catch {
              return {suggestions: []}
            }
          },
        })
        void requestPluginDiagnostics()
      }
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
    [doSave, key, kind, pluginLanguage, requestPluginDiagnostics],
  )

  useEffect(() => {
    return () => {
      applyDiagnosticsRef.current([])
      applyDiagnosticsRef.current = () => {}
      lspRegistrationRef.current?.dispose()
      lspRegistrationRef.current = null
      pluginCompletionRegistrationRef.current?.dispose()
      pluginCompletionRegistrationRef.current = null
      if (pluginDiagnosticsTimerRef.current !== null) window.clearTimeout(pluginDiagnosticsTimerRef.current)
      pluginDiagnosticsTimerRef.current = null
      pluginDiagnosticsGenerationRef.current++
      const model = editorRef.current?.getModel()
      if (model && monacoRef.current) monacoRef.current.editor.setModelMarkers(model, "gorc-plugin", [])
      const client = lspClientRef.current
      lspClientRef.current = null
      diagnosticsRef.current = []
      void client?.close()
    }
  }, [])

  // Keep editor options in sync as coding settings change.
  useEffect(() => {
    editorRef.current?.updateOptions({fontFamily: settings.fontFamily, fontSize: settings.fontSize, tabSize: settings.tabSize})
  }, [settings.fontFamily, settings.fontSize, settings.tabSize])

  useEffect(() => {
    if (!editorReady || !monacoRef.current) return
    let active = true
    const registrations: Array<{dispose(): void}> = []
    const refreshLanguages = async () => {
      registrations.splice(0).forEach(registration => registration.dispose())
      try {
        const languages = await rcService.getPluginMonacoLanguages()
        if (!active || !monacoRef.current) return
        for (const language of languages ?? []) {
          if (!language.id.trim()) continue
          try { registrations.push(monacoRef.current.languages.register(language)) } catch { /* another provider may own this global language id */ }
        }
      } catch { /* plugin language metadata is optional */ }
    }
    void refreshLanguages()
    const offLanguages = Events.On("plugin:monaco-languages", () => void refreshLanguages())
    return () => {
      active = false
      offLanguages()
      registrations.forEach(registration => registration.dispose())
    }
  }, [editorReady])

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

  const discardAndClose = useCallback(async () => {
    try {
      if (!await draft.discard()) return
      setConfirmClose(false)
      setDirty(false)
      await draft.setDirty(false)
      await draft.close()
    } catch (error) {
      toast.error(t("editor.draftClearFailed"), {description: String(error)})
    }
  }, [kind, key, draft, t])

  const applyGalleryInsert = useCallback((nextContent: string) => {
    editorRef.current?.setValue(nextContent)
    contentRef.current = nextContent
    setContent(nextContent)
    draft.stage(nextContent)
    setDirty(nextContent !== originalRef.current)
    setGalleryOpen(false)
  }, [draft])

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2">
        <h1 className="text-sm font-semibold">
          {kind}: {(kind === "npc" || kind === "npcflags" || kind === "npcattr") ? (scriptName || key) : key}
        </h1>
        {readOnly && <span className="text-muted-foreground text-xs">{t("editor.readOnly")}</span>}
        {dirty && !readOnly && <span className="text-amber-500 text-xs">{t("editor.unsaved")}</span>}
        {saving && <Loader2 className="text-muted-foreground size-3.5 animate-spin" />}
        {(galleryType || (!readOnly && dirty)) && (
          <div className="ml-auto flex items-center gap-1.5">
            {galleryType && (
              <Button variant="outline" size="sm" onClick={() => setGalleryOpen(true)} disabled={loading || Boolean(loadError)}>
                <BookOpen className="size-4" />{t("gallery.view")}
              </Button>
            )}
            {!readOnly && dirty && (
              <>
            <Button variant="outline" size="sm" onClick={() => setShowChanges((value) => !value)}>
              <GitCompare className="size-4" />{t("editor.reviewChanges")}
            </Button>
            <Button size="sm" onClick={() => void doSave()} disabled={saving}>{t("editor.deployChanges")}</Button>
              </>
            )}
          </div>
        )}
      </header>
      {draftRestored && <p role="status" className="border-b bg-amber-500/10 px-4 py-2 text-sm">{t(draftRemoteChanged ? "editor.draftRemoteChanged" : "editor.draftRestored")}</p>}
      {draftError && <p role="alert" className="border-b bg-destructive/10 px-4 py-2 text-sm text-destructive">{t("editor.draftFailed")} {draftError}</p>}
      {showChanges && dirty && !readOnly && (
        <section className="border-b bg-muted/10 p-3">
          <div className="mb-2 flex items-center gap-2">
            <div className="min-w-0 flex-1">
              <h2 className="text-sm font-semibold">{t("editor.reviewChanges")}</h2>
              <p className="text-muted-foreground text-xs">{t("editor.reviewChangesDescription")}</p>
            </div>
            <Button variant="ghost" size="icon" className="size-7" onClick={() => setShowChanges(false)} aria-label={t("common.close")}><X className="size-4" /></Button>
          </div>
          <div className="h-64 overflow-hidden rounded-md border">
            <DiffEditor
              height="100%"
              original={original}
              modified={content}
              language={pluginLanguage}
              theme={settings.theme === "remoteTheme" && !remoteDef ? "vs-dark" : settings.theme}
              options={{readOnly: true, renderSideBySide: true, minimap: {enabled: false}, scrollBeyondLastLine: false, automaticLayout: true}}
            />
          </div>
        </section>
      )}
      {conflict && (
        <section className="border-b border-amber-500/30 bg-amber-500/5 p-3">
          <div className="mb-2 flex flex-wrap items-start gap-2">
            <div className="min-w-0 flex-1">
              <h2 className="text-sm font-semibold text-amber-200">{t("editor.conflictTitle")}</h2>
              <p className="text-muted-foreground mt-0.5 text-xs">{conflict.actor ? `${conflict.actor} changed this script.` : t("editor.conflictDescription")}</p>
            </div>
            <span className="rounded border border-amber-500/30 px-2 py-1 font-mono text-[10px] uppercase text-amber-300">{t("editor.conflictDiff")}</span>
          </div>
          <div className="h-56 overflow-hidden rounded-md border border-amber-500/20">
            <DiffEditor
              height="100%"
              original={conflict.local ?? ""}
              modified={conflict.server ?? ""}
              language="graalscript"
              theme={settings.theme === "remoteTheme" && !remoteDef ? "vs-dark" : settings.theme}
              options={{readOnly: true, renderSideBySide: true, minimap: {enabled: false}, scrollBeyondLastLine: false, automaticLayout: true}}
            />
          </div>
          <textarea
            value={mergeContent}
            onChange={(event) => setMergeContent(event.target.value)}
            aria-label={t("editor.conflictMergeContent")}
            className="mt-2 min-h-24 w-full resize-y rounded-md border bg-background p-2 font-mono text-xs outline-none focus:ring-2 focus:ring-ring"
          />
          <div className="mt-2 flex flex-wrap gap-2">
            <Button size="sm" variant="outline" onClick={() => void resolveInlineConflict("local")}>{t("editor.conflictKeepLocal")}</Button>
            <Button size="sm" variant="outline" onClick={() => void resolveInlineConflict("server")}>{t("editor.conflictUseServer")}</Button>
            <Button size="sm" onClick={() => void resolveInlineConflict("merge")}>{t("editor.conflictApplyMerge")}</Button>
          </div>
        </section>
      )}
      <div className="min-h-0 flex-1">
        {loading ? (
          <div className="text-muted-foreground flex h-full items-center justify-center gap-2 text-sm">
            <Loader2 className="size-4 animate-spin" /> {t("common.loading")}
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
              const isExternalSyncUpdate = pendingExternalContentRef.current === v
              if (isExternalSyncUpdate) pendingExternalContentRef.current = null
              setContent(v)
              contentRef.current = v
              if (!readOnly && !isExternalSyncUpdate) draft.stage(v)
              setDirty(isExternalSyncUpdate ? false : v !== originalRef.current)
              const lspClient = lspClientRef.current
              if (lspClient) {
                void lspClient
                  .change(v)
                  .then((diagnostics) => {
                    if (lspClientRef.current === lspClient && contentRef.current === v) {
                      applyDiagnosticsRef.current(diagnostics)
                    }
                  })
                  .catch(() => {})
              }
              schedulePluginDiagnostics()
            }}
            options={{
              fontFamily: settings.fontFamily,
              fontSize: settings.fontSize,
              tabSize: settings.tabSize,
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
            <AlertDialogDescription>{t("editor.scriptSaveBeforeClosingDescription")}</AlertDialogDescription>
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

      {galleryType && (
        <ScriptGalleryDialog
          open={galleryOpen}
          scriptType={galleryType}
          currentName={scriptName || key}
          currentContent={contentRef.current}
          onClose={() => setGalleryOpen(false)}
          onInsert={applyGalleryInsert}
        />
      )}
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

function toPluginMarker(value: unknown): MonacoMarker | null {
  if (!value || typeof value !== "object") return null
  const diagnostic = value as {
    message?: unknown
    severity?: unknown
    startLine?: unknown
    startColumn?: unknown
    endLine?: unknown
    endColumn?: unknown
    source?: unknown
  }
  if (typeof diagnostic.message !== "string") return null
  const startLine = Number(diagnostic.startLine)
  const startColumn = Number(diagnostic.startColumn)
  const endLine = Number(diagnostic.endLine)
  const endColumn = Number(diagnostic.endColumn)
  if (![startLine, startColumn, endLine, endColumn].every(Number.isFinite)) return null
  return {
    startLineNumber: Math.max(1, Math.floor(startLine)),
    startColumn: Math.max(1, Math.floor(startColumn)),
    endLineNumber: Math.max(1, Math.floor(endLine)),
    endColumn: Math.max(1, Math.floor(endColumn)),
    severity: pluginMarkerSeverity(diagnostic.severity),
    message: diagnostic.message,
    source: typeof diagnostic.source === "string" ? diagnostic.source : "gorc-plugin",
  }
}

function pluginMarkerSeverity(value: unknown): number {
  switch (Number(value)) {
    case 2: return 4 // Warning
    case 3: return 2 // Info
    case 4: return 1 // Hint
    default: return 8 // Error
  }
}
