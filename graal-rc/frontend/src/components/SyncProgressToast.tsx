import {useEffect} from "react"
import {toast} from "sonner"
import {RefreshCw} from "lucide-react"

import {useLanguage} from "@/hooks/useLanguage"
import {useSync} from "@/hooks/useSync"
import type {SyncStatus} from "@/types"

const INITIAL_SYNC_TOAST_ID = "rc-initial-sync-progress"

function ProgressCard({status}: {status: SyncStatus}) {
  const {t} = useLanguage()
  const progress = status.progress ?? {active: false, phase: "", current: "", completed: 0, total: 0}
  const total = progress.total
  const completed = Math.max(0, Math.min(progress.completed, total || progress.completed))
  const percentage = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : 0

  return (
    <div className="bg-popover text-popover-foreground w-[min(23rem,calc(100vw-2rem))] rounded-lg border p-3 shadow-lg">
      <div className="flex items-start gap-2">
        <RefreshCw className="text-primary mt-0.5 size-4 shrink-0 motion-safe:animate-spin" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-semibold">{t("sync.initialProgressTitle")}</p>
          <p className="text-muted-foreground mt-1 text-xs leading-relaxed">{t("sync.initialProgressDescription")}</p>
        </div>
      </div>
      <div className="bg-muted mt-3 h-2 overflow-hidden rounded-full" role="progressbar" aria-label={t("sync.initialProgressTitle")} aria-valuemin={0} aria-valuemax={total || 100} aria-valuenow={total ? completed : undefined}>
        <div className={`bg-primary h-full rounded-full transition-[width] duration-300 ${total === 0 ? "w-1/3 motion-safe:animate-pulse" : ""}`} style={total > 0 ? {width: `${percentage}%`} : undefined} />
      </div>
      <div className="text-muted-foreground mt-2 flex items-center justify-between gap-3 text-[11px]">
        <span className="truncate">{progress.current || progress.phase || t("sync.initialProgressPreparing")}</span>
        <span className="shrink-0 font-mono tabular-nums">{total > 0 ? t("sync.initialProgressCount", {completed, total}) : "…"}</span>
      </div>
    </div>
  )
}

export function SyncProgressToast() {
  const {status} = useSync()

  useEffect(() => {
    if (!status.initialSync) {
      toast.dismiss(INITIAL_SYNC_TOAST_ID)
      return
    }

    toast.custom(() => <ProgressCard status={status} />, {
      id: INITIAL_SYNC_TOAST_ID,
      duration: Infinity,
      dismissible: false,
      closeButton: false,
    })
  }, [status])

  useEffect(() => {
    return () => {
      toast.dismiss(INITIAL_SYNC_TOAST_ID)
    }
  }, [])

  return null
}
