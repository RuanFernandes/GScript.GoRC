// usePlayers polls rc_get_players for the active server while `active` is true.
// The reference client polls the player cache on its pump instead of relying on
// join/left callbacks, so we mirror that with a 1s cadence scoped to when the
// player panel is open. Requests are serialized because the native bridge also
// serializes DLL calls; an interval would otherwise build a backlog when one
// native snapshot takes longer than the polling period.
import {useEffect, useState} from "react"

import type {RcService} from "@/services/rcService"
import type {Player} from "@/types"

const POLL_MS = 1000

function playersEqual(left: Player[] | null, right: Player[]): boolean {
  if (left === null || left.length !== right.length) return false
  for (let index = 0; index < right.length; index++) {
    const previous = left[index]
    const current = right[index]
    if (
      previous.account !== current.account ||
      previous.id !== current.id ||
      previous.nick !== current.nick ||
      previous.level !== current.level ||
      previous.communityName !== current.communityName
    ) {
      return false
    }
  }
  return true
}

export interface UsePlayersResult {
  players: Player[]
  loading: boolean
}

export function usePlayers(service: RcService, active: boolean): UsePlayersResult {
  const [players, setPlayers] = useState<Player[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!active) return
    let cancelled = false
    let running = false
    let timer: number | null = null
    let previousPlayers: Player[] | null = null
    setLoading(true)

    const tick = async () => {
      if (cancelled || running || document.visibilityState === "hidden") return
      running = true
      try {
        const list = (await service.getPlayers()) ?? []
        if (!cancelled && !playersEqual(previousPlayers, list)) {
          previousPlayers = list
          setPlayers(list)
        }
      } catch {
        // Surface nothing on every tick; transient failures are common during
        // disconnect. The chat layer handles fatal errors.
      } finally {
        running = false
        if (!cancelled) {
          setLoading(false)
          schedule()
        }
      }
    }

    const schedule = (delay = POLL_MS) => {
      if (cancelled || document.visibilityState === "hidden") return
      if (timer !== null) window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        timer = null
        void tick()
      }, delay)
    }

    const onVisibilityChange = () => {
      if (document.visibilityState !== "visible" || cancelled || running) return
      if (timer !== null) {
        window.clearTimeout(timer)
        timer = null
      }
      if (previousPlayers === null) setLoading(true)
      void tick()
    }

    document.addEventListener("visibilitychange", onVisibilityChange)
    if (document.visibilityState === "visible") void tick()
    return () => {
      cancelled = true
      if (timer !== null) window.clearTimeout(timer)
      document.removeEventListener("visibilitychange", onVisibilityChange)
    }
  }, [service, active])

  return {players, loading}
}
