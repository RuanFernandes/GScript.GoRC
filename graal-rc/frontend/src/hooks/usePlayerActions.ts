import {useCallback, useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"
import {toast} from "sonner"

import type {RcService} from "@/services/rcService"
import type {Player, SessionStatus} from "@/types"
import type {PlayerEditKind} from "@/components/features/playerlist/PlayerContextMenu"
import {useLanguage} from "@/hooks/useLanguage"

export interface UsePlayerActionsResult {
  canBanPlayers: boolean
  openPM: (player: Player) => void
  editPlayer: (player: Player, kind: PlayerEditKind) => void
}

export function usePlayerActions(service: RcService): UsePlayerActionsResult {
  const {t} = useLanguage()
  const [canBanPlayers, setCanBanPlayers] = useState(false)

  useEffect(() => {
    let cancelled = false
    const refreshPermissions = async () => {
      try {
        const value = await service.status()
        if (cancelled || !value || typeof value !== "object") return
        const status = value as SessionStatus
        setCanBanPlayers(Boolean(status.rightsReady && status.canBanPlayers))
      } catch {
        if (!cancelled) setCanBanPlayers(false)
      }
    }

    void refreshPermissions()
    const off = Events.On("rc:evt", (event: {data: string}) => {
      try {
        const payload = JSON.parse(event.data) as {name?: string}
        if (payload.name === "rc:scriptPermissionsChanged") void refreshPermissions()
      } catch {
        // Ignore unrelated or malformed event payloads.
      }
    })
    return () => {
      cancelled = true
      off()
    }
  }, [service])

  const openPM = useCallback((player: Player) => {
    void service.openPlayerListPM(player.id).catch((error) => {
      toast.error(t("player.actionFailed"), {description: error instanceof Error ? error.message : String(error)})
    })
  }, [service, t])

  const editPlayer = useCallback((player: Player, kind: PlayerEditKind) => {
    const account = player.account.trim()
    if (!account) {
      toast.error(t("player.noAccount"))
      return
    }
    if ((kind === "ban" || kind === "banhistory") && !canBanPlayers) {
      toast.error(t("player.banPlayersRightRequired"))
      return
    }

    const open = async () => {
      switch (kind) {
        case "rights":
          await service.openRightsWindow(account)
          break
        case "ban":
          await service.openBanWindow(account)
          break
        case "attrs":
          await service.openAttrsWindow(account)
          break
        case "comments":
          await service.openCommentsWindow(account)
          break
        case "banhistory":
          await service.openBanHistoryWindow(account)
          break
        case "staffactivity":
          await service.openStaffActivityWindow(account)
          break
      }
    }

    void open().catch((error) => {
      toast.error(t("player.actionFailed"), {description: error instanceof Error ? error.message : String(error)})
    })
  }, [canBanPlayers, service, t])

  return {canBanPlayers, openPM, editPlayer}
}
