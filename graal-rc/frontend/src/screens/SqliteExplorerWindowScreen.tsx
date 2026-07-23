// SqliteExplorerWindowScreen is the SQLite explorer for a remote .db (opened via
// App.OpenRemoteFile on a .db double-click, URL "/#sqlite?p=<remotePath>"). The
// file is downloaded to a local cache that modernc edits in place.
//
// Editing is DEFERRED: cell edits, inserts (+Row), and deletes are staged in a
// frontend overlay and applied as ONE transaction on Save (atomic; a failing
// statement rolls back, nothing is written). Discard clears the overlay. Three
// tabs: Data (inline-editable grid), Diagram (reactflow ER overview), SQL
// (Monaco console with keyword/table/column autocomplete).
import {useCallback, useEffect, useMemo, useRef, useState} from "react"
import Editor, {type OnMount} from "@monaco-editor/react"
import {Events} from "@wailsio/runtime"
import {toast} from "sonner"
import {Loader2, Plus, RotateCcw, Save, Trash2} from "lucide-react"
import {ReactFlow, Background, Controls, MiniMap, type Node, type Edge} from "@xyflow/react"
import "@xyflow/react/dist/style.css"

import {AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogHeader, AlertDialogTitle} from "@/components/ui/alert-dialog"
import {Button} from "@/components/ui/button"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {rcService} from "@/services/rcService"
import type {SqliteChanges, SqliteResult, SqliteSchema, SqliteTable} from "@/types"

const KIND = "sqlite"

function parsePath(): string {
  const hash = typeof window !== "undefined" ? window.location.hash : ""
  const q = hash.indexOf("?")
  if (q < 0) return ""
  return new URLSearchParams(hash.slice(q + 1)).get("p") ?? ""
}

// coerceValue: edited cell text → JSON value (number when numeric, else text).
function coerceValue(text: string): unknown {
  if (text === "") return ""
  if (/^-?\d+$/.test(text)) return Number(text)
  if (/^-?\d+\.\d+$/.test(text)) return Number(text)
  return text
}

function cellText(v: unknown): string {
  if (v === null || v === undefined) return "NULL"
  if (typeof v === "string") return v.startsWith("base64:") ? "(blob)" : v
  return String(v)
}

interface PendingRow {
  tempId: string
  cells: Record<string, string>
}

// --- reactflow table card node ---
function TableNode({data}: {data: Record<string, unknown>}) {
  const t = data as unknown as SqliteSchema
  return (
    <div className="bg-popover w-52 overflow-hidden rounded-md border text-xs shadow-lg">
      <div className="bg-muted/60 px-2 py-1 font-semibold">{t.name}</div>
      <div>
        {(t.columns ?? []).map((c) => (
          <div key={c.name} className="flex items-center gap-1 border-t border-white/5 px-2 py-0.5">
            {c.pk && <span className="text-amber-400" title="primary key">★</span>}
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
  const remotePath = useRef(parsePath()).current
  const [tables, setTables] = useState<SqliteTable[]>([])
  const [schema, setSchema] = useState<SqliteSchema[]>([])
  const [active, setActive] = useState<string | null>(null)
  const [data, setData] = useState<SqliteResult | null>(null)
  const [loadingTables, setLoadingTables] = useState(true)
  const [loadingData, setLoadingData] = useState(false)

  // Deferred overlay.
  const [edits, setEdits] = useState<Map<string, unknown>>(new Map())
  const [deletes, setDeletes] = useState<Set<number>>(new Set())
  const [pending, setPending] = useState<PendingRow[]>([])
  const [edit, setEdit] = useState<{rowid?: number; tempId?: string; col: string; value: string} | null>(null)

  const [sql, setSql] = useState("")
  const [sqlResult, setSqlResult] = useState<SqliteResult | null>(null)
  const [running, setRunning] = useState(false)
  const [confirmClose, setConfirmClose] = useState(false)

  const dirty = edits.size > 0 || deletes.size > 0 || pending.length > 0

  const baseName = remotePath.includes("/") ? remotePath.slice(remotePath.lastIndexOf("/") + 1) : remotePath
  const cols = data?.columns ?? []
  const rows = data?.rows ?? []

  const loadTables = useCallback(async () => {
    setLoadingTables(true)
    try {
      const info = await rcService.getSqliteInfo(remotePath)
      setTables(info?.tables ?? [])
      const sc = await rcService.getSqliteSchema(remotePath).catch(() => null)
      setSchema(sc ?? [])
      if (!active && info?.tables?.length) setActive(info.tables[0].name)
    } catch (err) {
      toast.error("Could not read database", {description: String(err)})
    } finally {
      setLoadingTables(false)
    }
  }, [remotePath, active])

  const loadData = useCallback(async () => {
    if (!active) return
    setLoadingData(true)
    setEdit(null)
    try {
      const res = await rcService.sqliteQuery(remotePath, `SELECT rowid, * FROM "${active}" LIMIT 200`, [])
      setData(res)
    } catch (err) {
      toast.error("Query failed", {description: String(err)})
    } finally {
      setLoadingData(false)
    }
  }, [remotePath, active])

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

  const discardAll = useCallback(() => {
    setEdits(new Map())
    setDeletes(new Set())
    setPending([])
  }, [])

  // Commit one cell edit into the overlay (no DB write).
  const commitEdit = useCallback(() => {
    if (!edit) return
    if (edit.rowid !== undefined) {
      setEdits((m) => {
        const next = new Map(m)
        next.set(`${edit.rowid}:${edit.col}`, coerceValue(edit.value))
        return next
      })
    } else if (edit.tempId) {
      setPending((rows) => rows.map((r) => (r.tempId === edit.tempId ? {...r, cells: {...r.cells, [edit.col]: edit.value}} : r)))
    }
    setEdit(null)
  }, [edit])

  const addRow = useCallback(() => {
    setPending((rows) => [...rows, {tempId: `t${Date.now()}`, cells: {}}])
  }, [])

  const removeExisting = useCallback((rowid: number) => {
    setDeletes((s) => new Set(s).add(rowid))
  }, [])
  const removePending = useCallback((tempId: string) => {
    setPending((rows) => rows.filter((r) => r.tempId !== tempId))
  }, [])

  const save = useCallback(async () => {
    if (!active || !dirty) return
    const changes: SqliteChanges = {
      table: active,
      inserts: pending.map((r) => {
        const cells: Record<string, unknown> = {}
        for (const [k, v] of Object.entries(r.cells)) cells[k] = coerceValue(v)
        return cells
      }),
      updates: [...edits.entries()].map(([k, v]) => {
        const [rowid, col] = k.split(":")
        return {rowid: Number(rowid), column: col, value: v}
      }),
      deletes: [...deletes],
    }
    try {
      await rcService.commitSqlite(remotePath, changes)
      discardAll()
      await loadData()
      toast.success("Saved")
    } catch (err) {
      toast.error("Save failed", {description: String(err)})
    }
  }, [active, dirty, pending, edits, deletes, remotePath, discardAll, loadData])

  const saveAndClose = useCallback(async () => {
    setConfirmClose(false)
    await save()
  }, [save])

  const discardAndClose = useCallback(() => {
    setConfirmClose(false)
    discardAll()
    rcService.setEditorDirty(KIND, remotePath, false).catch(() => {})
    rcService.closeScriptEditor(KIND, remotePath).catch(() => {})
  }, [remotePath, discardAll])

  const runSql = useCallback(async () => {
    setRunning(true)
    try {
      const res = await rcService.sqliteQuery(remotePath, sql.trim(), [])
      setSqlResult(res)
    } catch (err) {
      toast.error("Query failed", {description: String(err)})
    } finally {
      setRunning(false)
    }
  }, [remotePath, sql])

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
        <h1 className="text-sm font-semibold">{baseName || "Database"}</h1>
        <span className="text-xs text-muted-foreground">SQLite explorer</span>
        {dirty && <span className="text-xs text-amber-500">• unsaved</span>}
        <div className="ml-auto flex items-center gap-2">
          {dirty && (
            <Button size="sm" variant="ghost" onClick={discardAll} title="Discard staged changes">
              <RotateCcw />
              Discard
            </Button>
          )}
          {active && (
            <Button size="sm" variant="outline" onClick={addRow} title="Add a pending row">
              <Plus />
              Row
            </Button>
          )}
          <Button size="sm" onClick={save} disabled={!dirty} title="Apply staged changes and upload (version-preserved)">
            <Save />
            Save
          </Button>
        </div>
      </header>
      <p className="border-b bg-amber-500/10 px-4 py-1 text-xs text-amber-200">
        Best-effort version preservation — old SQLite reads new files, but not byte-identical.
      </p>

      {loadingTables ? (
        <div className="flex flex-1 items-center justify-center gap-2 text-sm text-muted-foreground">
          <Loader2 className="size-4 animate-spin" /> Opening database…
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
                    onClick={() => setActive(t.name)}
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
                <TabsTrigger value="data">Data{active ? `: ${active}` : ""}</TabsTrigger>
                <TabsTrigger value="diagram">Diagram</TabsTrigger>
                <TabsTrigger value="sql">SQL</TabsTrigger>
              </TabsList>

              {/* DATA */}
              <TabsContent value="data" className="mt-2 min-h-0 flex-1 overflow-hidden">
                {loadingData ? (
                  <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading…</div>
                ) : cols.length > 0 ? (
                  <ScrollArea className="h-full">
                    <table className="w-full text-sm">
                      <thead className="sticky top-0 bg-muted/40">
                        <tr className="text-xs text-muted-foreground">
                          <th className="px-2 py-1.5 text-left font-medium">🗑</th>
                          {cols.map((c, i) => (
                            <th key={i} className="px-2 py-1.5 text-left font-medium">
                              {c}
                              {i === 0 && <span className="text-muted-foreground"> (rowid)</span>}
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
                                <button className="text-muted-foreground hover:text-destructive" title="Delete row" onClick={() => removeExisting(rowid)}>
                                  <Trash2 className="size-3.5" />
                                </button>
                              </td>
                              {row.map((v, c) => {
                                const col = cols[c]
                                const edited = c > 0 ? edits.get(`${rowid}:${col}`) : undefined
                                const shown = c > 0 && edited !== undefined ? edited : v
                                const editing = edit && edit.rowid === rowid && edit.col === col
                                return (
                                  <td
                                    key={c}
                                    className="cursor-text whitespace-nowrap px-2 py-1"
                                    onDoubleClick={() => c > 0 && setEdit({rowid, col, value: cellText(shown) === "NULL" ? "" : cellText(shown)})}
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
                                        {cellText(shown)}
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
                              <button className="text-muted-foreground hover:text-destructive" title="Remove row" onClick={() => removePending(p.tempId)}>
                                <Trash2 className="size-3.5" />
                              </button>
                            </td>
                            {cols.map((col, c) => {
                              const editing = edit && edit.tempId === p.tempId && edit.col === col
                              const shown = c > 0 ? p.cells[col] ?? "" : "(new)"
                              return (
                                <td
                                  key={c}
                                  className="cursor-text whitespace-nowrap px-2 py-1"
                                  onDoubleClick={() => c > 0 && setEdit({tempId: p.tempId, col, value: p.cells[col] ?? ""})}
                                >
                                  {c === 0 ? (
                                    <span className="text-muted-foreground italic">(new)</span>
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
                                    <span className="text-emerald-400">{shown || "—"}</span>
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
                  <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Empty table.</div>
                )}
              </TabsContent>

              {/* DIAGRAM */}
              <TabsContent value="diagram" className="mt-2 min-h-0 flex-1 overflow-hidden">
                <div className="h-full w-full">
                  {schema.length > 0 ? (
                    <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypes} fitView colorMode="dark">
                      <Background />
                      <Controls showInteractive={false} />
                      <MiniMap
                        pannable
                        zoomable
                        bgColor="#0a0a0a"
                        nodeColor="#22d3ee"
                        nodeStrokeColor="#0e7490"
                        nodeStrokeWidth={2}
                        nodeBorderRadius={4}
                        maskColor="rgb(0 0 0 / 0.7)"
                      />
                    </ReactFlow>
                  ) : (
                    <div className="flex h-full items-center justify-center text-sm text-muted-foreground">No tables.</div>
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
            <AlertDialogTitle>Save before closing?</AlertDialogTitle>
            <AlertDialogDescription>
              This database has unsaved staged changes. Save them before the window closes, or discard them.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setConfirmClose(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={discardAndClose}>
              Discard
            </Button>
            <Button onClick={saveAndClose}>Save</Button>
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
              fontSize: 13,
              automaticLayout: true,
              fixedOverflowWidgets: true,
              suggest: {showWords: false},
            }}
          />
        </div>
        <Button size="sm" onClick={onRun} disabled={running || !value.trim()}>
          {running ? <Loader2 className="size-4 animate-spin" /> : "Run"}
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
              <p className="p-2 text-xs text-muted-foreground">{result.rowsAffected} row(s) affected.</p>
            )}
          </ScrollArea>
        )}
      </div>
    </div>
  )
}
