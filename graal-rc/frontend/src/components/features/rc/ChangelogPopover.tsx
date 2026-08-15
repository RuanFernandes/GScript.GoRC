import {useEffect, useRef, useState} from "react"
import {ScrollText, X} from "lucide-react"

import {getChangelog} from "@/lib/changelog"
import {useLanguage} from "@/hooks/useLanguage"
import {rcService} from "@/services/rcService"
import type {ChangelogEntry} from "@/lib/changelog"

interface ChangelogPopoverProps {
  open: boolean
  onClose: () => void
}

export function ChangelogPopover({open, onClose}: ChangelogPopoverProps) {
  const {language, t} = useLanguage()
  const [changelog, setChangelog] = useState<ChangelogEntry[]>(() => getChangelog(language))
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setChangelog(getChangelog(language))
  }, [language])

  useEffect(() => {
    if (!open) return
    let cancelled = false
    void rcService.fetchRemoteChangelog().then((remote) => {
      if (cancelled || !remote.releases?.length) return
      setChangelog(remote.releases.map((release) => ({
        version: release.version,
        date: release.date,
        title: release.title,
        summary: release.summary,
        changes: release.changes ?? [],
        current: release.current,
      })))
    }).catch(() => {
      // The bundled changelog is kept when the release service is offline.
    })
    return () => { cancelled = true }
  }, [language, open])

  useEffect(() => {
    if (!open) return
    const onPointerDown = (event: PointerEvent) => {
      if (!panelRef.current?.contains(event.target as Node)) onClose()
    }
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose()
    }
    document.addEventListener("pointerdown", onPointerDown)
    document.addEventListener("keydown", onKeyDown)
    return () => {
      document.removeEventListener("pointerdown", onPointerDown)
      document.removeEventListener("keydown", onKeyDown)
    }
  }, [onClose, open])

  if (!open) return null

  return (
    <div
      ref={panelRef}
      role="dialog"
      aria-label={t("rc.changelog")}
      className="bg-popover text-popover-foreground absolute right-0 top-[calc(100%+0.5rem)] z-50 flex max-h-[min(42rem,calc(100vh-5rem))] w-[min(25rem,calc(100vw-2rem))] flex-col overflow-hidden rounded-xl border shadow-xl"
    >
      <div className="flex shrink-0 items-start gap-3 border-b px-4 py-3">
        <div className="bg-primary/10 text-primary mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-md">
          <ScrollText className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h2 className="text-sm font-semibold">{t("rc.changelog")}</h2>
          <p className="text-muted-foreground mt-0.5 text-xs">{t("rc.changelogDescription")}</p>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="text-muted-foreground hover:bg-accent hover:text-foreground rounded-md p-1.5 transition-colors"
          aria-label={t("rc.closeChangelog")}
          title={t("common.close")}
        >
          <X className="size-4" />
        </button>
      </div>
      <div className="min-h-0 overflow-y-auto px-4">
        {changelog.map((entry) => (
          <article key={entry.version} className="border-b py-4 last:border-b-0">
            <div className="flex items-center gap-2">
              <span className={`font-mono text-xs font-semibold ${entry.current ? "text-primary" : "text-foreground"}`}>
                v{entry.version}
              </span>
              {entry.current && <span className="bg-primary/10 text-primary rounded px-1.5 py-0.5 text-[10px] font-medium">{t("rc.current")}</span>}
              <time className="text-muted-foreground ml-auto text-[11px]">{entry.date}</time>
            </div>
            <h3 className="mt-2 text-sm font-medium">{entry.title}</h3>
            <p className="text-muted-foreground mt-1 text-xs leading-relaxed">{entry.summary}</p>
            <ul className="text-muted-foreground mt-2 grid gap-1.5 pl-4 text-xs leading-relaxed">
              {entry.changes.map((change) => <li key={change}>{change}</li>)}
            </ul>
          </article>
        ))}
      </div>
    </div>
  )
}
