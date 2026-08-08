import {useCallback, useEffect, useMemo, useRef, useState} from "react"

import type {CommandMacro, CommandMacroParameter, CommandMacroParameterType} from "@/types"

const MAX_MACROS = 50
const MAX_NAME_LENGTH = 80
const MAX_COMMAND_LENGTH = 2000
const MAX_PARAMETERS = 8
const MAX_PARAMETER_NAME_LENGTH = 40
const PARAMETER_NAME_PATTERN = /^[A-Za-z][A-Za-z0-9_-]*$/

function isParameterType(value: unknown): value is CommandMacroParameterType {
  return value === "text" || value === "number" || value === "boolean"
}

function normalizeLoadedParameters(value: unknown): CommandMacroParameter[] | undefined {
  if (!Array.isArray(value)) return undefined
  const seen = new Set<string>()
  const parameters: CommandMacroParameter[] = []
  for (const item of value.slice(0, MAX_PARAMETERS)) {
    if (!item || typeof item !== "object") continue
    const candidate = item as Partial<CommandMacroParameter>
    const name = typeof candidate.name === "string" ? candidate.name.trim().slice(0, MAX_PARAMETER_NAME_LENGTH) : ""
    if (!name || !PARAMETER_NAME_PATTERN.test(name) || !isParameterType(candidate.type)) continue
    const key = name.toLocaleLowerCase()
    if (seen.has(key)) continue
    seen.add(key)
    parameters.push({name, type: candidate.type})
  }
  return parameters
}

function sanitizeParameters(parameters: CommandMacroParameter[] | undefined): CommandMacroParameter[] | null {
  if (!parameters?.length) return []
  if (parameters.length > MAX_PARAMETERS) return null
  const seen = new Set<string>()
  const clean: CommandMacroParameter[] = []
  for (const parameter of parameters) {
    const name = parameter.name.trim().slice(0, MAX_PARAMETER_NAME_LENGTH)
    if (!name || !PARAMETER_NAME_PATTERN.test(name) || !isParameterType(parameter.type)) return null
    const key = name.toLocaleLowerCase()
    if (seen.has(key)) return null
    seen.add(key)
    clean.push({name, type: parameter.type})
  }
  return clean
}

function storageKey(serverName: string): string {
  return `graal-rc:commandMacros:${serverName.trim().toLocaleLowerCase() || "default"}`
}

function loadMacros(serverName: string): CommandMacro[] {
  if (typeof window === "undefined") return []
  try {
    const parsed = JSON.parse(window.localStorage.getItem(storageKey(serverName)) ?? "[]") as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.map((item): CommandMacro | null => {
      if (!item || typeof item !== "object") return null
      const value = item as Partial<CommandMacro> & {parameters?: unknown}
      if (typeof value.id !== "string" || typeof value.name !== "string" || typeof value.command !== "string"
        || typeof value.createdAt !== "number" || typeof value.updatedAt !== "number"
        || !value.name.trim() || !value.command.trim()) return null
      const parameters = normalizeLoadedParameters(value.parameters)
      return {
        id: value.id,
        name: value.name.trim().slice(0, MAX_NAME_LENGTH),
        command: value.command.trim().slice(0, MAX_COMMAND_LENGTH),
        ...(parameters?.length ? {parameters} : {}),
        createdAt: value.createdAt,
        updatedAt: value.updatedAt,
      }
    }).filter((item): item is CommandMacro => item !== null).slice(0, MAX_MACROS)
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
  save: (name: string, command: string, parameters?: CommandMacroParameter[]) => boolean
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

  const save = useCallback((name: string, command: string, parameters?: CommandMacroParameter[]): boolean => {
    const cleanName = name.trim().slice(0, MAX_NAME_LENGTH)
    const cleanCommand = command.trim().slice(0, MAX_COMMAND_LENGTH)
    const cleanParameters = sanitizeParameters(parameters)
    if (!cleanName || !cleanCommand || !cleanParameters) return false
    const now = Date.now()
    setMacros((current) => {
      const existing = current.find((item) => item.name.toLocaleLowerCase() === cleanName.toLocaleLowerCase())
      if (existing) {
        return current.map((item) => item.id === existing.id
          ? {...item, name: cleanName, command: cleanCommand, parameters: cleanParameters.length ? cleanParameters : undefined, updatedAt: now}
          : item)
      }
      return [{
        id: nextID(),
        name: cleanName,
        command: cleanCommand,
        ...(cleanParameters.length ? {parameters: cleanParameters} : {}),
        createdAt: now,
        updatedAt: now,
      }, ...current].slice(0, MAX_MACROS)
    })
    return true
  }, [])

  const remove = useCallback((id: string) => {
    setMacros((current) => current.filter((item) => item.id !== id))
  }, [])

  return {macros, save, remove}
}
