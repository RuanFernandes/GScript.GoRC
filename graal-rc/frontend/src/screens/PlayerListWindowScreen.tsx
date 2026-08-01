// PlayerListWindowScreen is the content rendered in the external "Players"
// window (opened via App.OpenPlayerList, URL "/#players"). It polls the shared
// backend service for the live player cache — same data the main window sees —
// renders a grouped avatar player list, and wires private messaging (single PM
// thread per player, Mass PM, and Admin Message), mirroring the reference C++
// client's TPlayerList. Inbound PMs arrive as rc:pm events on the uniform
// rc:evt channel and feed the per-player thread + unread badge.
import {useEffect, useMemo, useState} from "react"
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
import {useLanguage} from "@/hooks/useLanguage"
import {usePrivateMessages} from "@/hooks/usePrivateMessages"

export function PlayerListWindowScreen() {
  const {t} = useLanguage()
  const {players, loading} = usePlayers(rcService, true)
  const chat = useChatSettings()
  const [query, setQuery] = useState("")

  const {state: pmState, unreadById, markRead, recordOutgoing} = usePrivateMessages()
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

  useEffect(() => {
    const off = Events.On("rc:openPM", (e: {data: number}) => {
      const player = players.find((p) => p.id === Number(e.data))
      const conversation = pmState.conversations.find((c) => c.playerId === Number(e.data))
      if (player) openPM(player)
      else if (conversation) {
        setPmTarget({id: conversation.playerId, account: conversation.account, nick: conversation.nick || conversation.account})
        markRead(conversation.playerId)
      }
    })
    return off
  }, [players, pmState.conversations, markRead])

  const openPM = (player: Player) => {
    setPmTarget({id: player.id, account: player.account, nick: player.nick || player.account})
    markRead(player.id)
  }

  // Right-click admin actions: open the editor window for the row's (server-
  // supplied) account name.
  const editPlayer = (player: Player, kind: PlayerEditKind) => {
    const account = player.account
    if (!account) {
      toast.error(t("player.noAccount"))
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
      recordOutgoing(pmTarget.id, pmTarget.account, pmTarget.nick, message)
      if (pmTarget.account) rcService.appendPmLog(pmTarget.account, pmLogLine("out", message)).catch(() => {})
    } catch (err) {
      toast.error(t("player.pmFailed"), {description: err instanceof Error ? err.message : String(err)})
    }
  }

  const sendMassPM = async (message: string) => {
    const ids = players.map((p) => p.id)
    if (ids.length === 0) {
      toast.error(t("player.noPlayersMessage"))
      return
    }
    try {
      await rcService.sendMassPM(ids, message)
      toast.success(`Mass PM sent to ${ids.length} player${ids.length === 1 ? "" : "s"}`)
    } catch (err) {
      toast.error(t("player.massPmFailed"), {description: err instanceof Error ? err.message : String(err)})
    }
  }

  const sendAdminAll = async (message: string) => {
    try {
      await rcService.sendAdminMessageAll(message)
      toast.success(t("player.adminSent"))
    } catch (err) {
      toast.error(t("player.adminFailed"), {description: err instanceof Error ? err.message : String(err)})
    }
  }

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="border-b">
        <div className="flex items-center gap-2 px-4 py-2.5">
          <Users className="text-primary size-4" />
          <h1 className="text-base font-semibold">{t("player.title")}</h1>
          <span className="text-muted-foreground text-sm">({players.length})</span>
          {loading && <Loader2 className="text-muted-foreground size-4 animate-spin" />}
          <div className="ml-auto flex items-center gap-1.5">
            <Button variant="outline" size="sm" onClick={() => setMassPmOpen(true)} disabled={players.length === 0}>
              <Send className="size-4" />
              {t("player.massPm")}
            </Button>
            <Button variant="outline" size="sm" onClick={() => setAdminOpen(true)}>
              <Megaphone className="size-4" />
              {t("player.adminMessage")}
            </Button>
          </div>
        </div>
        <div className="px-4 pb-2.5">
          <div className="relative">
            <Search className="text-muted-foreground absolute left-2.5 top-1/2 size-4 -translate-y-1/2" />
            <Input
              className="h-8 pl-8 text-sm"
              placeholder={t("player.search")}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
        </div>
      </header>
      <ScrollArea className="min-h-0 flex-1 p-2">
        <PlayerTable players={filtered} loading={loading} unreadById={unreadById} onPM={openPM} onEdit={editPlayer} />
      </ScrollArea>

      <PmDialog
        target={pmTarget}
        lines={pmTarget ? (pmState.conversations.find((c) => c.playerId === pmTarget.id)?.lines ?? []).map((l) => ({dir: l.direction, text: l.text, ts: l.timestamp} as PmLine)) : []}
        onClose={() => setPmTarget(null)}
        onSend={sendPM}
      />
      <MessageComposeDialog
        open={massPmOpen}
        title={t("player.massPm")}
        recipientLabel={`all ${players.length} player${players.length === 1 ? "" : "s"}`}
        sendLabel={t("common.send")}
        onClose={() => setMassPmOpen(false)}
        onSend={sendMassPM}
      />
      <MessageComposeDialog
        open={adminOpen}
        title={t("player.adminMessage")}
        recipientLabel="all players"
        sendLabel={t("common.send")}
        singleLine
        onClose={() => setAdminOpen(false)}
        onSend={sendAdminAll}
      />
    </div>
  )
}
