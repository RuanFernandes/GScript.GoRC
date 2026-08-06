// RightsWindowScreen is the content of the "/openrights" external window (URL
// "/#rights?a=<account>"). Edits the 20 staff-rights bits, IP range, and folder
// rights for an account. Mirrors reference TPlayerList::handlePlayerRights
// (TPlayerList.cpp:460-531). The account is resolved to "self" by the backend
// when the command is issued with no argument; the resolved name is passed in
// the URL and also returned in the rights payload.
import {useEffect, useState} from "react"
import {Loader2} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {rcService} from "@/services/rcService"
import type {RightsData} from "@/types"
import {cn} from "@/lib/utils"
import {useLanguage} from "@/hooks/useLanguage"

const RIGHTS_LAYOUT: {label: string; bit: number}[] = [
  {label: "Warpto XY", bit: 0},
  {label: "Set server flags", bit: 15},
  {label: "Warpto player", bit: 1},
  {label: "Change rights", bit: 10},
  {label: "Warp players", bit: 2},
  {label: "Ban players", bit: 11},
  {label: "Update level", bit: 3},
  {label: "Change comments", bit: 12},
  {label: "Disconnect players", bit: 4},
  {label: "Change staff accounts", bit: 14},
  {label: "View player attributes", bit: 5},
  {label: "Change server options", bit: 16},
  {label: "Set player attributes", bit: 6},
  {label: "Edit folder configuration", bit: 17},
  {label: "Set the own attributes", bit: 7},
  {label: "Edit folder rights", bit: 18},
  {label: "Reset attributes", bit: 8},
  {label: "NPC-Control", bit: 19},
  {label: "Admin message", bit: 9},
]

function readAccount(): string {
  const hash = window.location.hash
  const q = hash.indexOf("?")
  const params = new URLSearchParams(q >= 0 ? hash.slice(q + 1) : "")
  return params.get("a") ?? ""
}

export function RightsWindowScreen() {
  const {t} = useLanguage()
  const account = readAccount()
  const [data, setData] = useState<RightsData | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [ipRange, setIpRange] = useState("")
  const [folders, setFolders] = useState("")
  const [flags, setFlags] = useState(0)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    rcService
      .openRights(account)
      .then((d) => {
        if (cancelled || !d) return
        setData(d)
        setIpRange(d.ipRange ?? "")
        setFolders(d.folderAccess ?? "")
        setFlags(d.rights ?? 0)
      })
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [account])

  const toggle = (bit: number) => setFlags((f) => f ^ (1 << bit))
  const target = data?.account ?? account

  const apply = async () => {
    setSaving(true)
    try {
      await rcService.setRights(target, flags, ipRange, folders)
      toast.success(t("rights.saved", {account: target}))
    } catch (e) {
      toast.error(t("common.saveFailed"), {description: e instanceof Error ? e.message : String(e)})
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2.5">
        <h1 className="text-sm font-semibold">{target}&apos;s Rights</h1>
        <Button className="ml-auto" size="sm" onClick={apply} disabled={loading || saving || !!error}>
          {saving && <Loader2 className="size-4 animate-spin" />} Apply
        </Button>
      </header>
      <ScrollArea className="min-h-0 flex-1 p-4">
        {loading && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> Loading rights…
          </div>
        )}
        {error && <div className="text-sm text-destructive">{error}</div>}
        {data && !loading && (
          <Tabs defaultValue="flags">
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="flags">IP Range & flags</TabsTrigger>
              <TabsTrigger value="folders">Folder rights</TabsTrigger>
            </TabsList>
            <TabsContent value="flags" className="space-y-3 pt-3">
              <div className="space-y-1">
                <span className="text-xs font-medium uppercase text-muted-foreground">Account</span>
                <Input value={data.account} readOnly className="h-8 bg-muted/40" />
              </div>
              <div className="space-y-1">
                <span className="text-xs font-medium uppercase text-muted-foreground">IP range(s)</span>
                <Input value={ipRange} onChange={(e) => setIpRange(e.target.value)} className="h-8" />
              </div>
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium uppercase text-muted-foreground">Right flags</span>
                <Button variant="ghost" size="sm" onClick={() => setFlags(0)}>Clear all</Button>
              </div>
              <div className="grid grid-cols-2 gap-1.5">
                {RIGHTS_LAYOUT.map(({label, bit}) => {
                  const on = (flags & (1 << bit)) !== 0
                  return (
                    <label
                      key={bit}
                      className={cn(
                        "flex cursor-pointer items-center gap-2 rounded-md border px-2 py-1.5 text-xs",
                        on ? "border-primary bg-primary/10" : "bg-card"
                      )}
                    >
                      <input type="checkbox" checked={on} onChange={() => toggle(bit)} />
                      <span className="truncate">{label}</span>
                    </label>
                  )
                })}
              </div>
            </TabsContent>
            <TabsContent value="folders" className="space-y-1 pt-3">
              <span className="text-xs font-medium uppercase text-muted-foreground">Folder rights</span>
              <textarea
                value={folders}
                onChange={(e) => setFolders(e.target.value)}
                className="bg-background min-h-[260px] w-full rounded-md border p-2 font-mono text-xs"
              />
            </TabsContent>
          </Tabs>
        )}
      </ScrollArea>
    </div>
  )
}
