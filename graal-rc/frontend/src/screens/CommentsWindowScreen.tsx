// CommentsWindowScreen is the content of the "/opencomments" external window
// (URL "/#comments?a=<account>"). A Monaco text editor for an account's
// comments. Mirrors reference TPlayerList::handlePlayerText (data_type
// "comments"). Loads via rcService.openComments; saves via rcService.setComments.
import {useEffect, useRef, useState} from "react"
import Editor from "@monaco-editor/react"
import {Loader2} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {rcService} from "@/services/rcService"
import {useLanguage} from "@/hooks/useLanguage"

function readAccount(): string {
  const hash = window.location.hash
  const q = hash.indexOf("?")
  const params = new URLSearchParams(q >= 0 ? hash.slice(q + 1) : "")
  return params.get("a") ?? ""
}

export function CommentsWindowScreen() {
  const {t} = useLanguage()
  const account = readAccount()
  const [resolved, setResolved] = useState(account)
  const [content, setContent] = useState("")
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const valueRef = useRef("")

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    rcService
      .openComments(account)
      .then((d) => {
        if (cancelled || !d) return
        setResolved(d.account || account)
        setContent(d.content ?? "")
        valueRef.current = d.content ?? ""
      })
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [account])

  const apply = async () => {
    setSaving(true)
    try {
      await rcService.setComments(resolved, valueRef.current)
      toast.success(t("comments.saved", {account: resolved}))
    } catch (e) {
      toast.error(t("common.saveFailed"), {description: e instanceof Error ? e.message : String(e)})
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2.5">
        <h1 className="text-sm font-semibold">{t("comments.title", {account: resolved})}</h1>
        <Button className="ml-auto" size="sm" onClick={apply} disabled={loading || saving || !!error}>
          {saving && <Loader2 className="size-4 animate-spin" />} {t("common.applyAction")}
        </Button>
      </header>
      <div className="min-h-0 flex-1">
        {loading ? (
          <div className="flex items-center gap-2 p-4 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> {t("comments.loading")}
          </div>
        ) : error ? (
          <div className="p-4 text-sm text-destructive">{error}</div>
        ) : (
          <Editor
            theme="vs-dark"
            language="plaintext"
            value={content}
            onChange={(v) => {
              valueRef.current = v ?? ""
              setContent(v ?? "")
            }}
            options={{
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
