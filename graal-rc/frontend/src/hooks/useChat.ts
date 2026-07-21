// useChat owns the chat surface for the active server session. It mirrors the
// reference C++ client (TRemoteFrame::appendChannelMessage) exactly:
//   - rc:message            -> server ("RC Chat") tab.
//   - rc:irc (channel,line) -> channel tab, created lazily on the first message
//     for that channel; never auto-removed (the reference doesn't destroy tabs
//     on part either). Empty channel -> server tab.
//   - rc:serverdata         -> nc_message renders as [NC]; other types gray.
// There is no join/left parsing: those are just regular lines rendered in the
// channel tab, which is what the official client does.
//
// Sending in a channel tab uses the server command:
//   /npc channelchat <channel> .<message>   (leading dot required).
import {useCallback, useEffect, useRef, useState} from "react"
import {toast} from "sonner"
import {Events} from "@wailsio/runtime"

import type {RcService} from "@/services/rcService"
import type {ChatMessage, ChatTab} from "@/types"

const SERVER_CHANNEL = ""
const MAX_LINES_PER_TAB = 1000

type Source = ChatMessage["source"]

function appendLine(tab: ChatTab, msg: ChatMessage): ChatTab {
  const messages = [...tab.messages, msg]
  if (messages.length > MAX_LINES_PER_TAB) {
    messages.splice(0, messages.length - MAX_LINES_PER_TAB)
  }
  return {...tab, messages}
}

export interface UseChatResult {
  tabs: ChatTab[]
  activeChannel: string
  setActiveChannel: (channel: string) => void
  send: (channel: string, text: string) => Promise<boolean>
  reorderTabs: (from: number, to: number) => void
}

export function useChat(service: RcService): UseChatResult {
  const idRef = useRef(0)
  const nextId = useCallback(() => ++idRef.current, [])
  const [tabs, setTabs] = useState<ChatTab[]>([
    {channel: SERVER_CHANNEL, label: "RC Chat", messages: []},
  ])
  const [activeChannel, setActiveChannel] = useState(SERVER_CHANNEL)
  // joined tracks which channels we're currently in (ref, not state, so it's
  // updated synchronously per-event and is immune to React batching). The
  // server burst sends Left then Joined per channel on login: Left is ignored
  // (not joined yet), Joined creates the tab, then the welcome lands.
  const joined = useRef<Set<string>>(new Set([SERVER_CHANNEL]))

  // push appends a line to a tab, creating the tab lazily for a new channel
  // (mirrors appendChannelMessage's lazy GtkWidget creation).
  const push = useCallback(
    (channel: string, text: string, source: Source) => {
      const target = channel || SERVER_CHANNEL
      const msg: ChatMessage = {id: nextId(), channel: target, text, source, ts: Date.now()}
      setTabs((prev) => {
        const idx = prev.findIndex((t) => t.channel === target)
        if (idx === -1) {
          return [...prev, {channel: target, label: target, messages: [msg]}]
        }
        const next = [...prev]
        next[idx] = appendLine(next[idx], msg)
        return next
      })
    },
    [nextId]
  )

  // ensureChannel creates an empty tab for a channel if it doesn't exist yet
  // (used on IRC join).
  const ensureChannel = useCallback((channel: string) => {
    const target = channel || SERVER_CHANNEL
    setTabs((prev) => (prev.some((t) => t.channel === target) ? prev : [...prev, {channel: target, label: target, messages: []}]))
  }, [])

  // removeChannel destroys a channel's tab (IRC part). If it was the active
  // tab, fall back to the server tab so the view isn't left empty.
  const removeChannel = useCallback((channel: string) => {
    const target = channel || SERVER_CHANNEL
    setTabs((prev) => prev.filter((t) => t.channel !== target))
    setActiveChannel((cur) => (cur === target ? SERVER_CHANNEL : cur))
  }, [])

  useEffect(() => {
    const offMessage = Events.On("rc:message", (e: {data: string}) => {
      const [text] = JSON.parse(e.data) as [string]
      push(SERVER_CHANNEL, text, "rc")
    })
    const offIrc = Events.On("rc:irc", (e: {data: string}) => {
      const [channel, line] = JSON.parse(e.data) as [string, string]
      const text = (line ?? "").trim()
      if (text.startsWith("* Joined")) {
        // Join only if not already joined (idempotent against bursts).
        if (!joined.current.has(channel)) {
          joined.current.add(channel)
          ensureChannel(channel)
        }
      } else if (text.startsWith("* Left")) {
        // Leave only if currently joined (ignores the login Left that precedes
        // the matching Joined).
        if (joined.current.has(channel)) {
          joined.current.delete(channel)
          removeChannel(channel)
        }
      } else {
        push(channel, text, "irc")
      }
    })
    const offData = Events.On("rc:serverdata", (e: {data: string}) => {
      const [dataType, content] = JSON.parse(e.data) as [string, string]
      if (dataType === "nc_message") {
        push(SERVER_CHANNEL, content, "nc")
      } else {
        push(SERVER_CHANNEL, `[${dataType}] ${content}`, "system")
      }
    })
    return () => {
      offMessage()
      offIrc()
      offData()
    }
  }, [push, ensureChannel, removeChannel])

  // clearChannel wipes one channel's rendered messages (frontend only).
  const clearChannel = useCallback((channel: string) => {
    const target = channel || SERVER_CHANNEL
    setTabs((prev) => prev.map((t) => (t.channel === target ? {...t, messages: []} : t)))
  }, [])

  const send = useCallback(
    async (channel: string, text: string): Promise<boolean> => {
      const trimmed = text.trim()
      if (!trimmed) return false
      // "/clear" is a client-only command: wipe the current channel's view
      // without sending anything to the server.
      if (trimmed === "/clear") {
        clearChannel(channel)
        return true
      }
      try {
        if (channel === SERVER_CHANNEL) {
          await service.execute(trimmed)
        } else {
          // IRC channel message via the server NPC command. Channel and message
          // are quoted (channel names can contain spaces); message keeps the
          // leading dot inside the quotes.
          await service.execute(`/npc channelchat "${channel}" ".${trimmed}"`)
        }
        return true
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        toast.error("Send failed", {description: message})
        return false
      }
    },
    [service, clearChannel]
  )

  // reorderTabs moves a tab (drag-and-drop reorder). The server tab stays in
  // place conceptually but can be repositioned too.
  const reorderTabs = useCallback((from: number, to: number) => {
    if (from === to) return
    setTabs((prev) => {
      if (from < 0 || from >= prev.length || to < 0 || to >= prev.length) return prev
      const next = [...prev]
      const [moved] = next.splice(from, 1)
      next.splice(to, 0, moved)
      return next
    })
  }, [])

  return {tabs, activeChannel, setActiveChannel, send, reorderTabs}
}
