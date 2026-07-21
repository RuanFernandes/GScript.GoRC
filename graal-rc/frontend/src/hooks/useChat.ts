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
  send: (channel: string, text: string) => Promise<boolean>
}

export function useChat(service: RcService): UseChatResult {
  const idRef = useRef(0)
  const nextId = useCallback(() => ++idRef.current, [])
  const [tabs, setTabs] = useState<ChatTab[]>([
    {channel: SERVER_CHANNEL, label: "RC Chat", messages: []},
  ])

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

  useEffect(() => {
    const offMessage = Events.On("rc:message", (e: {data: string}) => {
      const [text] = JSON.parse(e.data) as [string]
      push(SERVER_CHANNEL, text, "rc")
    })
    const offIrc = Events.On("rc:irc", (e: {data: string}) => {
      const [channel, line] = JSON.parse(e.data) as [string, string]
      console.log("[irc]", {channel, line})
      push(channel, line ?? "", "irc")
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
  }, [push])

  const send = useCallback(
    async (channel: string, text: string): Promise<boolean> => {
      const trimmed = text.trim()
      if (!trimmed) return false
      try {
        if (channel === SERVER_CHANNEL) {
          await service.execute(trimmed)
        } else {
          // IRC channel message via the server NPC command (leading dot required).
          await service.execute(`/npc channelchat ${channel} .${trimmed}`)
        }
        return true
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        toast.error("Send failed", {description: message})
        return false
      }
    },
    [service]
  )

  return {tabs, send}
}
