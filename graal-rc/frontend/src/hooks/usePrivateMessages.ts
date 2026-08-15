import {useCallback, useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"

import {rcService, type RcService} from "@/services/rcService"
import type {PMState} from "@/types"
import {normalizePrivateMessageText} from "@/lib/privateMessage"

const empty: PMState = {conversations: [], unreadTotal: 0}

function normalizeState(state: PMState): PMState {
  return {
    conversations: (state.conversations ?? []).map((conversation) => ({
      ...conversation,
      lines: (conversation.lines ?? []).map((line) => ({
        ...line,
        text: normalizePrivateMessageText(line.text ?? ""),
      })),
    })),
    unreadTotal: state.unreadTotal ?? 0,
  }
}

export function usePrivateMessages(service: RcService = rcService) {
  const [state, setState] = useState<PMState>(empty)

  useEffect(() => {
    let alive = true
    service.getPMState().then((next) => { if (alive && next) setState(normalizeState(next)) }).catch(() => {})
    const off = Events.On("rc:pmState", (event: {data: string}) => {
      try {
        const next = JSON.parse(event.data) as PMState
        if (alive) setState(normalizeState(next))
      } catch { /* ignore malformed event */ }
    })
    return () => { alive = false; off() }
  }, [service])

  const markRead = useCallback((playerID: number) => service.markPMRead(playerID).catch(() => {}), [service])
  const recordOutgoing = useCallback(
    (playerID: number, account: string, nick: string, message: string) =>
      service.recordOutgoingPM(playerID, account, nick, message).catch(() => {}),
    [service],
  )

  const unreadById: Record<number, number> = {}
  for (const conversation of state.conversations) unreadById[conversation.playerId] = conversation.unread
  return {state, unreadById, markRead, recordOutgoing}
}
