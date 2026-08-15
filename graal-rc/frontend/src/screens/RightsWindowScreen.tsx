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

const RIGHTS_LAYOUT: {labelKey: string; bit: number}[] = [
  {labelKey: "rights.label.warptoXY", bit: 0},
  {labelKey: "rights.label.setServerFlags", bit: 15},
  {labelKey: "rights.label.warptoPlayer", bit: 1},
  {labelKey: "rights.label.changeRights", bit: 10},
  {labelKey: "rights.label.warpPlayers", bit: 2},
  {labelKey: "rights.label.banPlayers", bit: 11},
  {labelKey: "rights.label.updateLevel", bit: 3},
  {labelKey: "rights.label.changeComments", bit: 12},
  {labelKey: "rights.label.disconnectPlayers", bit: 4},
  {labelKey: "rights.label.changeStaffAccounts", bit: 14},
  {labelKey: "rights.label.viewPlayerAttributes", bit: 5},
  {labelKey: "rights.label.changeServerOptions", bit: 16},
  {labelKey: "rights.label.setPlayerAttributes", bit: 6},
  {labelKey: "rights.label.editFolderConfiguration", bit: 17},
  {labelKey: "rights.label.ownAttributes", bit: 7},
  {labelKey: "rights.label.editFolderRights", bit: 18},
  {labelKey: "rights.label.resetAttributes", bit: 8},
  {labelKey: "rights.label.npcControl", bit: 19},
  {labelKey: "rights.label.adminMessage", bit: 9},
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
        <h1 className="text-sm font-semibold">{t("rights.title", {account: target})}</h1>
        <Button className="ml-auto" size="sm" onClick={apply} disabled={loading || saving || !!error}>
          {saving && <Loader2 className="size-4 animate-spin" />} {t("common.apply")}
        </Button>
      </header>
      <ScrollArea className="min-h-0 flex-1 p-4">
        {loading && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> {t("rights.loading")}
          </div>
        )}
        {error && <div className="text-sm text-destructive">{error}</div>}
        {data && !loading && (
          <Tabs defaultValue="flags">
            <TabsList className="grid w-full grid-cols-2">
              <TabsTrigger value="flags">{t("rights.ipFlagsTab")}</TabsTrigger>
              <TabsTrigger value="folders">{t("rights.folderTab")}</TabsTrigger>
            </TabsList>
            <TabsContent value="flags" className="space-y-3 pt-3">
              <div className="space-y-1">
                <span className="text-xs font-medium uppercase text-muted-foreground">{t("rights.account")}</span>
                <Input value={data.account} readOnly className="h-8 bg-muted/40" />
              </div>
              <div className="space-y-1">
                <span className="text-xs font-medium uppercase text-muted-foreground">{t("rights.ipRanges")}</span>
                <Input value={ipRange} onChange={(e) => setIpRange(e.target.value)} className="h-8" />
              </div>
              <div className="flex items-center justify-between">
                <span className="text-xs font-medium uppercase text-muted-foreground">{t("rights.flags")}</span>
                <Button variant="ghost" size="sm" onClick={() => setFlags(0)}>{t("rights.clearAll")}</Button>
              </div>
              <div className="grid grid-cols-2 gap-1.5">
                {RIGHTS_LAYOUT.map(({labelKey, bit}) => {
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
                      <span className="truncate">{t(labelKey)}</span>
                    </label>
                  )
                })}
              </div>
            </TabsContent>
            <TabsContent value="folders" className="space-y-1 pt-3">
              <span className="text-xs font-medium uppercase text-muted-foreground">{t("rights.folderRights")}</span>
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
