// useCodingSettings owns the Monaco editor appearance (theme, font family, font
// size). Persisted in the Go backend (coding.json) — NOT localStorage — because
// each Wails v3 window is its own webview and does not reliably share
// localStorage. The backend broadcasts an rc:codingSettings event on change so
// every open editor window updates live.
import {useCallback, useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"

import {rcService} from "@/services/rcService"
import type {CodingSettings} from "@/types"

export const DEFAULT_CODING_SETTINGS: CodingSettings = {
  theme: "vs-dark",
  fontFamily: "Consolas, 'Courier New', monospace",
  fontSize: 14,
}

export interface UseCodingSettingsResult {
  settings: CodingSettings
  loaded: boolean
  update: (patch: Partial<CodingSettings>) => void
  reset: () => void
}

// useCodingSettings reads from (and writes to) the backend so all windows agree.
export function useCodingSettings(): UseCodingSettingsResult {
  const [settings, setSettings] = useState<CodingSettings>(DEFAULT_CODING_SETTINGS)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    let cancelled = false
    rcService
      .getCodingSettings()
      .then((cs) => {
        if (!cancelled) setSettings(cs)
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setLoaded(true)
      })

    // Live updates from other windows (backend broadcasts on every save).
    const off = Events.On("rc:codingSettings", (e: {data: string}) => {
      try {
        setSettings(JSON.parse(e.data) as CodingSettings)
      } catch {
        // ignore malformed
      }
    })
    return () => {
      cancelled = true
      off()
    }
  }, [])

  const update = useCallback((patch: Partial<CodingSettings>) => {
    setSettings((prev) => {
      const next = {...prev, ...patch}
      rcService.setCodingSettings(next.theme, next.fontFamily, next.fontSize).catch(() => {})
      return next
    })
  }, [])

  const reset = useCallback(() => {
    rcService
      .setCodingSettings(
        DEFAULT_CODING_SETTINGS.theme,
        DEFAULT_CODING_SETTINGS.fontFamily,
        DEFAULT_CODING_SETTINGS.fontSize,
      )
      .catch(() => {})
    setSettings(DEFAULT_CODING_SETTINGS)
  }, [])

  return {settings, loaded, update, reset}
}
