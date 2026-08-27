import {useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"

import {PluginViewNode} from "@/components/features/plugins/PluginViewNode"
import {rcService} from "@/services/rcService"
import type {PluginUIView} from "@/plugins/types"
import {useLanguage} from "@/hooks/useLanguage"

type WindowInfo = {id: string; pluginId: string; title: string; width: number; height: number; view: PluginUIView}

export function PluginUIWindowScreen() {
  const {t} = useLanguage()
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
  if (!info) return <div className="bg-background flex h-svh items-center justify-center p-6 text-sm text-muted-foreground">{t("plugin.loadingView")}</div>
  return <div className="bg-background min-h-svh p-5"><PluginViewNode node={info.view} onAction={(action, value) => void rcService.pluginUIAction(pluginId, windowId, action, value ?? null)} /></div>
}
