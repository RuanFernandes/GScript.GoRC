// ThemeSelect is a fully dark-styled dropdown for picking the Monaco theme.
// Built from scratch (not a native <select>) because WebView2 renders the native
// select popup on a light background regardless of color-scheme, making theme
// names unreadable. Click-outside + Escape close it.
import {useEffect, useRef, useState} from "react"
import {ChevronDown, Check} from "lucide-react"

import {MONACO_THEME_OPTIONS} from "@/lib/monacoThemes"
import {cn} from "@/lib/utils"

interface ThemeSelectProps {
  value: string
  onChange: (key: string) => void
  // Optional extra entry (e.g. the active remote theme) shown at the top.
  extraOption?: {key: string; label: string}
  customOptions?: {key: string; label: string}[]
}

export function ThemeSelect({value, onChange, extraOption, customOptions = []}: ThemeSelectProps) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const selected =
    (extraOption && value === extraOption.key ? extraOption : undefined) ??
    MONACO_THEME_OPTIONS.find((o) => o.key === value) ??
    MONACO_THEME_OPTIONS[0]

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
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

  return (
    <div ref={ref} className="relative">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="bg-input border-input focus-visible:ring-ring flex h-9 w-full items-center justify-between rounded-md border px-2 text-sm focus-visible:ring-2 focus-visible:outline-none"
      >
        <span className="truncate">{selected.label}</span>
        <ChevronDown className="text-muted-foreground size-4 shrink-0" />
      </button>
      {open && (
        <ul className="bg-popover text-popover-foreground absolute z-50 mt-1 max-h-72 w-full overflow-auto rounded-md border shadow-md">
          {extraOption && (
            <li>
              <button
                type="button"
                onClick={() => {
                  onChange(extraOption.key)
                  setOpen(false)
                }}
                className={cn(
                  "flex w-full items-center justify-between px-2 py-1.5 text-left text-sm hover:bg-accent",
                  value === extraOption.key && "bg-accent",
                )}
              >
                <span className="truncate">{extraOption.label}</span>
                {value === extraOption.key && <Check className="size-4 shrink-0" />}
              </button>
            </li>
          )}
          {customOptions.map((o) => (
            <li key={o.key}>
              <button
                type="button"
                onClick={() => { onChange(o.key); setOpen(false) }}
                className={cn(
                  "flex w-full items-center justify-between px-2 py-1.5 text-left text-sm hover:bg-accent",
                  o.key === value && "bg-accent",
                )}
              >
                <span className="truncate">{o.label}</span>
                {o.key === value && <Check className="size-4 shrink-0" />}
              </button>
            </li>
          ))}
          {MONACO_THEME_OPTIONS.map((o) => (
            <li key={o.key}>
              <button
                type="button"
                onClick={() => {
                  onChange(o.key)
                  setOpen(false)
                }}
                className={cn(
                  "flex w-full items-center justify-between px-2 py-1.5 text-left text-sm hover:bg-accent",
                  o.key === value && "bg-accent",
                )}
              >
                <span className="truncate">{o.label}</span>
                {o.key === value && <Check className="size-4 shrink-0" />}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
