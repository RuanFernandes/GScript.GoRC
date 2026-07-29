// PlayerListWindowScreen is the content rendered in the external "Players"
// window (opened via App.OpenPlayerList, URL "/#players"). It polls the shared
// backend service for the live player cache — same data the main window sees —
// renders a grouped avatar player list, and wires private messaging (single PM
// thread per player, Mass PM, and Admin Message), mirroring the reference C++
// client's TPlayerList. Inbound PMs arrive as rc:pm events on the uniform
// rc:evt channel and feed the per-player thread + unread badge.
import {useEffect, useMemo, useRef, useState} from "react"
import {Events} from "@wailsio/runtime"
import {Loader2, Megaphone, Search, Send, Users} from "lucide-react"
import {toast} from "sonner"

import {MessageComposeDialog} from "@/components/features/playerlist/MessageComposeDialog"
import {PmDialog, type PmLine, type PmTarget} from "@/components/features/playerlist/PmDialog"
import {PlayerTable, type PlayerEditKind} from "@/components/features/playerlist/PlayerTable"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {usePlayers} from "@/hooks/usePlayers"
import {useChatSettings} from "@/hooks/useChatSettings"
import {rcService} from "@/services/rcService"
import type {Player} from "@/types"

type Evt = {seq: number; name: string; data: unknown[]}

export function PlayerListWindowScreen() {
  const {players, loading} = usePlayers(rcService, true)
  const chat = useChatSettings()
  const [query, setQuery] = useState("")

  // pmThreads holds the in-memory conversation per player id.
  const [pmThreads, setPmThreads] = useState<Record<number, PmLine[]>>({})
  // unread counts inbound lines the user has not yet read (PM dialog closed).
  const [unread, setUnread] = useState<Record<number, number>>({})
  const [pmTarget, setPmTarget] = useState<PmTarget | null>(null)
  const [massPmOpen, setMassPmOpen] = useState(false)
  const [adminOpen, setAdminOpen] = useState(false)

  // Push PM-log config to the backend (same App process as the main window, but
  // this window issues the AppendPmLog calls, so ensure the config is set).
  useEffect(() => {
    rcService.setPmLogConfig(chat.settings.pmLog, chat.settings.pmLogDir).catch(() => {})
  }, [chat.settings.pmLog, chat.settings.pmLogDir])

  // pmLogLine builds a timestamped log line for a PM direction.
  const pmLogLine = (dir: "in" | "out", text: string) => {
    const now = new Date()
    const hh = String(now.getHours()).padStart(2, "0")
    const mm = String(now.getMinutes()).padStart(2, "0")
    return `[${hh}:${mm}] ${dir === "out" ? "->" : "<-"} ${text}`
  }

  // Refs so the event listener (bound once) can read fresh state without
  // re-subscribing each render.
  const openIdRef = useRef<number | null>(null)
  openIdRef.current = pmTarget?.id ?? null

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return players
    return players.filter(
      (p) =>
        p.account.toLowerCase().includes(q) ||
        (p.nick || "").toLowerCase().includes(q) ||
        String(p.id).includes(q)
    )
  }, [players, query])

  // Subscribe to inbound PMs on the uniform rc:evt channel. PMs are low-rate so
  // the seq reorder buffer the chat hook uses is unnecessary here.
  useEffect(() => {
    const off = Events.On("rc:evt", (e: {data: string}) => {
      const evt = JSON.parse(e.data) as Evt
      if (evt.name !== "rc:pm") return
      const [id, account, nick, message] = evt.data as [number, string, string, string]
      const text = (message ?? "").trim()
      if (!text) return
      const line: PmLine = {dir: "in", text, ts: Date.now()}
      setPmThreads((prev) => ({...prev, [id]: [...(prev[id] ?? []), line]}))
      if (account) rcService.appendPmLog(account, pmLogLine("in", text)).catch(() => {})
      // If this dialog is open, the line is seen immediately; else bump unread.
      if (openIdRef.current === id) return
      setUnread((prev) => ({...prev, [id]: (prev[id] ?? 0) + 1}))
      const who = nick || account
      toast(`PM from ${who}`, {description: text})
    })
    return () => {
      off()
    }
  }, [])

  const openPM = (player: Player) => {
    setPmTarget({id: player.id, account: player.account, nick: player.nick || player.account})
    setUnread((prev) => (prev[player.id] ? {...prev, [player.id]: 0} : prev))
  }

  // Right-click admin actions: open the editor window for the row's (server-
  // supplied) account name.
  const editPlayer = (player: Player, kind: PlayerEditKind) => {
    const account = player.account
    if (!account) {
      toast.error("This player has no account name")
      return
    }
    switch (kind) {
      case "rights":
        void rcService.openRightsWindow(account)
        break
      case "ban":
        void rcService.openBanWindow(account)
        break
      case "attrs":
        void rcService.openAttrsWindow(account)
        break
      case "comments":
        void rcService.openCommentsWindow(account)
        break
      case "banhistory":
        void rcService.openBanHistoryWindow(account)
        break
      case "staffactivity":
        void rcService.openStaffActivityWindow(account)
        break
    }
  }

  const sendPM = async (message: string) => {
    if (!pmTarget) return
    try {
      await rcService.sendPrivateMessage(pmTarget.id, message)
      setPmThreads((prev) => ({
        ...prev,
        [pmTarget.id]: [...(prev[pmTarget.id] ?? []), {dir: "out", text: message, ts: Date.now()}],
      }))
      if (pmTarget.account) rcService.appendPmLog(pmTarget.account, pmLogLine("out", message)).catch(() => {})
    } catch (err) {
      toast.error("PM failed", {description: err instanceof Error ? err.message : String(err)})
    }
  }

  const sendMassPM = async (message: string) => {
    const ids = players.map((p) => p.id)
    if (ids.length === 0) {
      toast.error("No players to message")
      return
    }
    try {
      await rcService.sendMassPM(ids, message)
      toast.success(`Mass PM sent to ${ids.length} player${ids.length === 1 ? "" : "s"}`)
    } catch (err) {
      toast.error("Mass PM failed", {description: err instanceof Error ? err.message : String(err)})
    }
  }

  const sendAdminAll = async (message: string) => {
    try {
      await rcService.sendAdminMessageAll(message)
      toast.success("Admin message sent")
    } catch (err) {
      toast.error("Admin message failed", {description: err instanceof Error ? err.message : String(err)})
    }
  }

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="border-b">
        <div className="flex items-center gap-2 px-4 py-2.5">
          <Users className="text-primary size-4" />
          <h1 className="text-base font-semibold">Players</h1>
          <span className="text-muted-foreground text-sm">({players.length})</span>
          {loading && <Loader2 className="text-muted-foreground size-4 animate-spin" />}
          <div className="ml-auto flex items-center gap-1.5">
            <Button variant="outline" size="sm" onClick={() => setMassPmOpen(true)} disabled={players.length === 0}>
              <Send className="size-4" />
              Mass PM
            </Button>
            <Button variant="outline" size="sm" onClick={() => setAdminOpen(true)}>
              <Megaphone className="size-4" />
              Admin Msg
            </Button>
          </div>
        </div>
        <div className="px-4 pb-2.5">
          <div className="relative">
            <Search className="text-muted-foreground absolute left-2.5 top-1/2 size-4 -translate-y-1/2" />
            <Input
              className="h-8 pl-8 text-sm"
              placeholder="Search nick, account, or id…"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
        </div>
      </header>
      <ScrollArea className="min-h-0 flex-1 p-2">
        <PlayerTable players={filtered} loading={loading} unreadById={unread} onPM={openPM} onEdit={editPlayer} />
      </ScrollArea>

      <PmDialog
        target={pmTarget}
        lines={pmTarget ? pmThreads[pmTarget.id] ?? [] : []}
        onClose={() => setPmTarget(null)}
        onSend={sendPM}
      />
      <MessageComposeDialog
        open={massPmOpen}
        title="Mass PM"
        recipientLabel={`all ${players.length} player${players.length === 1 ? "" : "s"}`}
        sendLabel="Send to all"
        onClose={() => setMassPmOpen(false)}
        onSend={sendMassPM}
      />
      <MessageComposeDialog
        open={adminOpen}
        title="Admin Message"
        recipientLabel="all players"
        sendLabel="Broadcast"
        singleLine
        onClose={() => setAdminOpen(false)}
        onSend={sendAdminAll}
      />
    </div>
  )
}
