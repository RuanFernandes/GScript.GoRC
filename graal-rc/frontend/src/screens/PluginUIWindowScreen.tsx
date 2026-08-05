import {useEffect, useState} from "react"
import type {ReactNode} from "react"
import {Events} from "@wailsio/runtime"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {rcService} from "@/services/rcService"
import type {PluginUIPrimitive, PluginUIView} from "@/plugins/types"

type WindowInfo = {id: string; pluginId: string; title: string; width: number; height: number; view: PluginUIView}

export function PluginUIWindowScreen() {
  const params = new URLSearchParams(window.location.hash.split("?")[1] ?? "")
  const pluginId = params.get("plugin") ?? ""
  const windowId = params.get("window") ?? ""
  const [info, setInfo] = useState<WindowInfo | null>(null)
  const [error, setError] = useState("")

  useEffect(() => {
    let active = true
    void rcService.getPluginUIWindow(pluginId, windowId).then(value => {
      if (active) setInfo(value as WindowInfo)
    }).catch(reason => {
      if (active) setError(String(reason))
    })
    const offUpdated = Events.On("plugin:ui-updated", event => {
      try {
        const next = JSON.parse(event.data) as WindowInfo
        if (next.pluginId === pluginId && next.id === windowId) setInfo(next)
      } catch { /* ignore malformed host events */ }
    })
    return () => {
      active = false
      offUpdated()
    }
  }, [pluginId, windowId])

  if (error) return <div className="bg-background flex h-svh items-center justify-center p-6 text-sm text-destructive">{error}</div>
  if (!info) return <div className="bg-background flex h-svh items-center justify-center p-6 text-sm text-muted-foreground">Loading plugin view…</div>
  return <div className="bg-background min-h-svh p-5"><PluginViewNode node={info.view} pluginId={pluginId} windowId={windowId} /></div>
}

function PluginViewNode({node, pluginId, windowId}: {node: PluginUIView; pluginId: string; windowId: string}): ReactNode {
  switch (node.type) {
    case "stack": return <div className="grid" style={{gap: node.gap ?? 12}}>{node.children.map((child, index) => <PluginViewNode key={index} node={child} pluginId={pluginId} windowId={windowId} />)}</div>
    case "row": return <div className="flex flex-wrap items-end" style={{gap: node.gap ?? 8}}>{node.children.map((child, index) => <PluginViewNode key={index} node={child} pluginId={pluginId} windowId={windowId} />)}</div>
    case "heading": return <h1 className="text-lg font-semibold">{node.text}</h1>
    case "text": return <p className={node.tone === "muted" ? "text-sm text-muted-foreground" : node.tone === "danger" ? "text-sm text-destructive" : node.tone === "success" ? "text-sm text-emerald-400" : "text-sm"}>{node.text}</p>
    case "divider": return <hr className="border-border" />
    case "button": return <Button variant={node.variant === "danger" ? "destructive" : node.variant === "secondary" ? "secondary" : "default"} disabled={node.disabled} onClick={() => void rcService.pluginUIAction(pluginId, windowId, node.action, null)}>{node.label}</Button>
    case "input": return <label className="grid min-w-48 gap-1.5 text-xs"><span className="text-muted-foreground">{node.label}</span><Input defaultValue={node.value ?? ""} placeholder={node.placeholder} onChange={event => { if (node.action) void rcService.pluginUIAction(pluginId, windowId, node.action, event.target.value) }} /></label>
    case "select": return <label className="grid min-w-48 gap-1.5 text-xs"><span className="text-muted-foreground">{node.label}</span><select className="h-9 rounded-md border bg-background px-2 text-sm" defaultValue={node.value ?? ""} onChange={event => { if (node.action) void rcService.pluginUIAction(pluginId, windowId, node.action, event.target.value) }}>{node.options.map(option => <option key={option.value} value={option.value}>{option.label}</option>)}</select></label>
    case "code": return <pre className="max-h-[32rem] overflow-auto rounded-md border bg-black/30 p-3 font-mono text-xs leading-5">{node.value}</pre>
    case "table": return <div className="overflow-auto rounded-md border"><table className="w-full text-left text-xs"><thead className="border-b bg-muted/30"><tr>{node.columns.map(column => <th key={column.key} className="px-3 py-2 font-medium">{column.label}</th>)}</tr></thead><tbody>{node.rows.map((row, index) => <tr key={index} className="border-b last:border-0">{node.columns.map(column => <td key={column.key} className="px-3 py-2">{formatValue(row[column.key])}</td>)}</tr>)}</tbody></table></div>
  }
}

function formatValue(value: PluginUIPrimitive): string {
  if (value === null) return ""
  return String(value)
}
