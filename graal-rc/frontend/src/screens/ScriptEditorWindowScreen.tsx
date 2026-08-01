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
  }
  languages: {
    register(language: {id: string}): void
    setMonarchTokensProvider(languageId: string, provider: unknown): void
    setLanguageConfiguration(languageId: string, config: unknown): void
  }
}
interface EditorInstance {
  addCommand(keybinding: number, handler: () => void): void
  updateOptions(opts: {fontFamily?: string; fontSize?: number; readOnly?: boolean}): void
  getValue(): string
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
  const [content, setContent] = useState<string>("")
  const [scriptName, setScriptName] = useState("")
  const [original, setOriginal] = useState<string>("")
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const editorRef = useRef<EditorInstance | null>(null)
  const monacoRef = useRef<MonacoInstance | null>(null)
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

  const doSave = useCallback(async () => {
    if (readOnly) return
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
    } catch (err) {
      toast.error(t("editor.saveFailed"), {description: String(err)})
    } finally {
      setSaving(false)
    }
  }, [kind, key, readOnly, t])

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
    },
    [doSave],
  )

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
    await doSave()
    setClosingAfterSave(true)
  }, [doSave])

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
            }}
            options={{
              fontFamily: settings.fontFamily,
              fontSize: settings.fontSize,
              readOnly,
              minimap: {enabled: false},
              scrollBeyondLastLine: false,
              automaticLayout: true,
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
    </div>
  )
}
