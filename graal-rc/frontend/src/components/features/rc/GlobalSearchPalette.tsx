import {useEffect, useMemo, useRef, useState} from "react"
import {ArchiveRestore, Code2, FileSearch, FolderOpen, MessageSquare, RefreshCw, Search, Settings, Users, X} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import type {Player} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface GlobalSearchPaletteProps {
  open: boolean
  players: Player[]
  onOpen: () => void
  onClose: () => void
  onOpenPlayers: () => void
  onOpenScripts: () => void
  onOpenFiles: () => void
  onOpenSync: () => void
  onOpenDeployments: () => void
  onOpenSettings: () => void
  onOpenPlayerPM: (player: Player) => void
}

type SearchResult = {
  id: string
  title: string
  description: string
  group: string
  icon: typeof Search
  run: () => void
}

export function GlobalSearchPalette({
  open,
  players,
  onOpen,
  onClose,
  onOpenPlayers,
  onOpenScripts,
  onOpenFiles,
  onOpenSync,
  onOpenDeployments,
  onOpenSettings,
  onOpenPlayerPM,
}: GlobalSearchPaletteProps) {
  const {t} = useLanguage()
  const inputRef = useRef<HTMLInputElement>(null)
  const [query, setQuery] = useState("")
  const [activeIndex, setActiveIndex] = useState(0)

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault()
        if (!open) {
          setQuery("")
          setActiveIndex(0)
          onOpen()
        }
      }
      if (open && event.key === "Escape") onClose()
    }
    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [onClose, onOpen, open])

  useEffect(() => {
    if (open) {
      setQuery("")
      setActiveIndex(0)
      window.setTimeout(() => inputRef.current?.focus(), 0)
    }
  }, [open])

  const results = useMemo<SearchResult[]>(() => {
    const actions: SearchResult[] = [
      {id: "players", title: t("dashboard.openPlayers"), description: t("dashboard.searchPlayersDescription"), group: t("dashboard.commands"), icon: Users, run: onOpenPlayers},
      {id: "scripts", title: t("dashboard.openScripts"), description: t("dashboard.searchScriptsDescription"), group: t("dashboard.commands"), icon: Code2, run: onOpenScripts},
      {id: "files", title: t("dashboard.openFiles"), description: t("dashboard.searchFilesDescription"), group: t("dashboard.commands"), icon: FolderOpen, run: onOpenFiles},
      {id: "sync", title: t("dashboard.openSync"), description: t("dashboard.searchSyncDescription"), group: t("dashboard.commands"), icon: RefreshCw, run: onOpenSync},
      {id: "history", title: t("dashboard.openHistory"), description: t("dashboard.searchHistoryDescription"), group: t("dashboard.commands"), icon: ArchiveRestore, run: onOpenDeployments},
      {id: "settings", title: t("rc.settings"), description: t("dashboard.searchSettingsDescription"), group: t("dashboard.commands"), icon: Settings, run: onOpenSettings},
    ]
    const q = query.trim().toLocaleLowerCase()
    const matches = (value: string) => !q || value.toLocaleLowerCase().includes(q)
    const playerResults: SearchResult[] = players
      .filter((player) => matches(`${player.nick} ${player.account} ${player.id}`))
      .slice(0, 12)
      .map((player) => ({
        id: `player:${player.id}`,
        title: player.nick || player.account,
        description: `${player.account} · #${player.id}`,
        group: t("dashboard.players"),
        icon: MessageSquare,
        run: () => onOpenPlayerPM(player),
      }))
    return [...actions.filter((action) => matches(`${action.title} ${action.description}`)), ...playerResults]
  }, [onOpenDeployments, onOpenFiles, onOpenPlayerPM, onOpenPlayers, onOpenScripts, onOpenSettings, onOpenSync, players, query, t])

  useEffect(() => {
    setActiveIndex((current) => Math.min(current, Math.max(0, results.length - 1)))
  }, [results.length])

  if (!open) return null

  const run = (result: SearchResult | undefined) => {
    if (!result) return
    result.run()
    onClose()
  }

  return (
    <div className="fixed inset-0 z-50 bg-black/20 p-4" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <div className="bg-popover text-popover-foreground mx-auto mt-[8vh] flex max-w-2xl flex-col overflow-hidden rounded-lg border shadow-lg" role="dialog" aria-modal="true" aria-label={t("dashboard.searchTitle")}>
        <div className="flex items-center gap-2 border-b px-3">
          <Search className="text-muted-foreground size-4 shrink-0" />
          <Input
            ref={inputRef}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value)
              setActiveIndex(0)
            }}
            onKeyDown={(event) => {
              if (event.key === "ArrowDown") {
                event.preventDefault()
                setActiveIndex((current) => Math.min(current + 1, Math.max(0, results.length - 1)))
              } else if (event.key === "ArrowUp") {
                event.preventDefault()
                setActiveIndex((current) => Math.max(0, current - 1))
              } else if (event.key === "Enter") {
                event.preventDefault()
                run(results[activeIndex])
              }
            }}
            className="h-12 border-0 px-1 shadow-none focus-visible:ring-0"
            placeholder={t("dashboard.searchPlaceholder")}
            aria-controls="global-search-results"
          />
          <kbd className="text-muted-foreground hidden rounded border px-1.5 py-0.5 text-[10px] sm:inline">ESC</kbd>
          <Button variant="ghost" size="icon" className="size-8" onClick={onClose} aria-label={t("common.close")}><X className="size-4" /></Button>
        </div>
        <div id="global-search-results" role="listbox" className="max-h-[min(60vh,30rem)] overflow-y-auto p-1.5">
          {results.length === 0 ? (
            <div className="text-muted-foreground flex min-h-24 items-center justify-center gap-2 text-sm"><FileSearch className="size-4" />{t("dashboard.noResults")}</div>
          ) : (
            results.map((result, index) => {
              const Icon = result.icon
              return (
                <button
                  key={result.id}
                  type="button"
                  role="option"
                  aria-selected={index === activeIndex}
                  onMouseEnter={() => setActiveIndex(index)}
                  onClick={() => run(result)}
                  className={`flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-left ${index === activeIndex ? "bg-accent text-accent-foreground" : "hover:bg-accent/60"}`}
                >
                  <Icon className="size-4 shrink-0" />
                  <span className="min-w-0 flex-1">
                    <span className="block truncate text-sm font-medium">{result.title}</span>
                    <span className="text-muted-foreground block truncate text-xs">{result.description}</span>
                  </span>
                  <span className="text-muted-foreground shrink-0 text-[10px]">{result.group}</span>
                </button>
              )
            })
          )}
        </div>
        <div className="text-muted-foreground flex items-center gap-3 border-t px-3 py-2 text-[10px]">
          <span>↑↓ {t("dashboard.navigate")}</span><span>Enter {t("dashboard.open")}</span><span>Esc {t("common.close")}</span>
        </div>
      </div>
    </div>
  )
}
