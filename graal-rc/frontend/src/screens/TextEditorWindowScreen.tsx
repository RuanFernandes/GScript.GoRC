// TextEditorWindowScreen is the plain-text editor for a remote text file (opened
// via App.OpenRemoteFile when a .txt/.json/.ini/... is double-clicked, URL
// "/#textfile?p=<remotePath>"). Monaco runs with no syntax highlight (plaintext).
// Ctrl+S writes the content back to the server; unsaved changes prompt on close
// (reuses the generic editor dirty/close-confirm wiring keyed "textfile:<path>").
import {useCallback, useEffect, useRef, useState} from "react"
import Editor, {DiffEditor, type OnMount} from "@monaco-editor/react"
import {Events} from "@wailsio/runtime"
import {toast} from "sonner"
import {GitCompare, Loader2, X} from "lucide-react"

import {AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogHeader, AlertDialogTitle} from "@/components/ui/alert-dialog"
import {Button} from "@/components/ui/button"
import {useCodingSettings} from "@/hooks/useCodingSettings"
import {ensureTheme} from "@/lib/monacoThemes"
import {rcService} from "@/services/rcService"
import {useLanguage} from "@/hooks/useLanguage"

interface MonacoInstance {
  KeyMod: {CtrlCmd: number}
  KeyCode: {KeyS: number}
  editor: {defineTheme(name: string, data: unknown): void; setTheme(name: string): void}
}
interface EditorInstance {
  addCommand(keybinding: number, handler: () => void): void
  updateOptions(opts: {fontFamily?: string; fontSize?: number}): void
  getValue(): string
}

function parsePath(): string {
  const hash = typeof window !== "undefined" ? window.location.hash : ""
  const q = hash.indexOf("?")
  if (q < 0) return ""
  return new URLSearchParams(hash.slice(q + 1)).get("p") ?? ""
}

const KIND = "textfile"

export function TextEditorWindowScreen() {
  const {t} = useLanguage()
  const remotePath = useRef(parsePath()).current
  const {settings} = useCodingSettings()
  const [content, setContent] = useState("")
  const [original, setOriginal] = useState("")
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [confirmClose, setConfirmClose] = useState(false)
  const [closingAfterSave, setClosingAfterSave] = useState(false)
  const [showChanges, setShowChanges] = useState(false)
  const editorRef = useRef<EditorInstance | null>(null)
  const monacoRef = useRef<MonacoInstance | null>(null)
  const contentRef = useRef("")
  const [editorReady, setEditorReady] = useState(false)

  const baseName = remotePath.includes("/") ? remotePath.slice(remotePath.lastIndexOf("/") + 1) : remotePath

  useEffect(() => {
    let cancelled = false
    rcService
      .getTextFile(remotePath)
      .then((text) => {
        if (cancelled) return
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
  }, [remotePath])

  useEffect(() => {
    rcService.setEditorDirty(KIND, remotePath, dirty).catch(() => {})
  }, [remotePath, dirty])

  useEffect(() => {
    const myKey = `${KIND}:${remotePath}`
    const off = Events.On("rc:editorConfirmClose", (e: {data: string}) => {
      if (e.data === myKey) setConfirmClose(true)
    })
    return () => {
      off()
    }
  }, [remotePath])

  useEffect(() => {
    if (closingAfterSave && !dirty) {
      rcService.closeScriptEditor(KIND, remotePath).catch(() => {})
      setClosingAfterSave(false)
    }
  }, [closingAfterSave, dirty, remotePath])

  const doSave = useCallback(async () => {
    const text = contentRef.current
    setSaving(true)
    try {
      await rcService.saveTextFile(remotePath, text)
      setOriginal(text)
      setDirty(false)
      toast.success(t("editor.saved"))
    } catch (err) {
      toast.error(t("editor.saveFailed"), {description: String(err)})
    } finally {
      setSaving(false)
    }
  }, [remotePath, t])

  const handleMount: OnMount = useCallback(
    (editor, monaco) => {
      editorRef.current = editor as unknown as EditorInstance
      monacoRef.current = monaco as unknown as MonacoInstance
      setEditorReady(true)
      editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => {
        void doSave()
      })
    },
    [doSave],
  )

  useEffect(() => {
    editorRef.current?.updateOptions({fontFamily: settings.fontFamily, fontSize: settings.fontSize})
  }, [settings.fontFamily, settings.fontSize])

  useEffect(() => {
    const m = monacoRef.current
    if (!m || !editorReady) return
    ensureTheme(m, settings.theme)
    m.editor.setTheme(settings.theme === "remoteTheme" ? "vs-dark" : settings.theme)
  }, [settings.theme, editorReady])

  const saveAndClose = useCallback(async () => {
    setConfirmClose(false)
    await doSave()
    setClosingAfterSave(true)
  }, [doSave])

  const discardAndClose = useCallback(() => {
    setConfirmClose(false)
    setDirty(false)
    rcService.setEditorDirty(KIND, remotePath, false).catch(() => {})
    rcService.closeScriptEditor(KIND, remotePath).catch(() => {})
  }, [remotePath])

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2">
        <h1 className="text-sm font-semibold">{baseName || t("editor.textFile")}</h1>
        {dirty && <span className="text-xs text-amber-500">• unsaved</span>}
        {saving && <Loader2 className="size-3.5 animate-spin text-muted-foreground" />}
        <div className="ml-auto flex items-center gap-1.5">
          {dirty && <Button variant="outline" size="sm" onClick={() => setShowChanges((value) => !value)}><GitCompare className="size-4" />{t("editor.reviewChanges")}</Button>}
          <Button size="sm" onClick={doSave} disabled={!dirty || saving}>
            {t("editor.deployChanges")}
          </Button>
        </div>
      </header>
      {showChanges && dirty && (
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
              language="plaintext"
              theme={settings.theme === "remoteTheme" ? "vs-dark" : settings.theme}
              options={{readOnly: true, renderSideBySide: true, minimap: {enabled: false}, scrollBeyondLastLine: false, automaticLayout: true}}
            />
          </div>
        </section>
      )}
      <div className="min-h-0 flex-1">
        {loading ? (
          <div className="flex h-full items-center justify-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading…
          </div>
        ) : loadError ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 p-6 text-center text-sm text-muted-foreground">
            <p className="font-medium text-destructive">{t("editor.couldNotOpen")}</p>
            <p className="max-w-md">{loadError}</p>
          </div>
        ) : (
          <Editor
            theme={settings.theme === "remoteTheme" ? "vs-dark" : settings.theme}
            language="plaintext"
            value={content}
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
              fontLigatures: true,
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
              This file has unsaved changes. Save them before the window closes, or discard them.
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
