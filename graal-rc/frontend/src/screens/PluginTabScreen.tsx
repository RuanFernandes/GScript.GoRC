import {PluginViewNode} from "@/components/features/plugins/PluginViewNode"
import {pluginRuntime} from "@/plugins/runtime"
import type {PluginUITabInfo} from "@/plugins/types"

export function PluginTabScreen({tab}: {tab: PluginUITabInfo}) {
  return (
    <div className="bg-background h-full min-h-0 overflow-y-auto rounded-md border p-4 sm:p-6">
      <div className="mx-auto max-w-6xl">
        <PluginViewNode
          node={tab.view}
          onAction={(action, value) => { pluginRuntime.sendTabAction(tab.pluginId, tab.id, action, value ?? null) }}
        />
      </div>
    </div>
  )
}
