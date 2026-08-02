// Presentational server table. Highlights the selected row and reports
// selection + double-click-to-connect through callbacks.
import {ArrowDownWideNarrow, Search, Users} from "lucide-react"
import {useMemo, useState} from "react"

import {Badge} from "@/components/ui/badge"
import {ScrollArea} from "@/components/ui/scroll-area"
import {cn} from "@/lib/utils"
import {serverDisplay, tierBadge} from "@/lib/server"
import type {Server} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface ServerTableProps {
  servers: Server[]
  selectedIndex: number
  busy: boolean
  onSelect: (index: number) => void
  onConnect: (index: number) => void
}

export function ServerTable({servers, selectedIndex, busy, onSelect, onConnect}: ServerTableProps) {
  const {t} = useLanguage()
  const [query, setQuery] = useState("")
  const filteredServers = useMemo(() => {
    const normalizedQuery = query.trim().toLocaleLowerCase()
    return servers
      .map((server, index) => ({server, index}))
      .filter(({server}) => !normalizedQuery || server.name.toLocaleLowerCase().includes(normalizedQuery))
      .sort((a, b) => b.server.players - a.server.players || a.index - b.index)
  }, [query, servers])

  return (
    <section className="server-list-panel flex min-h-0 flex-col overflow-hidden rounded-xl border" aria-label={t("server.title")}>
      <div className="server-list-heading flex items-center justify-between border-b px-5 py-4">
        <div>
          <p className="server-section-kicker">{t("server.server")}</p>
          <p className="text-muted-foreground mt-1 text-xs">{t("server.doubleClickToConnect")}</p>
        </div>
        <div className="server-list-tools">
          <span className="server-sort-indicator"><ArrowDownWideNarrow aria-hidden="true" />{t("server.mostActive")}</span>
          <label className="server-search-field">
            <Search aria-hidden="true" />
            <span className="sr-only">{t("server.search")}</span>
            <input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("server.search")} />
          </label>
        </div>
      </div>
      <ScrollArea className="min-h-0 flex-1">
        <div className="server-card-list p-3">
          {filteredServers.map(({server, index}) => {
            const {label, tier} = serverDisplay(server.name)
            const badge = tierBadge(tier)
            return (
              <button
                type="button"
                key={`${server.name}-${index}`}
                aria-pressed={index === selectedIndex}
                className={cn("server-item", index === selectedIndex && "server-item-selected", busy && "opacity-60")}
                onClick={() => onSelect(index)}
                onDoubleClick={() => onConnect(index)}
                tabIndex={0}
                onKeyDown={(event) => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault()
                    onSelect(index)
                  }
                }}
              >
                <span className="server-item-topline">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="server-online-dot" aria-hidden="true" />
                    <span className="server-item-name truncate">{label}</span>
                  </span>
                  {badge && <Badge variant={tier === "gold" ? "default" : "secondary"}>{badge}</Badge>}
                </span>
                <span className="server-item-meta">
                  <span><span className="server-item-label">{t("server.language")}</span>{server.language || "—"}</span>
                  <span className="server-item-players"><Users aria-hidden="true" />{server.players}<span className="sr-only"> {t("server.players")}</span></span>
                </span>
                {server.description?.trim() && <span className="server-item-description">{server.description.trim()}</span>}
              </button>
            )
          })}
          {filteredServers.length === 0 && <div className="server-empty-state">{t("server.noResults")}</div>}
        </div>
      </ScrollArea>
    </section>
  )
}
