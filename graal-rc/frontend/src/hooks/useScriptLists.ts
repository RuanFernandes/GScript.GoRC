// useScriptLists fetches the weapon/class/npc caches and re-fetches whenever the
// backend emits a cache-changed or permission-changed event. The backend owns
// the permission-aware filtering so the same rules are used by the Sync engine
// and the Script Manager.
import {useCallback, useEffect, useRef, useState} from "react"
import {Events} from "@wailsio/runtime"

import type {RcService} from "@/services/rcService"
import type {Class, NPC, Weapon} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

type Evt = {seq: number; name: string; data: unknown[]}

const SCRIPT_LIST_REFRESH_DEBOUNCE_MS = 200

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

  // There can be two consumers of this hook at once (the main RC screen and
  // the Script Manager). Events can also arrive in bursts while the NC cache
  // is being rebuilt. Keep one request in flight per webview and schedule at
  // most one follow-up refresh for the whole burst.
  const refreshInFlightRef = useRef<Promise<void> | null>(null)
  const refreshPendingRef = useRef(false)
  const refreshTimerRef = useRef<number | null>(null)
  const mountedRef = useRef(false)
  const scheduleRefreshRef = useRef<() => void>(() => {})
  const currentContextRef = useRef({service, onlyReadable})
  currentContextRef.current = {service, onlyReadable}

  const refresh = useCallback(() => {
    if (refreshTimerRef.current !== null) {
      window.clearTimeout(refreshTimerRef.current)
      refreshTimerRef.current = null
    }
    if (refreshInFlightRef.current) {
      refreshPendingRef.current = true
      return refreshInFlightRef.current
    }

    const requestService = service
    const requestOnlyReadable = onlyReadable
    const isCurrentContext = () =>
      mountedRef.current &&
      currentContextRef.current.service === requestService &&
      currentContextRef.current.onlyReadable === requestOnlyReadable

    const request = (async () => {
      if (!mountedRef.current) return
      setLoading(true)
      setError(null)
      try {
        const lists = await requestService.getScriptLists(requestOnlyReadable)
        if (!isCurrentContext()) return
        if (!lists) throw new Error(t("scripts.noLists"))
        // Keep malformed entries for the manager's disabled fallback row. The
        // backend already filters them before any NC request; retaining them
        // here makes a stale/native cache entry visible without making it
        // actionable or rendering a blank glyph.
        setWeapons(lists.weapons ?? [])
        setClasses(lists.classes ?? [])
        setNPCs(lists.npcs ?? [])
      } catch (err) {
        if (!isCurrentContext()) return
        setWeapons([])
        setClasses([])
        setNPCs([])
        setError(err instanceof Error ? err.message : String(err))
      } finally {
        if (isCurrentContext()) setLoading(false)
      }
    })()

    refreshInFlightRef.current = request
    const finishRequest = () => {
      if (refreshInFlightRef.current !== request) return
      refreshInFlightRef.current = null
      if (!refreshPendingRef.current || !mountedRef.current) {
        refreshPendingRef.current = false
        return
      }
      refreshPendingRef.current = false
      scheduleRefreshRef.current()
    }
    void request.then(finishRequest, finishRequest)
    return request
  }, [onlyReadable, service, t])

  const scheduleRefresh = useCallback(() => {
    if (!mountedRef.current || refreshTimerRef.current !== null) return
    refreshTimerRef.current = window.setTimeout(() => {
      refreshTimerRef.current = null
      void refresh()
    }, SCRIPT_LIST_REFRESH_DEBOUNCE_MS)
  }, [refresh])
  scheduleRefreshRef.current = scheduleRefresh

  useEffect(() => {
    mountedRef.current = true
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
          scheduleRefresh()
        }
      } catch {
        // ignore malformed events
      }
    })
    let lastSyncGeneration = 0
    const offSync = Events.On("rc:syncStatus", (e: {data: string}) => {
      try {
        const status = JSON.parse(e.data) as {syncGeneration?: number; initialSync?: boolean}
        const generation = status.syncGeneration ?? 0
        if (status.initialSync || generation <= lastSyncGeneration || generation <= 0) return
        lastSyncGeneration = generation
        scheduleRefresh()
      } catch {
        // ignore malformed events
      }
    })
    return () => {
      mountedRef.current = false
      refreshPendingRef.current = false
      if (refreshTimerRef.current !== null) {
        window.clearTimeout(refreshTimerRef.current)
        refreshTimerRef.current = null
      }
      off()
      offSync()
    }
  }, [refresh, scheduleRefresh])

  return {weapons, classes, npcs, loading, error, refresh}
}
