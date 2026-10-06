// SyncReviewWindowScreen lists scripts awaiting human resolution (conflicts,
// server changes that need human review. Each row provides a diff and an
// editable merge buffer; nothing is overwritten until the user chooses.
import {useEffect, useRef, useState} from "react"
import {DiffEditor, Editor, type BeforeMount} from "@monaco-editor/react"

import {Button} from "@/components/ui/button"
import {Badge} from "@/components/ui/badge"
import {Skeleton} from "@/components/ui/skeleton"
import {useSync} from "@/hooks/useSync"
import type {SyncReviewItem} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"
import {registerGraalScript} from "@/lib/monacoGraalScript"

const STATE_BADGE: Record<string, "destructive" | "secondary" | "default"> = {
  conflict: "destructive",
  "initial-conflict": "destructive",
  "new-local": "secondary",
  "local-missing-keep": "secondary",
  "server-missing-keep": "secondary",
}

const syncReviewLanguage = (kind: string) =>
  kind === "weapon" || kind === "class" || kind === "npc" ? "graalscript" : "plaintext"

// Under server-truth, the only items that reach review are local files with no
// server counterpart (new-local) or whose server entry was deleted. The local
// file is never deleted — the single action quarantines it to .rejected and
// stops tracking.
function ReviewRow({
  item,
  onResolve,
}: {
  item: SyncReviewItem
  onResolve: (kind: string, key: string, choice: "local" | "server" | "merge", mergeContent?: string) => Promise<void>
}) {
  const {t} = useLanguage()
  const [open, setOpen] = useState(false)
  const [merged, setMerged] = useState(item.local ?? "")
  const diffHostRef = useRef<HTMLDivElement>(null)
  const [compactDiff, setCompactDiff] = useState(false)
  const language = syncReviewLanguage(item.kind)
  const handleBeforeMount: BeforeMount = (monaco) => {
    registerGraalScript(monaco as unknown as Parameters<typeof registerGraalScript>[0])
  }

  useEffect(() => {
    const element = diffHostRef.current
    if (!element) return
    const update = () => setCompactDiff(element.clientWidth < 720)
    update()
    const observer = new ResizeObserver(update)
    observer.observe(element)
    return () => observer.disconnect()
  }, [open])

  return (
    <div className="border-b last:border-b-0">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        aria-expanded={open}
        className="hover:bg-accent flex min-w-0 w-full flex-wrap items-center gap-x-3 gap-y-1.5 px-3 py-2 text-left sm:px-4"
      >
        <Badge variant={STATE_BADGE[item.state] ?? "default"}>
          {t(`sync.state.${item.state === "initial-conflict" ? "initial" : item.state === "new-local" ? "newLocal" : item.state === "local-missing-keep" ? "localMissing" : item.state === "server-missing-keep" ? "serverMissing" : "conflict"}`)}
        </Badge>
        <span className="min-w-0 flex-1 truncate font-medium">{item.name}</span>
        <span className="text-muted-foreground text-xs uppercase">{item.kind}</span>
        {item.actor && (
          <span className="text-muted-foreground ml-0 max-w-full truncate text-xs sm:ml-auto">by {item.actor}</span>
        )}
      </button>
      {open && (
        <div className="grid gap-3 px-3 pb-3 sm:px-4">
          <div className="grid grid-cols-1 gap-1 text-xs sm:grid-cols-2 sm:gap-2">
            <div className="text-muted-foreground">{t("sync.localCopy")} {item.local ? "" : t("sync.absent")}</div>
            <div className="text-muted-foreground">{t("sync.server")} {item.server ? "" : t("sync.absent")}</div>
          </div>
          <div ref={diffHostRef} className="h-64 min-h-0 overflow-hidden rounded-md border sm:h-80">
            <DiffEditor
              height="100%"
              original={item.local ?? ""}
              modified={item.server ?? ""}
              language={language}
              theme="vs-dark"
              beforeMount={handleBeforeMount}
              options={{
                readOnly: true,
                renderSideBySide: !compactDiff,
                fontLigatures: true,
                minimap: {enabled: false},
                scrollBeyondLastLine: false,
                automaticLayout: true,
              }}
            />
          </div>
          <div className="h-40 min-h-32 w-full resize-y overflow-auto rounded-md border">
            <Editor
              height="100%"
              value={merged}
              language={language}
              theme="vs-dark"
              beforeMount={handleBeforeMount}
              onChange={(value) => setMerged(value ?? "")}
              options={{
                minimap: {enabled: false},
                scrollBeyondLastLine: false,
                fontLigatures: true,
                fontSize: 12,
                wordWrap: "on",
                automaticLayout: true,
                ariaLabel: t("sync.mergedContent"),
              }}
            />
          </div>
          <div className="flex flex-wrap gap-2">
            <Button className="min-w-32 flex-1 sm:flex-none" variant="outline" onClick={() => onResolve(item.kind, item.key, "local")}>{t("sync.keepLocal")}</Button>
            <Button className="min-w-32 flex-1 sm:flex-none" variant="outline" onClick={() => onResolve(item.kind, item.key, "server")}>{t("sync.useServer")}</Button>
            <Button className="min-w-32 flex-1 sm:flex-none" onClick={() => onResolve(item.kind, item.key, "merge", merged)}>{t("sync.applyMerge")}</Button>
          </div>
        </div>
      )}
    </div>
  )
}

export function SyncReviewWindowScreen() {
  const {t} = useLanguage()
  const {status, loaded, resolveConflict, pause, resume} = useSync()

  return (
    <div className="bg-background flex h-svh min-w-0 flex-col">
      <header className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-3 py-2.5 sm:px-4">
        <h1 className="min-w-0 flex-1 text-base font-semibold">{t("sync.reviewTitle")}</h1>
        <div className="flex w-full flex-wrap items-center justify-end gap-2 sm:w-auto">
          {status.paused ? (
            <Button variant="outline" size="sm" onClick={resume}>
              {t("sync.resume")}
            </Button>
          ) : (
            <Button variant="ghost" size="sm" onClick={pause}>
              {t("sync.pause")}
            </Button>
          )}
          {status.reviewCount > 0 && (
            <Badge variant="destructive">
              {status.reviewCount} {t("sync.pending")}
            </Badge>
          )}
        </div>
      </header>
      <main className="min-h-0 flex-1 overflow-y-auto">
        {!loaded ? (
          <div className="grid gap-2 p-4">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        ) : (status.items ?? []).length === 0 ? (
          <p className="text-muted-foreground p-6 text-sm">
            {t("sync.noConflicts")}
          </p>
        ) : (
          (status.items ?? []).map((it) => (
            <ReviewRow key={`${it.kind}:${it.key}`} item={it} onResolve={resolveConflict} />
          ))
        )}
      </main>
    </div>
  )
}
