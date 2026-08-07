import {Activity, AlertTriangle, ArchiveRestore, CheckCircle2, Code2, Database, FileText, FolderOpen, MessageSquare, RefreshCw, Search, Server, Settings, Users, Wifi, WifiOff} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Card, CardContent, CardDescription, CardHeader, CardTitle} from "@/components/ui/card"
import type {ChatMessage, NCStatus, Player, SyncStatus} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface OperationsOverviewProps {
  serverName: string
  players: Player[]
  nc: NCStatus
  sync: SyncStatus
  unreadTotal: number
  scriptCounts: {weapons: number; classes: number; npcs: number}
  recentMessages: ChatMessage[]
  onOpenPlayers: () => void
  onOpenScripts: () => void
  onOpenFiles: () => void
  onOpenSync: () => void
  onOpenDeployments: () => void
  onOpenSettings: () => void
}

function relativeTime(timestamp: number, locale: string): string {
  const seconds = Math.max(0, Math.round((Date.now() - timestamp) / 1000))
  if (seconds < 10) return locale === "pt-BR" ? "agora" : seconds === 0 ? "now" : `${seconds}s`
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  return `${Math.floor(minutes / 60)}h`
}

function ConnectionDot({online}: {online: boolean}) {
  return <span aria-hidden className={`size-2 rounded-full ${online ? "bg-emerald-500" : "bg-muted-foreground/40"}`} />
}

export function OperationsOverview({
  serverName,
  players,
  nc,
  sync,
  unreadTotal,
  scriptCounts,
  recentMessages,
  onOpenPlayers,
  onOpenScripts,
  onOpenFiles,
  onOpenSync,
  onOpenDeployments,
  onOpenSettings,
}: OperationsOverviewProps) {
  const {t, language} = useLanguage()
  const totalScripts = scriptCounts.weapons + scriptCounts.classes + scriptCounts.npcs
  const syncNeedsReview = sync.reviewCount > 0
  const syncLabel = syncNeedsReview
    ? t("dashboard.syncConflicts", {count: sync.reviewCount, suffix: sync.reviewCount === 1 ? "" : "s"})
    : sync.progress.active
      ? t("dashboard.syncing")
      : sync.enabled && !sync.ncDown
        ? t("dashboard.syncReady")
        : t("dashboard.syncDisabled")

  return (
    <div className="flex h-full min-h-0 flex-col gap-3 overflow-y-auto pr-1">
      <section className="flex flex-wrap items-start justify-between gap-3 border-b pb-3">
        <div>
          <div className="text-muted-foreground flex items-center gap-2 text-xs font-medium">
            <Activity className="size-3.5" />
            {t("dashboard.overview")}
          </div>
          <h1 className="mt-1 text-xl font-semibold tracking-tight">{t("dashboard.title")}</h1>
          <p className="text-muted-foreground mt-1 text-sm">{t("dashboard.subtitle", {server: serverName})}</p>
        </div>
        <Button variant="outline" size="sm" onClick={onOpenSettings}>
          <Settings className="size-4" />
          {t("rc.settings")}
        </Button>
      </section>

      <section className="grid gap-3 md:grid-cols-3" aria-label={t("dashboard.statusSummary")}>
        <Card className="gap-3 py-4">
          <CardHeader className="gap-1 px-4">
            <CardDescription className="flex items-center gap-2"><Server className="size-3.5" />{t("dashboard.connection")}</CardDescription>
            <CardTitle className="flex items-center gap-2 text-base"><ConnectionDot online /><span>{t("dashboard.rcConnected")}</span></CardTitle>
          </CardHeader>
          <CardContent className="flex items-center gap-3 px-4 text-xs">
            <span className="text-muted-foreground flex items-center gap-1.5"><ConnectionDot online={nc.connected && nc.authenticated} />NC</span>
            <span className="text-muted-foreground">{serverName}</span>
          </CardContent>
        </Card>

        <Card className="gap-3 py-4">
          <CardHeader className="gap-1 px-4">
            <CardDescription className="flex items-center gap-2"><Users className="size-3.5" />{t("dashboard.players")}</CardDescription>
            <CardTitle className="text-base">{players.length}</CardTitle>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-2 px-4 text-xs">
            <span className="text-muted-foreground">{t("dashboard.onlineNow")}</span>
            <Button variant="ghost" size="sm" className="h-7 px-2" onClick={onOpenPlayers}>{t("dashboard.openPlayers")}</Button>
          </CardContent>
        </Card>

        <Card className={`gap-3 py-4 ${syncNeedsReview ? "border-amber-500/50" : ""}`}>
          <CardHeader className="gap-1 px-4">
            <CardDescription className="flex items-center gap-2"><RefreshCw className="size-3.5" />{t("dashboard.sync")}</CardDescription>
            <CardTitle className="flex items-center gap-2 text-base">
              {syncNeedsReview ? <AlertTriangle className="size-4 text-amber-500" /> : sync.enabled && !sync.ncDown ? <CheckCircle2 className="size-4 text-emerald-500" /> : <WifiOff className="text-muted-foreground size-4" />}
              {syncLabel}
            </CardTitle>
          </CardHeader>
          <CardContent className="flex items-center justify-between gap-2 px-4 text-xs">
            <span className="text-muted-foreground">{totalScripts} {t("dashboard.scriptsIndexed")}</span>
            {syncNeedsReview && <Button variant="outline" size="sm" className="h-7 px-2" onClick={onOpenSync}>{t("dashboard.review")}</Button>}
          </CardContent>
        </Card>
      </section>

      <section className="grid min-h-0 gap-3 lg:grid-cols-[minmax(0,1.1fr)_minmax(18rem,0.9fr)]">
        <Card className="min-h-0 gap-3 py-4">
          <CardHeader className="gap-1 px-4">
            <CardTitle className="flex items-center gap-2 text-sm"><MessageSquare className="size-4" />{t("dashboard.recentActivity")}</CardTitle>
            <CardDescription>{t("dashboard.recentActivityDescription")}</CardDescription>
          </CardHeader>
          <CardContent className="min-h-0 px-4">
            {recentMessages.length === 0 ? (
              <div className="text-muted-foreground flex min-h-28 items-center justify-center text-sm">{t("dashboard.noActivity")}</div>
            ) : (
              <div className="grid gap-1.5">
                {recentMessages.map((message) => (
                  <div key={`${message.id}-${message.ts}`} className="flex min-w-0 items-start gap-2 rounded-md px-2 py-1.5 hover:bg-accent/50">
                    <span className="text-muted-foreground mt-0.5 w-7 shrink-0 text-right font-mono text-[10px] tabular-nums">{relativeTime(message.ts, language)}</span>
                    <Badge variant="outline" className="mt-0.5 h-5 shrink-0 px-1.5 text-[10px]">{message.source.toUpperCase()}</Badge>
                    <span className="min-w-0 truncate text-xs">{message.text}</span>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="gap-3 py-4">
          <CardHeader className="gap-1 px-4">
            <CardTitle className="text-sm">{t("dashboard.quickActions")}</CardTitle>
            <CardDescription>{t("dashboard.quickActionsDescription")}</CardDescription>
          </CardHeader>
          <CardContent className="grid gap-1 px-4">
            <Button variant="ghost" className="justify-start" onClick={onOpenPlayers}><Users className="size-4" />{t("dashboard.openPlayers")}</Button>
            <Button variant="ghost" className="justify-start" onClick={onOpenScripts}><Code2 className="size-4" />{t("dashboard.openScripts")}</Button>
            <Button variant="ghost" className="justify-start" onClick={onOpenFiles}><FolderOpen className="size-4" />{t("dashboard.openFiles")}</Button>
            <Button variant="ghost" className="justify-start" onClick={onOpenSync}><RefreshCw className="size-4" />{t("dashboard.openSync")}</Button>
            <Button variant="ghost" className="justify-start" onClick={onOpenDeployments}><ArchiveRestore className="size-4" />{t("dashboard.openHistory")}</Button>
            <div className="text-muted-foreground mt-2 flex items-center gap-2 border-t pt-3 text-xs">
              <Wifi className="size-3.5" />
              {unreadTotal > 0 ? t("dashboard.unreadMessages", {count: unreadTotal, suffix: unreadTotal === 1 ? "" : "s"}) : t("dashboard.noUnreadMessages")}
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="text-muted-foreground flex items-center gap-2 border-t pt-3 text-xs">
        <Database className="size-3.5" />
        {t("dashboard.scriptBreakdown", {weapons: scriptCounts.weapons, classes: scriptCounts.classes, npcs: scriptCounts.npcs})}
        <FileText className="ml-auto size-3.5" />
        <span>{t("dashboard.searchHint")}</span>
        <Search className="size-3.5" />
      </section>
    </div>
  )
}
