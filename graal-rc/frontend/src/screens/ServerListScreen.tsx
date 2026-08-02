// ServerListScreen composes the table + details + toolbar. It receives the
// session state and intents via props; it owns no backend knowledge.
import {LogOut, RefreshCw, Server as ServerIcon, Users} from "lucide-react"

import {ServerTable} from "@/components/features/serverlist/ServerTable"
import {Button} from "@/components/ui/button"
import {Separator} from "@/components/ui/separator"

import type {Server} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface ServerListScreenProps {
  servers: Server[]
  selectedIndex: number
  statusText: string
  busy: boolean
  onSelect: (index: number) => void
  onConnect: (index: number) => void
  onRefresh: () => void
  onLogout: () => void
}

export function ServerListScreen({
  servers,
  selectedIndex,
  statusText,
  busy,
  onSelect,
  onConnect,
  onRefresh,
  onLogout,
}: ServerListScreenProps) {
  const {t} = useLanguage()
  const totalPlayers = servers.reduce((total, server) => total + server.players, 0)

  return (
    <div className="server-picker-screen bg-background flex h-svh flex-col">
      <header className="server-picker-header flex items-center gap-3 border-b px-6 py-4">
        <div className="server-picker-brand flex items-center gap-3">
          <div className="server-picker-brand-icon" aria-hidden="true">
            <ServerIcon />
          </div>
          <div><p className="server-picker-brand-overline">Graal remote control</p><h1 className="text-sm font-semibold tracking-tight">{t("server.title")}</h1></div>
        </div>
        <div className="ml-auto flex items-center gap-2">
          <Button className="server-picker-refresh" variant="outline" size="sm" onClick={onRefresh} disabled={busy}>
            <RefreshCw className={busy ? "animate-spin" : undefined} />
            {t("server.refresh")}
          </Button>
          <Button variant="ghost" size="sm" onClick={onLogout}>
            <LogOut />
            {t("server.logout")}
          </Button>
        </div>
      </header>

      <main className="server-picker-layout min-h-0 flex-1 px-6 pb-6 pt-5">
        <section className="server-selection-column flex h-full min-h-0 flex-col gap-5">
          <div className="server-selection-intro flex items-end justify-between gap-5">
            <div className="min-w-0">
              <p className="server-section-kicker">{t("server.available")}</p>
              <h2 className="server-selection-title">{t("server.chooseServer")}</h2>
              <p className="text-muted-foreground mt-2 max-w-xl text-sm">{statusText}</p>
            </div>
            <div className="server-summary-grid shrink-0">
              <div><strong className="tabular-nums">{servers.length}</strong><span>{t("server.server")}</span></div>
              <div><strong className="tabular-nums">{totalPlayers}</strong><span><Users aria-hidden="true" />{t("server.totalPlayers")}</span></div>
            </div>
          </div>
          <ServerTable servers={servers} selectedIndex={selectedIndex} busy={busy} onSelect={onSelect} onConnect={onConnect} />
        </section>
      </main>
    </div>
  )
}
