// ServerListScreen composes the table + details + toolbar. It receives the
// session state and intents via props; it owns no backend knowledge.
import {LogOut, RefreshCw} from "lucide-react"
import {Browser} from "@wailsio/runtime"

import {ServerDetails} from "@/components/features/serverlist/ServerDetails"
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
  const selected = servers[selectedIndex] ?? null

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="flex items-center gap-3 border-b px-4 py-2.5">
        <h1 className="text-base font-semibold">{t("server.title")}</h1>
        <Separator orientation="vertical" className="h-5" />
        <span className="text-muted-foreground truncate text-sm">{statusText}</span>
        <div className="ml-auto flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={onRefresh} disabled={busy}>
            <RefreshCw className={busy ? "animate-spin" : undefined} />
            {t("server.refresh")}
          </Button>
          <Button variant="ghost" size="sm" onClick={onLogout}>
            <LogOut />
            {t("server.logout")}
          </Button>
        </div>
      </header>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-3 p-3 lg:grid-cols-[1fr_360px]">
        <ServerTable
          servers={servers}
          selectedIndex={selectedIndex}
          busy={busy}
          onSelect={onSelect}
          onConnect={onConnect}
        />
        <ServerDetails
          server={selected}
          busy={busy}
          onConnect={() => onConnect(selectedIndex)}
          onOpenHomepage={(url) => Browser.OpenURL(url)}
        />
      </div>
    </div>
  )
}
