import type {ReactNode} from "react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import type {PluginUIPrimitive, PluginUIView} from "@/plugins/types"

export type PluginViewAction = (action: string, value?: PluginUIPrimitive) => void

export function PluginViewNode({node, onAction}: {node: PluginUIView; onAction: PluginViewAction}): ReactNode {
  switch (node.type) {
    case "stack":
      return <div className="grid" style={{gap: node.gap ?? 12}}>{node.children.map((child, index) => <PluginViewNode key={index} node={child} onAction={onAction} />)}</div>
    case "row":
      return <div className="flex flex-wrap items-end" style={{gap: node.gap ?? 8}}>{node.children.map((child, index) => <PluginViewNode key={index} node={child} onAction={onAction} />)}</div>
    case "card":
      return (
        <section className="bg-card/40 grid gap-3 rounded-lg border p-4">
          {(node.title || node.description) && (
            <div className="grid gap-1">
              {node.title && <h2 className="text-sm font-semibold">{node.title}</h2>}
              {node.description && <p className="text-muted-foreground text-xs leading-5">{node.description}</p>}
            </div>
          )}
          <div className="grid gap-3">{node.children.map((child, index) => <PluginViewNode key={index} node={child} onAction={onAction} />)}</div>
        </section>
      )
    case "heading":
      return <h1 className="text-lg font-semibold">{node.text}</h1>
    case "text":
      return <p className={toneClass(node.tone)}>{node.text}</p>
    case "badge":
      return <span className={`inline-flex w-fit items-center rounded-full border px-2 py-0.5 text-[11px] font-medium ${badgeClass(node.tone)}`}>{node.text}</span>
    case "divider":
      return <hr className="border-border" />
    case "button":
      return <Button variant={node.variant === "danger" ? "destructive" : node.variant === "secondary" ? "secondary" : "default"} disabled={node.disabled} onClick={() => onAction(node.action, null)}>{node.label}</Button>
    case "input":
      return (
        <label className="grid min-w-48 gap-1.5 text-xs">
          <span className="text-muted-foreground">{node.label}</span>
          <Input defaultValue={node.value ?? ""} placeholder={node.placeholder} onChange={event => { if (node.action) onAction(node.action, event.target.value) }} />
        </label>
      )
    case "textarea":
      return (
        <label className="grid min-w-64 gap-1.5 text-xs">
          <span className="text-muted-foreground">{node.label}</span>
          <textarea
            defaultValue={node.value ?? ""}
            placeholder={node.placeholder}
            rows={Math.max(2, Math.min(16, node.rows ?? 4))}
            className="border-input bg-transparent placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 min-h-20 w-full rounded-md border px-3 py-2 text-sm outline-none focus-visible:ring-[3px]"
            onChange={event => { if (node.action) onAction(node.action, event.target.value) }}
          />
        </label>
      )
    case "checkbox":
      return (
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" defaultChecked={node.value ?? false} className="size-4 accent-primary" onChange={event => { if (node.action) onAction(node.action, event.target.checked) }} />
          <span>{node.label}</span>
        </label>
      )
    case "select":
      return (
        <label className="grid min-w-48 gap-1.5 text-xs">
          <span className="text-muted-foreground">{node.label}</span>
          <select className="h-9 rounded-md border bg-background px-2 text-sm" defaultValue={node.value ?? ""} onChange={event => { if (node.action) onAction(node.action, event.target.value) }}>
            {node.options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}
          </select>
        </label>
      )
    case "progress": {
      const max = node.max ?? 100
      const percentage = max > 0 ? Math.max(0, Math.min(100, (node.value / max) * 100)) : 0
      return (
        <div className="grid gap-1.5">
          {node.label && <div className="flex items-center justify-between gap-3 text-xs"><span>{node.label}</span><span className="text-muted-foreground">{Math.round(percentage)}%</span></div>}
          <div className="bg-muted h-2 overflow-hidden rounded-full"><div className="bg-primary h-full rounded-full transition-[width]" style={{width: `${percentage}%`}} /></div>
        </div>
      )
    }
    case "empty":
      return <div className="border-border bg-muted/20 rounded-lg border border-dashed px-4 py-8 text-center"><p className="text-sm font-medium">{node.title}</p>{node.description && <p className="text-muted-foreground mt-1 text-xs leading-5">{node.description}</p>}</div>
    case "code":
      return <pre className="max-h-[32rem] overflow-auto rounded-md border bg-black/30 p-3 font-mono text-xs leading-5">{node.value}</pre>
    case "table":
      return <div className="overflow-auto rounded-md border"><table className="w-full text-left text-xs"><thead className="border-b bg-muted/30"><tr>{node.columns.map(column => <th key={column.key} className="px-3 py-2 font-medium">{column.label}</th>)}</tr></thead><tbody>{node.rows.map((row, index) => <tr key={index} className="border-b last:border-0">{node.columns.map(column => <td key={column.key} className="px-3 py-2">{formatValue(row[column.key])}</td>)}</tr>)}</tbody></table></div>
  }
}

function toneClass(tone: "default" | "muted" | "danger" | "success" | undefined): string {
  if (tone === "muted") return "text-muted-foreground text-sm"
  if (tone === "danger") return "text-destructive text-sm"
  if (tone === "success") return "text-emerald-400 text-sm"
  return "text-sm"
}

function badgeClass(tone: "default" | "muted" | "danger" | "success" | undefined): string {
  if (tone === "muted") return "text-muted-foreground bg-muted/30"
  if (tone === "danger") return "text-destructive border-destructive/30 bg-destructive/10"
  if (tone === "success") return "text-emerald-300 border-emerald-500/30 bg-emerald-500/10"
  return "text-foreground bg-primary/10 border-primary/20"
}

function formatValue(value: PluginUIPrimitive): string {
  if (value === null) return ""
  return String(value)
}
