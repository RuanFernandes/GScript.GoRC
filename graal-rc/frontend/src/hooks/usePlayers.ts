// usePlayers polls rc_get_players for the active server while `active` is true.
// The reference client polls the player cache on its pump instead of relying on
// join/left callbacks, so we mirror that with a 1s interval scoped to when the
// player panel is open.
import {useEffect, useState} from "react"

import type {RcService} from "@/services/rcService"
import type {Player} from "@/types"

const POLL_MS = 1000

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
    setLoading(true)

    const tick = async () => {
      try {
        const list = (await service.getPlayers()) ?? []
        if (!cancelled) setPlayers(list)
      } catch {
        // Surface nothing on every tick; transient failures are common during
        // disconnect. The chat layer handles fatal errors.
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    tick()
    const handle = window.setInterval(tick, POLL_MS)
    return () => {
      cancelled = true
      window.clearInterval(handle)
    }
  }, [service, active])

  return {players, loading}
}
