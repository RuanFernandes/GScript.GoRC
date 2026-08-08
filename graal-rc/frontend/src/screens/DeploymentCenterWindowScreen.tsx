import {useCallback, useEffect, useMemo, useState} from "react"
import {Events} from "@wailsio/runtime"
import {ArchiveRestore, CheckCircle2, ClipboardList, Filter, History, RefreshCw, RotateCcw, Search, ShieldAlert, Trash2, XCircle} from "lucide-react"
import {toast} from "sonner"

import {ConfirmDialog} from "@/components/ConfirmDialog"
import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {rcService} from "@/services/rcService"
import type {AuditEntry, DeploymentBackup} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

function formatDate(timestamp: number): string {
  if (!timestamp) return "—"
  return new Date(timestamp).toLocaleString()
}

function formatBytes(size: number): string {
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

export function DeploymentCenterWindowScreen() {
  const {t} = useLanguage()
  const [audit, setAudit] = useState<AuditEntry[]>([])
  const [backups, setBackups] = useState<DeploymentBackup[]>([])
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [pendingRollback, setPendingRollback] = useState<DeploymentBackup | null>(null)
  const [clearOpen, setClearOpen] = useState(false)

  const refresh = useCallback(async (initial = false) => {
    if (initial) setLoading(true)
    else setRefreshing(true)
    try {
      const [entries, snapshots] = await Promise.all([rcService.getAuditEntries(300), rcService.getDeploymentBackups(200)])
      setAudit(entries ?? [])
      setBackups(snapshots ?? [])
    } catch (err) {
      toast.error(t("deploy.loadFailed"), {description: err instanceof Error ? err.message : String(err)})
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [t])

  useEffect(() => {
    void refresh(true)
    const off = Events.On("rc:auditChanged", () => void refresh())
    return off
  }, [refresh])

  const filteredAudit = useMemo(() => {
    const q = query.trim().toLocaleLowerCase()
    if (!q) return audit
    return audit.filter((entry) => `${entry.action} ${entry.resource} ${entry.target} ${entry.outcome} ${entry.detail ?? ""}`.toLocaleLowerCase().includes(q))
  }, [audit, query])

  const rollback = async () => {
    if (!pendingRollback) return
    const backup = pendingRollback
    setPendingRollback(null)
    try {
      await rcService.rollbackDeployment(backup.id)
      toast.success(t("deploy.rollbackComplete"))
      await refresh()
    } catch (err) {
      toast.error(t("deploy.rollbackFailed"), {description: err instanceof Error ? err.message : String(err)})
    }
  }

  const clearAudit = async () => {
    setClearOpen(false)
    try {
      await rcService.clearAuditEntries()
      toast.success(t("deploy.auditCleared"))
      await refresh()
    } catch (err) {
      toast.error(t("deploy.clearFailed"), {description: err instanceof Error ? err.message : String(err)})
    }
  }

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3">
        <div className="flex min-w-0 items-center gap-2">
          <ShieldAlert className="text-primary size-4" />
          <div className="min-w-0">
            <h1 className="truncate text-sm font-semibold">{t("deploy.title")}</h1>
            <p className="text-muted-foreground text-xs">{t("deploy.subtitle")}</p>
          </div>
        </div>
        <div className="ml-auto flex items-center gap-1.5">
          <Button variant="outline" size="sm" onClick={() => void refresh()} disabled={refreshing}>
            <RefreshCw className={refreshing ? "size-4 animate-spin" : "size-4"} />{t("common.refresh")}
          </Button>
          <Button variant="ghost" size="sm" onClick={() => setClearOpen(true)} disabled={audit.length === 0}>
            <Trash2 className="size-4" />{t("deploy.clearAudit")}
          </Button>
        </div>
      </header>

      <div className="flex min-h-0 flex-1 flex-col p-3">
        <Tabs defaultValue="audit" className="flex min-h-0 flex-1 flex-col">
          <div className="flex flex-wrap items-center gap-2">
            <TabsList className="w-auto">
              <TabsTrigger value="audit"><ClipboardList className="size-4" />{t("deploy.auditTab")} <Badge variant="outline" className="h-5 px-1.5 text-[10px]">{audit.length}</Badge></TabsTrigger>
              <TabsTrigger value="backups"><ArchiveRestore className="size-4" />{t("deploy.backupsTab")} <Badge variant="outline" className="h-5 px-1.5 text-[10px]">{backups.length}</Badge></TabsTrigger>
            </TabsList>
            <div className="relative ml-auto min-w-48 flex-1 sm:max-w-xs">
              <Search className="text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
              <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("deploy.filterPlaceholder")} className="h-9 pl-8" />
            </div>
          </div>

          <TabsContent value="audit" className="mt-3 min-h-0 flex-1">
            <ScrollArea className="h-full rounded-md border">
              {loading ? (
                <div className="text-muted-foreground flex h-32 items-center justify-center text-sm">{t("common.loading")}</div>
              ) : filteredAudit.length === 0 ? (
                <div className="text-muted-foreground flex h-32 items-center justify-center gap-2 text-sm"><Filter className="size-4" />{t("deploy.noAudit")}</div>
              ) : (
                <div className="divide-y">
                  {filteredAudit.map((entry) => (
                    <div key={entry.id} className="grid gap-2 px-3 py-3 text-xs sm:grid-cols-[minmax(8rem,0.75fr)_minmax(12rem,1.4fr)_minmax(8rem,0.75fr)_auto] sm:items-center">
                      <div className="flex items-center gap-2">
                        {entry.outcome === "success" ? <CheckCircle2 className="size-4 text-emerald-500" /> : <XCircle className="text-destructive size-4" />}
                        <span className="font-medium">{entry.action}</span>
                        <Badge variant={entry.outcome === "success" ? "secondary" : "destructive"}>{entry.outcome}</Badge>
                      </div>
                      <div className="min-w-0">
                        <p className="truncate font-medium">{entry.target}</p>
                        <p className="text-muted-foreground truncate">{entry.resource}{entry.detail ? ` · ${entry.detail}` : ""}</p>
                      </div>
                      <div className="text-muted-foreground flex items-center gap-1.5"><History className="size-3.5" />{formatDate(entry.timestamp)}</div>
                      <span className="text-muted-foreground truncate font-mono text-[10px]">{entry.server || t("deploy.localOnly")}</span>
                    </div>
                  ))}
                </div>
              )}
            </ScrollArea>
          </TabsContent>

          <TabsContent value="backups" className="mt-3 min-h-0 flex-1">
            <ScrollArea className="h-full rounded-md border">
              {loading ? (
                <div className="text-muted-foreground flex h-32 items-center justify-center text-sm">{t("common.loading")}</div>
              ) : backups.length === 0 ? (
                <div className="text-muted-foreground flex h-32 items-center justify-center gap-2 text-sm"><ArchiveRestore className="size-4" />{t("deploy.noBackups")}</div>
              ) : (
                <div className="divide-y">
                  {backups.map((backup) => (
                    <div key={backup.id} className="flex flex-wrap items-center gap-3 px-3 py-3 text-xs">
                      <ArchiveRestore className="text-primary size-4 shrink-0" />
                      <div className="min-w-48 flex-1">
                        <p className="truncate font-medium">{backup.target}</p>
                        <p className="text-muted-foreground truncate">{backup.resource} · {formatBytes(backup.size)} · {formatDate(backup.timestamp)}</p>
                      </div>
                      <code className="text-muted-foreground hidden max-w-52 truncate sm:block">{backup.sha256.slice(0, 16)}…</code>
                      <Button variant="outline" size="sm" onClick={() => setPendingRollback(backup)}><RotateCcw className="size-4" />{t("deploy.rollback")}</Button>
                    </div>
                  ))}
                </div>
              )}
            </ScrollArea>
          </TabsContent>
        </Tabs>
      </div>

      <ConfirmDialog
        open={pendingRollback !== null}
        title={t("deploy.rollbackTitle")}
        description={pendingRollback ? t("deploy.rollbackDescription", {target: pendingRollback.target}) : ""}
        confirmLabel={t("deploy.rollback")}
        destructive
        onConfirm={() => void rollback()}
        onCancel={() => setPendingRollback(null)}
      />
      <ConfirmDialog
        open={clearOpen}
        title={t("deploy.clearAuditTitle")}
        description={t("deploy.clearAuditDescription")}
        confirmLabel={t("deploy.clearAudit")}
        destructive
        onConfirm={() => void clearAudit()}
        onCancel={() => setClearOpen(false)}
      />
    </div>
  )
}
