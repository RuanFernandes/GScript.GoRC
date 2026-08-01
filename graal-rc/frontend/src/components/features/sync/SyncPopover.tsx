// SyncPopover — the inline Sync control. In the sidebar it renders as a rail row
// (icon + label); clicking opens a panel docked to the RIGHT of the button
// (left-full) so it never overflows the left edge, regardless of window size.
// The panel stays open independently of the sidebar hover state; closes on
// outside-click / Escape. A badge shows pending review conflicts.
import {useCallback, useEffect, useRef, useState} from "react"
import {RefreshCw} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Badge} from "@/components/ui/badge"
import {SyncSection} from "@/components/features/settings/SyncSection"
import {useSync} from "@/hooks/useSync"
import {rcService} from "@/services/rcService"
import {useLanguage} from "@/hooks/useLanguage"

export function SyncPopover({rail = false}: {rail?: boolean}) {
  const [open, setOpen] = useState(false)
  const [panelPosition, setPanelPosition] = useState({top: 8, left: 8})
  const {status} = useSync()
  const {t} = useLanguage()
  const wrapRef = useRef<HTMLDivElement>(null)

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
    status.reviewCount > 0 ? (
      <Badge variant="destructive" className="h-4 px-1 text-[10px]">
        {status.reviewCount}
      </Badge>
    ) : null

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
    </div>
  )
}
