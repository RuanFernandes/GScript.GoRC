// SyncReviewWindowScreen lists scripts awaiting human resolution (conflicts,
// new-local files, and delete-keep states). Each row expands to a Monaco
// DiffEditor (local vs server) with Keep-local / Keep-server actions. The
// engine never auto-resolves these — and never deletes in either direction.
import {useState} from "react"
import {DiffEditor} from "@monaco-editor/react"

import {Button} from "@/components/ui/button"
import {Badge} from "@/components/ui/badge"
import {Skeleton} from "@/components/ui/skeleton"
import {useSync} from "@/hooks/useSync"
import type {SyncReviewItem, SyncState} from "@/types"

const STATE_LABEL: Record<string, string> = {
  conflict: "Conflict",
  "initial-conflict": "First-run conflict",
  "new-local": "New local file",
  "local-missing-keep": "Local deleted",
  "server-missing-keep": "Server deleted",
}

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
function actionsFor(): {single: {label: string; choice: "local"}} {
  return {single: {label: "Quarantine (move to .rejected)", choice: "local"}}
}

function ReviewRow({
  item,
  onResolve,
}: {
  item: SyncReviewItem
  onResolve: (kind: string, key: string, choice: "local" | "server") => Promise<void>
}) {
  const [open, setOpen] = useState(false)
  const acts = actionsFor()

  return (
    <div className="border-b">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="hover:bg-accent flex w-full items-center gap-3 px-3 py-2 text-left"
      >
        <Badge variant={STATE_BADGE[item.state] ?? "default"}>
          {STATE_LABEL[item.state] ?? item.state}
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
            <div className="text-muted-foreground">Local {item.local ? "" : "(absent)"}</div>
            <div className="text-muted-foreground">Server {item.server ? "" : "(absent)"}</div>
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
          <div className="flex gap-2">
            <Button onClick={() => onResolve(item.kind, item.key, acts.single.choice)}>
              {acts.single.label}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}

export function SyncReviewWindowScreen() {
  const {status, loaded, resolveConflict, pause, resume} = useSync()

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-3 border-b px-4 py-2.5">
        <h1 className="text-base font-semibold">Sync Review</h1>
        {status.paused ? (
          <Button variant="outline" size="sm" onClick={resume}>
            Resume
          </Button>
        ) : (
          <Button variant="ghost" size="sm" onClick={pause}>
            Pause 1h
          </Button>
        )}
        {status.reviewCount > 0 && (
          <Badge variant="destructive" className="ml-auto">
            {status.reviewCount} pending
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
            No conflicts — all scripts in sync.
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
