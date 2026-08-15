import {useCallback, useEffect, useMemo, useRef, useState} from "react"

import {rcService} from "@/services/rcService"
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

function normalizeMacroList(value: unknown): CommandMacro[] {
  if (!Array.isArray(value)) return []
  return value.map((item): CommandMacro | null => {
    if (!item || typeof item !== "object") return null
    const candidate = item as Partial<CommandMacro> & {parameters?: unknown}
    if (typeof candidate.id !== "string" || typeof candidate.name !== "string" || typeof candidate.command !== "string"
      || typeof candidate.createdAt !== "number" || !Number.isFinite(candidate.createdAt)
      || typeof candidate.updatedAt !== "number" || !Number.isFinite(candidate.updatedAt)
      || !candidate.id.trim() || !candidate.name.trim() || !candidate.command.trim()) return null
    const parameters = normalizeLoadedParameters(candidate.parameters)
    return {
      id: candidate.id.trim(),
      name: candidate.name.trim().slice(0, MAX_NAME_LENGTH),
      command: candidate.command.trim().slice(0, MAX_COMMAND_LENGTH),
      ...(parameters?.length ? {parameters} : {}),
      createdAt: candidate.createdAt,
      updatedAt: candidate.updatedAt,
    }
  }).filter((item): item is CommandMacro => item !== null).slice(0, MAX_MACROS)
}

function loadMacros(serverName: string): CommandMacro[] {
  if (typeof window === "undefined") return []
  try {
    return normalizeMacroList(JSON.parse(window.localStorage.getItem(storageKey(serverName)) ?? "[]"))
  } catch {
    return []
  }
}

function writeLocalMacros(serverName: string, macros: CommandMacro[]): void {
  if (typeof window === "undefined") return
  try {
    window.localStorage.setItem(storageKey(serverName), JSON.stringify(macros.slice(0, MAX_MACROS)))
  } catch {
    // Ignore quota and private-mode failures; the user-level file is primary.
  }
}

function upsertMacro(current: CommandMacro[], macro: CommandMacro): CommandMacro[] {
  const existing = current.find((item) => item.id === macro.id || item.name.toLocaleLowerCase() === macro.name.toLocaleLowerCase())
  if (!existing) return [macro, ...current].slice(0, MAX_MACROS)
  return current.map((item) => item.id === existing.id ? macro : item)
}

function nextID(): string {
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID()
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`
}

export interface CommandMacrosResult {
  macros: CommandMacro[]
  save: (name: string, command: string, parameters?: CommandMacroParameter[]) => Promise<boolean>
  remove: (id: string) => Promise<boolean>
}

export function useCommandMacros(serverName: string): CommandMacrosResult {
  const [macros, setMacros] = useState<CommandMacro[]>(() => loadMacros(serverName))
  const key = useMemo(() => storageKey(serverName), [serverName])
  const macrosRef = useRef(macros)
  const migrationRef = useRef<{key: string; promise: Promise<void>} | null>(null)
  macrosRef.current = macros

  useEffect(() => {
    let cancelled = false
    const legacyMacros = loadMacros(serverName)
    migrationRef.current = null
    macrosRef.current = legacyMacros
    setMacros(legacyMacros)

    // Load the user-level file first. The localStorage copy is only used to
    // migrate macros created by older RC versions that did not have a backend
    // persistence layer yet.
    rcService.getCommandMacros(serverName).then((store) => {
      if (cancelled) return
      const remoteMacros = normalizeMacroList(store?.macros)
      const nextMacros = store?.exists ? remoteMacros : legacyMacros
      macrosRef.current = nextMacros
      setMacros(nextMacros)
      writeLocalMacros(serverName, nextMacros)

      if (!store?.exists && legacyMacros.length > 0) {
        const migration = rcService.setCommandMacros(serverName, legacyMacros).catch(() => {})
        const entry = {key, promise: migration}
        migrationRef.current = entry
        void migration.finally(() => {
          if (migrationRef.current === entry) migrationRef.current = null
        })
      }
    }).catch(() => {
      // Keep the local copy usable if the app is running outside Wails or the
      // user-level file is temporarily unavailable.
      if (!cancelled) setMacros(legacyMacros)
    })

    return () => {
      cancelled = true
    }
  }, [key, serverName])

  const save = useCallback(async (name: string, command: string, parameters?: CommandMacroParameter[]): Promise<boolean> => {
    const cleanName = name.trim().slice(0, MAX_NAME_LENGTH)
    const cleanCommand = command.trim().slice(0, MAX_COMMAND_LENGTH)
    const cleanParameters = sanitizeParameters(parameters)
    if (!cleanName || !cleanCommand || !cleanParameters) return false

    const migration = migrationRef.current
    if (migration?.key === key) await migration.promise

    let saved: CommandMacro
    try {
      const persisted = await rcService.saveCommandMacro(serverName, cleanName, cleanCommand, cleanParameters)
      const normalized = normalizeMacroList(persisted ? [persisted] : [])[0]
      if (!normalized) return false
      saved = normalized
    } catch {
      // Keep the old browser-only behavior as a fallback for development mode.
      // The desktop build normally uses the user-level file above.
      const now = Date.now()
      const existing = macrosRef.current.find((item) => item.name.toLocaleLowerCase() === cleanName.toLocaleLowerCase())
      saved = existing
        ? {...existing, name: cleanName, command: cleanCommand, parameters: cleanParameters.length ? cleanParameters : undefined, updatedAt: now}
        : {id: nextID(), name: cleanName, command: cleanCommand, ...(cleanParameters.length ? {parameters: cleanParameters} : {}), createdAt: now, updatedAt: now}
    }

    const next = upsertMacro(macrosRef.current, saved)
    macrosRef.current = next
    setMacros(next)
    writeLocalMacros(serverName, next)
    return true
  }, [key, serverName])

  const remove = useCallback(async (id: string): Promise<boolean> => {
    const migration = migrationRef.current
    if (migration?.key === key) await migration.promise

    try {
      await rcService.deleteCommandMacro(serverName, id)
    } catch {
      return false
    }
    const next = macrosRef.current.filter((item) => item.id !== id)
    macrosRef.current = next
    setMacros(next)
    writeLocalMacros(serverName, next)
    return true
  }, [key, serverName])

  return {macros, save, remove}
}
