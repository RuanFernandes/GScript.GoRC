// ContextMenu is a small dark right-click menu rendered absolutely at the cursor.
// It closes on click-outside, Escape, scroll, or item click. Reusable: pass an
// items list (label + onSelect + optional disabled/danger).
import {useEffect, useRef} from "react"

export interface ContextMenuItem {
  label: string
  onSelect: () => void
  disabled?: boolean
  danger?: boolean
  separator?: boolean // render a divider before this item
}

export function ContextMenu({
  x,
  y,
  items,
  onClose,
}: {
  x: number
  y: number
  items: ContextMenuItem[]
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose()
    }
    const onScroll = () => onClose()
    window.addEventListener("mousedown", onDown)
    window.addEventListener("keydown", onKey)
    window.addEventListener("scroll", onScroll, true)
    window.addEventListener("resize", onScroll)
    return () => {
      window.removeEventListener("mousedown", onDown)
      window.removeEventListener("keydown", onKey)
      window.removeEventListener("scroll", onScroll, true)
      window.removeEventListener("resize", onScroll)
    }
  }, [onClose])

  return (
    <div
      ref={ref}
      className="bg-popover text-popover-foreground fixed z-50 min-w-[160px] rounded-md border p-1 text-sm shadow-lg"
      style={{left: x, top: y}}
    >
      {items.map((item, i) =>
        item.separator ? (
          <div key={i} className="bg-border my-1 h-px" />
        ) : (
          <button
            key={i}
            disabled={item.disabled}
            onClick={() => {
              if (item.disabled) return
              item.onSelect()
              onClose()
            }}
            className={`hover:bg-accent flex w-full items-center rounded px-2 py-1.5 text-left disabled:cursor-not-allowed disabled:opacity-40 ${
              item.danger ? "text-destructive hover:bg-destructive/15" : ""
            }`}
          >
            {item.label}
          </button>
        ),
      )}
    </div>
  )
}
