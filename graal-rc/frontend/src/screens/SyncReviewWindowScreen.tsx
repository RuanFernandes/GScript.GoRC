// SyncReviewWindowScreen lists scripts awaiting human resolution (conflicts,
// server changes that need human review. Each row provides a diff and an
// editable merge buffer; nothing is overwritten until the user chooses.
import {useState} from "react"
import {DiffEditor} from "@monaco-editor/react"

import {Button} from "@/components/ui/button"
import {Badge} from "@/components/ui/badge"
import {Skeleton} from "@/components/ui/skeleton"
import {useSync} from "@/hooks/useSync"
import type {SyncReviewItem, SyncState} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

const STATE_BADGE: Record<string, "destructive" | "secondary" | "default"> = {
  conflict: "destructive",
  "initial-conflict": "destructive",
  "new-local": "secondary",
  "local-missing-keep": "secondary",
  "server-missing-keep": "secondary",
}

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

  return (
    <div className="border-b">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="hover:bg-accent flex w-full items-center gap-3 px-3 py-2 text-left"
      >
        <Badge variant={STATE_BADGE[item.state] ?? "default"}>
          {t(`sync.state.${item.state === "initial-conflict" ? "initial" : item.state === "new-local" ? "newLocal" : item.state === "local-missing-keep" ? "localMissing" : item.state === "server-missing-keep" ? "serverMissing" : "conflict"}`)}
        </Badge>
        <span className="font-medium">{item.name}</span>
        <span className="text-muted-foreground text-xs uppercase">{item.kind}</span>
        {item.actor && (
          <span className="text-muted-foreground ml-auto text-xs">by {item.actor}</span>
        )}
      </button>
      {open && (
        <div className="grid gap-2 px-3 pb-3">
          <div className="grid grid-cols-2 gap-2 text-xs">
            <div className="text-muted-foreground">{t("sync.local")} {item.local ? "" : t("sync.absent")}</div>
            <div className="text-muted-foreground">{t("sync.server")} {item.server ? "" : t("sync.absent")}</div>
          </div>
          <div className="h-[320px] overflow-hidden rounded-md border">
            <DiffEditor
              height={320}
              original={item.local ?? ""}
              modified={item.server ?? ""}
              language="plaintext"
              theme="vs-dark"
              options={{
                readOnly: true,
                renderSideBySide: true,
                minimap: {enabled: false},
                scrollBeyondLastLine: false,
              }}
            />
          </div>
          <textarea
            value={merged}
            onChange={(e) => setMerged(e.target.value)}
            aria-label={t("sync.mergedContent")}
            className="min-h-40 w-full rounded-md border bg-background p-3 font-mono text-xs outline-none focus:ring-2 focus:ring-ring"
          />
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onResolve(item.kind, item.key, "local")}>{t("sync.keepLocal")}</Button>
            <Button variant="outline" onClick={() => onResolve(item.kind, item.key, "server")}>{t("sync.useServer")}</Button>
            <Button onClick={() => onResolve(item.kind, item.key, "merge", merged)}>{t("sync.applyMerge")}</Button>
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
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-3 border-b px-4 py-2.5">
        <h1 className="text-base font-semibold">{t("sync.reviewTitle")}</h1>
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
          <Badge variant="destructive" className="ml-auto">
            {status.reviewCount} {t("sync.pending")}
          </Badge>
        )}
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
