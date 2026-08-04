// ScriptManagerWindowScreen is the content of the external "Script Manager"
// window (opened via App.OpenScriptManager, URL "/#scripts"). Three tabs —
// Weapons, Classes, NPCs — each a searchable list with Refresh / Add / Delete
// (and for NPCs: Reset / Edit Flags / View Attributes). Double-clicking a row
// opens that script in its own editor window.
import {useEffect, useMemo, useRef, useState} from "react"
import {toast} from "sonner"
import {Flag, LocateFixed, Loader2, RotateCcw, UserRound} from "lucide-react"

import {Button} from "@/components/ui/button"
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
import {rcService} from "@/services/rcService"
import type {NPC} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

// openScriptEditorOrFail opens the editor; OpenScriptEditor fetches the script
// server-side first and only opens a window on success. A failure (e.g. the
// account lacks read permission and the server never replies → timeout) cancels
// the open and surfaces a toast instead.
async function openScriptEditorOrFail(scriptType: string, key: string) {
  try {
    await rcService.openScriptEditor(scriptType, key)
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err)
    toast.error("Couldn't open script", {
      description: msg.includes("timed out")
        ? "No response from server — your account likely lacks read permission."
        : msg,
    })
  }
}

export function ScriptManagerWindowScreen() {
  const {t} = useLanguage()
  const [onlyReadable, setOnlyReadable] = useState(false)
  const lists = useScriptLists(rcService, onlyReadable)
  const [tab, setTab] = useState<"weapons" | "classes" | "npcs">("weapons")

  useEffect(() => {
    if (lists.error) {
      toast.error(t("scripts.refreshFailed"), {description: lists.error})
    }
  }, [lists.error, t])

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2.5">
        <h1 className="text-base font-semibold">{t("scripts.title")}</h1>
        <label className="text-muted-foreground ml-auto inline-flex cursor-pointer items-center gap-2 text-xs">
          <input
            type="checkbox"
            checked={onlyReadable}
            onChange={(event) => setOnlyReadable(event.target.checked)}
            className="accent-primary"
          />
          <span>{t("scripts.onlyReadable")}</span>
        </label>
      </header>
      <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)} className="flex min-h-0 flex-1 flex-col p-3">
        <TabsList>
          <TabsTrigger value="weapons">{t("scripts.weapons")} ({lists.weapons.length})</TabsTrigger>
          <TabsTrigger value="classes">{t("scripts.classes")} ({lists.classes.length})</TabsTrigger>
          <TabsTrigger value="npcs">{t("scripts.npcs")} ({lists.npcs.length})</TabsTrigger>
        </TabsList>
        <TabsContent value="weapons" className="mt-3 min-h-0 flex-1">
          <WeaponClassTab
            kind="weapon"
            rows={lists.weapons.map((w) => ({key: w.name, cols: [w.name]}))}
            loading={lists.loading}
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
            rows={lists.classes.map((c) => ({key: c.name, cols: [c.name]}))}
            loading={lists.loading}
            onRefresh={lists.refresh}
          />
        </TabsContent>
        <TabsContent value="npcs" className="mt-3 min-h-0 flex-1">
          <NPCTab npcs={lists.npcs} loading={lists.loading} onRefresh={lists.refresh} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

interface Row {
  key: string
  cols: string[]
}

// WeaponClassTab handles weapon and class lists (both name-keyed; weapons add an
// Image column). Add takes a single name; delete takes the selected name.
function WeaponClassTab({
  kind,
  rows,
  loading,
  onRefresh,
}: {
  kind: "weapon" | "class"
  rows: Row[]
  loading: boolean
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
      toast.success(`${kind === "weapon" ? "Weapon" : "Class"} "${n}" added`)
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
      await openScriptEditorOrFail(kind, key)
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
      toast.success(`Deleted "${selected}"`)
      setSelected(null)
      await onRefresh()
    } catch (err) {
      toast.error(t("scripts.deleteFailed"), {description: String(err)})
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex items-center gap-2">
        <Input
          placeholder={t("scripts.filter")}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="max-w-56"
        />
        <div className="ml-auto flex gap-2">
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
        <table className="w-full text-sm">
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
                  aria-disabled={opening}
                  onClick={() => { if (!opening) setSelected(r.key) }}
                  onDoubleClick={() => { if (!opening) void openRow(r.key) }}
                  className={`border-b ${
                    opening
                      ? "cursor-wait opacity-60"
                      : `cursor-pointer ${selected === r.key ? "bg-accent" : "hover:bg-accent/50"}`
                  }`}
                >
                  {r.cols.map((c, i) => (
                    <td key={i} className="px-3 py-1.5">
                      {i === 0 ? (
                        <span className="inline-flex items-center gap-2">
                          {opening && <Loader2 className="text-muted-foreground size-3.5 animate-spin" aria-label="Loading" />}
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
              Enter a name. The script can be edited after creation.
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
  onRefresh,
}: {
  npcs: NPC[]
  loading: boolean
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
        n.name.toLowerCase().includes(q) ||
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
  }, [npcs, filter, sortKey, sortDir])

  const toggleSort = (key: "id" | "name" | "type" | "level") => {
    if (sortKey === key) setSortDir((d) => (d === "asc" ? "desc" : "asc"))
    else {
      setSortKey(key)
      setSortDir("asc")
    }
  }

  const selectedNPC = npcs.find((n) => n.id === selected) ?? null

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
    if (selected == null) return
    try {
      await rcService.deleteNPC(selected)
      toast.success(`Deleted NPC ${selected}`)
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
      toast.success(`Reset NPC ${npcID}`)
    } catch (err) {
      toast.error(t("scripts.resetFailed"), {description: String(err)})
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex items-center gap-2">
        <Input
          placeholder={t("scripts.filter")}
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="max-w-56"
        />
        <div className="ml-auto flex gap-2">
          <Button variant="outline" size="sm" onClick={() => onRefresh()}>
            {t("scripts.refresh")}
          </Button>
          <Button size="sm" onClick={() => setAdding(true)}>
            {t("scripts.addNpc")}
          </Button>
          <Button variant="destructive" size="sm" disabled={!selectedNPC} onClick={() => setDeleteOpen(true)}>
            {t("scripts.delete")}
          </Button>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1 rounded-md border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 sticky top-0">
            <tr>
              {[["id", "ID"], ["name", "Name"], ["type", "Type"], ["level", "Level"]].map(([key, label]) => (
                <th key={key} className="px-3 py-2 text-left font-medium">
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 hover:text-foreground"
                    onClick={() => toggleSort(key as "id" | "name" | "type" | "level")}
                  >
                    {label} {sortKey === key ? (sortDir === "asc" ? "▲" : "▼") : ""}
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
                onClick={() => setSelected(n.id)}
                onDoubleClick={() => openScriptEditorOrFail("npc", String(n.id))}
                onContextMenu={(event) => {
                  event.preventDefault()
                  setSelected(n.id)
                  setNpcMenu({npc: n, left: Math.min(event.clientX, window.innerWidth - 220), top: Math.min(event.clientY, window.innerHeight - 220)})
                }}
                className={`cursor-pointer border-b ${
                  selected === n.id ? "bg-accent" : "hover:bg-accent/50"
                }`}
              >
                <td className="px-3 py-1.5">{n.id}</td>
                <td className="px-3 py-1.5">{n.name}</td>
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
          <button type="button" className="hover:bg-accent flex w-full items-center gap-2 px-3 py-2 text-left" onClick={() => { setNpcMenu(null); void openScriptEditorOrFail("npcflags", String(npcMenu.npc.id)) }}>
            <Flag className="size-4" />{t("scripts.editFlags")}
          </button>
          <button type="button" className="hover:bg-accent flex w-full items-center gap-2 px-3 py-2 text-left" onClick={() => { setNpcMenu(null); void openScriptEditorOrFail("npcattr", String(npcMenu.npc.id)) }}>
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
      toast.success(`Warped NPC ${npcId}`)
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
          <AlertDialogDescription>Move the NPC to a level and position.</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-2">
          {field("Level:", level, setLevel, true)}
          {field("X:", x, setX)}
          {field("Y:", y, setY)}
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
      toast.success(`NPC "${name.trim()}" created`)
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
          <AlertDialogDescription>Create a new DB NPC on the server.</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-2">
          {field("Name:", name, setName, {autoFocus: true})}
          {field("ID:", id, setId)}
          {field("Type:", type, setType)}
          {field("Scripter:", scripter, setScripter)}
          {field("Level:", level, setLevel)}
          {field("X:", x, setX)}
          {field("Y:", y, setY)}
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
