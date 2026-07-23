// Modal is a minimal centered overlay used by the player-list PM / message
// compose dialogs. The project ships no generic Dialog primitive (only
// AlertDialog), and these dialogs need a textarea body AlertDialog does not
// render well, so this lightweight portal-less overlay avoids a new dependency.
import {X} from "lucide-react"
import {type ReactNode, useEffect} from "react"

import {Button} from "@/components/ui/button"
import {cn} from "@/lib/utils"

interface ModalProps {
  open: boolean
  title: string
  description?: string
  onClose: () => void
  children: ReactNode
  footer?: ReactNode
  className?: string
}

export function Modal({open, title, description, onClose, children, footer, className}: ModalProps) {
  useEffect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
      <div className="bg-black/60 absolute inset-0 backdrop-blur-sm" onClick={onClose} />
      <div
        role="dialog"
        aria-modal="true"
        aria-label={title}
        className={cn(
          "bg-card text-card-foreground relative z-10 flex max-h-[85vh] w-full max-w-md flex-col overflow-hidden rounded-xl border shadow-2xl",
          className
        )}
      >
        <header className="flex items-start justify-between gap-3 border-b px-5 py-3.5">
          <div className="min-w-0">
            <h2 className="truncate text-sm font-semibold">{title}</h2>
            {description && <p className="text-muted-foreground mt-0.5 truncate text-xs">{description}</p>}
          </div>
          <Button variant="ghost" size="icon" className="size-7" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">{children}</div>
        {footer && <footer className="flex justify-end gap-2 border-t px-5 py-3">{footer}</footer>}
      </div>
    </div>
  )
}
