// PlayerListWindowScreen is the content rendered in the external "Players"
// window (opened via App.OpenPlayerList, URL "/#players"). It polls the shared
// backend service for the live player cache — same data the main window sees —
// renders a grouped avatar player list, and wires private-message window
// actions, Mass PM, and Admin Message, mirroring the reference C++ client's
// TPlayerList. Inbound PMs arrive through the shared backend state and keep
// their unread badge here while each conversation lives in its own window.
import {useEffect, useMemo, useState} from "react"
import {Loader2, Megaphone, Search, Send, Users} from "lucide-react"
import {toast} from "sonner"

import {MessageComposeDialog} from "@/components/features/playerlist/MessageComposeDialog"
import {PlayerInspector} from "@/components/features/playerlist/PlayerInspector"
import {PlayerTable, type PlayerEditKind} from "@/components/features/playerlist/PlayerTable"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {usePlayers} from "@/hooks/usePlayers"
import {usePlayerActions} from "@/hooks/usePlayerActions"
import {rcService} from "@/services/rcService"
import type {Player} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"
import {usePrivateMessages} from "@/hooks/usePrivateMessages"
import {containsUnsafePrivateMessageMarkup} from "@/lib/privateMessage"

export function PlayerListWindowScreen() {
  const {t} = useLanguage()
  const {players, loading} = usePlayers(rcService, true)
  const {canBanPlayers, openPM, editPlayer} = usePlayerActions(rcService)
  const [query, setQuery] = useState("")
  const [selectedPlayerId, setSelectedPlayerId] = useState<number | null>(null)
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const {unreadById} = usePrivateMessages()
  const [massPmOpen, setMassPmOpen] = useState(false)
  const [adminOpen, setAdminOpen] = useState(false)

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return players
    return players.filter(
      (p) =>
        p.account.toLowerCase().includes(q) ||
        (p.nick || "").toLowerCase().includes(q) ||
        (p.communityName || "").toLowerCase().includes(q) ||
        String(p.id).includes(q)
    )
  }, [players, query])

  const selectedPlayer = players.find((player) => player.id === selectedPlayerId) ?? null

  useEffect(() => {
    const online = new Set(players.map((player) => player.id))
    setSelectedIds((current) => {
      const next = new Set([...current].filter((id) => online.has(id)))
      return next.size === current.size ? current : next
    })
    if (selectedPlayerId !== null && !online.has(selectedPlayerId)) setSelectedPlayerId(null)
  }, [players, selectedPlayerId])

  const toggleSelection = (player: Player) => {
    setSelectedIds((current) => {
      const next = new Set(current)
      if (next.has(player.id)) next.delete(player.id)
      else next.add(player.id)
      return next
    })
  }

  const clearSelection = () => setSelectedIds(new Set())
  const selectedRecipients = selectedIds.size > 0 ? players.filter((player) => selectedIds.has(player.id)) : players
  const selectionLabel = selectedIds.size > 0
    ? t("player.selectedCount", {count: selectedIds.size, suffix: selectedIds.size === 1 ? "" : "s"})
    : t("player.allPlayers")

  const sendMassPM = async (message: string) => {
    const ids = selectedRecipients.map((p) => p.id)
    if (ids.length === 0) {
      toast.error(t("player.noPlayersMessage"))
      return
    }
    if (containsUnsafePrivateMessageMarkup(message)) {
      toast.error(t("player.pmUnsafeContent"))
      return
    }
    try {
      await rcService.sendMassPM(ids, message)
      toast.success(t("player.massPmSent", {count: ids.length, suffix: ids.length === 1 ? "" : "s"}))
      clearSelection()
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
            {selectedIds.size > 0 && <Button variant="ghost" size="sm" onClick={clearSelection}>{t("player.clearSelection")}</Button>}
            <Button variant="outline" size="sm" onClick={() => setMassPmOpen(true)} disabled={players.length === 0}>
              <Send className="size-4" />
              {t("player.massPm")} {selectedIds.size > 0 ? `(${selectedIds.size})` : ""}
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
      <div className="flex min-h-0 flex-1 flex-col gap-2 p-2 lg:flex-row">
        <ScrollArea className="min-h-0 flex-1">
          <PlayerTable
            players={filtered}
            loading={loading}
            emptyMessage={query.trim() ? t("player.noResults") : undefined}
            unreadById={unreadById}
            canBanPlayers={canBanPlayers}
            selectedIds={selectedIds}
            onSelect={(player) => setSelectedPlayerId(player.id)}
            onToggleSelection={toggleSelection}
            onPM={openPM}
            onEdit={editPlayer}
          />
        </ScrollArea>
        {selectedPlayer && (
          <PlayerInspector player={selectedPlayer} canBanPlayers={canBanPlayers} onPM={openPM} onEdit={editPlayer} onClose={() => setSelectedPlayerId(null)} />
        )}
      </div>

      <MessageComposeDialog
        open={massPmOpen}
        title={t("player.massPm")}
        recipientLabel={selectionLabel}
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
