// useChat owns the chat surface for the active server session. It mirrors the
// reference C++ client (TRemoteFrame::appendChannelMessage). All grclib events
// arrive on a single uniform "rc:evt" carrying {seq,name,data}; this hook
// applies them through a seq-keyed reorder buffer (Wails v3 dispatches each
// Emit on its own goroutine, so without reordering the server's login chat
// burst would scramble). Dispatched event names:
//   - rc:message            -> server ("RC Chat") tab.
//   - rc:irc (channel,line) -> channel tab, created lazily on the first message
//     for that channel. Empty channel -> server tab.
//   - rc:serverdata         -> nc_message renders as [NC]; other types gray.
//
// Tab lifecycle for IRC channels is NOT derived from parsing the join/left
// text lines here. Go (connection.Service) owns the authoritative joined set
// and debounces parts (a PART waits channelLeaveCooldown before removing; a
// rejoin JOIN cancels it) since the server always sends PART before JOIN. It
// emits rc:channels snapshots; this hook reconciles its tabs against each
// snapshot.
//
// Sending in a channel tab uses the server command:
//   /npc channelchat <channel> .<message>   (leading dot required).
import {useCallback, useEffect, useRef, useState} from "react"
import {toast} from "sonner"
import {Events} from "@wailsio/runtime"

import type {RcService} from "@/services/rcService"
import type {ChatMessage, ChatTab, GsFunction} from "@/types"
import {loadGsFunctions, refreshGsFunctions, searchFunctions} from "@/lib/gscriptApi"
import {pluginRuntime} from "@/plugins/runtime"

const SERVER_CHANNEL = ""
const MAX_LINES_PER_TAB = 1000

// SERVERDATA_HIDDEN are serverdata dataTypes that are pure server-state pushes
// with no human-readable value for chat — they previously rendered as bare grey
// "[clearweapons] " / "[staffguilds] …" / "[statuslist] …" noise lines. They are
// still emitted by the backend (and logged there) but no longer dumped into RC
// chat. Add more here as they're identified.
const SERVERDATA_HIDDEN = new Set<string>(["clearweapons", "staffguilds", "statuslist"])

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

  // reconcileChannels mirrors the tabs against Go's authoritative joined-set
  // snapshot: keep the server tab plus any tab still joined, append tabs for
  // newly joined channels, drop tabs for channels no longer joined.
  const reconcileChannels = useCallback((channels: string[]) => {
    const want = new Set(channels)
    setTabs((prev) => {
      let next = prev.filter((t) => t.channel === SERVER_CHANNEL || want.has(t.channel))
      for (const ch of channels) {
        if (!next.some((t) => t.channel === ch)) {
          next = [...next, {channel: ch, label: ch, messages: []}]
        }
      }
      return next
    })
    setActiveChannel((cur) => (cur !== SERVER_CHANNEL && !want.has(cur) ? SERVER_CHANNEL : cur))
  }, [])

  useEffect(() => {
    // Every grclib event arrives as one uniform "rc:evt" carrying {seq,name,data}.
    // Wails v3 dispatches each Emit to the webview on its own goroutine, so
    // events can land out of order; a sliding-window reorder buffer keyed by seq
    // applies them strictly in sequence, restoring deterministic ordering (the
    // server's login chat burst otherwise scrambles).
    type Evt = {seq: number; name: string; data: unknown[]}
    let nextSeq = 1
    let lastAdvance = Date.now()
    const pending = new Map<number, Evt>()
    // Seqs we've already dispatched. Used to dedupe a genuine redelivery by the
    // IPC layer (same seq delivered twice) without dropping a late straggler we
    // fast-forwarded past — see deliver.
    const applied = new Set<number>()
    // If this listener subscribes after the server's initial burst has already
    // been emitted, the earliest seqs never arrive and nextSeq would stall
    // forever waiting for them. After this long with no forward progress we
    // assume the gap was missed (pre-subscription or dropped by the IPC layer)
    // and fast-forward. A straggler for the skipped seq still renders later.
    const stallMs = 300

    const dispatch = (m: Evt) => {
      switch (m.name) {
        case "rc:message": {
          const [text] = m.data as [string]
          push(SERVER_CHANNEL, text, "rc")
          break
        }
        case "rc:irc": {
          const [channel, line] = m.data as [string, string]
          const text = (line ?? "").trim()
          // Join/left markers drive tab lifecycle via the rc:channels snapshot
          // from Go; skip them here so they don't render as chat lines.
          if (text.startsWith("* Joined") || text.startsWith("* Left")) {
            break
          }
          push(channel, text, "irc")
          break
        }
        case "rc:channels": {
          // rc:channels is a [channels] 1-tuple whose sole element is the
          // joined-channel array (the emitter marshals each event's args as a
          // JSON array).
          const [channels] = m.data as [string[]]
          reconcileChannels(channels ?? [])
          break
        }
        case "rc:serverdata": {
          const [dataType, content] = m.data as [string, string]
          if (dataType === "nc_message") {
            push(SERVER_CHANNEL, content, "nc")
          } else if (!SERVERDATA_HIDDEN.has(dataType)) {
            push(SERVER_CHANNEL, `[${dataType}] ${content}`, "system")
          }
          break
        }
        default:
          break
      }
    }

    const drainPending = () => {
      let p = pending.get(nextSeq)
      while (p) {
        pending.delete(nextSeq)
        applied.add(nextSeq)
        dispatch(p)
        nextSeq++
        lastAdvance = Date.now()
        p = pending.get(nextSeq)
      }
      // `applied` only needs to cover stragglers with seq < nextSeq; drop older
      // entries so a long session doesn't leak memory.
      if (applied.size > 2000) {
        for (const s of applied) {
          if (s < nextSeq - 1000) applied.delete(s)
        }
      }
    }

    const deliver = (m: Evt) => {
      if (applied.has(m.seq)) return // genuine redelivery of an already-applied seq
      // Stalled on a gap that won't fill (missed pre-subscription / IPC drop):
      // jump to the oldest buffered event and resume from there.
      if (pending.size > 0 && Date.now() - lastAdvance > stallMs) {
        nextSeq = Math.min(...pending.keys())
      }
      if (m.seq < nextSeq) {
        // Late straggler for a seq we already skipped past via fast-forward.
        // The old behavior dropped these, which silently lost chat lines when
        // the IPC layer delivered an event >stallMs after the ones that followed
        // it. Render it instead — a mildly reordered line beats a missing one.
        applied.add(m.seq)
        dispatch(m)
        return
      }
      if (m.seq === nextSeq) {
        applied.add(nextSeq)
        dispatch(m)
        nextSeq++
        lastAdvance = Date.now()
        drainPending()
        return
      }
      // Out-of-order: buffer until the gap fills.
      pending.set(m.seq, m)
    }

    const off = Events.On("rc:evt", (e: {data: string}) => {
      deliver(JSON.parse(e.data) as Evt)
    })
    // Fast-forward check on a timer too, so a burst that goes quiet right after
    // a missed-start gap still resumes (deliver wouldn't be called again).
    const stallTimer = window.setInterval(() => {
      if (pending.size > 0 && Date.now() - lastAdvance > stallMs) {
        nextSeq = Math.min(...pending.keys())
        drainPending()
      }
    }, stallMs)
    return () => {
      off()
      window.clearInterval(stallTimer)
    }
  }, [push, reconcileChannels])

  // clearChannel wipes one channel's rendered messages (frontend only).
  const clearChannel = useCallback((channel: string) => {
    const target = channel || SERVER_CHANNEL
    setTabs((prev) => prev.map((t) => (t.channel === target ? {...t, messages: []} : t)))
  }, [])

  // pushScriptHelp appends a /scripthelp2 result line: a system-style line whose
  // scriptHelp payload the chat renders as a hoverable function list.
  const pushScriptHelp = useCallback(
    (channel: string, query: string, entries: GsFunction[]) => {
      const target = channel || SERVER_CHANNEL
      const msg: ChatMessage = {
        id: nextId(),
        channel: target,
        text: query,
        source: "system",
        ts: Date.now(),
        scriptHelp: entries,
      }
      setTabs((prev) => {
        const idx = prev.findIndex((t) => t.channel === target)
        if (idx === -1) return [...prev, {channel: target, label: target, messages: [msg]}]
        const next = [...prev]
        next[idx] = appendLine(next[idx], msg)
        return next
      })
    },
    [nextId]
  )

  // handleScriptHelp fetches (cached) the gscript.dev reference and pushes the
  // matching functions for the query.
  const handleScriptHelp = useCallback(
    async (channel: string, query: string) => {
      try {
        const all = await loadGsFunctions()
        pushScriptHelp(channel, query, searchFunctions(all, query))
      } catch (err) {
        push(channel, `scripthelp2 failed: ${err instanceof Error ? err.message : String(err)}`, "system")
      }
    },
    [pushScriptHelp, push]
  )

  const handleRefreshLsp = useCallback(
    async (channel: string) => {
      try {
        await service.refreshGraalScriptDocApi()
        const entries = await refreshGsFunctions()
        push(channel, `GraalScript doc API e contexto do servidor atualizados (${entries.length} definições).`, "system")
      } catch (err) {
        push(channel, `refreshlsp falhou: ${err instanceof Error ? err.message : String(err)}`, "system")
      }
    },
    [service, push]
  )

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
      if (trimmed.toLowerCase() === "/refreshlsp") {
        void handleRefreshLsp(channel)
        return true
      }
      // /openrights, /openaccess, /open {account} open a client-side editor
      // window. An omitted account targets the account resolved by the latest
      // openrights response for this RC session. Intercepted locally — never
      // sent to the server.
      const adminCmd = trimmed.match(/^\/(openrights|openaccess|opencomments|open)(?:\s+(.*))?$/i)
      if (adminCmd) {
        const arg = (adminCmd[2] ?? "").trim()
        const cmdName = adminCmd[1].toLowerCase()
        try {
          switch (cmdName) {
            case "openrights":
              await service.openRightsWindow(arg)
              break
            case "openaccess":
              await service.openBanWindow(arg)
              break
            case "opencomments":
              await service.openCommentsWindow(arg)
              break
            case "open":
              await service.openAttrsWindow(arg)
              break
          }
        } catch (err) {
          toast.error("Open failed", {description: err instanceof Error ? err.message : String(err)})
        }
        return true
      }
      // "/scripthelp2 <query>" is a client-only command: search the cached
      // gscript.dev function reference and render hoverable results. (/scripthelp
      // without the 2 is the server's own outdated command, left untouched.)
      if (trimmed === "/scripthelp2" || trimmed.startsWith("/scripthelp2 ")) {
        const query = trimmed.slice("/scripthelp2".length).trim()
        if (!query) {
          push(channel, "Usage: /scripthelp2 <name> — e.g. /scripthelp2 setani", "system")
        } else {
          void handleScriptHelp(channel, query)
        }
        return true
      }
      const pluginCommand = trimmed.match(/^\/([^\s]+)(?:\s+(.*))?$/)
      if (pluginCommand) {
        const args = pluginCommand[2]?.trim() ? pluginCommand[2].trim().split(/\s+/) : []
        if (pluginRuntime.executeCommand(pluginCommand[1], args)) return true
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
    [service, clearChannel, handleScriptHelp, handleRefreshLsp]
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
