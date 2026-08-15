// SyncPopover — the inline Sync control. In the sidebar it renders as a rail row
// (icon + label); clicking opens a panel docked to the RIGHT of the button
// (left-full) so it never overflows the left edge, regardless of window size.
// The panel stays open independently of the sidebar hover state; closes on
// outside-click / Escape. A badge shows pending review conflicts.
import {useCallback, useEffect, useRef, useState} from "react"
import {AlertTriangle, RefreshCw} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Badge} from "@/components/ui/badge"
import {AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle} from "@/components/ui/alert-dialog"
import {SyncSection} from "@/components/features/settings/SyncSection"
import {useSync} from "@/hooks/useSync"
import {rcService} from "@/services/rcService"
import {useLanguage} from "@/hooks/useLanguage"

export function SyncPopover({rail = false}: {rail?: boolean}) {
  const [open, setOpen] = useState(false)
  const [panelPosition, setPanelPosition] = useState({top: 8, left: 8})
  const {status, normalizeSync, rebuildSync} = useSync()
  const {t} = useLanguage()
  const wrapRef = useRef<HTMLDivElement>(null)
  const seenPanicAt = useRef<number | null>(null)
  const [panicPromptOpen, setPanicPromptOpen] = useState(false)
  const [recovering, setRecovering] = useState(false)

  useEffect(() => {
    if (!status.panicMode) {
      seenPanicAt.current = null
      return
    }
    const panicKey = status.panicAt || 1
    if (seenPanicAt.current !== panicKey) {
      seenPanicAt.current = panicKey
      setPanicPromptOpen(true)
    }
  }, [status.panicMode, status.panicAt])

  const updatePanelPosition = useCallback(() => {
    const anchor = wrapRef.current?.getBoundingClientRect()
    if (!anchor) return

    const panelWidth = Math.min(520, window.innerWidth * 0.84)
    const gap = 8
    const left = Math.max(gap, Math.min(anchor.right + gap, window.innerWidth - panelWidth - gap))
    setPanelPosition({top: gap, left})
  }, [])

  useEffect(() => {
    if (!open) return
    updatePanelPosition()
    window.addEventListener("resize", updatePanelPosition)

    const onDown = (e: MouseEvent) => {
      // AlertDialog renders through a portal, outside wrapRef. Do not close
      // this popover on the dialog's mousedown, otherwise the dialog unmounts
      // before its buttons receive their click event.
      const target = e.target instanceof Element ? e.target : null
      if (target?.closest('[data-slot="alert-dialog-overlay"], [data-slot="alert-dialog-content"]')) return
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", onDown)
    document.addEventListener("keydown", onKey)
    return () => {
      window.removeEventListener("resize", updatePanelPosition)
      document.removeEventListener("mousedown", onDown)
      document.removeEventListener("keydown", onKey)
    }
  }, [open])

  const badge =
    status.panicMode ? (
      <Badge variant="destructive" className="h-4 px-1 text-[10px]">!</Badge>
    ) : status.reviewCount > 0 ? (
      <Badge variant="destructive" className="h-4 px-1 text-[10px]">
        {status.reviewCount}
      </Badge>
    ) : null

  const normalize = async () => {
    setRecovering(true)
    try {
      await normalizeSync()
      toast.success(t("sync.panicNormalized"))
    } catch {
      toast.error(t("sync.panicRecoveryFailed"))
    } finally {
      setRecovering(false)
    }
  }

  const rebuild = async () => {
    setRecovering(true)
    try {
      await rebuildSync()
      setPanicPromptOpen(false)
      toast.success(t("sync.panicRebuildStarted"))
    } catch {
      toast.error(t("sync.panicRecoveryFailed"))
    } finally {
      setRecovering(false)
    }
  }

  return (
    <div ref={wrapRef} className="relative">
      {rail ? (
        <button
          type="button"
          title={t("sync.local")}
          aria-label={t("sync.local")}
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
          className={`flex h-11 w-full items-center gap-3 px-3 text-sm font-medium transition-colors hover:bg-accent focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring active:bg-accent ${
            open ? "bg-accent" : ""
          }`}
        >
          <RefreshCw className="size-4 shrink-0" />
          <span className="sidebar-label flex-1 whitespace-nowrap text-left">{t("sidebar.sync")}</span>
          {badge}
        </button>
      ) : (
        <Button
          variant="outline"
          size="sm"
          title={t("sync.local")}
          aria-label={t("sync.local")}
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          <RefreshCw />
          {t("sidebar.sync")}
          {badge}
        </Button>
      )}
      {open && (
        <div
          role="dialog"
          aria-label={t("sync.local")}
          className="fixed z-50 w-[min(520px,84vw)] max-h-[calc(100vh-16px)] overflow-y-auto rounded-lg border bg-popover p-4 shadow-xl"
          style={{top: panelPosition.top, left: panelPosition.left}}
        >
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-sm font-semibold">{t("sync.local")}</h2>
            {status.paused && <Badge variant="secondary">{t("sync.paused")}</Badge>}
          </div>
          {status.panicMode && (
            <div className="border-destructive/40 bg-destructive/10 mb-3 rounded-md border p-3 text-xs">
              <div className="flex items-start gap-2">
                <AlertTriangle className="text-destructive mt-0.5 size-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="font-semibold">{t("sync.panicTitle")}</p>
                  <p className="text-muted-foreground mt-1">{t("sync.panicDescription")}</p>
                </div>
              </div>
              <div className="mt-3 grid gap-2 sm:grid-cols-2">
                <Button variant="outline" size="sm" onClick={() => void normalize()} disabled={recovering}>
                  {t("sync.panicNormalize")}
                </Button>
                <Button variant="destructive" size="sm" onClick={() => setPanicPromptOpen(true)} disabled={recovering}>
                  {t("sync.panicRebuild")}
                </Button>
              </div>
            </div>
          )}
          <SyncSection />
          {status.reviewCount > 0 && (
            <Button
              variant="destructive"
              size="sm"
              className="mt-4 w-full"
              onClick={() => rcService.openSyncReview()}
            >
              {t("sync.review", {count: status.reviewCount, suffix: status.reviewCount > 1 ? "s" : ""})}
            </Button>
          )}
        </div>
      )}
      <AlertDialog open={panicPromptOpen} onOpenChange={(value) => !recovering && setPanicPromptOpen(value)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("sync.panicPromptTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("sync.panicPromptDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={recovering}>{t("sync.panicKeepBlocked")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => void rebuild()} disabled={recovering}>
              {t("sync.panicDeleteAndRedownload")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
