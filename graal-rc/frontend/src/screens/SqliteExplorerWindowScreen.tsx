// SqliteExplorerWindowScreen is the SQLite explorer for a remote .db (opened via
// App.OpenRemoteFile on a .db double-click, URL "/#sqlite?p=<remotePath>"). The
// file was downloaded to a local cache file that modernc edits in place; this UI
// browses tables, edits cells inline (via UPDATE ... WHERE rowid), runs arbitrary
// SQL, and on Save re-uploads the file (best-effort version-preserved in Go).
import {useCallback, useEffect, useRef, useState} from "react"
import {Loader2, Plus, Save, Trash2} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {rcService} from "@/services/rcService"
import type {SqliteResult, SqliteTable} from "@/types"

function parsePath(): string {
  const hash = typeof window !== "undefined" ? window.location.hash : ""
  const q = hash.indexOf("?")
  if (q < 0) return ""
  return new URLSearchParams(hash.slice(q + 1)).get("p") ?? ""
}

// coerceValue turns an edited cell's text into a JSON-friendly value: integers
// and floats stay numeric (so REAL/INTEGER affinity is respected), everything
// else is text. NULL is set via the SQL console.
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

export function SqliteExplorerWindowScreen() {
  const remotePath = useRef(parsePath()).current
  const [tables, setTables] = useState<SqliteTable[]>([])
  const [active, setActive] = useState<string | null>(null)
  const [data, setData] = useState<SqliteResult | null>(null)
  const [loadingTables, setLoadingTables] = useState(true)
  const [loadingData, setLoadingData] = useState(false)
  const [edit, setEdit] = useState<{r: number; c: number; value: string} | null>(null)
  const [sql, setSql] = useState("")
  const [sqlResult, setSqlResult] = useState<SqliteResult | null>(null)
  const [running, setRunning] = useState(false)

  const baseName = remotePath.includes("/") ? remotePath.slice(remotePath.lastIndexOf("/") + 1) : remotePath

  const loadTables = useCallback(async () => {
    setLoadingTables(true)
    try {
      const info = await rcService.getSqliteInfo(remotePath)
      setTables(info?.tables ?? [])
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

  const commitEdit = useCallback(async () => {
    if (!edit || !data) return
    const rows = data.rows ?? []
    const cols = data.columns ?? []
    const rowid = rows[edit.r]?.[0]
    const column = cols[edit.c]
    if (rowid === undefined || !column || !active) return
    try {
      await rcService.sqliteUpdateCell(remotePath, active, column, Number(rowid), coerceValue(edit.value))
      setEdit(null)
      await loadData()
    } catch (err) {
      toast.error("Edit failed", {description: String(err)})
    }
  }, [edit, data, active, remotePath, loadData])

  const addRow = useCallback(async () => {
    if (!active) return
    try {
      await rcService.sqliteInsertRow(remotePath, active)
      await loadData()
    } catch (err) {
      toast.error("Insert failed", {description: String(err)})
    }
  }, [active, remotePath, loadData])

  const deleteRow = useCallback(
    async (rowid: number) => {
      if (!active) return
      try {
        await rcService.sqliteDeleteRow(remotePath, active, rowid)
        await loadData()
      } catch (err) {
        toast.error("Delete failed", {description: String(err)})
      }
    },
    [active, remotePath, loadData],
  )

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

  const save = useCallback(async () => {
    try {
      await rcService.saveSqliteFile(remotePath)
      toast.success("Uploaded")
    } catch (err) {
      toast.error("Save failed", {description: String(err)})
    }
  }, [remotePath])

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2">
        <h1 className="text-sm font-semibold">{baseName || "Database"}</h1>
        <span className="text-xs text-muted-foreground">SQLite explorer</span>
        <div className="ml-auto flex items-center gap-2">
          {active && (
            <Button size="sm" variant="outline" onClick={addRow} title="Insert row">
              <Plus />
              Row
            </Button>
          )}
          <Button size="sm" onClick={save} title="Re-upload (version-preserved)">
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
                <TabsTrigger value="sql">SQL</TabsTrigger>
              </TabsList>

              <TabsContent value="data" className="mt-2 min-h-0 flex-1 overflow-hidden">
                {loadingData ? (
                  <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Loading…</div>
                ) : data && (data.columns ?? []).length > 0 ? (
                  <ScrollArea className="h-full">
                    <table className="w-full text-sm">
                      <thead className="sticky top-0 bg-muted/40">
                        <tr className="text-xs text-muted-foreground">
                          <th className="px-2 py-1.5 text-left font-medium">🗑</th>
                          {(data.columns ?? []).map((c, i) => (
                            <th key={i} className="px-2 py-1.5 text-left font-medium">
                              {c}
                            </th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {(data.rows ?? []).map((row, r) => (
                          <tr key={r} className="border-b border-white/5 last:border-0 hover:bg-accent/40">
                            <td className="px-2 py-1">
                              <button
                                className="text-muted-foreground hover:text-destructive"
                                title="Delete row"
                                onClick={() => deleteRow(Number((row ?? [])[0]))}
                              >
                                <Trash2 className="size-3.5" />
                              </button>
                            </td>
                            {(row ?? []).map((v, c) => (
                              <td
                                key={c}
                                className="cursor-text whitespace-nowrap px-2 py-1"
                                onDoubleClick={() => c > 0 && setEdit({r, c, value: cellText(v) === "NULL" ? "" : cellText(v)})}
                              >
                                {edit && edit.r === r && edit.c === c ? (
                                  <input
                                    autoFocus
                                    className="bg-input w-24 rounded border px-1 text-sm [color-scheme:dark]"
                                    value={edit.value}
                                    onChange={(e) => setEdit({...edit, value: e.target.value})}
                                    onBlur={commitEdit}
                                    onKeyDown={(e) => {
                                      if (e.key === "Enter") commitEdit()
                                      if (e.key === "Escape") setEdit(null)
                                    }}
                                  />
                                ) : (
                                  <span className={v === null ? "text-muted-foreground italic" : ""}>{cellText(v)}</span>
                                )}
                              </td>
                            ))}
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </ScrollArea>
                ) : (
                  <div className="flex h-full items-center justify-center text-sm text-muted-foreground">Empty table.</div>
                )}
              </TabsContent>

              <TabsContent value="sql" className="mt-2 min-h-0 flex-1 overflow-hidden">
                <div className="flex h-full flex-col gap-2">
                  <div className="flex gap-2">
                    <textarea
                      className="bg-input min-h-[72px] flex-1 rounded border p-2 font-mono text-xs [color-scheme:dark]"
                      placeholder="SELECT * FROM …  — or  UPDATE / INSERT / DELETE / CREATE"
                      value={sql}
                      onChange={(e) => setSql(e.target.value)}
                    />
                    <Button size="sm" onClick={runSql} disabled={running || !sql.trim()}>
                      {running ? <Loader2 className="size-4 animate-spin" /> : "Run"}
                    </Button>
                  </div>
                  <div className="min-h-0 flex-1">
                    {sqlResult && (
                      <ScrollArea className="h-full">
                        {(sqlResult.columns ?? []).length > 0 ? (
                          <table className="w-full text-sm">
                            <thead className="sticky top-0 bg-muted/40">
                              <tr className="text-xs text-muted-foreground">
                                {(sqlResult.columns ?? []).map((c, i) => (
                                  <th key={i} className="px-2 py-1.5 text-left font-medium">
                                    {c}
                                  </th>
                                ))}
                              </tr>
                            </thead>
                            <tbody>
                              {(sqlResult.rows ?? []).map((row, r) => (
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
                          <p className="p-2 text-xs text-muted-foreground">{sqlResult.rowsAffected} row(s) affected.</p>
                        )}
                      </ScrollArea>
                    )}
                  </div>
                </div>
              </TabsContent>
            </Tabs>
          </section>
        </div>
      )}
    </div>
  )
}
