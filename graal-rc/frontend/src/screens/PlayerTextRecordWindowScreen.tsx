// PlayerTextRecordWindowScreen is the content of the read-only record viewer
// windows (URL "/#banhistory?a=<account>" or "/#staffactivity?a=<account>").
// It fetches an account's ban-history or staff-activity free text from the
// server and renders it in a read-only Monaco editor. Mirrors the data_type
// "banhistory" / "staffactivity" replies (BanListData callback). Read-only: these
// records are server-side logs, not editable from the RC.
import {useEffect, useRef, useState} from "react"
import Editor from "@monaco-editor/react"
import {Copy, Loader2, RefreshCw} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {rcService} from "@/services/rcService"
import {useLanguage} from "@/hooks/useLanguage"

type RecordKind = "banhistory" | "staffactivity"

const KIND_META: Record<RecordKind, {label: string; fetch: (account: string) => Promise<string>}> = {
  banhistory: {label: "record.banHistory", fetch: (a) => rcService.requestBanHistory(a)},
  staffactivity: {label: "record.staffActivity", fetch: (a) => rcService.requestStaffActivity(a)},
}

function readKind(): RecordKind | null {
  const hash = window.location.hash
  if (hash.startsWith("#banhistory")) return "banhistory"
  if (hash.startsWith("#staffactivity")) return "staffactivity"
  return null
}

function readAccount(): string {
  const hash = window.location.hash
  const q = hash.indexOf("?")
  const params = new URLSearchParams(q >= 0 ? hash.slice(q + 1) : "")
  return params.get("a") ?? ""
}

export function PlayerTextRecordWindowScreen() {
  const {t} = useLanguage()
  const kind = readKind()
  const account = readAccount()
  const meta = kind ? KIND_META[kind] : null

  const [content, setContent] = useState("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = async (showBusy: boolean) => {
    if (!meta) return
    if (showBusy) setBusy(true)
    else setLoading(true)
    setError(null)
    try {
      const text = await meta.fetch(account)
      setContent(text ?? "")
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      if (showBusy) setBusy(false)
      else setLoading(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    if (!meta) {
      setError(t("record.unknownViewer"))
      setLoading(false)
      return
    }
    setLoading(true)
    meta
      .fetch(account)
      .then((text) => !cancelled && setContent(text ?? ""))
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kind, account])

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(content)
      toast.success(t("record.copied"))
    } catch {
      toast.error(t("record.copyFailed"))
    }
  }

  const title = t("record.title", {account: account || t("record.self"), label: meta ? t(meta.label) : t("record.record")})

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2.5">
        <h1 className="text-sm font-semibold">{title}</h1>
        <div className="ml-auto flex items-center gap-1.5">
          <Button variant="outline" size="sm" onClick={() => load(true)} disabled={!meta || loading || busy}>
            {(busy || loading) && <Loader2 className="size-4 animate-spin" />}
            {(!busy && !loading) && <RefreshCw className="size-4" />}
            {t("common.refreshAction")}
          </Button>
          <Button variant="outline" size="sm" onClick={copy} disabled={!content}>
            <Copy className="size-4" />
            {t("common.copy")}
          </Button>
        </div>
      </header>
      <div className="min-h-0 flex-1">
        {loading ? (
          <div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> {t("record.loading")}
          </div>
        ) : error ? (
          <div className="p-4 text-sm text-destructive">{error}</div>
        ) : (
          <Editor
            theme="vs-dark"
            language="plaintext"
            value={content}
            onChange={(v) => setContent(v ?? "")}
            options={{
              readOnly: true,
              fontFamily: "monospace",
              fontSize: 12,
              fontLigatures: true,
              minimap: {enabled: false},
              scrollBeyondLastLine: false,
              automaticLayout: true,
              wordWrap: "on",
            }}
          />
        )}
      </div>
    </div>
  )
}
