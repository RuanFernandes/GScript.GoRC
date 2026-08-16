import {useCallback, useEffect, useLayoutEffect, useMemo, useState} from "react"
import {Events} from "@wailsio/runtime"

import {rcService} from "@/services/rcService"
import type {AppThemeStore} from "@/types"
import {
  applyAppTheme,
  APP_THEME_PENDING_CLASS,
  cacheAppTheme,
  DEFAULT_APP_THEME_KEY,
  getInitialAppThemeState,
  mergeAppThemeStore,
  normalizeAppTheme,
  type EditableAppTheme,
} from "@/lib/appThemes"

export interface UseAppThemeResult {
  activeKey: string
  activeTheme: EditableAppTheme
  themes: EditableAppTheme[]
  loaded: boolean
  select: (key: string) => Promise<void>
  save: (theme: EditableAppTheme) => Promise<void>
  remove: (key: string) => Promise<void>
  reset: () => Promise<void>
}

export function useAppTheme(): UseAppThemeResult {
  const [state, setState] = useState(getInitialAppThemeState)
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    let cancelled = false
    void rcService.getAppThemeStore().then((store) => {
      if (!cancelled) setState(mergeAppThemeStore(store))
    }).catch(() => {
      // Built-in dark remains the safe fallback when the config is unavailable.
    }).finally(() => {
      if (!cancelled) setLoaded(true)
    })

    const off = Events.On("rc:appTheme", (event: {data: string}) => {
      try {
        setState(mergeAppThemeStore(JSON.parse(event.data) as AppThemeStore))
      } catch {
        // Ignore malformed cross-window payloads.
      }
    })
    return () => {
      cancelled = true
      off()
    }
  }, [])

  const activeTheme = useMemo(
    () => state.themes.find((theme) => theme.key === state.activeKey) ?? state.themes[0],
    [state.activeKey, state.themes],
  )

  useLayoutEffect(() => {
    applyAppTheme(activeTheme)
    if (!loaded) return
    cacheAppTheme(activeTheme)
    if (typeof document !== "undefined") document.documentElement.classList.remove(APP_THEME_PENDING_CLASS)
  }, [activeTheme, loaded])

  const select = useCallback(async (key: string) => {
    if (!state.themes.some((theme) => theme.key === key)) return
    await rcService.setActiveAppTheme(key)
    setState((previous) => ({...previous, activeKey: key}))
  }, [state.themes])

  const save = useCallback(async (theme: EditableAppTheme) => {
    const normalized = normalizeAppTheme(theme)
    await rcService.saveAppTheme(normalized)
    await rcService.setActiveAppTheme(normalized.key)
    setState((previous) => ({
      activeKey: normalized.key,
      themes: [...previous.themes.filter((item) => item.key !== normalized.key), normalized],
    }))
  }, [])

  const remove = useCallback(async (key: string) => {
    await rcService.deleteAppTheme(key)
    setState((previous) => ({
      activeKey: previous.activeKey === key ? DEFAULT_APP_THEME_KEY : previous.activeKey,
      themes: previous.themes.filter((item) => item.key !== key),
    }))
  }, [])

  const reset = useCallback(async () => {
    await select(DEFAULT_APP_THEME_KEY)
  }, [select])

  return {activeKey: state.activeKey, activeTheme, themes: state.themes, loaded, select, save, remove, reset}
}
