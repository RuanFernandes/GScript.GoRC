import {useEffect, useMemo, useState} from "react"

import {
  applyCompletion,
  completionItems,
  completionKey,
  defaultSelection,
  keepsOpenAfterAccept,
  type CompletionItem,
} from "@/lib/chatCompletion"
import type {Player} from "@/types"
import {pluginRuntime} from "@/plugins/runtime"

// useChatAutocomplete holds the state of the chat input's suggestion list. The
// list only opens from typing (or Tab / Ctrl+Space), never from text that was
// set by history recall or a macro, so the arrows keep walking the history.
export function useChatAutocomplete(players: Player[], text: string, setText: (value: string) => void) {
  const [pluginCommands, setPluginCommands] = useState<string[]>(() => pluginRuntime.getCommands().map(command => command.id))
  const [open, setOpen] = useState(false)
  // The row moved to with the keyboard, by value so a playerlist update does not
  // shift the selection. null means the default row.
  const [picked, setPicked] = useState<string | null>(null)

  useEffect(() => {
    let disposed = false
    const update = () => setPluginCommands([...new Set(pluginRuntime.getCommands().map(command => command.id))])
    update()
    window.addEventListener("gorc:plugin-commands", update)
    // The plugin manager can be opened in another Wails window after the main
    // RC has already started. Refreshing here makes the first chat render
    // authoritative even when that window's event was missed.
    void pluginRuntime.refresh().then(() => {
      if (!disposed) update()
    }).catch(() => {})
    return () => {
      disposed = true
      window.removeEventListener("gorc:plugin-commands", update)
    }
  }, [])

  const items = useMemo(() => completionItems(text, players, pluginCommands), [text, players, pluginCommands])
  const pickedIndex = picked === null ? -1 : items.findIndex((item) => item.value === picked)
  const selected = pickedIndex >= 0 ? pickedIndex : defaultSelection(text, items)

  const show = (next: boolean) => {
    setOpen(next)
    setPicked(null)
  }

  const accept = (item: CompletionItem) => {
    setText(applyCompletion(text, item))
    show(keepsOpenAfterAccept(item))
  }

  // handleKeyDown returns true when the list consumed the key.
  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>): boolean => {
    const action = completionKey(
      {
        key: e.key,
        shift: e.shiftKey,
        ctrl: e.ctrlKey,
        alt: e.altKey,
        meta: e.metaKey,
        composing: e.nativeEvent.isComposing,
      },
      {open, text, items, selected},
    )
    if (action.type === "pass") {
      if (action.close) show(false)
      return false
    }
    e.preventDefault()
    e.stopPropagation()
    if (action.type === "move") setPicked(items[action.index].value)
    else if (action.type === "accept") accept(items[action.index])
    else show(action.type === "open")
    return true
  }

  return {
    visible: open && items.length > 0,
    items,
    selected,
    accept,
    handleKeyDown,
    onEdit: () => show(true),
    close: () => show(false),
  }
}
