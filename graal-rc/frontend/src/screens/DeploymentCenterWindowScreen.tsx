import {useCallback, useEffect, useMemo, useRef, useState} from "react"
import {DiffEditor, type BeforeMount} from "@monaco-editor/react"
import {Events} from "@wailsio/runtime"
import {ArchiveRestore, CheckCircle2, ClipboardList, Diff, Filter, GitCompare, History, Loader2, RefreshCw, RotateCcw, Save, Search, Settings2, ShieldAlert, Trash2, XCircle} from "lucide-react"
import {toast} from "sonner"

import {ConfirmDialog} from "@/components/ConfirmDialog"
import {AlertDialog, AlertDialogContent, AlertDialogDescription, AlertDialogHeader, AlertDialogTitle} from "@/components/ui/alert-dialog"
import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {useCodingSettings} from "@/hooks/useCodingSettings"
import {registerGraalScript} from "@/lib/monacoGraalScript"
import {ensureTheme} from "@/lib/monacoThemes"
import {registerServerConfig} from "@/lib/monacoServerConfig"
import {rcService} from "@/services/rcService"
import type {AuditEntry, ChangeRetentionSettings, DeploymentBackup, DeploymentBackupDiff} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

const DEFAULT_BACKUP_COUNT = 3
const MIN_BACKUP_COUNT = 1
const MAX_BACKUP_COUNT = 100
const DEFAULT_RETENTION: ChangeRetentionSettings = {auditDays: 0, backupDays: 0, backupCount: DEFAULT_BACKUP_COUNT}

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
  const {settings} = useCodingSettings()
  const [audit, setAudit] = useState<AuditEntry[]>([])
  const [backups, setBackups] = useState<DeploymentBackup[]>([])
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [pendingRollback, setPendingRollback] = useState<DeploymentBackup | null>(null)
  const [pendingDelete, setPendingDelete] = useState<DeploymentBackup | null>(null)
  const [diffBackup, setDiffBackup] = useState<DeploymentBackup | null>(null)
  const [backupDiff, setBackupDiff] = useState<DeploymentBackupDiff | null>(null)
  const [diffLoading, setDiffLoading] = useState(false)
  const [diffError, setDiffError] = useState<string | null>(null)
  const diffRequestRef = useRef(0)
  const [clearOpen, setClearOpen] = useState(false)
  const [activeTab, setActiveTab] = useState("audit")
  const [retention, setRetention] = useState<ChangeRetentionSettings>(DEFAULT_RETENTION)
  const [retentionDraft, setRetentionDraft] = useState<ChangeRetentionSettings>(DEFAULT_RETENTION)
  const [retentionLoading, setRetentionLoading] = useState(true)
  const [retentionSaving, setRetentionSaving] = useState(false)

  const retentionOptions = useMemo(() => [
    {value: 0, label: t("deploy.retentionNever")},
    {value: 1, label: t("deploy.retentionDay")},
    {value: 7, label: t("deploy.retentionDays", {days: 7})},
    {value: 30, label: t("deploy.retentionDays", {days: 30})},
    {value: 90, label: t("deploy.retentionDays", {days: 90})},
    {value: 180, label: t("deploy.retentionDays", {days: 180})},
    {value: 365, label: t("deploy.retentionDays", {days: 365})},
    {value: 730, label: t("deploy.retentionDays", {days: 730})},
    {value: 1825, label: t("deploy.retentionDays", {days: 1825})},
    {value: 3650, label: t("deploy.retentionDays", {days: 3650})},
  ], [t])

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

  const loadRetention = useCallback(async () => {
    setRetentionLoading(true)
    try {
      const settings = await rcService.getChangeRetention()
      const next: ChangeRetentionSettings = {
        auditDays: settings?.auditDays ?? DEFAULT_RETENTION.auditDays,
        backupDays: settings?.backupDays ?? DEFAULT_RETENTION.backupDays,
        backupCount: settings?.backupCount && settings.backupCount >= MIN_BACKUP_COUNT
          ? Math.min(settings.backupCount, MAX_BACKUP_COUNT)
          : DEFAULT_BACKUP_COUNT,
      }
      setRetention(next)
      setRetentionDraft(next)
    } catch (err) {
      toast.error(t("deploy.retentionLoadFailed"), {description: err instanceof Error ? err.message : String(err)})
    } finally {
      setRetentionLoading(false)
    }
  }, [t])

  useEffect(() => {
    void refresh(true)
    void loadRetention()
    const off = Events.On("rc:auditChanged", () => void refresh())
    return off
  }, [loadRetention, refresh])

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

  const closeBackupDiff = useCallback(() => {
    diffRequestRef.current += 1
    setDiffBackup(null)
    setBackupDiff(null)
    setDiffError(null)
    setDiffLoading(false)
  }, [])

  const openBackupDiff = useCallback((backup: DeploymentBackup) => {
    const requestID = ++diffRequestRef.current
    setDiffBackup(backup)
    setBackupDiff(null)
    setDiffError(null)
    setDiffLoading(true)
    void rcService.getDeploymentBackupDiff(backup.id)
      .then((diff) => {
        if (requestID !== diffRequestRef.current) return
        setBackupDiff(diff)
      })
      .catch((err: unknown) => {
        if (requestID !== diffRequestRef.current) return
        setDiffError(err instanceof Error ? err.message : String(err))
      })
      .finally(() => {
        if (requestID === diffRequestRef.current) setDiffLoading(false)
      })
  }, [])

  const handleDiffBeforeMount = useCallback<BeforeMount>((monaco) => {
    if (backupDiff?.language === "graalscript") {
      registerGraalScript(monaco as Parameters<typeof registerGraalScript>[0])
    } else if (backupDiff?.language === "serverconfig") {
      registerServerConfig(monaco as Parameters<typeof registerServerConfig>[0])
    }
    ensureTheme(monaco as Parameters<typeof ensureTheme>[0], settings.theme)
  }, [backupDiff?.language, settings.theme])

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

  const deleteBackup = async () => {
    if (!pendingDelete) return
    const backup = pendingDelete
    setPendingDelete(null)
    try {
      await rcService.deleteDeploymentBackup(backup.id)
      toast.success(t("deploy.backupDeleted"))
      await refresh()
    } catch (err) {
      toast.error(t("deploy.backupDeleteFailed"), {description: err instanceof Error ? err.message : String(err)})
    }
  }

  const saveRetention = async () => {
    setRetentionSaving(true)
    try {
      await rcService.setChangeRetention(retentionDraft)
      setRetention(retentionDraft)
      toast.success(t("deploy.retentionSaved"))
      await refresh()
    } catch (err) {
      toast.error(t("deploy.retentionSaveFailed"), {description: err instanceof Error ? err.message : String(err)})
    } finally {
      setRetentionSaving(false)
    }
  }

  const retentionChanged = retention.auditDays !== retentionDraft.auditDays
    || retention.backupDays !== retentionDraft.backupDays
    || retention.backupCount !== retentionDraft.backupCount

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
        <Tabs value={activeTab} onValueChange={setActiveTab} className="flex min-h-0 flex-1 flex-col">
          <div className="flex flex-wrap items-center gap-2">
            <TabsList className="w-auto">
              <TabsTrigger value="audit"><ClipboardList className="size-4" />{t("deploy.auditTab")} <Badge variant="outline" className="h-5 px-1.5 text-[10px]">{audit.length}</Badge></TabsTrigger>
              <TabsTrigger value="backups"><ArchiveRestore className="size-4" />{t("deploy.backupsTab")} <Badge variant="outline" className="h-5 px-1.5 text-[10px]">{backups.length}</Badge></TabsTrigger>
              <TabsTrigger value="settings"><Settings2 className="size-4" />{t("deploy.settingsTab")}</TabsTrigger>
            </TabsList>
            {activeTab !== "settings" ? (
              <div className="relative ml-auto min-w-48 flex-1 sm:max-w-xs">
                <Search className="text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
                <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("deploy.filterPlaceholder")} className="h-9 pl-8" />
              </div>
            ) : null}
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
                      <div className="flex items-center gap-1.5">
                        <Button variant="outline" size="sm" onClick={() => openBackupDiff(backup)}><GitCompare className="size-4" />{t("deploy.viewDiff")}</Button>
                        <Button variant="ghost" size="icon" onClick={() => setPendingDelete(backup)} title={t("deploy.deleteBackup")} aria-label={t("deploy.deleteBackup")}><Trash2 className="text-muted-foreground size-4" /></Button>
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </ScrollArea>
          </TabsContent>

          <TabsContent value="settings" className="mt-3 min-h-0 flex-1">
            <ScrollArea className="h-full rounded-md border">
              <div className="mx-auto w-full max-w-2xl space-y-4 p-4 sm:p-6">
                <div>
                  <div className="flex items-center gap-2">
                    <Settings2 className="text-primary size-4" />
                    <h2 className="text-sm font-semibold">{t("deploy.settingsTitle")}</h2>
                  </div>
                  <p className="text-muted-foreground mt-1 text-xs">{t("deploy.settingsDescription")}</p>
                </div>

                <div className="grid gap-3 sm:grid-cols-2">
                  <label className="grid gap-1.5 text-sm">
                    <span className="font-medium">{t("deploy.auditRetention")}</span>
                    <span className="text-muted-foreground text-xs">{t("deploy.auditRetentionDescription")}</span>
                    <select
                      className="bg-background h-9 rounded-md border px-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                      value={retentionDraft.auditDays}
                      onChange={(event) => setRetentionDraft((current) => ({...current, auditDays: Number(event.target.value)}))}
                      disabled={retentionLoading || retentionSaving}
                    >
                      {retentionOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                    </select>
                  </label>
                  <label className="grid gap-1.5 text-sm">
                    <span className="font-medium">{t("deploy.backupRetention")}</span>
                    <span className="text-muted-foreground text-xs">{t("deploy.backupRetentionDescription")}</span>
                    <select
                      className="bg-background h-9 rounded-md border px-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                      value={retentionDraft.backupDays}
                      onChange={(event) => setRetentionDraft((current) => ({...current, backupDays: Number(event.target.value)}))}
                      disabled={retentionLoading || retentionSaving}
                    >
                      {retentionOptions.map((option) => <option key={option.value} value={option.value}>{option.label}</option>)}
                    </select>
                  </label>
                  <label className="grid gap-1.5 text-sm">
                    <span className="font-medium">{t("deploy.backupCount")}</span>
                    <span className="text-muted-foreground text-xs">{t("deploy.backupCountDescription")}</span>
                    <Input
                      type="number"
                      min={MIN_BACKUP_COUNT}
                      max={MAX_BACKUP_COUNT}
                      step={1}
                      value={retentionDraft.backupCount}
                      onChange={(event) => {
                        const value = Number(event.target.value)
                        setRetentionDraft((current) => ({
                          ...current,
                          backupCount: Number.isFinite(value)
                            ? Math.min(MAX_BACKUP_COUNT, Math.max(MIN_BACKUP_COUNT, Math.trunc(value)))
                            : DEFAULT_BACKUP_COUNT,
                        }))
                      }}
                      disabled={retentionLoading || retentionSaving}
                    />
                  </label>
                </div>

                <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
                  <p className="text-muted-foreground max-w-xl text-xs">{t("deploy.retentionHint")}</p>
                  <Button onClick={() => void saveRetention()} disabled={retentionLoading || retentionSaving || !retentionChanged}>
                    <Save className="size-4" />{t("common.save")}
                  </Button>
                </div>
              </div>
            </ScrollArea>
          </TabsContent>
        </Tabs>
      </div>

      <AlertDialog
        open={diffBackup !== null}
        onOpenChange={(open) => {
          if (!open) closeBackupDiff()
        }}
      >
        <AlertDialogContent className="flex h-[min(86svh,780px)] max-w-6xl flex-col gap-3">
          <AlertDialogHeader>
            <AlertDialogTitle className="flex items-center gap-2">
              <GitCompare className="text-primary size-4" />
              {diffBackup ? t("deploy.diffTitle", {target: diffBackup.target}) : t("deploy.diffTitleFallback")}
            </AlertDialogTitle>
            <AlertDialogDescription>{t("deploy.diffDescription")}</AlertDialogDescription>
          </AlertDialogHeader>

          <div className="min-h-0 flex-1">
            {diffLoading ? (
              <div className="text-muted-foreground flex h-full items-center justify-center gap-2 text-sm">
                <Loader2 className="size-4 animate-spin" />{t("deploy.diffLoading")}
              </div>
            ) : diffError ? (
              <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center text-sm">
                <p className="text-destructive font-medium">{t("deploy.diffLoadFailed")}</p>
                <p className="text-muted-foreground max-w-xl">{diffError}</p>
              </div>
            ) : backupDiff ? (
              <div className="flex h-full min-h-0 flex-col gap-3">
                <div className="grid shrink-0 gap-2 text-xs sm:grid-cols-2">
                  <div className="rounded-md border p-2">
                    <p className="font-medium">{t("deploy.backupVersion")}</p>
                    <p className="text-muted-foreground mt-1">{formatDate(backupDiff.backup.timestamp)} · {formatBytes(backupDiff.backup.size)}</p>
                    <p className="text-muted-foreground mt-1 truncate font-mono text-[10px]">{backupDiff.backup.sha256}</p>
                  </div>
                  <div className="rounded-md border p-2">
                    <p className="font-medium">{t("deploy.serverVersion")}</p>
                    <p className="text-muted-foreground mt-1">{backupDiff.currentExists ? formatBytes(backupDiff.currentSize) : t("deploy.serverVersionUnavailable")}</p>
                    <p className="text-muted-foreground mt-1 truncate font-mono text-[10px]">{backupDiff.currentSha256 || "—"}</p>
                  </div>
                </div>

                {backupDiff.diffable ? (
                  <div className="flex min-h-0 flex-1 flex-col gap-1">
                    <div className="text-muted-foreground flex shrink-0 justify-between px-2 text-[11px]">
                      <span>{t("deploy.backupVersion")}</span>
                      <span>{t("deploy.serverVersion")}</span>
                    </div>
                    <div className="min-h-0 flex-1 overflow-hidden rounded-md border">
                      <DiffEditor
                        height="100%"
                        original={backupDiff.backupContent}
                        modified={backupDiff.currentContent}
                        language={backupDiff.language}
                        theme={settings.theme === "remoteTheme" ? "vs-dark" : settings.theme}
                        beforeMount={handleDiffBeforeMount}
                        options={{readOnly: true, renderSideBySide: true, minimap: {enabled: false}, scrollBeyondLastLine: false, automaticLayout: true}}
                      />
                    </div>
                  </div>
                ) : (
                  <div className="bg-muted/20 flex min-h-0 flex-1 flex-col items-center justify-center gap-3 rounded-md border p-6 text-center">
                    <Diff className="text-muted-foreground size-8" />
                    <p className="text-sm font-medium">
                      {backupDiff.diffReason === "too_large" ? t("deploy.diffTooLarge") : t("deploy.diffBinary")}
                    </p>
                    <p className="text-muted-foreground max-w-xl text-xs">{t("deploy.diffMetadataHint")}</p>
                  </div>
                )}
              </div>
            ) : null}
          </div>

          <div className="flex shrink-0 flex-col-reverse gap-2 sm:flex-row sm:justify-end">
            <Button variant="outline" onClick={closeBackupDiff}>{t("common.cancel")}</Button>
            <Button
              onClick={() => {
                if (!backupDiff) return
                const backup = backupDiff.backup
                closeBackupDiff()
                setPendingRollback(backup)
              }}
              disabled={!backupDiff || diffLoading}
            >
              <RotateCcw className="size-4" />{t("deploy.rollback")}
            </Button>
          </div>
        </AlertDialogContent>
      </AlertDialog>

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
      <ConfirmDialog
        open={pendingDelete !== null}
        title={t("deploy.deleteBackupTitle")}
        description={pendingDelete ? t("deploy.deleteBackupDescription", {target: pendingDelete.target}) : ""}
        confirmLabel={t("deploy.deleteBackup")}
        destructive
        onConfirm={() => void deleteBackup()}
        onCancel={() => setPendingDelete(null)}
      />
    </div>
  )
}
