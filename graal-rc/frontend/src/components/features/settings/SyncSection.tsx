// SyncSection — the inline Sync control (config + status). Rendered inside the
// SyncPopover docked to the header Sync button. Server-truth mode: local folder
// mirrors the server; local edits are never pushed; deletes never propagate.
import {useSync} from "@/hooks/useSync"
import type {ReactNode} from "react"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {Badge} from "@/components/ui/badge"
import {rcService} from "@/services/rcService"

function Toggle({checked, onChange, label}: {checked: boolean; onChange: (v: boolean) => void; label: string}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className="flex w-full items-center justify-between rounded-md border border-border px-3 py-2.5 text-left transition-colors hover:bg-accent"
    >
      <span className="text-sm font-medium">{label}</span>
      <span
        className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${
          checked ? "bg-primary" : "bg-input"
        }`}
      >
        <span
          className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow transition-all ${
            checked ? "left-[18px]" : "left-0.5"
          }`}
        />
      </span>
    </button>
  )
}

function Field({label, htmlFor, children}: {label: string; htmlFor?: string; children: ReactNode}) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={htmlFor} className="text-muted-foreground text-xs font-medium">
        {label}
      </Label>
      {children}
    </div>
  )
}

export function SyncSection() {
  const {config, status, progress, loaded, saveConfig, syncNow} = useSync()

  const browse = async () => {
    const chosen = await rcService.chooseDirectory()
    if (chosen) saveConfig({outputDir: chosen})
  }

  const enabled = config.enabled && config.outputDir !== ""
  const stateLabel = status.ncDown
    ? "NC offline"
    : status.paused
      ? "Paused"
      : !config.enabled
        ? "Disabled"
        : !config.outputDir
          ? "Choose an output folder"
          : progress && progress.total > 0
            ? `Fetching ${progress.done}/${progress.total}`
            : "Ready"
  const stateTone = status.ncDown || status.paused ? "bg-amber-500" : enabled ? "bg-emerald-500" : "bg-muted-foreground/50"

  return (
    <div className="grid gap-4">
      {status.server && (
        <p className="text-muted-foreground text-xs">
          Syncing <span className="text-foreground font-semibold">{status.server}</span>
        </p>
      )}

      <div className="bg-muted/40 flex items-center gap-2 rounded-md border px-3 py-2">
        <span className={`size-2 shrink-0 rounded-full ${stateTone}`} />
        <div className="min-w-0">
          <p className="text-sm font-medium">{stateLabel}</p>
          <p className="text-muted-foreground truncate text-xs">
            {status.lastSyncAt
              ? `Last sync ${new Date(status.lastSyncAt * 1000).toLocaleTimeString()}`
              : "No sync completed yet"}
          </p>
        </div>
      </div>

      <Toggle
        checked={config.enabled}
        onChange={(v) => saveConfig({enabled: v})}
        label="Enable sync"
      />

      <Field label="Output folder" htmlFor="sync-dir">
        <div className="flex gap-2">
          <Input id="sync-dir" value={config.outputDir} readOnly placeholder="Not set" className="flex-1" />
          <Button variant="outline" size="sm" onClick={browse} className="shrink-0">
            Browse
          </Button>
        </div>
      </Field>

      <Field label="Poll interval (minutes)" htmlFor="sync-poll">
        <Input
          id="sync-poll"
          type="number"
          min={1}
          value={config.pollingMinutes}
          onChange={(e) => saveConfig({pollingMinutes: Math.max(1, Number(e.target.value) || 1)})}
          className="w-24"
        />
      </Field>

      <p className="bg-muted/60 text-muted-foreground -mx-1 rounded-md px-3 py-2 text-xs leading-relaxed">
        <span className="text-foreground font-medium">Server-truth:</span> local mirrors the
        server. Server changes overwrite local; local edits are never pushed. Deletes never
        propagate.
      </p>

      <Button onClick={syncNow} disabled={!loaded || !enabled} className="w-full">
        Sync now
      </Button>

      <div className="flex flex-wrap items-center gap-1.5">
        {status.ncDown && <Badge variant="destructive">NC down</Badge>}
        {status.paused && <Badge variant="secondary">Paused</Badge>}
        {status.outputDirMissing && <Badge variant="secondary">No folder</Badge>}
        {progress && progress.total > 0 && (
          <Badge variant="default">
            {progress.done}/{progress.total}
          </Badge>
        )}
        <span className="text-muted-foreground ml-auto text-xs">
          {status.lastSyncAt ? new Date(status.lastSyncAt * 1000).toLocaleTimeString() : "never"}
        </span>
      </div>
    </div>
  )
}
