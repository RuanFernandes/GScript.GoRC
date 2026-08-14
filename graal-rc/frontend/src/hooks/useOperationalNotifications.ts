import {useCallback, useEffect, useMemo, useState} from "react"
import {Events} from "@wailsio/runtime"

import {useLanguage} from "@/hooks/useLanguage"
import type {OperationalNotification, OperationalNotificationLevel} from "@/types"

const STORAGE_KEY = "graal-rc:operationalNotifications"
const MAX_NOTIFICATIONS = 80

function loadNotifications(): OperationalNotification[] {
  if (typeof window === "undefined") return []
  try {
    const parsed = JSON.parse(window.localStorage.getItem(STORAGE_KEY) ?? "[]") as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter((item): item is OperationalNotification => {
      if (!item || typeof item !== "object") return false
      const value = item as Partial<OperationalNotification>
      return typeof value.id === "string"
        && (value.level === "info" || value.level === "success" || value.level === "warning" || value.level === "error")
        && typeof value.title === "string"
        && typeof value.message === "string"
        && typeof value.timestamp === "number"
        && typeof value.read === "boolean"
    }).slice(0, MAX_NOTIFICATIONS)
  } catch {
    return []
  }
}

function nextID(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

function parseObject(data: unknown): Record<string, unknown> | null {
  if (typeof data !== "string") return typeof data === "object" && data !== null ? data as Record<string, unknown> : null
  try {
    const parsed = JSON.parse(data) as unknown
    return typeof parsed === "object" && parsed !== null ? parsed as Record<string, unknown> : null
  } catch {
    return null
  }
}

export interface OperationalNotificationsResult {
  notifications: OperationalNotification[]
  unreadCount: number
  markRead: (id: string) => void
  markAllRead: () => void
  clear: () => void
}

export function useOperationalNotifications(): OperationalNotificationsResult {
  const {t} = useLanguage()
  const [notifications, setNotifications] = useState<OperationalNotification[]>(loadNotifications)

  useEffect(() => {
    try {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(notifications.slice(0, MAX_NOTIFICATIONS)))
    } catch {
      // Ignore private-mode and quota failures; notifications remain in memory.
    }
  }, [notifications])

  const push = useCallback((level: OperationalNotificationLevel, title: string, message: string) => {
    const normalizedMessage = message.trim() || title
    const timestamp = Date.now()
    setNotifications((current) => {
      const previous = current[0]
      if (previous && previous.level === level && previous.title === title && previous.message === normalizedMessage && timestamp - previous.timestamp < 1500) {
        return current
      }
      return [{id: nextID(), level, title, message: normalizedMessage, timestamp, read: false}, ...current].slice(0, MAX_NOTIFICATIONS)
    })
  }, [])

  useEffect(() => {
    const handleEnvelope = (event: {data: string}) => {
      const envelope = parseObject(event.data)
      const name = typeof envelope?.name === "string" ? envelope.name : ""
      const values = Array.isArray(envelope?.data) ? envelope.data : []
      const reason = typeof values[0] === "string" ? values[0] : ""
      switch (name) {
        case "rc:connected":
          push("success", t("notifications.connection"), t("notifications.connected"))
          break
        case "rc:disconnected":
          push("warning", t("notifications.connection"), reason || t("notifications.disconnected"))
          break
        case "rc:pumpError":
          push("error", t("notifications.connection"), reason || t("notifications.pumpError"))
          break
        case "rc:scriptPermissionsChanged":
          push("warning", t("notifications.permissions"), t("notifications.permissionsChanged"))
          break
        case "rc:scriptIdentityChanged":
          push("info", t("notifications.identity"), t("notifications.identityChanged"))
          break
        default:
          break
      }
    }
    const handleConflict = (event: {data: string}) => {
      const item = parseObject(event.data)
      const name = typeof item?.name === "string" ? item.name : t("notifications.script")
      push("warning", t("notifications.sync"), t("notifications.syncConflict", {name}))
    }

    const offs = [
      Events.On("rc:evt", handleEnvelope),
      Events.On("rc:syncConflict", handleConflict),
    ]
    return () => offs.forEach((off) => off())
  }, [push, t])

  const markRead = useCallback((id: string) => {
    setNotifications((current) => current.map((item) => item.id === id ? {...item, read: true} : item))
  }, [])

  const markAllRead = useCallback(() => {
    setNotifications((current) => current.map((item) => ({...item, read: true})))
  }, [])

  const clear = useCallback(() => setNotifications([]), [])
  const unreadCount = useMemo(() => notifications.filter((item) => !item.read).length, [notifications])

  return {notifications, unreadCount, markRead, markAllRead, clear}
}
