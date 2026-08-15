// useScriptLists fetches the weapon/class/npc caches and re-fetches whenever the
// backend emits a cache-changed or permission-changed event. The backend owns
// the permission-aware filtering so the same rules are used by the Sync engine
// and the Script Manager.
import {useCallback, useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"

import type {RcService} from "@/services/rcService"
import type {Class, NPC, Weapon} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

type Evt = {seq: number; name: string; data: unknown[]}

export interface UseScriptListsResult {
  weapons: Weapon[]
  classes: Class[]
  npcs: NPC[]
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
}

export function useScriptLists(service: RcService, onlyReadable: boolean): UseScriptListsResult {
  const {t} = useLanguage()
  const [weapons, setWeapons] = useState<Weapon[]>([])
  const [classes, setClasses] = useState<Class[]>([])
  const [npcs, setNPCs] = useState<NPC[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const lists = await service.getScriptLists(onlyReadable)
      if (!lists) throw new Error(t("scripts.noLists"))
      // Keep malformed entries for the manager's disabled fallback row. The
      // backend already filters them before any NC request; retaining them
      // here makes a stale/native cache entry visible without making it
      // actionable or rendering a blank glyph.
      setWeapons(lists.weapons ?? [])
      setClasses(lists.classes ?? [])
      setNPCs(lists.npcs ?? [])
    } catch (err) {
      setWeapons([])
      setClasses([])
      setNPCs([])
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setLoading(false)
    }
  }, [onlyReadable, service, t])

  useEffect(() => {
    void refresh()
    // Re-fetch on any cache-changed event. Parsing rc:evt (same uniform envelope
    // the chat hook consumes) keeps a single subscription channel.
    const off = Events.On("rc:evt", (e: {data: string}) => {
      try {
        const m = JSON.parse(e.data) as Evt
        if (
          m.name === "rc:weaponsChanged" ||
          m.name === "rc:classesChanged" ||
          m.name === "rc:npcsChanged" ||
          m.name === "rc:scriptPermissionsChanged"
        ) {
          void refresh()
        }
      } catch {
        // ignore malformed events
      }
    })
    return () => {
      off()
    }
  }, [refresh])

  return {weapons, classes, npcs, loading, error, refresh}
}
