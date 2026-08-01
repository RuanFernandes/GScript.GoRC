// SyncPopover — the inline Sync control. In the sidebar it renders as a rail row
// (icon + label); clicking opens a panel docked to the RIGHT of the button
// (left-full) so it never overflows the left edge, regardless of window size.
// The panel stays open independently of the sidebar hover state; closes on
// outside-click / Escape. A badge shows pending review conflicts.
import {useEffect, useRef, useState} from "react"
import {RefreshCw} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Badge} from "@/components/ui/badge"
import {SyncSection} from "@/components/features/settings/SyncSection"
import {useSync} from "@/hooks/useSync"
import {rcService} from "@/services/rcService"

export function SyncPopover({rail = false}: {rail?: boolean}) {
  const [open, setOpen] = useState(false)
  const {status} = useSync()
  const wrapRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
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
          title="Local Sync"
          aria-label="Local Sync"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
          className={`flex h-11 w-full items-center gap-3 px-3 text-sm font-medium transition-colors hover:bg-accent focus-visible:z-10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring active:bg-accent ${
            open ? "bg-accent" : ""
          }`}
        >
          <RefreshCw className="size-4 shrink-0" />
          <span className="sidebar-label flex-1 whitespace-nowrap text-left">Sync</span>
          {badge}
        </button>
      ) : (
        <Button
          variant="outline"
          size="sm"
          title="Local Sync"
          aria-label="Local Sync"
          aria-expanded={open}
          onClick={() => setOpen((o) => !o)}
        >
          <RefreshCw />
          Sync
          {badge}
        </Button>
      )}
      {open && (
        <div
          role="dialog"
          aria-label="Local Sync"
          className="absolute left-full top-0 z-50 ml-2 w-[min(460px,80vw)] max-h-[80vh] overflow-y-auto rounded-lg border bg-popover p-4 shadow-xl"
        >
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-sm font-semibold">Local Sync</h2>
            {status.paused && <Badge variant="secondary">Paused</Badge>}
          </div>
          <SyncSection />
          {status.reviewCount > 0 && (
            <Button
              variant="destructive"
              size="sm"
              className="mt-4 w-full"
              onClick={() => rcService.openSyncReview()}
            >
              Review {status.reviewCount} conflict{status.reviewCount > 1 ? "s" : ""}
            </Button>
          )}
        </div>
      )}
    </div>
  )
}
