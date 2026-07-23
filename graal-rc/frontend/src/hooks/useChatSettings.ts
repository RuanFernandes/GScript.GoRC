// useChatSettings owns the user's chat color preferences, persisted to
// localStorage so they survive restarts. Colors drive the timestamp, [RC]/[NC]
// prefixes, speaker name, and content rendering in the chat panes.
import {useCallback, useEffect, useState} from "react"

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
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return DEFAULT_CHAT_SETTINGS
    return {...DEFAULT_CHAT_SETTINGS, ...(JSON.parse(raw) as Partial<ChatSettings>)}
  } catch {
    return DEFAULT_CHAT_SETTINGS
  }
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
      localStorage.setItem(STORAGE_KEY, JSON.stringify(settings))
    } catch {
      // ignore quota / private-mode failures
    }
  }, [settings])

  const update = useCallback((patch: Partial<ChatSettings>) => {
    setSettings((prev) => ({...prev, ...patch}))
  }, [])

  const reset = useCallback(() => setSettings(DEFAULT_CHAT_SETTINGS), [])

  return {settings, update, reset}
}
