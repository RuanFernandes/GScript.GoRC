// ScriptManagerWindowScreen is the content of the external "Script Manager"
// window (opened via App.OpenScriptManager, URL "/#scripts"). Three tabs —
// Weapons, Classes, NPCs — each a searchable list with Refresh / Add / Delete
// (and for NPCs: Reset / Edit Flags / View Attributes). Double-clicking a row
// opens that script in its own editor window.
import {useEffect, useMemo, useState} from "react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {
  AlertDialog,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {Label} from "@/components/ui/label"
import {useScriptLists} from "@/hooks/useScriptLists"
import {rcService} from "@/services/rcService"
import type {NPC} from "@/types"

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
  const lists = useScriptLists(rcService)
  const [tab, setTab] = useState<"weapons" | "classes" | "npcs">("weapons")

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2.5">
        <h1 className="text-base font-semibold">Script Manager</h1>
      </header>
      <Tabs value={tab} onValueChange={(v) => setTab(v as typeof tab)} className="flex min-h-0 flex-1 flex-col p-3">
        <TabsList>
          <TabsTrigger value="weapons">Weapons ({lists.weapons.length})</TabsTrigger>
          <TabsTrigger value="classes">Classes ({lists.classes.length})</TabsTrigger>
          <TabsTrigger value="npcs">NPCs ({lists.npcs.length})</TabsTrigger>
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
                toast.success("Weapon list refreshed")
              } catch (err) {
                toast.error("Refresh failed", {description: String(err)})
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
  const [selected, setSelected] = useState<string | null>(null)
  const [filter, setFilter] = useState("")
  const [adding, setAdding] = useState(false)
  const [name, setName] = useState("")

  const filtered = useMemo(
    () => rows.filter((r) => r.key.toLowerCase().includes(filter.toLowerCase())),
    [rows, filter],
  )
  const headers = ["Name"]

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
      toast.error("Add failed", {description: String(err)})
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
      toast.error("Delete failed", {description: String(err)})
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex items-center gap-2">
        <Input
          placeholder="Filter…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="max-w-56"
        />
        <div className="ml-auto flex gap-2">
          <Button variant="outline" size="sm" onClick={() => onRefresh()}>
            Refresh
          </Button>
          <Button size="sm" onClick={() => setAdding(true)}>
            Add {kind === "weapon" ? "Weapon" : "Class"}
          </Button>
          <Button
            variant="destructive"
            size="sm"
            disabled={!selected}
            onClick={doDelete}
          >
            Delete
          </Button>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1 rounded-md border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 sticky top-0">
            <tr>
              {headers.map((h) => (
                <th key={h} className="px-3 py-2 text-left font-medium">
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && !loading && (
              <tr>
                <td className="text-muted-foreground px-3 py-4">No entries.</td>
              </tr>
            )}
            {filtered.map((r) => (
              <tr
                key={r.key}
                onClick={() => setSelected(r.key)}
                onDoubleClick={() => openScriptEditorOrFail(kind, r.key)}
                className={`cursor-pointer border-b ${
                  selected === r.key ? "bg-accent" : "hover:bg-accent/50"
                }`}
              >
                {r.cols.map((c, i) => (
                  <td key={i} className="px-3 py-1.5">
                    {c}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </ScrollArea>

      <AlertDialog open={adding} onOpenChange={(v) => setAdding(v)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Add {kind === "weapon" ? "Weapon" : "Class"}</AlertDialogTitle>
            <AlertDialogDescription>
              Enter a name. The script can be edited after creation.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="add-name">Name</Label>
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
              Cancel
            </Button>
            <Button onClick={doAdd} disabled={!name.trim()}>
              Add
            </Button>
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
  const [selected, setSelected] = useState<number | null>(null)
  const [filter, setFilter] = useState("")
  const [adding, setAdding] = useState(false)
  const [warping, setWarping] = useState(false)

  const filtered = useMemo(() => {
    const q = filter.toLowerCase()
    return npcs.filter(
      (n) => n.name.toLowerCase().includes(q) || String(n.id).includes(q) || n.type.toLowerCase().includes(q),
    )
  }, [npcs, filter])

  const selectedNPC = npcs.find((n) => n.id === selected) ?? null

  const doDelete = async () => {
    if (selected == null) return
    try {
      await rcService.deleteNPC(selected)
      toast.success(`Deleted NPC ${selected}`)
      setSelected(null)
      await onRefresh()
    } catch (err) {
      toast.error("Delete failed", {description: String(err)})
    }
  }

  const doReset = async () => {
    if (selected == null) return
    try {
      await rcService.resetNPC(selected)
      toast.success(`Reset NPC ${selected}`)
    } catch (err) {
      toast.error("Reset failed", {description: String(err)})
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-2">
      <div className="flex items-center gap-2">
        <Input
          placeholder="Filter…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          className="max-w-56"
        />
        <div className="ml-auto flex gap-2">
          <Button variant="outline" size="sm" onClick={() => onRefresh()}>
            Refresh
          </Button>
          <Button size="sm" onClick={() => setAdding(true)}>
            Add NPC
          </Button>
          <Button variant="outline" size="sm" disabled={!selectedNPC} onClick={doReset}>
            Reset
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!selectedNPC}
            onClick={() => selectedNPC && openScriptEditorOrFail("npcflags", String(selectedNPC.id))}
          >
            Edit Flags
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={!selectedNPC}
            onClick={() => selectedNPC && openScriptEditorOrFail("npcattr", String(selectedNPC.id))}
          >
            View Attributes
          </Button>
          <Button variant="outline" size="sm" disabled={!selectedNPC} onClick={() => setWarping(true)}>
            Warp
          </Button>
          <Button variant="destructive" size="sm" disabled={!selectedNPC} onClick={doDelete}>
            Delete
          </Button>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1 rounded-md border">
        <table className="w-full text-sm">
          <thead className="bg-muted/50 sticky top-0">
            <tr>
              <th className="w-20 px-3 py-2 text-left font-medium">ID</th>
              <th className="px-3 py-2 text-left font-medium">Name</th>
              <th className="px-3 py-2 text-left font-medium">Type</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && !loading && (
              <tr>
                <td className="text-muted-foreground px-3 py-4">No NPCs.</td>
              </tr>
            )}
            {filtered.map((n) => (
              <tr
                key={n.id}
                onClick={() => setSelected(n.id)}
                onDoubleClick={() => openScriptEditorOrFail("npc", String(n.id))}
                className={`cursor-pointer border-b ${
                  selected === n.id ? "bg-accent" : "hover:bg-accent/50"
                }`}
              >
                <td className="px-3 py-1.5">{n.id}</td>
                <td className="px-3 py-1.5">{n.name}</td>
                <td className="px-3 py-1.5">{n.type}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </ScrollArea>

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
      toast.error("Warp failed", {description: String(err)})
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
          <AlertDialogTitle>Warp NPC {npcId ?? ""}</AlertDialogTitle>
          <AlertDialogDescription>Move the NPC to a level and position.</AlertDialogDescription>
        </AlertDialogHeader>
        <div className="grid gap-2">
          {field("Level:", level, setLevel, true)}
          {field("X:", x, setX)}
          {field("Y:", y, setY)}
        </div>
        <div className="flex justify-end gap-2">
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={submit}>Warp</Button>
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
      toast.error("Name is required")
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
      toast.error("Create NPC failed", {description: String(err)})
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
          <AlertDialogTitle>Add NPC</AlertDialogTitle>
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
            Cancel
          </Button>
          <Button onClick={submit} disabled={!name.trim()}>
            Create
          </Button>
        </div>
      </AlertDialogContent>
    </AlertDialog>
  )
}
