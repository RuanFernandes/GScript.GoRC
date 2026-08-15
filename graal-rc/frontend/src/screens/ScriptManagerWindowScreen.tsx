// ScriptManagerWindowScreen is the content of the external "Script Manager"
// window (opened via App.OpenScriptManager, URL "/#scripts"). Three tabs —
// Weapons, Classes, NPCs — each a searchable list with Refresh / Add / Delete
// (and for NPCs: Reset / Edit Flags / View Attributes). Double-clicking a row
// opens that script in its own editor window.
import {useEffect, useMemo, useRef, useState} from "react"
import {toast} from "sonner"
import {Events} from "@wailsio/runtime"
import {CircleAlert, CircleCheck, CircleOff, Flag, LocateFixed, Loader2, RotateCcw, UserRound} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Badge} from "@/components/ui/badge"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {Label} from "@/components/ui/label"
import {Skeleton} from "@/components/ui/skeleton"
import {useScriptLists} from "@/hooks/useScriptLists"
import {scriptCompare} from "@/lib/scriptSort"
import {isUsableScriptName} from "@/lib/scriptName"
import {rcService} from "@/services/rcService"
import type {NPC} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"
import {useSync} from "@/hooks/useSync"
import {ScriptSyncRequiredDialog} from "@/components/ScriptSyncRequiredDialog"

// openScriptEditorOrFail opens the editor; OpenScriptEditor fetches the script
// server-side first and only opens a window on success. A failure (e.g. the
// account lacks read permission and the server never replies → timeout) cancels
// the open and surfaces a toast instead. A mandatory-sync rejection gets a
// blocking translated prompt so the user cannot miss why editing is locked.
async function openScriptEditorOrFail(
  scriptType: string,
  key: string,
  t: (key: string, vars?: Record<string, string | number>) => string,
  onSyncRequired: () => void,
) {
  try {
    await rcService.openScriptEditor(scriptType, key)
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    if (msg.toLowerCase().includes("script sync is required")) {
      onSyncRequired()
      return
    }
    toast.error(t("scripts.openFailed"), {
      description: msg.includes("timed out")
        ? t("scripts.noPermission")
        : msg,
    })
  }
}

function LspStatusBadge() {
  const {t} = useLanguage()
  const {config, status, loaded} = useSync()
  const [requestState, setRequestState] = useState<"ready" | "error" | null>(null)
  const [requestError, setRequestError] = useState("")
  const configured = (status.enabled && Boolean(status.outputDir?.trim())) || (config.enabled && Boolean(config.outputDir?.trim()))
  const hasError = Boolean(status.permissionsError)
  const starting = configured && !hasError && (!status.permissionsReady || Boolean(status.progress?.active))
  useEffect(() => {
    const off = Events.On("rc:lspStatus", (event: {data: string}) => {
      try {
        const payload = JSON.parse(event.data) as {state?: "ready" | "error"; message?: string}
        if (payload.state !== "ready" && payload.state !== "error") return
        setRequestState(payload.state)
        setRequestError(payload.message ?? "")
      } catch {
        // Ignore malformed cross-window status events.
      }
    })
    return off
  }, [])

  useEffect(() => {
    if (!configured) {
      setRequestState(null)
      setRequestError("")
    }
  }, [configured])

  const state = !loaded ? "starting" : hasError || requestState === "error" ? "error" : !configured ? "disabled" : starting ? "starting" : "ready"
  let details = t("scripts.lspReady")
  if (!loaded || starting) details = t("scripts.lspStarting")
  else if (hasError || requestState === "error") details = t("scripts.lspError")
  else if (!configured) details = t("scripts.lspDisabled")
  const Icon = state === "ready" ? CircleCheck : state === "error" ? CircleAlert : state === "disabled" ? CircleOff : Loader2
  const tone = state === "ready"
    ? "border-emerald-500/30 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
    : state === "error"
      ? "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300"
      : state === "starting"
        ? "border-primary/30 bg-primary/10 text-primary"
        : "text-muted-foreground"

  return (
    <Badge variant="outline" className={`max-w-full ${tone}`} title={requestError || details} aria-label={details}>
      <Icon className={state === "starting" ? "animate-spin" : undefined} />
      <span className="truncate">{details}</span>
    </Badge>
  )
}

export function ScriptManagerWindowScreen() {
  const {t} = useLanguage()
  const [onlyReadable, setOnlyReadable] = useState(false)
  const [syncPromptOpen, setSyncPromptOpen] = useState(false)
  const lists = useScriptLists(rcService, onlyReadable)
  const [tab, setTab] = useState<"weapons" | "classes" | "npcs">("weapons")
  const weaponRows = useMemo(() => lists.weapons.map((weapon, index) => {
    const name = weapon.name?.trim() ?? ""
    return isUsableScriptName(name)
      ? {key: name, cols: [name]}
      : {key: `__invalid_weapon_${index}`, cols: [t("scripts.invalidWeapon")], disabled: true}
  }), [lists.weapons, t])
  const classRows = useMemo(() => lists.classes.map((scriptClass, index) => {
    const name = scriptClass.name?.trim() ?? ""
    return isUsableScriptName(name)
      ? {key: name, cols: [name]}
      : {key: `__invalid_class_${index}`, cols: [t("scripts.invalidClass")], disabled: true}
  }), [lists.classes, t])

  useEffect(() => {
    if (lists.error) {
      toast.error(t("scripts.refreshFailed"), {description: lists.error})
    }
  }, [lists.error, t])

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-3 py-2.5 sm:px-4">
        <h1 className="min-w-0 text-base font-semibold">{t("scripts.title")}</h1>
        <div className="ml-auto flex min-w-0 flex-wrap items-center justify-end gap-2">
          <LspStatusBadge />
          <label className="text-muted-foreground inline-flex max-w-full cursor-pointer items-center gap-2 text-xs">
          <input
            type="checkbox"
            checked={onlyReadable}
            onChange={(event) => setOnlyReadable(event.target.checked)}
            className="accent-primary"
          />
            <span className="truncate">{t("scripts.onlyReadable")}</span>
          </label>
        </div>
      </header>
      <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)} className="flex min-h-0 flex-1 flex-col p-2 sm:p-3">
        <TabsList>
          <TabsTrigger value="weapons">{t("scripts.weapons")} ({lists.weapons.length})</TabsTrigger>
          <TabsTrigger value="classes">{t("scripts.classes")} ({lists.classes.length})</TabsTrigger>
          <TabsTrigger value="npcs">{t("scripts.npcs")} ({lists.npcs.length})</TabsTrigger>
        </TabsList>
        <TabsContent value="weapons" className="mt-3 min-h-0 flex-1">
          <WeaponClassTab
            kind="weapon"
            rows={weaponRows}
            loading={lists.loading}
            onSyncRequired={() => setSyncPromptOpen(true)}
            onRefresh={async () => {
              try {
                await rcService.refreshWeapons()
                await lists.refresh()
                toast.success(t("scripts.refresh"))
              } catch (err) {
                toast.error(t("scripts.refreshFailed"), {description: String(err)})
              }
            }}
          />
        </TabsContent>
        <TabsContent value="classes" className="mt-3 min-h-0 flex-1">
          <WeaponClassTab
            kind="class"
            rows={classRows}
            loading={lists.loading}
            onSyncRequired={() => setSyncPromptOpen(true)}
            onRefresh={lists.refresh}
          />
        </TabsContent>
        <TabsContent value="npcs" className="mt-3 min-h-0 flex-1">
          <NPCTab npcs={lists.npcs} loading={lists.loading} onSyncRequired={() => setSyncPromptOpen(true)} onRefresh={lists.refresh} />
        </TabsContent>
      </Tabs>
      <ScriptSyncRequiredDialog open={syncPromptOpen} onClose={() => setSyncPromptOpen(false)} />
    </div>
  )
}

interface Row {
  key: string
  cols: string[]
  disabled?: boolean
}

// WeaponClassTab handles weapon and class lists (both name-keyed; weapons add an
// Image column). Add takes a single name; delete takes the selected name.
function WeaponClassTab({
  kind,
  rows,
  loading,
  onSyncRequired,
  onRefresh,
}: {
  kind: "weapon" | "class"
  rows: Row[]
  loading: boolean
  onSyncRequired: () => void
  onRefresh: () => Promise<void> | void
}) {
  const {t} = useLanguage()
  const [selected, setSelected] = useState<string | null>(null)
  const [filter, setFilter] = useState("")
  const [adding, setAdding] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [name, setName] = useState("")
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc")
  const openingKeysRef = useRef<Set<string>>(new Set())
  const [openingKeys, setOpeningKeys] = useState<Set<string>>(new Set())

  const filtered = useMemo(() => {
    const f = rows.filter((r) => r.key.toLowerCase().includes(filter.toLowerCase()))
    f.sort((a, b) => {
      const c = scriptCompare(a.key, b.key)
      return sortDir === "asc" ? c : -c
    })
    return f
  }, [rows, filter, sortDir])

  const doAdd = async () => {
    const n = name.trim()
    if (!n) return
    try {
      if (kind === "weapon") await rcService.addWeapon(n)
      else await rcService.addClass(n)
      toast.success(t(kind === "weapon" ? "scripts.weaponAdded" : "scripts.classAdded", {name: n}))
      setName("")
      setAdding(false)
      await onRefresh()
    } catch (err) {
      toast.error(t("scripts.addFailed"), {description: String(err)})
    }
  }

  const openRow = async (key: string) => {
    if (openingKeysRef.current.has(key)) return
    openingKeysRef.current.add(key)
    setOpeningKeys(new Set(openingKeysRef.current))
    try {
      await openScriptEditorOrFail(kind, key, t, onSyncRequired)
    } finally {
      openingKeysRef.current.delete(key)
      setOpeningKeys(new Set(openingKeysRef.current))
    }
  }

  const doDelete = async () => {
    if (!selected) return
    try {
      if (kind === "weapon") await rcService.deleteWeapon(selected)
      else await rcService.deleteClass(selected)
      toast.success(t("scripts.deleted", {name: selected}))
      setSelected(null)
      await onRefresh()
    } catch (err) {
      toast.error(t("scripts.deleteFailed"), {description: String(err)})
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder={t("scripts.filter")}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="w-full min-w-0 sm:max-w-56"
        />
        <div className="ml-auto flex w-full flex-wrap justify-end gap-2 sm:w-auto">
          <Button variant="outline" size="sm" onClick={() => onRefresh()}>
            {t("scripts.refresh")}
          </Button>
          <Button size="sm" onClick={() => setAdding(true)}>
            {kind === "weapon" ? t("scripts.addWeapon") : t("scripts.addClass")}
          </Button>
          <Button
            variant="destructive"
            size="sm"
            disabled={!selected}
            onClick={() => setDeleteOpen(true)}
          >
            {t("scripts.delete")}
          </Button>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1 rounded-md border">
        <table className="w-full min-w-60 text-sm">
          <thead className="bg-muted/50 sticky top-0">
            <tr>
              <th className="px-3 py-2 text-left font-medium">
                <button
                  type="button"
                  className="inline-flex items-center gap-1 hover:text-foreground"
                  onClick={() => setSortDir((d) => (d === "asc" ? "desc" : "asc"))}
                >
                  {t("scripts.name")} {sortDir === "asc" ? "▲" : "▼"}
                </button>
              </th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && !loading && (
              <tr>
                <td className="text-muted-foreground px-3 py-4">{t("scripts.noEntries")}</td>
              </tr>
            )}
            {loading && filtered.length === 0 &&
              Array.from({length: 8}).map((_, i) => (
                <tr key={`sk-${i}`} className="border-b">
                  <td className="px-3 py-1.5">
                    <Skeleton className="h-4" style={{width: `${40 + ((i * 37) % 50)}%`}} />
                  </td>
                </tr>
              ))}
            {filtered.map((r) => {
              const opening = openingKeys.has(r.key)
              return (
                <tr
                  key={r.key}
                  aria-busy={opening}
                  aria-disabled={opening || r.disabled}
                  onClick={() => { if (!opening && !r.disabled) setSelected(r.key) }}
                  onDoubleClick={() => { if (!opening && !r.disabled) void openRow(r.key) }}
                  className={`border-b ${
                    r.disabled
                      ? "cursor-not-allowed opacity-60"
                      : opening
                        ? "cursor-wait opacity-60"
                      : `cursor-pointer ${selected === r.key ? "bg-accent" : "hover:bg-accent/50"}`
                  }`}
                >
                  {r.cols.map((c, i) => (
                    <td key={i} className="px-3 py-1.5">
                      {i === 0 ? (
                        <span className={r.disabled ? "text-muted-foreground italic" : "inline-flex items-center gap-2"}>
                          {opening && <Loader2 className="text-muted-foreground size-3.5 animate-spin" aria-label={t("scripts.loading")} />}
                          {c}
                        </span>
                      ) : c}
                    </td>
                  ))}
                </tr>
              )
            })}
          </tbody>
        </table>
      </ScrollArea>

      <AlertDialog open={adding} onOpenChange={(v) => setAdding(v)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{kind === "weapon" ? t("scripts.addWeapon") : t("scripts.addClass")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("scripts.addDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="add-name">{t("scripts.name")}</Label>
            <Input
              id="add-name"
              value={name}
              autoFocus
              onChange={(e) => setName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") doAdd()
              }}
            />
          </div>
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={() => setAdding(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={doAdd} disabled={!name.trim()}>
              {t("scripts.add")}
            </Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("scripts.confirmDelete")}</AlertDialogTitle>
            <AlertDialogDescription>{t("scripts.confirmDeleteDescription", {name: selected ?? ""})}</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex justify-end gap-2">
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <Button variant="destructive" onClick={() => { setDeleteOpen(false); void doDelete() }}>{t("scripts.delete")}</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

// NPCTab handles the NPC list: id/name/type columns, plus Reset / Edit Flags /
// View Attributes actions on the selected NPC. Add opens a 7-field dialog
// (mirrors the reference client's TNPCList::onAdd).
function NPCTab({
  npcs,
  loading,
  onSyncRequired,
  onRefresh,
}: {
  npcs: NPC[]
  loading: boolean
  onSyncRequired: () => void
  onRefresh: () => Promise<void> | void
}) {
  const {t} = useLanguage()
  const [selected, setSelected] = useState<number | null>(null)
  const [filter, setFilter] = useState("")
  const [adding, setAdding] = useState(false)
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [warping, setWarping] = useState(false)
  const [npcMenu, setNpcMenu] = useState<{npc: NPC; left: number; top: number} | null>(null)
  const [sortKey, setSortKey] = useState<"id" | "name" | "type" | "level">("name")
  const [sortDir, setSortDir] = useState<"asc" | "desc">("asc")

  const filtered = useMemo(() => {
    const q = filter.toLowerCase()
    const f = npcs.filter(
      (n) =>
        (isUsableScriptName(n.name) ? n.name.trim() : t("scripts.invalidNpc", {id: n.id})).toLowerCase().includes(q) ||
        String(n.id).includes(q) ||
        n.type.toLowerCase().includes(q) ||
        (n.level ?? "").toLowerCase().includes(q),
    )
    const dir = sortDir === "asc" ? 1 : -1
    f.sort((a, b) => {
      switch (sortKey) {
        case "id":
          return (a.id - b.id) * dir
        case "name":
          return scriptCompare(a.name, b.name) * dir
        case "type":
          return a.type.localeCompare(b.type, undefined, {sensitivity: "base"}) * dir
        case "level":
          return (a.level ?? "").localeCompare(b.level ?? "", undefined, {sensitivity: "base"}) * dir
      }
    })
    return f
  }, [npcs, filter, sortKey, sortDir, t])

  const toggleSort = (key: "id" | "name" | "type" | "level") => {
    if (sortKey === key) setSortDir((d) => (d === "asc" ? "desc" : "asc"))
    else {
      setSortKey(key)
      setSortDir("asc")
    }
  }

  const selectedNPC = npcs.find((n) => n.id === selected) ?? null
  const selectedNPCUsable = selectedNPC !== null && isUsableScriptName(selectedNPC.name)

  useEffect(() => {
    if (!npcMenu) return
    const close = () => setNpcMenu(null)
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") close() }
    document.addEventListener("mousedown", close)
    document.addEventListener("keydown", escape)
    return () => {
      document.removeEventListener("mousedown", close)
      document.removeEventListener("keydown", escape)
    }
  }, [npcMenu])

  const doDelete = async () => {
    if (selected == null || !selectedNPCUsable) return
    try {
      await rcService.deleteNPC(selected)
      toast.success(t("scripts.npcDeleted", {id: selected}))
      setSelected(null)
      await onRefresh()
    } catch (err) {
      toast.error(t("scripts.deleteFailed"), {description: String(err)})
    }
  }

  const doReset = async (npcID = selected) => {
    if (npcID == null) return
    try {
      await rcService.resetNPC(npcID)
      toast.success(t("scripts.npcReset", {id: npcID}))
    } catch (err) {
      toast.error(t("scripts.resetFailed"), {description: String(err)})
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Input
          placeholder={t("scripts.filter")}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="w-full min-w-0 sm:max-w-56"
        />
        <div className="ml-auto flex w-full flex-wrap justify-end gap-2 sm:w-auto">
          <Button variant="outline" size="sm" onClick={() => onRefresh()}>
            {t("scripts.refresh")}
          </Button>
          <Button size="sm" onClick={() => setAdding(true)}>
            {t("scripts.addNpc")}
          </Button>
          <Button variant="destructive" size="sm" disabled={!selectedNPCUsable} onClick={() => setDeleteOpen(true)}>
            {t("scripts.delete")}
          </Button>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1 rounded-md border">
        <table className="w-full min-w-[30rem] text-sm">
          <thead className="bg-muted/50 sticky top-0">
            <tr>
              {[["id", "scripts.header.id"], ["name", "scripts.header.name"], ["type", "scripts.header.type"], ["level", "scripts.header.level"]].map(([key, labelKey]) => (
                <th key={key} className="px-3 py-2 text-left font-medium">
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 hover:text-foreground"
                    onClick={() => toggleSort(key as "id" | "name" | "type" | "level")}
                  >
                    {t(labelKey)} {sortKey === key ? (sortDir === "asc" ? "▲" : "▼") : ""}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && !loading && (
              <tr>
              <td className="text-muted-foreground px-3 py-4">{t("scripts.noNpcs")}</td>
              </tr>
            )}
            {loading && filtered.length === 0 &&
              Array.from({length: 8}).map((_, i) => (
                <tr key={`sk-${i}`} className="border-b">
                  <td className="px-3 py-1.5">
                    <Skeleton className="h-4 w-8" />
                  </td>
                  <td className="px-3 py-1.5">
                    <Skeleton className="h-4" style={{width: `${30 + ((i * 37) % 45)}%`}} />
                  </td>
                  <td className="px-3 py-1.5">
                    <Skeleton className="h-4 w-12" />
                  </td>
                  <td className="px-3 py-1.5">
                    <Skeleton className="h-4 w-24" />
                  </td>
                </tr>
              ))}
            {filtered.map((n) => (
              <tr
                key={n.id}
                onClick={() => { if (isUsableScriptName(n.name)) setSelected(n.id) }}
                onDoubleClick={() => { if (isUsableScriptName(n.name)) void openScriptEditorOrFail("npc", String(n.id), t, onSyncRequired) }}
                onContextMenu={(event) => {
                  event.preventDefault()
                  if (!isUsableScriptName(n.name)) return
                  setSelected(n.id)
                  setNpcMenu({npc: n, left: Math.min(event.clientX, window.innerWidth - 220), top: Math.min(event.clientY, window.innerHeight - 220)})
                }}
                className={`border-b ${
                  !isUsableScriptName(n.name)
                    ? "cursor-not-allowed opacity-60"
                    : `cursor-pointer ${selected === n.id ? "bg-accent" : "hover:bg-accent/50"}`
                }`}
              >
                <td className="px-3 py-1.5">{n.id}</td>
                <td className={isUsableScriptName(n.name) ? "px-3 py-1.5" : "text-muted-foreground px-3 py-1.5 italic"}>
                  {isUsableScriptName(n.name) ? n.name : t("scripts.invalidNpc", {id: n.id})}
                </td>
                <td className="px-3 py-1.5">{n.type}</td>
                <td className="text-muted-foreground px-3 py-1.5">{n.level}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </ScrollArea>

      {npcMenu && (
        <div
          className="bg-popover text-popover-foreground fixed z-50 min-w-52 overflow-hidden rounded-md border py-1 text-sm shadow-xl"
          style={{left: npcMenu.left, top: npcMenu.top}}
          onMouseDown={(event) => event.stopPropagation()}
        >
          <div className="text-muted-foreground border-b px-3 py-2 text-xs">
            {npcMenu.npc.name} <span className="font-mono">#{npcMenu.npc.id}</span>
          </div>
          <button type="button" className="hover:bg-accent flex w-full items-center gap-2 px-3 py-2 text-left" onClick={() => { void doReset(npcMenu.npc.id); setNpcMenu(null) }}>
            <RotateCcw className="size-4" />{t("scripts.reset")}
          </button>
          <button type="button" className="hover:bg-accent flex w-full items-center gap-2 px-3 py-2 text-left" onClick={() => { setNpcMenu(null); void openScriptEditorOrFail("npcflags", String(npcMenu.npc.id), t, onSyncRequired) }}>
            <Flag className="size-4" />{t("scripts.editFlags")}
          </button>
          <button type="button" className="hover:bg-accent flex w-full items-center gap-2 px-3 py-2 text-left" onClick={() => { setNpcMenu(null); void openScriptEditorOrFail("npcattr", String(npcMenu.npc.id), t, onSyncRequired) }}>
            <UserRound className="size-4" />{t("scripts.viewAttributes")}
          </button>
          <button type="button" className="hover:bg-accent flex w-full items-center gap-2 px-3 py-2 text-left" onClick={() => { setNpcMenu(null); setSelected(npcMenu.npc.id); setWarping(true) }}>
            <LocateFixed className="size-4" />{t("scripts.warp")}
          </button>
        </div>
      )}

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("scripts.confirmDelete")}</AlertDialogTitle>
            <AlertDialogDescription>{t("scripts.confirmDeleteNpcDescription", {name: selectedNPC ? `${selectedNPC.name} (#${selectedNPC.id})` : ""})}</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex justify-end gap-2">
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <Button variant="destructive" onClick={() => { setDeleteOpen(false); void doDelete() }}>{t("scripts.delete")}</Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>

      <AddNPCDialog
        open={adding}
        onClose={() => setAdding(false)}
        npcs={npcs}
        onCreated={async () => {
          setAdding(false)
          await onRefresh()
        }}
      />

      <WarpDialog
        open={warping}
        npcId={selected}
        onClose={() => setWarping(false)}
      />
    </div>
  )
}

// WarpDialog collects level/x/y to warp the selected NPC (rc_warp_npc). x/y
// default to "0" (mirrors TNPCList::onWarp).
function WarpDialog({
  open,
  npcId,
  onClose,
}: {
  open: boolean
  npcId: number | null
  onClose: () => void
}) {
  const {t} = useLanguage()
  const [level, setLevel] = useState("")
  const [x, setX] = useState("0")
  const [y, setY] = useState("0")

  useEffect(() => {
    if (open) {
      setLevel("")
      setX("0")
      setY("0")
    }
  }, [open])

  const submit = async () => {
    if (npcId == null) return
    try {
      await rcService.warpNPC(npcId, Number(x) || 0, Number(y) || 0, level)
      toast.success(t("scripts.npcWarped", {id: npcId}))
      onClose()
    } catch (err) {
      toast.error(t("scripts.warpFailed"), {description: String(err)})
    }
  }

  const field = (label: string, value: string, set: (v: string) => void, autoFocus = false) => (
    <div key={label} className="grid grid-cols-[100px_1fr] items-center gap-2">
      <Label>{label}</Label>
      <Input
        value={value}
        autoFocus={autoFocus}
        onChange={(e) => set(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") submit()
        }}
      />
    </div>
  )

  return (
    <AlertDialog open={open} onOpenChange={(v) => !v && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("scripts.warp")} NPC {npcId ?? ""}</AlertDialogTitle>
          <AlertDialogDescription>{t("scripts.moveDescription")}</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-2">
          {field(t("scripts.field.level"), level, setLevel, true)}
          {field(t("scripts.field.x"), x, setX)}
          {field(t("scripts.field.y"), y, setY)}
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit}>{t("scripts.warp")}</Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}

// AddNPCDialog collects the 7 creation fields (name, id, type, scripter, level,
// x, y). id defaults to the first free id >= 1000 (mirrors
// TNPCList::firstFreeNPCId); x/y default to "0".
function AddNPCDialog({
  open,
  onClose,
  npcs,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  npcs: NPC[]
  onCreated: () => void | Promise<void>
}) {
  const {t} = useLanguage()
  const firstFreeId = useMemo(() => {
    const ids = new Set(npcs.map((n) => n.id))
    let id = 1000
    while (ids.has(id)) id++
    return id
  }, [npcs])

  const [name, setName] = useState("")
  const [id, setId] = useState(String(firstFreeId))
  const [type, setType] = useState("")
  const [scripter, setScripter] = useState("")
  const [level, setLevel] = useState("")
  const [x, setX] = useState("0")
  const [y, setY] = useState("0")

  // Reset id default when reopened with a new first-free value.
  useEffect(() => {
    if (open) setId(String(firstFreeId))
  }, [open, firstFreeId])

  const submit = async () => {
    if (!name.trim()) {
      toast.error(t("scripts.nameRequired"))
      return
    }
    try {
      await rcService.createNPC(name.trim(), Number(id) || firstFreeId, type, scripter, level, x, y)
      toast.success(t("scripts.npcCreated", {name: name.trim()}))
      setName("")
      setType("")
      setScripter("")
      setLevel("")
      await onCreated()
    } catch (err) {
      toast.error(t("scripts.createFailed"), {description: String(err)})
    }
  }

  const field = (
    label: string,
    value: string,
    set: (v: string) => void,
    opts: {autoFocus?: boolean} = {},
  ) => (
    <div key={label} className="grid grid-cols-[100px_1fr] items-center gap-2">
      <Label>{label}</Label>
      <Input
        value={value}
        autoFocus={opts.autoFocus}
        onChange={(e) => set(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") submit()
        }}
      />
    </div>
  )

  return (
    <AlertDialog open={open} onOpenChange={(v) => !v && onClose()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("scripts.addNpc")}</AlertDialogTitle>
          <AlertDialogDescription>{t("scripts.createDescription")}</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-2">
          {field(t("scripts.field.name"), name, setName, {autoFocus: true})}
          {field(t("scripts.field.id"), id, setId)}
          {field(t("scripts.field.type"), type, setType)}
          {field(t("scripts.field.scripter"), scripter, setScripter)}
          {field(t("scripts.field.level"), level, setLevel)}
          {field(t("scripts.field.x"), x, setX)}
          {field(t("scripts.field.y"), y, setY)}
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button onClick={submit} disabled={!name.trim()}>
            {t("scripts.create")}
          </Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}
