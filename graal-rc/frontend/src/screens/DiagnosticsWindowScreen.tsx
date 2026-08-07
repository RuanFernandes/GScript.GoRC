import {useCallback, useEffect, useMemo, useState} from "react"
import {Events} from "@wailsio/runtime"
import {Activity, Check, Clipboard, Download, RefreshCw, ShieldCheck, Wifi, WifiOff} from "lucide-react"
import {toast} from "sonner"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from "@/components/ui/card"
import {rcService} from "@/services/rcService"
import type {DiagnosticsSnapshot} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

function formatTime(timestamp: number): string {
  return timestamp ? new Date(timestamp).toLocaleString() : "—"
}

function valueLabel(value: boolean, t: (key: string) => string): string {
  return value ? t("diagnostics.yes") : t("diagnostics.no")
}

function Detail({label, value}: {label: string; value: string | number}) {
  const displayValue = typeof value === "number" ? value.toLocaleString() : value || "—"
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground text-[11px] uppercase tracking-wide">{label}</dt>
      <dd className="mt-1 truncate text-sm font-medium" title={String(displayValue)}>{displayValue}</dd>
    </div>
  )
}

export function DiagnosticsWindowScreen() {
  const {t} = useLanguage()
  const [snapshot, setSnapshot] = useState<DiagnosticsSnapshot | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [copied, setCopied] = useState(false)

  const refresh = useCallback(async (initial = false) => {
    if (initial) setLoading(true)
    else setRefreshing(true)
    try {
      setSnapshot(await rcService.getDiagnostics())
    } catch (err) {
      toast.error(t("diagnostics.loadFailed"), {description: err instanceof Error ? err.message : String(err)})
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [t])

  useEffect(() => {
    void refresh(true)
    const events = ["rc:reconnect", "rc:reconnected", "rc:reconnectFailed", "rc:pumpError"]
    const offs = events.map((name) => Events.On(name, () => void refresh()))
    return () => offs.forEach((off) => off())
  }, [refresh])

  const serialized = useMemo(() => snapshot ? JSON.stringify(snapshot, null, 2) : "", [snapshot])

  const copy = async () => {
    if (!serialized) return
    try {
      await navigator.clipboard.writeText(serialized)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
      toast.success(t("diagnostics.copied"))
    } catch (err) {
      toast.error(t("diagnostics.copyFailed"), {description: err instanceof Error ? err.message : String(err)})
    }
  }

  const exportReport = async () => {
    try {
      const path = await rcService.exportDiagnostics()
      if (path) toast.success(t("diagnostics.exported", {path}))
    } catch (err) {
      toast.error(t("diagnostics.exportFailed"), {description: err instanceof Error ? err.message : String(err)})
    }
  }

  return (
    <div className="bg-background flex h-svh min-w-0 flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <Activity className="text-primary size-4" />
          <div className="min-w-0">
            <h1 className="truncate text-sm font-semibold">{t("diagnostics.title")}</h1>
            <p className="text-muted-foreground text-xs">{t("diagnostics.subtitle")}</p>
          </div>
        </div>
        <div className="ml-auto flex flex-wrap items-center gap-1.5">
          <Button variant="outline" size="sm" onClick={() => void refresh()} disabled={refreshing}>
            <RefreshCw className={refreshing ? "size-4 animate-spin" : "size-4"} />{t("common.refresh")}
          </Button>
          <Button variant="outline" size="sm" onClick={() => void copy()} disabled={!serialized}>
            {copied ? <Check className="size-4" /> : <Clipboard className="size-4" />}{copied ? t("diagnostics.copied") : t("diagnostics.copy")}
          </Button>
          <Button size="sm" onClick={() => void exportReport()} disabled={!snapshot}>
            <Download className="size-4" />{t("diagnostics.export")}
          </Button>
        </div>
      </header>

      <main className="min-h-0 flex-1 overflow-y-auto p-3 sm:p-4">
        {loading || !snapshot ? (
          <div className="text-muted-foreground flex h-40 items-center justify-center text-sm">{t("common.loading")}</div>
        ) : (
          <div className="mx-auto grid max-w-5xl gap-3">
            <div className="grid gap-3 md:grid-cols-2">
              <Card className="gap-4 py-4">
                <CardHeader className="gap-1 px-4">
                  <CardTitle className="flex items-center gap-2 text-sm"><ShieldCheck className="size-4" />{t("diagnostics.runtime")}</CardTitle>
                  <CardDescription>{t("diagnostics.runtimeDescription")}</CardDescription>
                </CardHeader>
                <CardContent className="px-4">
                  <dl className="grid grid-cols-2 gap-4">
                    <Detail label={t("diagnostics.platform")} value={`${snapshot.os} · ${snapshot.arch}`} />
                    <Detail label={t("diagnostics.goVersion")} value={snapshot.goVersion} />
                    <Detail label={t("diagnostics.dllLoaded")} value={valueLabel(snapshot.dllLoaded, t)} />
                    <Detail label={t("diagnostics.dllPath")} value={snapshot.dllPath || "—"} />
                  </dl>
                </CardContent>
              </Card>

              <Card className="gap-4 py-4">
                <CardHeader className="gap-1 px-4">
                  <CardTitle className="flex items-center gap-2 text-sm">{snapshot.connected ? <Wifi className="size-4 text-emerald-500" /> : <WifiOff className="text-muted-foreground size-4" />}{t("diagnostics.connection")}</CardTitle>
                  <CardDescription>{t("diagnostics.connectionDescription")}</CardDescription>
                </CardHeader>
                <CardContent className="px-4">
                  <dl className="grid grid-cols-2 gap-4">
                    <Detail label={t("diagnostics.server")} value={snapshot.serverName || "—"} />
                    <Detail label={t("diagnostics.authenticated")} value={valueLabel(snapshot.authenticated, t)} />
                    <Detail label="NC" value={snapshot.nc.authenticated ? t("diagnostics.connected") : snapshot.nc.connected ? t("diagnostics.connecting") : t("diagnostics.off")} />
                    <Detail label={t("diagnostics.lastPumpError")} value={snapshot.pumpError || t("diagnostics.none")} />
                  </dl>
                </CardContent>
              </Card>
            </div>

            <Card className="gap-4 py-4">
              <CardHeader className="gap-1 px-4">
                <CardTitle className="flex items-center gap-2 text-sm"><RefreshCw className="size-4" />{t("diagnostics.recovery")}</CardTitle>
                <CardDescription>{t("diagnostics.recoveryDescription")}</CardDescription>
              </CardHeader>
              <CardContent className="px-4">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge variant={snapshot.reconnect.active ? "secondary" : snapshot.reconnect.lastError ? "destructive" : "outline"}>
                    {snapshot.reconnect.active ? t("diagnostics.reconnecting") : snapshot.reconnect.lastError ? t("diagnostics.reconnectFailed") : t("diagnostics.idle")}
                  </Badge>
                  {snapshot.reconnect.active && <span className="text-sm">{t("diagnostics.attempt", {attempt: snapshot.reconnect.attempt, max: snapshot.reconnect.maxAttempts})}</span>}
                  {snapshot.reconnect.nextAttemptAt ? <span className="text-muted-foreground text-xs">{t("diagnostics.nextAttempt", {time: formatTime(snapshot.reconnect.nextAttemptAt)})}</span> : null}
                  {snapshot.reconnect.lastError ? <span className="text-destructive min-w-0 truncate text-xs" title={snapshot.reconnect.lastError}>{snapshot.reconnect.lastError}</span> : null}
                </div>
              </CardContent>
            </Card>

            <Card className="gap-4 py-4">
              <CardHeader className="gap-1 px-4">
                <CardTitle className="text-sm">{t("diagnostics.sync")}</CardTitle>
                <CardDescription>{t("diagnostics.syncDescription")}</CardDescription>
              </CardHeader>
              <CardContent className="px-4">
                <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
                  <Detail label={t("diagnostics.enabled")} value={valueLabel(snapshot.sync.enabled, t)} />
                  <Detail label={t("diagnostics.syncServer")} value={snapshot.sync.server || "—"} />
                  <Detail label={t("diagnostics.lastSync")} value={formatTime(snapshot.sync.lastSyncAt)} />
                  <Detail label={t("diagnostics.reviewItems")} value={snapshot.sync.reviewCount} />
                </dl>
              </CardContent>
            </Card>

            <div className="text-muted-foreground flex flex-wrap justify-between gap-2 px-1 text-[11px]">
              <span>{t("diagnostics.generatedAt", {time: formatTime(snapshot.generatedAt)})}</span>
              <code className="max-w-full truncate">{t("diagnostics.redacted")}</code>
            </div>
            <pre className="bg-muted/30 max-h-56 overflow-auto rounded-md border p-3 font-mono text-[11px] leading-relaxed">{serialized}</pre>
          </div>
        )}
      </main>
    </div>
  )
}
