import {useState, type ReactNode} from "react"
import {AlertTriangle, Check, FolderSync, Gauge, Info, RefreshCw} from "lucide-react"

import {useSync} from "@/hooks/useSync"
import {useLanguage} from "@/hooks/useLanguage"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {Badge} from "@/components/ui/badge"
import {rcService} from "@/services/rcService"
import {ScriptSyncRequiredDialog} from "@/components/ScriptSyncRequiredDialog"

function Toggle({checked, onChange, label}: {checked: boolean; onChange: (v: boolean) => void; label: string}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="border-border bg-background hover:bg-accent/60 flex w-full items-center justify-between rounded-md border px-3 py-2.5 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
    >
      <span className="text-sm font-medium">{label}</span>
      <span className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${checked ? "bg-primary" : "bg-input"}`}>
        <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-all ${checked ? "left-[18px]" : "left-0.5"}`} />
      </span>
    </button>
  )
}

function Field({label, htmlFor, children}: {label: string; htmlFor?: string; children: ReactNode}) {
  return <div className="grid gap-1.5"><Label htmlFor={htmlFor} className="text-muted-foreground text-xs font-medium">{label}</Label>{children}</div>
}

function translateProgressPhase(phase: string, t: (key: string, vars?: Record<string, string | number>) => string): string {
  const keys: Record<string, string> = {
    Downloading: "sync.downloading",
    Writing: "sync.writing",
    Comparing: "sync.comparing",
  }
  return t(keys[phase] ?? phase)
}

export function SyncSection() {
  const {config, status, loaded, saveConfig, syncNow} = useSync()
  const {t} = useLanguage()
  const [syncPromptOpen, setSyncPromptOpen] = useState(false)
  const [disablePromptOpen, setDisablePromptOpen] = useState(false)

  const browse = async () => {
    const chosen = await rcService.chooseDirectory()
    if (chosen) saveConfig({outputDir: chosen})
  }

  // Status comes from the engine for the currently connected server. It is
  // authoritative during reconnects, while config is still being refreshed.
  const enabled = status.enabled && status.outputDir !== ""
  const syncRequired = status.syncRequired
  const configuredFolder = Boolean((config.outputDir || status.outputDir).trim())
  const progress = status.progress ?? {active: false, phase: "", current: "", completed: 0, total: 0}
  const total = progress.total
  const completed = Math.min(progress.completed, total || progress.completed)
  const percentage = total > 0 ? Math.min(100, Math.round((completed / total) * 100)) : progress.active ? 8 : 0
  const state = status.panicMode ? t("sync.panic") : status.permissionsError ? t("sync.permissionsUnavailable") : status.ncDown ? t("sync.offline") : status.paused ? t("sync.paused") : !status.enabled ? t("sync.disabled") : !status.outputDir ? t("sync.chooseFolder") : progress.active ? translateProgressPhase(progress.phase, t) : t("sync.watching")
  const tone = status.panicMode ? "bg-red-500" : status.permissionsError || status.ncDown || status.paused ? "bg-amber-500" : enabled ? "bg-emerald-500" : "bg-muted-foreground/50"
  const lastSync = status.lastSyncAt ? new Date(status.lastSyncAt * 1000).toLocaleTimeString() : t("common.never")
  const syncBusy = status.initialSync || progress.active

  const toggleSync = (next: boolean) => {
    if (syncRequired && !next) {
      setDisablePromptOpen(true)
      return
    }
    if (syncRequired && !configuredFolder) {
      setSyncPromptOpen(true)
      return
    }
    saveConfig({enabled: next})
  }

  return (
    <div className="grid gap-4">
      <div className="border-border bg-muted/25 rounded-lg border p-3">
        <div className="flex items-start gap-3">
          <div className={`mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md ${enabled ? "bg-emerald-500/12 text-emerald-500" : "bg-muted text-muted-foreground"}`}>
            {status.panicMode ? <AlertTriangle className="text-red-500 size-4" /> : progress.active ? <RefreshCw className="size-4 animate-spin" /> : <FolderSync className="size-4" />}
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2"><p className="text-sm font-semibold">{state}</p><span className={`size-1.5 rounded-full ${tone}`} /></div>
            <p className="text-muted-foreground mt-0.5 truncate text-xs">{status.server ? t("sync.connectedTo", {server: status.server}) : t("sync.noServer")}</p>
          </div>
          {status.reviewCount > 0 && <Badge variant="destructive">{status.reviewCount} conflict{status.reviewCount === 1 ? "" : "s"}</Badge>}
        </div>

        <div className="mt-4">
          <div className="mb-1.5 flex items-center justify-between text-xs"><span className="text-muted-foreground">{progress.active ? progress.current || t("sync.checking") : t("sync.lastCycle")}</span><span className="font-mono font-medium tabular-nums">{total > 0 ? `${completed}/${total}` : progress.active ? "..." : t("sync.ready")}</span></div>
          <div className="bg-muted h-2 overflow-hidden rounded-full" role="progressbar" aria-label={t("sync.progress")} aria-valuemin={0} aria-valuemax={total || 100} aria-valuenow={total ? completed : percentage}><div className={`h-full rounded-full transition-[width] duration-300 ${status.reviewCount > 0 ? "bg-amber-500" : "bg-primary"}`} style={{width: `${percentage}%`}} /></div>
          <div className="text-muted-foreground mt-2 flex items-center justify-end text-[11px]"><span>{t("sync.lastCompleted", {time: lastSync})}</span></div>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <div className="bg-muted/40 rounded-md px-3 py-2"><div className="text-muted-foreground flex items-center gap-1.5 text-[11px]"><Check className="size-3 text-emerald-500" />{t("sync.synced")}</div><p className="mt-1 font-mono text-lg font-semibold tabular-nums">{completed}</p></div>
        <div className="bg-muted/40 rounded-md px-3 py-2"><div className="text-muted-foreground flex items-center gap-1.5 text-[11px]"><Gauge className="size-3 text-primary" />{t("sync.totalScripts")}</div><p className="mt-1 font-mono text-lg font-semibold tabular-nums">{total || "—"}</p></div>
      </div>

      {status.reviewCount > 0 && <div className="border-amber-500/30 bg-amber-500/10 text-amber-200 flex items-start gap-2 rounded-md border px-3 py-2 text-xs"><AlertTriangle className="mt-0.5 size-3.5 shrink-0" /><span>{t(status.reviewCount === 1 ? "sync.reviewRequiredOne" : "sync.reviewRequiredMany", {count: status.reviewCount})}</span></div>}

      {status.permissionsError && <div className="border-amber-500/30 bg-amber-500/10 text-amber-200 flex items-start gap-2 rounded-md border px-3 py-2 text-xs"><AlertTriangle className="mt-0.5 size-3.5 shrink-0" /><span>{t("sync.permissionsUnavailable")}</span></div>}

      {status.panicMode && <div role="alert" className="border-destructive/40 bg-destructive/10 text-destructive-foreground flex items-start gap-2 rounded-md border px-3 py-2 text-xs"><AlertTriangle className="mt-0.5 size-3.5 shrink-0" /><span>{t("sync.panicDescription")}</span></div>}

      {syncRequired && <div className="border-primary/30 bg-primary/10 text-muted-foreground flex items-start gap-2 rounded-md border px-3 py-2 text-xs leading-relaxed"><Info className="text-primary mt-0.5 size-3.5 shrink-0" /><span>{t("sync.requiredDescription")}</span></div>}

      <Toggle checked={status.enabled} onChange={toggleSync} label={t("sync.enable")} />

      <div className="border-primary/25 bg-primary/8 text-muted-foreground flex items-start gap-2 rounded-md border px-3 py-2 text-xs leading-relaxed">
        <Info className="text-primary mt-0.5 size-3.5 shrink-0" />
        <span>{t("sync.lspDescription")}</span>
      </div>

      <Field label={t("sync.outputFolder")} htmlFor="sync-dir"><div className="flex gap-2"><Input id="sync-dir" value={config.outputDir} readOnly placeholder={t("settings.notSet")} className="flex-1" /><Button variant="outline" size="sm" onClick={browse} className="shrink-0">{t("common.browse")}</Button></div></Field>

      <p className="text-muted-foreground bg-muted/60 -mx-1 rounded-md px-3 py-2 text-xs leading-relaxed">{t("sync.description")}</p>

      <Button onClick={syncNow} disabled={!loaded || !enabled || status.panicMode || syncBusy} className="w-full gap-2">
        <RefreshCw className={`size-4 ${syncBusy ? "animate-spin" : ""}`} />
        {syncBusy ? t("sync.syncingNow") : t("sync.resync")}
      </Button>

      <ScriptSyncRequiredDialog open={syncPromptOpen} onClose={() => setSyncPromptOpen(false)} />
      <ScriptSyncRequiredDialog
        open={disablePromptOpen}
        onClose={() => setDisablePromptOpen(false)}
        onConfirmDisable={() => {
          setDisablePromptOpen(false)
          saveConfig({enabled: false})
        }}
      />
    </div>
  )
}
