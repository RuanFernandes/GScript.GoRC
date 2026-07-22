// ScriptHelpResult renders a /scripthelp2 result line in the chat: a count line
// plus a row of function-name chips. Hovering a chip opens a translucent tooltip
// (position:fixed, so it escapes the chat's scroll clipping) with the function's
// signature, scope/returns, description, and a GS2-syntax-highlighted example.
import {useEffect, useState} from "react"
import {createPortal} from "react-dom"

import {tokenizeGS2, TOKEN_COLORS} from "@/lib/highlightGS2"
import type {GsFunction} from "@/types"

interface ScriptHelpResultProps {
  query: string
  entries: GsFunction[]
}

export function ScriptHelpResult({query, entries}: ScriptHelpResultProps) {
  const [hover, setHover] = useState<{entry: GsFunction; x: number; y: number} | null>(null)

  // Keep the tooltip near the cursor; clamp so it stays on screen.
  useEffect(() => {
    if (!hover) return
    const onMove = (e: MouseEvent) => {
      setHover((h) => (h ? {...h, x: e.clientX, y: e.clientY} : h))
    }
    document.addEventListener("mousemove", onMove)
    return () => document.removeEventListener("mousemove", onMove)
  }, [hover !== null])

  return (
    <div className="my-1">
      <div className="text-muted-foreground text-xs">
        {entries.length === 0
          ? `No functions matching "${query}".`
          : `${entries.length} function${entries.length === 1 ? "" : "s"} matching "${query}" — hover to preview:`}
      </div>
      <div className="mt-1 flex flex-wrap gap-1.5">
        {entries.map((e) => (
          <button
            key={e.name}
            type="button"
            className="bg-secondary hover:bg-secondary/70 rounded px-2 py-0.5 font-mono text-xs"
            onMouseEnter={(ev) => setHover({entry: e, x: ev.clientX, y: ev.clientY})}
            onMouseLeave={() => setHover(null)}
          >
            {e.name}
          </button>
        ))}
      </div>
      {hover &&
        createPortal(
          <div
            className="bg-popover/80 supports-[backdrop-filter]:bg-popover/60 text-popover-foreground pointer-events-none fixed z-50 w-96 max-w-[90vw] rounded-lg border p-3 shadow-2xl backdrop-blur-md"
            style={{
              left: Math.min(hover.x + 14, (typeof window !== "undefined" ? window.innerWidth : 9999) - 390),
              top: Math.max(8, hover.y - 40),
            }}
          >
            <FunctionDetails entry={hover.entry} />
          </div>,
          document.body,
        )}
    </div>
  )
}

function FunctionDetails({entry}: {entry: GsFunction}) {
  return (
    <div className="grid gap-2">
      <div className="flex items-baseline gap-2">
        <span className="text-sm font-semibold">{entry.name}</span>
        {entry.scope && (
          <span className="bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px] uppercase">
            {entry.scope}
          </span>
        )}
        {entry.returns && entry.returns !== "void" && (
          <span className="text-muted-foreground text-[11px]">→ {entry.returns}</span>
        )}
      </div>

      <code className="bg-muted/60 rounded px-2 py-1 font-mono text-xs">
        <span style={{color: `#${TOKEN_COLORS.function}`}}>{entry.name}</span>
        <span style={{color: `#${TOKEN_COLORS.punct}`}}>(</span>
        {entry.params.map((p, i) => (
          <span key={p + i} style={{color: `#${TOKEN_COLORS.variable}`}}>
            {i > 0 && <span style={{color: `#${TOKEN_COLORS.punct}`}}>, </span>}
            {p}
          </span>
        ))}
        <span style={{color: `#${TOKEN_COLORS.punct}`}}>)</span>
      </code>

      {entry.description && (
        <p className="text-muted-foreground text-xs leading-relaxed">{entry.description}</p>
      )}

      {entry.example && (
        <pre className="bg-black/40 max-h-60 overflow-auto rounded p-2 font-mono text-xs leading-relaxed">
          {tokenizeGS2(entry.example).map((t, i) => (
            <span key={i} style={{color: `#${TOKEN_COLORS[t.type] ?? TOKEN_COLORS.text}`}}>
              {t.text}
            </span>
          ))}
        </pre>
      )}
    </div>
  )
}
