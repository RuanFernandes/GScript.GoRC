// useScriptLists fetches the weapon/class/npc caches and re-fetches whenever the
// backend emits a cache-changed event (rc:weaponsChanged / rc:classesChanged /
// rc:npcsChanged), which fire from grclib's add/delete push packets. The lists
// are read directly from the server-maintained caches, so no reorder buffer is
// needed here (a transient duplicate fetch is harmless).
import {useCallback, useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"

import type {RcService} from "@/services/rcService"
import type {Class, NPC, Weapon} from "@/types"
import {scriptCompare} from "@/lib/scriptSort"

type Evt = {seq: number; name: string; data: unknown[]}

export interface UseScriptListsResult {
  weapons: Weapon[]
  classes: Class[]
  npcs: NPC[]
  loading: boolean
  refresh: () => Promise<void>
}

export function useScriptLists(service: RcService): UseScriptListsResult {
  const [weapons, setWeapons] = useState<Weapon[]>([])
  const [classes, setClasses] = useState<Class[]>([])
  const [npcs, setNPCs] = useState<NPC[]>([])
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    const [w, c, n] = await Promise.all([
      service.getWeapons().catch(() => null),
      service.getClasses().catch(() => null),
      service.getNPCs().catch(() => null),
    ])
    setWeapons((w ?? []).slice().sort((a, b) => scriptCompare(a.name, b.name)))
    setClasses((c ?? []).slice().sort((a, b) => scriptCompare(a.name, b.name)))
    setNPCs((n ?? []).slice().sort((a, b) => scriptCompare(a.name, b.name)))
    setLoading(false)
  }, [service])

  useEffect(() => {
    refresh()
    // Re-fetch on any cache-changed event. Parsing rc:evt (same uniform envelope
    // the chat hook consumes) keeps a single subscription channel.
    const off = Events.On("rc:evt", (e: {data: string}) => {
      try {
        const m = JSON.parse(e.data) as Evt
        if (
          m.name === "rc:weaponsChanged" ||
          m.name === "rc:classesChanged" ||
          m.name === "rc:npcsChanged"
        ) {
          refresh()
        }
      } catch {
        // ignore malformed events
      }
    })
    return () => {
      off()
    }
  }, [refresh])

  return {weapons, classes, npcs, loading, refresh}
}
