// SqliteExplorerWindowScreen is the SQLite explorer for a remote .db (opened via
// App.OpenRemoteFile on a .db double-click, URL "/#sqlite?p=<remotePath>"). The
// file is downloaded to a local cache that modernc edits in place.
//
// Editing is DEFERRED: cell edits, inserts (+Row), and deletes are staged in a
// frontend overlay and applied as ONE transaction on Save (atomic; a failing
// statement rolls back, nothing is written). Discard clears the overlay. The
// overlay belongs to one table: switching is blocked until it is saved or
// discarded, and editing is locked while a save is running. Three
// tabs: Data (inline-editable grid), Diagram (reactflow ER overview), SQL
// (Monaco console with keyword/table/column autocomplete).
import {useCallback, useEffect, useMemo, useRef, useState} from "react"
import Editor, {type OnMount} from "@monaco-editor/react"
import {Events} from "@wailsio/runtime"
import {toast} from "sonner"
import {Loader2, Plus, RotateCcw, Save, Trash2} from "lucide-react"
import {ReactFlow, Background, Controls, type Node, type Edge} from "@xyflow/react"
import "@xyflow/react/dist/style.css"

import {AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogHeader, AlertDialogTitle} from "@/components/ui/alert-dialog"
import {Button} from "@/components/ui/button"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {rcService} from "@/services/rcService"
import type {SqliteResult, SqliteSchema, SqliteTable} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"
import {
  addPendingRow, buildChanges, canSwitchTable, coerceValue, createLatestGate, createSingleFlight, editPendingCell,
  emptyStaged, isDirty, removePendingRow, stageCell, stageDelete, stagedFor, type StagedChanges,
} from "@/lib/sqliteStaging"

const KIND = "sqlite"

function parsePath(): string {
  const hash = typeof window !== "undefined" ? window.location.hash : ""
  const q = hash.indexOf("?")
  if (q < 0) return ""
  return new URLSearchParams(hash.slice(q + 1)).get("p") ?? ""
}

function cellText(v: unknown): string {
  if (v === null || v === undefined) return "NULL"
  if (typeof v === "string") return v.startsWith("base64:") ? "(blob)" : v
  return String(v)
}

// --- reactflow table card node ---
function TableNode({data}: {data: Record<string, unknown>}) {
  const {t} = useLanguage()
  const table = data as unknown as SqliteSchema
  return (
    <div className="bg-popover w-52 overflow-hidden rounded-md border text-xs shadow-lg">
      <div className="bg-muted/60 px-2 py-1 font-semibold">{table.name}</div>
      <div>
        {(table.columns ?? []).map((c) => (
          <div key={c.name} className="flex items-center gap-1 border-t border-white/5 px-2 py-0.5">
            {c.pk && <span className="text-amber-400" title={t("sqlite.primaryKey")}>★</span>}
            <span className="truncate">{c.name}</span>
            <span className="text-muted-foreground ml-auto text-[10px]">{c.type || "?"}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

const nodeTypes = {table: TableNode}

export function SqliteExplorerWindowScreen() {
  const {t} = useLanguage()
  const remotePath = useRef(parsePath()).current
  const [tables, setTables] = useState<SqliteTable[]>([])
  const [schema, setSchema] = useState<SqliteSchema[]>([])
  const [active, setActive] = useState<string | null>(null)
  // Rows are tagged with their table so they are never shown or edited as
  // another table's rows.
  const [data, setData] = useState<{table: string; result: SqliteResult} | null>(null)
  const [loadingTables, setLoadingTables] = useState(true)
  const [loadingData, setLoadingData] = useState(false)

  // Deferred overlay. The ref lets event handlers and save() read the latest
  // staged changes without waiting for a render.
  const [staged, setStaged] = useState<StagedChanges>(() => emptyStaged())
  const stagedRef = useRef(staged)
  const [saving, setSaving] = useState(false)
  const savingRef = useRef(false)
  const saveFlight = useRef(createSingleFlight<boolean>()).current
  const dataGate = useRef(createLatestGate()).current
  const [edit, setEdit] = useState<{rowid?: number; tempId?: string; col: string; value: string} | null>(null)

  const [sql, setSql] = useState("")
  const [sqlResult, setSqlResult] = useState<SqliteResult | null>(null)
  const [running, setRunning] = useState(false)
  const [confirmClose, setConfirmClose] = useState(false)

  const dirty = isDirty(staged)

  const baseName = remotePath.includes("/") ? remotePath.slice(remotePath.lastIndexOf("/") + 1) : remotePath
  const shown = data && data.table === active ? data.result : null
  const shownTable = shown ? active : null
  const {edits, deletes, pending} = stagedFor(staged, shownTable)
  const cols = shown?.columns ?? []
  const rows = shown?.rows ?? []

  const loadTables = useCallback(async () => {
    setLoadingTables(true)
    try {
      const info = await rcService.getSqliteInfo(remotePath)
      setTables(info?.tables ?? [])
      const sc = await rcService.getSqliteSchema(remotePath).catch(() => null)
      setSchema(sc ?? [])
      if (!active && info?.tables?.length) setActive(info.tables[0].name)
    } catch (err) {
      toast.error(t("sqlite.readFailed"), {description: String(err)})
    } finally {
      setLoadingTables(false)
    }
  }, [remotePath, active, t])

  const loadData = useCallback(async () => {
    if (!active) return
    const isCurrent = dataGate()
    setLoadingData(true)
    setEdit(null)
    try {
      const res = await rcService.sqliteQuery(remotePath, `SELECT rowid, * FROM "${active}" LIMIT 200`, [])
      if (isCurrent()) setData({table: active, result: res})
    } catch (err) {
      if (isCurrent()) toast.error(t("sqlite.queryFailed"), {description: String(err)})
    } finally {
      if (isCurrent()) setLoadingData(false)
    }
  }, [remotePath, active, t, dataGate])

  useEffect(() => {
    void loadTables()
  }, [loadTables])
  useEffect(() => {
    void loadData()
  }, [loadData])
  useEffect(() => {
    rcService.setEditorDirty(KIND, remotePath, dirty).catch(() => {})
  }, [remotePath, dirty])

  // Close-confirm: backend cancels a dirty close and emits this event.
  useEffect(() => {
    const myKey = `${KIND}:${remotePath}`
    const off = Events.On("rc:editorConfirmClose", (e: {data: string}) => {
      if (e.data === myKey) setConfirmClose(true)
    })
    return () => {
      off()
    }
  }, [remotePath])

  const replaceStaged = useCallback((next: StagedChanges) => {
    stagedRef.current = next
    setStaged(next)
  }, [])
  // Every user edit goes through here, so nothing changes while saving.
  const updateStaged = useCallback((apply: (current: StagedChanges) => StagedChanges) => {
    if (!savingRef.current) replaceStaged(apply(stagedRef.current))
  }, [replaceStaged])

  const discardAll = useCallback(() => {
    updateStaged(() => emptyStaged())
  }, [updateStaged])

  const selectTable = useCallback((name: string) => {
    if (!canSwitchTable(stagedRef.current, savingRef.current, active, name)) {
      toast.warning(t("sqlite.saveOrDiscardBeforeSwitching"))
      return
    }
    if (name !== active) setEdit(null)
    setActive(name)
  }, [active, t])

  // Commit one cell edit into the overlay (no DB write).
  const commitEdit = useCallback(() => {
    if (!edit || !shownTable) return
    const {rowid, tempId, col, value} = edit
    if (rowid !== undefined) {
      updateStaged((s) => stageCell(s, shownTable, rowid, col, coerceValue(value)))
    } else if (tempId) {
      updateStaged((s) => editPendingCell(s, shownTable, tempId, col, value))
    }
    setEdit(null)
  }, [edit, shownTable, updateStaged])

  const startEdit = useCallback((next: {rowid?: number; tempId?: string; col: string; value: string}) => {
    if (!savingRef.current) setEdit(next)
  }, [])

  const addRow = useCallback(() => {
    if (shownTable) updateStaged((s) => addPendingRow(s, shownTable, `t${Date.now()}`))
  }, [shownTable, updateStaged])

  const removeExisting = useCallback((rowid: number) => {
    if (shownTable) updateStaged((s) => stageDelete(s, shownTable, rowid))
  }, [shownTable, updateStaged])
  const removePending = useCallback((tempId: string) => {
    if (shownTable) updateStaged((s) => removePendingRow(s, shownTable, tempId))
  }, [shownTable, updateStaged])

  // Resolves true when nothing is left unsaved. A call made while a save is
  // running joins it, so staged inserts are never committed twice.
  const save = useCallback(() => saveFlight(async () => {
    const changes = buildChanges(stagedRef.current)
    if (!changes) return true
    savingRef.current = true
    setSaving(true)
    setEdit(null)
    try {
      await rcService.commitSqlite(remotePath, changes)
      replaceStaged(emptyStaged())
      await loadData()
      toast.success(t("sqlite.saved"))
      return true
    } catch (err) {
      toast.error(t("common.saveFailed"), {description: String(err)})
      return false
    } finally {
      savingRef.current = false
      setSaving(false)
    }
  }), [saveFlight, remotePath, replaceStaged, loadData, t])

  const closeWindow = useCallback(async () => {
    // Clear the backend flag first so the close hook does not prompt again.
    await rcService.setEditorDirty(KIND, remotePath, false)
    await rcService.closeScriptEditor(KIND, remotePath)
  }, [remotePath])

  const saveAndClose = useCallback(async () => {
    setConfirmClose(false)
    if (await save()) await closeWindow().catch(() => {})
  }, [save, closeWindow])

  const discardAndClose = useCallback(() => {
    if (savingRef.current) return
    setConfirmClose(false)
    discardAll()
    closeWindow().catch(() => {})
  }, [discardAll, closeWindow])

  const runSql = useCallback(async () => {
    setRunning(true)
    try {
      const res = await rcService.sqliteQuery(remotePath, sql.trim(), [])
      setSqlResult(res)
    } catch (err) {
      toast.error(t("sqlite.queryFailed"), {description: String(err)})
    } finally {
      setRunning(false)
    }
  }, [remotePath, sql, t])

  // --- Diagram nodes/edges (grid layout) ---
  const {nodes, edges} = useMemo(() => {
    const n = schema.length
    const perRow = Math.ceil(Math.sqrt(n)) || 1
    const ns: Node[] = schema.map((t, i) => ({
      id: t.name,
      type: "table",
      position: {x: (i % perRow) * 280, y: Math.floor(i / perRow) * 240},
      data: t as unknown as Record<string, unknown>,
    }))
    const es: Edge[] = []
    schema.forEach((t) => {
      t.fks?.forEach((fk, i) => {
        es.push({
          id: `${t.name}-${fk.from}-${i}`,
          source: t.name,
          target: fk.table,
          label: `${fk.from}→${fk.to}`,
          animated: false,
        })
      })
    })
    return {nodes: ns, edges: es}
  }, [schema])

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2">
        <h1 className="text-sm font-semibold">{baseName || t("common.database")}</h1>
        <span className="text-xs text-muted-foreground">{t("sqlite.explorer")}</span>
        {dirty && <span className="text-xs text-amber-500">{t("sqlite.unsaved")}</span>}
        <div className="ml-auto flex items-center gap-2">
          {dirty && (
            <Button size="sm" variant="ghost" onClick={discardAll} disabled={saving} title={t("sqlite.discardChanges")}>
              <RotateCcw />
              {t("sqlite.discard")}
            </Button>
          )}
          {active && (
            <Button size="sm" variant="outline" onClick={addRow} disabled={saving || !shownTable} title={t("sqlite.addRow")}>
              <Plus />
              {t("sqlite.row")}
            </Button>
          )}
          <Button size="sm" onClick={() => void save()} disabled={!dirty || saving} title={t("sqlite.applyUpload")}>
            {saving ? <Loader2 className="animate-spin" /> : <Save />}
            {t("sqlite.save")}
          </Button>
        </div>
      </header>
      <p className="border-b bg-amber-500/10 px-4 py-1 text-xs text-amber-200">
        {t("sqlite.versionWarning")}
      </p>

      {loadingTables ? (
        <div className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> {t("sqlite.opening")}
        </div>
      ) : (
        <div className="flex min-h-0 flex-1">
          <aside className="w-52 shrink-0 overflow-y-auto border-r">
            <ul className="p-1.5">
              {tables.map((t) => (
                <li key={t.name}>
                  <button
                    className={`hover:bg-accent flex w-full items-center rounded px-2 py-1.5 text-left text-sm ${
                      active === t.name ? "bg-accent" : ""
                    }`}
                    onClick={() => selectTable(t.name)}
                    disabled={saving}
                    title={t.schema}
                  >
                    <span className="truncate">{t.name}</span>
                  </button>
                </li>
              ))}
            </ul>
          </aside>

          <section className="min-w-0 flex-1">
            <Tabs defaultValue="data" className="flex h-full min-h-0 flex-col p-2">
              <TabsList>
                <TabsTrigger value="data">{t("sqlite.data")}{active ? `: ${active}` : ""}</TabsTrigger>
                <TabsTrigger value="diagram">{t("sqlite.diagram")}</TabsTrigger>
                <TabsTrigger value="sql">{t("sqlite.sql")}</TabsTrigger>
              </TabsList>

              {/* DATA */}
              <TabsContent value="data" className="mt-2 min-h-0 flex-1 overflow-hidden">
                {loadingData ? (
                  <div className="flex h-full items-center justify-center text-sm text-muted-foreground">{t("sqlite.loading")}</div>
                ) : cols.length > 0 ? (
                  <ScrollArea className="h-full">
                    <table className="w-full text-sm">
                      <thead className="sticky top-0 bg-muted/40">
                        <tr className="text-xs text-muted-foreground">
                          <th className="px-2 py-1.5 text-left font-medium">🗑</th>
                          {cols.map((c, i) => (
                            <th key={i} className="px-2 py-1.5 text-left font-medium">
                              {c}
                              {i === 0 && <span className="text-muted-foreground">{t("sqlite.rowid")}</span>}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {rows.map((row, r) => {
                          if (!row) return null
                          const rowid = Number(row[0])
                          if (deletes.has(rowid)) return null
                          return (
                            <tr key={r} className="border-b border-white/5 last:border-0 hover:bg-accent/40">
                              <td className="px-2 py-1">
                                <button className="text-muted-foreground hover:text-destructive" disabled={saving} title={t("sqlite.deleteRow")} onClick={() => removeExisting(rowid)}>
                                  <Trash2 className="size-3.5" />
                                </button>
                              </td>
                              {row.map((v, c) => {
                                const col = cols[c]
                                const edited = c > 0 ? edits.get(`${rowid}:${col}`) : undefined
                                const value = c > 0 && edited !== undefined ? edited : v
                                const editing = edit && edit.rowid === rowid && edit.col === col
                                return (
                                  <td
                                    key={c}
                                    className="cursor-text whitespace-nowrap px-2 py-1"
                                    onDoubleClick={() => c > 0 && startEdit({rowid, col, value: cellText(value) === "NULL" ? "" : cellText(value)})}
                                  >
                                    {editing ? (
                                      <input
                                        autoFocus
                                        className="bg-input w-24 rounded border px-1 text-sm [color-scheme:dark]"
                                        value={edit!.value}
                                        onChange={(e) => setEdit({...edit!, value: e.target.value})}
                                        onBlur={commitEdit}
                                        onKeyDown={(e) => {
                                          if (e.key === "Enter") commitEdit()
                                          if (e.key === "Escape") setEdit(null)
                                        }}
                                      />
                                    ) : (
                                      <span className={c > 0 && edited !== undefined ? "text-sky-400" : v === null ? "text-muted-foreground italic" : ""}>
                                        {cellText(value)}
                                      </span>
                                    )}
                                  </td>
                                )
                              })}
                            </tr>
                          )
                        })}
                        {/* Pending (new) rows — fully editable. */}
                        {pending.map((p) => (
                          <tr key={p.tempId} className="border-b border-white/5 bg-emerald-500/5">
                            <td className="px-2 py-1">
                              <button className="text-muted-foreground hover:text-destructive" disabled={saving} title={t("sqlite.removeRow")} onClick={() => removePending(p.tempId)}>
                                <Trash2 className="size-3.5" />
                              </button>
                            </td>
                            {cols.map((col, c) => {
                              const editing = edit && edit.tempId === p.tempId && edit.col === col
                              const value = c > 0 ? p.cells[col] ?? "" : t("sqlite.newRow")
                              return (
                                <td
                                  key={c}
                                  className="cursor-text whitespace-nowrap px-2 py-1"
                                  onDoubleClick={() => c > 0 && startEdit({tempId: p.tempId, col, value: p.cells[col] ?? ""})}
                                >
                                  {c === 0 ? (
                                    <span className="text-muted-foreground italic">{t("sqlite.newRow")}</span>
                                  ) : editing ? (
                                    <input
                                      autoFocus
                                      className="bg-input w-24 rounded border px-1 text-sm [color-scheme:dark]"
                                      value={edit!.value}
                                      onChange={(e) => setEdit({...edit!, value: e.target.value})}
                                      onBlur={commitEdit}
                                      onKeyDown={(e) => {
                                        if (e.key === "Enter") commitEdit()
                                        if (e.key === "Escape") setEdit(null)
                                      }}
                                    />
                                  ) : (
                                    <span className="text-emerald-400">{value || "—"}</span>
                                  )}
                                </td>
                              )
                            })}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </ScrollArea>
                ) : (
                  <div className="flex h-full items-center justify-center text-sm text-muted-foreground">{t("sqlite.emptyTable")}</div>
                )}
              </TabsContent>

              {/* DIAGRAM */}
              <TabsContent value="diagram" className="mt-2 min-h-0 flex-1 overflow-hidden">
                <div className="h-full w-full">
                  {schema.length > 0 ? (
                    <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} fitView colorMode="dark">
                      <Background />
                      <Controls showInteractive={false} />
                    </ReactFlow>
                  ) : (
                    <div className="flex h-full items-center justify-center text-sm text-muted-foreground">{t("sqlite.noTables")}</div>
                  )}
                </div>
              </TabsContent>

              {/* SQL */}
              <TabsContent value="sql" className="mt-2 min-h-0 flex-1 overflow-hidden">
                <SqlConsole remotePath={remotePath} schema={schema} value={sql} onChange={setSql} onRun={runSql} running={running} result={sqlResult} />
              </TabsContent>
            </Tabs>
          </section>
        </div>
      )}

      <AlertDialog open={confirmClose} onOpenChange={(v) => !v && setConfirmClose(false)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("sqlite.saveBeforeClosing")}</AlertDialogTitle>
            <AlertDialogDescription>{t("sqlite.saveBeforeClosingDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmClose(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="destructive" onClick={discardAndClose} disabled={saving}>
              {t("sqlite.discard")}
            </Button>
            <Button onClick={saveAndClose}>{t("sqlite.save")}</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

// --- Monaco SQL console with autocomplete ---

interface MonacoT {
  KeyMod: {CtrlCmd: number; chord?: unknown}
  KeyCode: {Enter: number}
  languages: {
    registerCompletionItemProvider(lang: string, p: unknown): {dispose(): unknown}
  }
}
interface WordAtPos {
  word: string
  startColumn: number
  endColumn: number
}
interface TextModelT {
  getValueInRange(r: unknown): string
  getWordUntilPosition(p: {lineNumber: number; column: number}): WordAtPos
}
interface EditorT {
  addCommand(kb: number, handler: () => void): void
  getValue(): string
}

const SQL_KEYWORDS: {label: string; insert: string; snippet?: boolean}[] = [
  {label: "SELECT", insert: "SELECT"},
  {label: "SELECT DISTINCT", insert: "SELECT DISTINCT "},
  {label: "FROM", insert: "FROM"},
  {label: "WHERE", insert: "WHERE"},
  {label: "AS", insert: "AS "},
  {label: "INSERT INTO", insert: "INSERT INTO ${1:table} (${2:cols}) VALUES (${3:vals})", snippet: true},
  {label: "UPDATE", insert: "UPDATE ${1:table} SET ${2:col} = ${3:val} WHERE ${4:rowid} = ${5:0}", snippet: true},
  {label: "DELETE FROM", insert: "DELETE FROM ${1:table} WHERE ${2:rowid} = ${3:0}", snippet: true},
  {label: "JOIN", insert: "JOIN"},
  {label: "INNER JOIN", insert: "INNER JOIN"},
  {label: "LEFT JOIN", insert: "LEFT JOIN"},
  {label: "RIGHT JOIN", insert: "RIGHT JOIN"},
  {label: "ON", insert: "ON "},
  {label: "GROUP BY", insert: "GROUP BY"},
  {label: "HAVING", insert: "HAVING"},
  {label: "ORDER BY", insert: "ORDER BY"},
  {label: "ASC", insert: "ASC"},
  {label: "DESC", insert: "DESC"},
  {label: "LIMIT", insert: "LIMIT"},
  {label: "OFFSET", insert: "OFFSET"},
  {label: "AND", insert: "AND"},
  {label: "OR", insert: "OR"},
  {label: "NOT", insert: "NOT"},
  {label: "IN", insert: "IN"},
  {label: "LIKE", insert: "LIKE"},
  {label: "BETWEEN", insert: "BETWEEN"},
  {label: "IS NULL", insert: "IS NULL"},
  {label: "IS NOT NULL", insert: "IS NOT NULL"},
  {label: "DISTINCT", insert: "DISTINCT"},
  {label: "COUNT", insert: "COUNT($0)", snippet: true},
  {label: "SUM", insert: "SUM($0)", snippet: true},
  {label: "AVG", insert: "AVG($0)", snippet: true},
  {label: "MIN", insert: "MIN($0)", snippet: true},
  {label: "MAX", insert: "MAX($0)", snippet: true},
  {label: "CREATE TABLE", insert: "CREATE TABLE ${1:name} (\n\t${2:col} ${3:TYPE}\n)", snippet: true},
  {label: "DROP TABLE", insert: "DROP TABLE ${1:name}", snippet: true},
  {label: "ALTER TABLE", insert: "ALTER TABLE ${1:name}", snippet: true},
  {label: "PRIMARY KEY", insert: "PRIMARY KEY"},
  {label: "NOT NULL", insert: "NOT NULL"},
  {label: "AUTOINCREMENT", insert: "AUTOINCREMENT"},
  {label: "PRAGMA", insert: "PRAGMA "},
]

function SqlConsole({
  remotePath,
  schema,
  value,
  onChange,
  onRun,
  running,
  result,
}: {
  remotePath: string
  schema: SqliteSchema[]
  value: string
  onChange: (v: string) => void
  onRun: () => void
  running: boolean
  result: SqliteResult | null
}) {
  const {t} = useLanguage()
  const schemaRef = useRef(schema)
  schemaRef.current = schema
  const providerRef = useRef<{dispose(): unknown} | null>(null)
  const runRef = useRef(onRun)
  runRef.current = onRun

  const handleMount: OnMount = useCallback((editor, monaco) => {
    const m = monaco as unknown as MonacoT
    const ed = editor as unknown as EditorT
    ed.addCommand(m.KeyMod.CtrlCmd | m.KeyCode.Enter, () => runRef.current())

    // Completion provider: keywords + tables + columns with context hints.
    providerRef.current = m.languages.registerCompletionItemProvider("sql", {
      triggerCharacters: [" ", "."],
      provideCompletionItems(model: TextModelT, position: {lineNumber: number; column: number}) {
        const textUntil = model.getValueInRange({
          startLineNumber: 1,
          startColumn: 1,
          endLineNumber: position.lineNumber,
          endColumn: position.column,
        })
        const before = textUntil.toUpperCase()
        const sc = schemaRef.current
        const tables = sc.map((t) => t.name)
        const allCols = Array.from(new Set(sc.flatMap((t) => (t.columns ?? []).map((c) => c.name))))

        const wordBefore = before.match(/([A-Za-z_][\w]*)\s*\.?\s*$/)
        const afterDot = /\.\s*$/.test(before)
        const afterTableWord = /(?:FROM|JOIN|INTO|UPDATE|TABLE)\s+[\w]*$/i.test(before)

        // Word range so Monaco filters/inserts correctly (zero-width ranges drop
        // keyword suggestions when a partial word precedes the cursor).
        const word = model.getWordUntilPosition(position)
        const range = {
          startLineNumber: position.lineNumber,
          startColumn: word.startColumn,
          endLineNumber: position.lineNumber,
          endColumn: word.endColumn,
        }
        const suggestions: unknown[] = []

        if (afterDot && wordBefore) {
          const tbl = wordBefore[1].toLowerCase()
          const t = sc.find((x) => x.name.toLowerCase() === tbl)
          if (t) {
            (t.columns ?? []).forEach((c) => suggestions.push({label: c.name, kind: 5, sortText: "1" + c.name, insertText: c.name, range}))
            return {suggestions}
          }
        }

        SQL_KEYWORDS.forEach((k) =>
          suggestions.push({
            label: k.label,
            kind: 14, // Keyword
            sortText: "0" + k.label,
            insertText: k.insert,
            insertTextRules: k.snippet ? 4 : 0,
            range,
          }),
        )
        const tblKind = afterTableWord ? 25 /* Class/Module-ish, ranked high */ : 9
        tables.forEach((t) => suggestions.push({label: t, kind: tblKind, sortText: "2" + t, insertText: t, range}))
        allCols.forEach((c) => suggestions.push({label: c, kind: 5 /* Field */, sortText: "3" + c, insertText: c, range}))
        return {suggestions}
      },
    })
  }, [])

  useEffect(() => {
    return () => {
      providerRef.current?.dispose()
    }
  }, [])

  void remotePath // (window identity only)

  const resCols = result?.columns ?? []
  return (
    <div className="flex h-full flex-col gap-2">
      <div className="flex gap-2">
        <div className="min-h-[80px] flex-1 overflow-hidden rounded border">
          <Editor
            theme="vs-dark"
            language="sql"
            value={value}
            onChange={(v) => onChange(v ?? "")}
            onMount={handleMount}
            options={{
              minimap: {enabled: false},
              scrollBeyondLastLine: false,
              fontLigatures: true,
              fontSize: 13,
              automaticLayout: true,
              fixedOverflowWidgets: true,
              suggest: {showWords: false},
            }}
          />
        </div>
        <Button size="sm" onClick={onRun} disabled={running || !value.trim()}>
          {running ? <Loader2 className="size-4 animate-spin" /> : t("sqlite.run")}
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        {result && (
          <ScrollArea className="h-full">
            {resCols.length > 0 ? (
              <table className="w-full text-sm">
                <thead className="sticky top-0 bg-muted/40">
                  <tr className="text-xs text-muted-foreground">
                    {resCols.map((c, i) => (
                      <th key={i} className="px-2 py-1.5 text-left font-medium">
                        {c}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {(result.rows ?? []).map((row, r) => (
                    <tr key={r} className="border-b border-white/5 last:border-0">
                      {(row ?? []).map((v, c) => (
                        <td key={c} className="whitespace-nowrap px-2 py-1">
                          {cellText(v)}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="p-2 text-xs text-muted-foreground">{t("sqlite.rowsAffected", {count: result.rowsAffected})}</p>
            )}
          </ScrollArea>
        )}
      </div>
    </div>
  )
}
