import {useCallback, useEffect, useMemo, useRef, useState} from "react"

import type {CommandMacro} from "@/types"

const MAX_MACROS = 50
const MAX_NAME_LENGTH = 80
const MAX_COMMAND_LENGTH = 2000

function storageKey(serverName: string): string {
  return `graal-rc:commandMacros:${serverName.trim().toLocaleLowerCase() || "default"}`
}

function loadMacros(serverName: string): CommandMacro[] {
  if (typeof window === "undefined") return []
  try {
    const parsed = JSON.parse(window.localStorage.getItem(storageKey(serverName)) ?? "[]") as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter((item): item is CommandMacro => {
      if (!item || typeof item !== "object") return false
      const value = item as Partial<CommandMacro>
      return typeof value.id === "string" && typeof value.name === "string" && typeof value.command === "string"
        && typeof value.createdAt === "number" && typeof value.updatedAt === "number"
        && value.name.trim().length > 0 && value.command.trim().length > 0
    }).slice(0, MAX_MACROS)
  } catch {
    return []
  }
}

function nextID(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export interface CommandMacrosResult {
  macros: CommandMacro[]
  save: (name: string, command: string) => boolean
  remove: (id: string) => void
}

export function useCommandMacros(serverName: string): CommandMacrosResult {
  const [macros, setMacros] = useState<CommandMacro[]>(() => loadMacros(serverName))
  const key = useMemo(() => storageKey(serverName), [serverName])
  const persistedKey = useRef(key)

  useEffect(() => {
    // A server switch changes the storage namespace. Skip one persistence
    // pass so the previous server's in-memory list cannot overwrite the new
    // server's saved macros before they are loaded.
    if (persistedKey.current !== key) {
      persistedKey.current = key
      setMacros(loadMacros(serverName))
      return
    }
    try {
      window.localStorage.setItem(key, JSON.stringify(macros.slice(0, MAX_MACROS)))
    } catch {
      // Ignore quota and private-mode failures; the current window still works.
    }
  }, [key, macros, serverName])

  const save = useCallback((name: string, command: string): boolean => {
    const cleanName = name.trim().slice(0, MAX_NAME_LENGTH)
    const cleanCommand = command.trim().slice(0, MAX_COMMAND_LENGTH)
    if (!cleanName || !cleanCommand) return false
    const now = Date.now()
    setMacros((current) => {
      const existing = current.find((item) => item.name.toLocaleLowerCase() === cleanName.toLocaleLowerCase())
      if (existing) {
        return current.map((item) => item.id === existing.id ? {...item, name: cleanName, command: cleanCommand, updatedAt: now} : item)
      }
      return [{id: nextID(), name: cleanName, command: cleanCommand, createdAt: now, updatedAt: now}, ...current].slice(0, MAX_MACROS)
    })
    return true
  }, [])

  const remove = useCallback((id: string) => {
    setMacros((current) => current.filter((item) => item.id !== id))
  }, [])

  return {macros, save, remove}
}
