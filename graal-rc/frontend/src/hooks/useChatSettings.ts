// useChatSettings owns the user's chat color preferences, persisted by the Go
// backend so every Wails window sees the same values. localStorage remains a
// migration fallback for installations that already had chat preferences.
import {useCallback, useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"

import {rcService} from "@/services/rcService"
import type {ChatSettings} from "@/types"

const STORAGE_KEY = "graal-rc:chatSettings"

export const DEFAULT_CHAT_SETTINGS: ChatSettings = {
  timestamp: "#22c55e",
  rcPrefix: "#22c55e",
  ncPrefix: "#38bdf8",
  ircPrefix: "#a78bfa",
  speaker: "#facc15",
  content: "#e5e7eb",
  logChat: false,
  logDir: "",
  pmLog: false,
  pmLogDir: "",
}

function load(): ChatSettings {
  try {
    const raw = window.localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULT_CHAT_SETTINGS
    return {...DEFAULT_CHAT_SETTINGS, ...(JSON.parse(raw) as Partial<ChatSettings>)}
  } catch {
    return DEFAULT_CHAT_SETTINGS
  }
}

function isCompleteColor(value: string): boolean {
  return /^#[0-9a-f]{6}$/i.test(value)
}

function canPersist(settings: ChatSettings): boolean {
  return [settings.timestamp, settings.rcPrefix, settings.ncPrefix, settings.ircPrefix, settings.speaker, settings.content].every(isCompleteColor)
}

function hasLegacyCustomization(settings: ChatSettings): boolean {
  return settings.timestamp !== DEFAULT_CHAT_SETTINGS.timestamp
    || settings.rcPrefix !== DEFAULT_CHAT_SETTINGS.rcPrefix
    || settings.ncPrefix !== DEFAULT_CHAT_SETTINGS.ncPrefix
    || settings.ircPrefix !== DEFAULT_CHAT_SETTINGS.ircPrefix
    || settings.speaker !== DEFAULT_CHAT_SETTINGS.speaker
    || settings.content !== DEFAULT_CHAT_SETTINGS.content
    || settings.logChat !== DEFAULT_CHAT_SETTINGS.logChat
    || settings.logDir !== DEFAULT_CHAT_SETTINGS.logDir
    || settings.pmLog !== DEFAULT_CHAT_SETTINGS.pmLog
    || settings.pmLogDir !== DEFAULT_CHAT_SETTINGS.pmLogDir
}

export interface UseChatSettingsResult {
  settings: ChatSettings
  update: (patch: Partial<ChatSettings>) => void
  reset: () => void
}

export function useChatSettings(): UseChatSettingsResult {
  const [settings, setSettings] = useState<ChatSettings>(load)

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(settings))
    } catch {
      // ignore quota / private-mode failures
    }
  }, [settings])

  useEffect(() => {
    let cancelled = false
    const apply = (next: ChatSettings) => {
      if (!cancelled) setSettings({...DEFAULT_CHAT_SETTINGS, ...next})
    }

    rcService.getChatSettings().then((state) => {
      if (cancelled) return
      if (state?.exists) {
        apply(state.settings)
        return
      }

      // Migrate settings saved by older builds without overwriting them with
      // the new backend defaults.
      const legacy = load()
      apply(legacy)
      if (canPersist(legacy) && hasLegacyCustomization(legacy)) void rcService.setChatSettings(legacy).catch(() => {})
    }).catch(() => {})

    const off = Events.On("rc:chatSettings", (event: {data: string}) => {
      try {
        apply(JSON.parse(event.data) as ChatSettings)
      } catch {
        // ignore malformed cross-window settings events
      }
    })
    return () => {
      cancelled = true
      off()
    }
  }, [])

  const update = useCallback((patch: Partial<ChatSettings>) => {
    setSettings((prev) => {
      const next = {...prev, ...patch}
      if (canPersist(next)) void rcService.setChatSettings(next).catch(() => {})
      return next
    })
  }, [])

  const reset = useCallback(() => {
    setSettings(DEFAULT_CHAT_SETTINGS)
    void rcService.setChatSettings(DEFAULT_CHAT_SETTINGS).catch(() => {})
  }, [])

  return {settings, update, reset}
}
