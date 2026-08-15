import {useEffect, useMemo, useState} from "react"
import {MessageSquare} from "lucide-react"
import {toast} from "sonner"

import {PmConversation, type PmLine, type PmTarget} from "@/components/features/playerlist/PmConversation"
import {useChatSettings} from "@/hooks/useChatSettings"
import {useLanguage} from "@/hooks/useLanguage"
import {usePrivateMessages} from "@/hooks/usePrivateMessages"
import {rcService} from "@/services/rcService"
import type {Player} from "@/types"

function readPlayerID(): number | null {
  const hash = window.location.hash
  const queryStart = hash.indexOf("?")
  const params = new URLSearchParams(queryStart >= 0 ? hash.slice(queryStart + 1) : "")
  const value = Number(params.get("id"))
  return Number.isInteger(value) && value >= 0 ? value : null
}

function pmLogLine(direction: "in" | "out", text: string): string {
  const now = new Date()
  const hh = String(now.getHours()).padStart(2, "0")
  const mm = String(now.getMinutes()).padStart(2, "0")
  return `[${hh}:${mm}] ${direction === "out" ? "->" : "<-"} ${text}`
}

export function PmWindowScreen() {
  const {t} = useLanguage()
  const playerID = readPlayerID()
  const chat = useChatSettings()
  const {state, markRead, recordOutgoing} = usePrivateMessages()
  const [player, setPlayer] = useState<Player | null>(null)

  useEffect(() => {
    if (playerID === null) return
    let cancelled = false
    void rcService.getPlayers().then((players) => {
      if (!cancelled) setPlayer((players ?? []).find((candidate) => candidate.id === playerID) ?? null)
    }).catch(() => {})
    return () => { cancelled = true }
  }, [playerID])

  useEffect(() => {
    if (playerID !== null) markRead(playerID)
  }, [markRead, playerID])

  useEffect(() => {
    void rcService.setPmLogConfig(chat.settings.pmLog, chat.settings.pmLogDir).catch(() => {})
  }, [chat.settings.pmLog, chat.settings.pmLogDir])

  const conversation = state.conversations.find((candidate) => candidate.playerId === playerID)
  const target = useMemo<PmTarget | null>(() => {
    if (playerID === null) return null
    if (conversation) {
      return {
        id: playerID,
        account: conversation.account || player?.account || `#${playerID}`,
        nick: conversation.nick || player?.nick || "",
      }
    }
    return {
      id: playerID,
      account: player?.account || `#${playerID}`,
      nick: player?.nick || "",
    }
  }, [conversation, player, playerID])

  const lines = useMemo<PmLine[]>(
    () => (conversation?.lines ?? []).map((line) => ({dir: line.direction, text: line.text, ts: line.timestamp})),
    [conversation],
  )

  if (!target) {
    return (
      <div className="bg-background text-muted-foreground flex h-full items-center justify-center p-6 text-sm">
        {t("common.noDescription")}
      </div>
    )
  }

  const label = target.nick || target.account
  const description = target.nick && target.account && target.nick !== target.account
    ? `${target.nick} (${target.account})`
    : target.account

  const send = async (message: string) => {
    try {
      await rcService.sendPrivateMessage(target.id, message)
      recordOutgoing(target.id, target.account, target.nick, message)
      if (target.account) void rcService.appendPmLog(target.account, pmLogLine("out", message)).catch(() => {})
    } catch (error) {
      toast.error(t("player.pmFailed"), {description: error instanceof Error ? error.message : String(error)})
      throw error
    }
  }

  return (
    <div className="bg-background flex h-full min-h-0 flex-col">
      <header className="flex shrink-0 items-center gap-3 border-b px-4 py-3">
        <div className="bg-primary/15 text-primary flex size-8 shrink-0 items-center justify-center rounded-md">
          <MessageSquare className="size-4" />
        </div>
        <div className="min-w-0">
          <h2 className="truncate text-sm font-semibold">PM · {label}</h2>
          <p className="text-muted-foreground truncate text-xs">{description}</p>
        </div>
      </header>
      <PmConversation target={target} lines={lines} onSend={send} />
    </div>
  )
}
