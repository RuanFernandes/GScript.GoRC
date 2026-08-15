// BanWindowScreen is the content of the "/openaccess" external window (URL
// "/#ban?a=<account>"). Edits the 4 ban scopes (Local/Global/Computer/Global-
// Computer), ban-type dropdown, time-left, reset, and reason. Mirrors reference
// TLocalBanWindow.cpp. Loads via rcService.openBan + rcService.getBanTypes;
// saves each scope via rcService.setBan.
import {useEffect, useMemo, useState} from "react"
import {Loader2} from "lucide-react"
import {toast} from "sonner"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {rcService} from "@/services/rcService"
import type {BanData} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface ScopeState {
  target: string
  banned: boolean
  reset: boolean
  banType: string
  releaseTime: string
  reason: string
}

const SCOPE_KEYS = ["ban.scopeLocal", "ban.scopeGlobal", "ban.scopeComputer", "ban.scopeGlobalComputer"] as const

function trimBanName(s: string): string {
  return s.replace(/^\s+|\s+$/g, "")
}

function parseBanTypes(raw: string): {name: string; seconds: number}[] {
  const out: {name: string; seconds: number}[] = []
  for (const line of raw.split(/\r?\n/)) {
    if (!line.trim()) continue
    const comma = line.lastIndexOf(",")
    if (comma < 0) {
      out.push({name: trimBanName(line), seconds: 0})
    } else {
      out.push({name: trimBanName(line.slice(0, comma)), seconds: parseInt(line.slice(comma + 1), 10) || 0})
    }
  }
  return out
}

function buildScopes(account: string, computerId: string, details: string, typeNames: string[]): ScopeState[] {
  const scopes: ScopeState[] = SCOPE_KEYS.map((_, index) => ({
    target: index < 2 ? account : computerId ? "pc:" + computerId : "",
    banned: false,
    reset: false,
    banType: typeNames[0] ?? "",
    releaseTime: "",
    reason: "",
  }))
  for (const record of details.split(/\r?\n/)) {
    if (!record.trim()) continue
    const fields: Record<string, string> = {}
    for (const kv of record.split(",")) {
      const sep = kv.indexOf("=")
      if (sep >= 0) fields[kv.slice(0, sep)] = kv.slice(sep + 1)
    }
    const target = fields.account
    const world = fields.world
    if (target === undefined || world === undefined) continue
    const computer = target.startsWith("pc:")
    const index = (computer ? 2 : 0) + (world === "all" ? 1 : 0)
    if (!scopes[index].target) continue
    scopes[index].banned = true
    if (fields.bantype) {
      const bt = trimBanName(fields.bantype)
      if (typeNames.includes(bt)) scopes[index].banType = bt
    }
    if (fields.releasetime) scopes[index].releaseTime = fields.releasetime
    if (fields.reason) scopes[index].reason = fields.reason
  }
  return scopes
}

function banTimeText(seconds: number, t: (key: string, vars?: Record<string, string | number>) => string): string {
  if (seconds <= 0) return t("ban.permanent")
  const days = seconds / 86400
  if (days >= 1) return t("ban.days", {count: days.toFixed(days % 1 ? 1 : 0)})
  const hours = seconds / 3600
  if (hours >= 1) return t("ban.hours", {count: hours.toFixed(0)})
  return t("ban.minutes", {count: (seconds / 60).toFixed(0)})
}

function readAccount(): string {
  const hash = window.location.hash
  const q = hash.indexOf("?")
  const params = new URLSearchParams(q >= 0 ? hash.slice(q + 1) : "")
  return params.get("a") ?? ""
}

export function BanWindowScreen() {
  const {t} = useLanguage()
  const account = readAccount()
  const [data, setData] = useState<BanData | null>(null)
  const [typeNames, setTypeNames] = useState<string[]>([])
  const [typeDurations, setTypeDurations] = useState<number[]>([])
  const [scopes, setScopes] = useState<ScopeState[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [history, setHistory] = useState<string | null>(null)
  const [activity, setActivity] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    Promise.all([rcService.openBan(account), rcService.getBanTypes().catch(() => "")])
      .then(([d, typesRaw]) => {
        if (cancelled || !d) return
        const parsed = parseBanTypes(typesRaw ?? "")
        const names = parsed.map((p) => p.name)
        setData(d)
        setTypeNames(names)
        setTypeDurations(parsed.map((p) => p.seconds))
        setScopes(buildScopes(d.account, d.computerId ?? "", d.details ?? "", names))
      })
      .catch((e) => !cancelled && setError(e instanceof Error ? e.message : String(e)))
      .finally(() => !cancelled && setLoading(false))
    return () => {
      cancelled = true
    }
  }, [account])

  const availableScopes = useMemo(
    () => scopes.map((s, i) => ({i, s, available: !!s.target})).filter((x) => x.available),
    [scopes]
  )
  const patch = (index: number, p: Partial<ScopeState>) =>
    setScopes((prev) => prev.map((s, i) => (i === index ? {...s, ...p} : s)))

  const applyScope = async (index: number) => {
    const s = scopes[index]
    if (!s.target) return
    try {
      const world = index % 2 === 0 ? "local" : "all"
      await rcService.setBan(s.target, world, s.banned, s.banType, s.reset ? "" : s.releaseTime, s.reason)
      toast.success(t("ban.saved", {scope: t(SCOPE_KEYS[index] ?? "ban.scopeLocal")}))
    } catch (e) {
      toast.error(t("common.saveFailed"), {description: e instanceof Error ? e.message : String(e)})
    }
  }

  const showHistory = async () => {
    setHistory(t("ban.loading"))
    try {
      setHistory(await rcService.requestBanHistory(data?.account ?? account))
    } catch (e) {
      setHistory(e instanceof Error ? e.message : String(e))
    }
  }
  const showActivity = async () => {
    setActivity(t("ban.loading"))
    try {
      setActivity(await rcService.requestStaffActivity(data?.account ?? account))
    } catch (e) {
      setActivity(e instanceof Error ? e.message : String(e))
    }
  }

  const target = data?.account ?? account

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-2 border-b px-4 py-2.5">
        <h1 className="text-sm font-semibold">
          {t("ban.title", {account: target})}{data?.computerId ? t("ban.computer", {id: data.computerId}) : ""}
        </h1>
        <div className="ml-auto flex items-center gap-1.5">
          <Button variant="outline" size="sm" onClick={showHistory}>{t("ban.history")}</Button>
          <Button variant="outline" size="sm" onClick={showActivity}>{t("ban.activity")}</Button>
        </div>
      </header>
      <ScrollArea className="min-h-0 flex-1 p-4">
        {loading && (
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2 className="size-4 animate-spin" /> {t("ban.loadingData")}
          </div>
        )}
        {error && <div className="text-sm text-destructive">{error}</div>}
        {data && !loading && availableScopes.length > 0 && (
          <Tabs defaultValue={String(availableScopes[0].i)}>
            <TabsList className="flex w-full">
              {availableScopes.map(({i}) => (
                <TabsTrigger key={i} value={String(i)} className="flex-1">{t(SCOPE_KEYS[i] ?? "ban.scopeLocal")}</TabsTrigger>
              ))}
            </TabsList>
            {availableScopes.map(({i, s}) => {
              const dur = typeDurations[typeNames.indexOf(s.banType)] ?? 0
              return (
                <TabsContent key={i} value={String(i)} className="space-y-3 pt-3">
                  <label className="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={s.banned} onChange={(e) => patch(i, {banned: e.target.checked})} />
                    {t("ban.banned")}
                  </label>
                  <div className="space-y-1">
                    <span className="text-xs font-medium uppercase text-muted-foreground">{t("ban.type")}</span>
                    <select
                      className="bg-background h-8 w-full rounded-md border px-2 text-sm"
                      value={s.banType}
                      onChange={(e) => patch(i, {banType: e.target.value})}
                    >
                      {typeNames.length === 0 && <option value="">{t("ban.noTypes")}</option>}
                      {typeNames.map((n) => <option key={n} value={n}>{n}</option>)}
                    </select>
                  </div>
                  <div className="text-xs text-muted-foreground">{t("ban.timeLeft", {time: s.banned ? banTimeText(dur, t) : "-"})}</div>
                  <label className="flex items-center gap-2 text-sm">
                    <input type="checkbox" checked={s.reset} onChange={(e) => patch(i, {reset: e.target.checked})} />
                    {t("ban.resetTime")}
                  </label>
                  <div className="space-y-1">
                    <span className="text-xs font-medium uppercase text-muted-foreground">{t("ban.reason")}</span>
                    <Input value={s.reason} onChange={(e) => patch(i, {reason: e.target.value})} className="h-8" />
                  </div>
                  <Button size="sm" onClick={() => applyScope(i)}>{t("common.apply")}</Button>
                </TabsContent>
              )
            })}
          </Tabs>
        )}
        {(history !== null || activity !== null) && (
          <div className="mt-4 space-y-3 border-t pt-3">
            {history !== null && (
              <div>
                <div className="text-xs font-medium uppercase text-muted-foreground">{t("ban.history")}</div>
                <pre className="bg-muted/40 max-h-40 overflow-auto rounded-md p-2 text-xs whitespace-pre-wrap">{history}</pre>
              </div>
            )}
            {activity !== null && (
              <div>
                <div className="text-xs font-medium uppercase text-muted-foreground">{t("ban.activity")}</div>
                <pre className="bg-muted/40 max-h-40 overflow-auto rounded-md p-2 text-xs whitespace-pre-wrap">{activity}</pre>
              </div>
            )}
          </div>
        )}
      </ScrollArea>
    </div>
  )
}
