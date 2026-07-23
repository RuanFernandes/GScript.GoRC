// ContextMenu is a small dark right-click menu rendered absolutely at the cursor.
// It closes on click-outside, Escape, scroll, or item click. Reusable: pass an
// items list (label + onSelect + optional disabled/danger).
import {useEffect, useLayoutEffect, useRef, useState} from "react"

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
  // Clamp the menu inside the viewport so a right-click near the window edge
  // (or a small child window) doesn't get clipped. Measured after first paint,
  // then re-checked on resize. 8px margin keeps it off the very edge.
  const [pos, setPos] = useState({left: x, top: y})
  const MARGIN = 8

  const clamp = () => {
    const el = ref.current
    if (!el) return
    const w = el.offsetWidth
    const h = el.offsetHeight
    setPos({
      left: Math.min(x, window.innerWidth - w - MARGIN),
      top: Math.min(y, window.innerHeight - h - MARGIN),
    })
  }

  useLayoutEffect(() => {
    clamp()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [x, y])

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
      style={{left: pos.left, top: pos.top}}
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
