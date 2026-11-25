// Staged (not yet saved) changes of the SQLite explorer. Rowids and column
// names only mean something inside one table, so the overlay records the table
// it was staged on and refuses to be shown over, or extended by, any other.
import type {SqliteChanges} from "../types"

export interface PendingRow {
  tempId: string
  cells: Record<string, string>
}

export interface StagedChanges {
  table: string | null
  edits: ReadonlyMap<string, unknown>
  deletes: ReadonlySet<number>
  pending: readonly PendingRow[]
}

export function emptyStaged(table: string | null = null): StagedChanges {
  return {table, edits: new Map(), deletes: new Set(), pending: []}
}

export function isDirty(staged: StagedChanges): boolean {
  return staged.edits.size > 0 || staged.deletes.size > 0 || staged.pending.length > 0
}

// stagedFor is the part of the overlay that belongs to table.
export function stagedFor(staged: StagedChanges, table: string | null): StagedChanges {
  return staged.table === table ? staged : emptyStaged(table)
}

function change(staged: StagedChanges, table: string, apply: (current: StagedChanges) => Partial<StagedChanges>): StagedChanges {
  if (isDirty(staged) && staged.table !== table) return staged
  const current = stagedFor(staged, table)
  return {...current, ...apply(current)}
}

export function stageCell(staged: StagedChanges, table: string, rowid: number, column: string, value: unknown): StagedChanges {
  return change(staged, table, (s) => ({edits: new Map(s.edits).set(`${rowid}:${column}`, value)}))
}

export function stageDelete(staged: StagedChanges, table: string, rowid: number): StagedChanges {
  return change(staged, table, (s) => ({deletes: new Set(s.deletes).add(rowid)}))
}

export function addPendingRow(staged: StagedChanges, table: string, tempId: string): StagedChanges {
  return change(staged, table, (s) => ({pending: [...s.pending, {tempId, cells: {}}]}))
}

export function editPendingCell(staged: StagedChanges, table: string, tempId: string, column: string, value: string): StagedChanges {
  return change(staged, table, (s) => ({
    pending: s.pending.map((r) => (r.tempId === tempId ? {...r, cells: {...r.cells, [column]: value}} : r)),
  }))
}

export function removePendingRow(staged: StagedChanges, table: string, tempId: string): StagedChanges {
  return change(staged, table, (s) => ({pending: s.pending.filter((r) => r.tempId !== tempId)}))
}

export function canSwitchTable(staged: StagedChanges, saving: boolean, current: string | null, next: string): boolean {
  return current === next || !saving && !isDirty(staged)
}

// coerceValue: edited cell text → JSON value (number when numeric, else text).
export function coerceValue(text: string): unknown {
  if (text === "") return ""
  if (/^-?\d+$/.test(text)) return Number(text)
  if (/^-?\d+\.\d+$/.test(text)) return Number(text)
  return text
}

// buildChanges targets the table the changes were staged on, never the one
// that happens to be selected.
export function buildChanges(staged: StagedChanges): SqliteChanges | null {
  if (staged.table === null || !isDirty(staged)) return null
  return {
    table: staged.table,
    inserts: staged.pending.map((r) => Object.fromEntries(Object.entries(r.cells).map(([k, v]) => [k, coerceValue(v)]))),
    updates: [...staged.edits].map(([key, value]) => {
      const colon = key.indexOf(":")
      return {rowid: Number(key.slice(0, colon)), column: key.slice(colon + 1), value}
    }),
    deletes: [...staged.deletes],
  }
}

// createLatestGate: each call opens a request and returns a check that turns
// false once a newer request has been opened.
export function createLatestGate() {
  let latest = 0
  return () => {
    const mine = ++latest
    return () => mine === latest
  }
}

// createSingleFlight: while a task is running, further calls join it.
export function createSingleFlight<T>() {
  let running: Promise<T> | null = null
  return (task: () => Promise<T>): Promise<T> => {
    if (running) return running
    const work = (async () => task())().finally(() => { running = null })
    running = work
    return work
  }
}
