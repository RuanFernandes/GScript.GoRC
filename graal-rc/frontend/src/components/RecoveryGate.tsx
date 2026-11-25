// RecoveryGate covers the current screen during an automatic reconnect without
// unmounting it, so chat history and typed input survive. The screen underneath
// is inert (no pointer or focus) and window keyboard shortcuts are stopped.
import {type ReactNode, useEffect, useRef} from "react"

import {Button} from "@/components/ui/button"
import {useLanguage} from "@/hooks/useLanguage"
import type {ConnectionRecoveryStatus} from "@/lib/connectionRecovery"

interface RecoveryGateProps {
  recovery: ConnectionRecoveryStatus
  onCancel: () => void
  children: ReactNode
}

export function RecoveryGate({recovery, onCancel, children}: RecoveryGateProps) {
  // The element tree around children never changes, so React keeps them mounted.
  // The inner classes pass the window frame's full-height sizing to the screen.
  return (
    <div className="relative h-full">
      <div inert={recovery.active} className="h-full [&>*]:!h-full [&>*]:!min-h-0 [&>*]:!min-w-0">
        {children}
      </div>
      {recovery.active && <RecoveryOverlay recovery={recovery} onCancel={onCancel} />}
    </div>
  )
}

function RecoveryOverlay({recovery, onCancel}: Omit<RecoveryGateProps, "children">) {
  const {t} = useLanguage()
  const dialogRef = useRef<HTMLDivElement>(null)
  const previousFocus = useRef(document.activeElement)

  useEffect(() => {
    // Focus the panel, not Cancel: a key already being typed must not cancel.
    dialogRef.current?.focus()
    // Capture on window runs before every other key listener. Default actions
    // (Tab, Enter on the Cancel button) still happen.
    const block = (event: KeyboardEvent) => event.stopPropagation()
    window.addEventListener("keydown", block, true)
    const previous = previousFocus.current
    return () => {
      window.removeEventListener("keydown", block, true)
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [])

  return (
    <div className="bg-background/80 absolute inset-0 z-[100] flex items-center justify-center p-6 backdrop-blur-sm">
      <div
        ref={dialogRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby="recovery-title"
        tabIndex={-1}
        className="bg-card w-full max-w-md space-y-4 rounded-lg border p-6 outline-none"
      >
        <div role="status" aria-live="polite" className="space-y-2">
          <h2 id="recovery-title" className="text-lg font-semibold">{t("recovery.title")}</h2>
          <p className="text-muted-foreground text-sm">{t("recovery.progress", {attempt: recovery.attempt, total: recovery.maxAttempts})}</p>
          <p className="text-muted-foreground text-sm">{t("recovery.description")}</p>
        </div>
        <Button variant="outline" onClick={onCancel}>{t("common.cancel")}</Button>
      </div>
    </div>
  )
}
