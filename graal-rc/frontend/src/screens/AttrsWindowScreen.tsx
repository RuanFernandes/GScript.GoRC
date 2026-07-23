// AttrsWindowScreen is the content of the "/open" external window (URL
// "/#attrs?a=<account>"). A tabbed INI attribute editor (Stats / Look / Basic
// Attributes / Chests / Weapons / Script Flags). Mirrors reference
// TPlayerList::handlePlayerAttributes (TPlayerList.cpp:532-719). Loads via
// rcService.openAttrs; on Apply rebuilds the INI text, converts to properties
// JSON via rcService.parseAttrsText, writes with setAttrs.
import {useEffect, useMemo, useState} from "react"
import Editor from "@monaco-editor/react"
import {Loader2} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {rcService} from "@/services/rcService"
import type {AttrsData} from "@/types"

const LOOK_FIELDS = [
  "Head Image", "Body Image", "Animation", "Skin Color", "Coat Color",
  "Sleeves Color", "Shoes Color", "Belt Color",
]
const BASIC_FIELDS = [
  "Level", "X", "Y", "Hearts", "Full Hearts", "AP", "MP", "Gralats",
  "Glove", "Bombs", "Arrows", "Sword Power", "Sword Image", "Shield Power", "Shield Image",
]
const COLOR_KEYS = new Set(["Skin Color", "Coat Color", "Sleeves Color", "Shoes Color", "Belt Color"])
const NAMED_COLORS = [
  "white", "yellow", "orange", "pink", "red", "darkred", "lightgreen", "green",
  "darkgreen", "lightblue", "blue", "darkblue", "brown", "cyan", "purple", "tan",
  "grey", "black", "transparent",
]

interface Section {
  name: string
  kv?: Record<string, string>
  order?: string[]
  raw?: string
}

function parseEditor(text: string): Section[] {
  const sections: Section[] = []
  let cur: Section | null = null
  const RAW_SECTIONS = new Set(["Chests", "Weapons", "Script Flags"])
  for (const line of text.split(/\r?\n/)) {
    const m = /^\[(.+)\]$/.exec(line.trim())
    if (m) {
      cur = {name: m[1]}
      sections.push(cur)
      continue
    }
    if (!cur) continue
    if (RAW_SECTIONS.has(cur.name)) {
      cur.raw = (cur.raw ?? "") + line + "\n"
      continue
    }
    const sep = line.indexOf(":")
    if (sep < 0) continue
    const k = line.slice(0, sep).trim()
    const v = line.slice(sep + 1).trim()
    if (!cur.kv) {
      cur.kv = {}
      cur.order = []
    }
    cur.kv[k] = v
    cur.order!.push(k)
  }
  return sections
}

const getKv = (s: Section[], n: string) => s.find((x) => x.name === n)?.kv ?? {}
const getRaw = (s: Section[], n: string) => s.find((x) => x.name === n)?.raw ?? ""

function readAccount(): string {
  const hash = window.location.hash
  const q = hash.indexOf("?")
  const params = new URLSearchParams(q >= 0 ? hash.slice(q + 1) : "")
  return params.get("a") ?? ""
}

export function AttrsWindowScreen() {
  const account = readAccount()
  const [data, setData] = useState<AttrsData | null>(null)
  const [sections, setSections] = useState<Section[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    rcService
      .openAttrs(account)
      .then((d) => {
        if (cancelled || !d) return
        setData(d)
        setSections(parseEditor(d.editorText ?? ""))
      })
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [account])

  const stats = useMemo(() => getKv(sections, "Stats"), [sections])
  const [look, setLook] = useState<Record<string, string>>({})
  const [basic, setBasic] = useState<Record<string, string>>({})
  const [male, setMale] = useState(false)
  const [weaponsOn, setWeaponsOn] = useState(false)
  const [spin, setSpin] = useState(false)
  const [chests, setChests] = useState("")
  const [weapons, setWeapons] = useState("")
  const [flags, setFlags] = useState("")

  useEffect(() => {
    if (!data) return
    const lookKv = getKv(sections, "Look")
    const basicKv = getKv(sections, "Basic Attributes")
    setLook({...lookKv})
    setBasic({...basicKv})
    setMale(basicKv["Male"] === "1" || lookKv["Male"] === "1")
    setWeaponsOn(basicKv["Weapons Enabled"] === "1")
    setSpin(basicKv["Spin Attack"] === "1")
    setChests(getRaw(sections, "Chests"))
    setWeapons(getRaw(sections, "Weapons"))
    setFlags(getRaw(sections, "Script Flags"))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data])

  const setLookField = (k: string, v: string) => setLook((p) => ({...p, [k]: v}))
  const setBasicField = (k: string, v: string) => setBasic((p) => ({...p, [k]: v}))

  const rebuildIni = (): string => {
    const out: string[] = []
    out.push("[Stats]")
    for (const k of Object.keys(stats)) out.push(`${k}: ${stats[k]}`)
    out.push("")
    out.push("[Look]")
    for (const k of LOOK_FIELDS) out.push(`${k}: ${look[k] ?? ""}`)
    out.push("")
    out.push("[Basic Attributes]")
    for (const k of BASIC_FIELDS) out.push(`${k}: ${basic[k] ?? ""}`)
    out.push(`Male: ${male ? "1" : "0"}`)
    out.push(`Weapons Enabled: ${weaponsOn ? "1" : "0"}`)
    out.push(`Spin Attack: ${spin ? "1" : "0"}`)
    out.push("")
    out.push("[Chests]", chests.trimEnd(), "")
    out.push("[Weapons]", weapons.trimEnd(), "")
    out.push("[Script Flags]", flags.trimEnd(), "")
    return out.join("\n")
  }

  const apply = async () => {
    setSaving(true)
    try {
      const json = await rcService.parseAttrsText(rebuildIni())
      await rcService.setAttrs(data?.account ?? account, json)
      toast.success(`Attributes saved for ${data?.account ?? account}`)
    } catch (e) {
      toast.error("Save failed", {description: e instanceof Error ? e.message : String(e)})
    } finally {
      setSaving(false)
    }
  }

  const target = data?.account ?? account

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2.5">
        <h1 className="text-sm font-semibold">{target}&apos;s Attributes</h1>
        <Button className="ml-auto" size="sm" onClick={apply} disabled={loading || saving || !!error}>
          {saving && <Loader2 className="size-4 animate-spin" />} Apply
        </Button>
      </header>
      <ScrollArea className="min-h-0 flex-1 p-4">
        {loading && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading attributes…
          </div>
        )}
        {error && <div className="text-sm text-destructive">{error}</div>}
        {data && !loading && (
          <Tabs defaultValue="stats">
            <TabsList className="flex w-full flex-wrap">
              <TabsTrigger value="stats">Stats</TabsTrigger>
              <TabsTrigger value="look">Look</TabsTrigger>
              <TabsTrigger value="basic">Basic</TabsTrigger>
              <TabsTrigger value="chests">Chests</TabsTrigger>
              <TabsTrigger value="weapons">Weapons</TabsTrigger>
              <TabsTrigger value="flags">Flags</TabsTrigger>
            </TabsList>

            <TabsContent value="stats" className="space-y-1 pt-3">
              {Object.keys(stats).length === 0 && <p className="text-xs text-muted-foreground">No stats.</p>}
              {Object.entries(stats).map(([k, v]) => (
                <div key={k} className="flex justify-between gap-2 text-xs">
                  <span className="text-muted-foreground">{k}</span>
                  <span className="font-mono truncate">{v}</span>
                </div>
              ))}
            </TabsContent>

            <TabsContent value="look" className="space-y-2 pt-3">
              {LOOK_FIELDS.map((k) => (
                <div key={k} className="space-y-1">
                  <span className="text-xs font-medium uppercase text-muted-foreground">{k}</span>
                  {COLOR_KEYS.has(k) ? (
                    <select
                      className="bg-background h-8 w-full rounded-md border px-2 text-sm"
                      value={look[k] ?? ""}
                      onChange={(e) => setLookField(k, e.target.value)}
                    >
                      <option value="">{look[k] ?? "(custom)"}</option>
                      {NAMED_COLORS.map((c) => <option key={c} value={c}>{c}</option>)}
                    </select>
                  ) : (
                    <Input value={look[k] ?? ""} onChange={(e) => setLookField(k, e.target.value)} className="h-8" />
                  )}
                </div>
              ))}
            </TabsContent>

            <TabsContent value="basic" className="space-y-2 pt-3">
              {BASIC_FIELDS.map((k) => (
                <div key={k} className="space-y-1">
                  <span className="text-xs font-medium uppercase text-muted-foreground">{k}</span>
                  <Input value={basic[k] ?? ""} onChange={(e) => setBasicField(k, e.target.value)} className="h-8" />
                </div>
              ))}
              <div className="flex flex-col gap-1.5 pt-1">
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={male} onChange={(e) => setMale(e.target.checked)} /> Male
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={weaponsOn} onChange={(e) => setWeaponsOn(e.target.checked)} /> Weapons Enabled
                </label>
                <label className="flex items-center gap-2 text-sm">
                  <input type="checkbox" checked={spin} onChange={(e) => setSpin(e.target.checked)} /> Spin Attack
                </label>
              </div>
            </TabsContent>

            <TabsContent value="chests" className="space-y-1 pt-3">
              <span className="text-xs font-medium uppercase text-muted-foreground">Chests (one per line)</span>
              <MonacoText value={chests} onChange={setChests} />
            </TabsContent>
            <TabsContent value="weapons" className="space-y-1 pt-3">
              <span className="text-xs font-medium uppercase text-muted-foreground">Weapons (one per line)</span>
              <MonacoText value={weapons} onChange={setWeapons} />
            </TabsContent>
            <TabsContent value="flags" className="space-y-1 pt-3">
              <span className="text-xs font-medium uppercase text-muted-foreground">Script Flags</span>
              <MonacoText value={flags} onChange={setFlags} />
            </TabsContent>
          </Tabs>
        )}
      </ScrollArea>
    </div>
  )
}

// MonacoText is a compact Monaco editor used for the Chests / Weapons / Script
// Flags list sections — line-based editing with monospace + line numbers.
function MonacoText({value, onChange}: {value: string; onChange: (v: string) => void}) {
  return (
    <div className="bg-background h-[280px] w-full overflow-hidden rounded-md border">
      <Editor
        theme="vs-dark"
        language="ini"
        value={value}
        onChange={(v) => onChange(v ?? "")}
        options={{
          fontFamily: "monospace",
          fontSize: 12,
          minimap: {enabled: false},
          scrollBeyondLastLine: false,
          automaticLayout: true,
          lineNumbers: "on",
          wordWrap: "on",
        }}
      />
    </div>
  )
}
