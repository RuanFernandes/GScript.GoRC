// useSync owns the Local Sync config + live status. Config persists in the Go
// backend (sync.json) — NOT localStorage — because each Wails v3 window is its
// own webview. The backend broadcasts rc:syncConfig (config) and rc:syncStatus
// (status) raw events so the Settings window and the Sync Review window stay in
// sync. Conflicts surface via rc:syncConflict.
import {useCallback, useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"
import {toast} from "sonner"

import {rcService} from "@/services/rcService"
import type {SyncConfig, SyncStatus, SyncReviewItem} from "@/types"

const EMPTY_STATUS: SyncStatus = {
  enabled: false,
  paused: false,
  ncDown: false,
  outputDirMissing: true,
  server: "",
  outputDir: "",
  lastSyncAt: 0,
  reviewCount: 0,
  items: [],
}

const DEFAULT_CONFIG: SyncConfig = {
  enabled: false,
  outputDir: "",
  pollingMinutes: 5,
  autoPushLocal: true,
  autoPullServer: true,
}

export interface UseSyncResult {
  config: SyncConfig
  status: SyncStatus
  progress: {done: number; total: number} | null
  loaded: boolean
  saveConfig: (patch: Partial<SyncConfig>) => void
  syncNow: () => Promise<void>
  resolveConflict: (kind: string, key: string, choice: "local" | "server") => Promise<void>
  pause: () => Promise<void>
  resume: () => Promise<void>
}

export function useSync(): UseSyncResult {
  const [config, setConfig] = useState<SyncConfig>(DEFAULT_CONFIG)
  const [status, setStatus] = useState<SyncStatus>(EMPTY_STATUS)
  const [progress, setProgress] = useState<{done: number; total: number} | null>(null)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    let cancelled = false
    Promise.all([rcService.getSyncConfig(), rcService.getSyncStatus()])
      .then(([cfg, st]) => {
        if (cancelled) return
        setConfig(cfg ?? DEFAULT_CONFIG)
        setStatus(st ?? EMPTY_STATUS)
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setLoaded(true)
      })

    const offCfg = Events.On("rc:syncConfig", (e: {data: string}) => {
      try {
        setConfig(JSON.parse(e.data) as SyncConfig)
      } catch {
        // ignore
      }
    })
    const offStatus = Events.On("rc:syncStatus", (e: {data: string}) => {
      try {
        const s = JSON.parse(e.data) as SyncStatus
        setStatus(s)
        // A finished reconcile updates lastSyncAt — clear the progress bar.
        if (s.lastSyncAt) setProgress(null)
      } catch {
        // ignore
      }
    })
    const offConflict = Events.On("rc:syncConflict", (e: {data: string}) => {
      let item: SyncReviewItem | undefined
      try {
        item = JSON.parse(e.data) as SyncReviewItem
      } catch {
        // ignore
      }
      if (item) {
        toast.info(`Sync conflict: ${item.name}`, {
          description: `Both sides changed — review needed.`,
          action: {
            label: "Review",
            onClick: () => rcService.openSyncReview(),
          },
        })
      }
    })
    const offProgress = Events.On("rc:syncProgress", (e: {data: string}) => {
      try {
        const p = JSON.parse(e.data) as {done: number; total: number}
        setProgress(p)
        if (p.total > 0 && p.done >= p.total) {
          // Clear shortly after completion so the bar can fade.
          setTimeout(() => setProgress(null), 800)
        }
      } catch {
        // ignore
      }
    })
    // A finished reconcile resets progress.
    return () => {
      cancelled = true
      offCfg()
      offStatus()
      offConflict()
      offProgress()
    }
  }, [])

  const saveConfig = useCallback(
    (patch: Partial<SyncConfig>) => {
      setConfig((prev) => {
        const next = {...prev, ...patch}
        rcService
          .setSyncConfig(
            next.enabled,
            next.outputDir,
            next.pollingMinutes,
            next.autoPushLocal,
            next.autoPullServer,
          )
          .catch((err) => toast.error("Failed to save sync config: " + String(err)))
        return next
      })
    },
    [],
  )

  const syncNow = useCallback(async () => {
    try {
      await rcService.syncNow()
      toast.success("Sync started")
    } catch (err) {
      toast.error("Sync failed: " + String(err))
    }
  }, [])

  const resolveConflict = useCallback(
    async (kind: string, key: string, choice: "local" | "server") => {
      try {
        await rcService.resolveConflict(kind, key, choice)
        toast.success("Resolved")
      } catch (err) {
        toast.error("Resolve failed: " + String(err))
      }
    },
    [],
  )

  const pause = useCallback(async () => {
    try {
      await rcService.pauseSync()
    } catch (err) {
      toast.error("Pause failed: " + String(err))
    }
  }, [])

  const resume = useCallback(async () => {
    try {
      await rcService.resumeSync()
    } catch (err) {
      toast.error("Resume failed: " + String(err))
    }
  }, [])

  return {config, status, progress, loaded, saveConfig, syncNow, resolveConflict, pause, resume}
}
