// Presentational details panel for the currently selected server.
import {ExternalLink, Users} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Card, CardContent, CardHeader, CardTitle} from "@/components/ui/card"
import {Separator} from "@/components/ui/separator"
import {serverDisplay, tierBadge} from "@/lib/server"
import type {Server} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

interface ServerDetailsProps {
  server: Server | null
  busy: boolean
  onConnect: () => void
  onOpenHomepage: (url: string) => void
}

export function ServerDetails({server, busy, onConnect, onOpenHomepage}: ServerDetailsProps) {
  const {t} = useLanguage()
  if (!server) {
    return (
      <Card className="server-details-card h-full">
        <CardContent className="text-muted-foreground flex h-full items-center justify-center py-10 text-sm">
          {t("server.selectDetails")}
        </CardContent>
      </Card>
    )
  }

  const {label, tier} = serverDisplay(server.name)
  const badge = tierBadge(tier)

  return (
    <Card className="server-details-card h-full">
      <CardHeader className="server-details-header">
        <p className="server-section-kicker">{t("server.server")}</p>
        <CardTitle className="mt-2 flex items-center gap-2 text-xl tracking-tight">
          {label}
          {badge && <Badge variant={tier === "gold" ? "default" : "secondary"}>{badge}</Badge>}
        </CardTitle>
        <div className="server-detail-status"><span className="server-online-dot" aria-hidden="true" />{t("server.readyToConnect")}</div>
      </CardHeader>
      <CardContent className="server-details-content grid gap-5 text-sm">
        <div className="server-player-highlight"><Users aria-hidden="true" /><div><strong>{server.players}</strong><span>{t("server.players")}</span></div></div>
        <dl className="server-details-grid">
          <div><dt>{t("server.language")}</dt><dd>{server.language || "—"}</dd></div>
          <div><dt>{t("server.version")}</dt><dd>{server.version || "—"}</dd></div>
          <div><dt>{t("server.players")}</dt><dd className="inline-flex items-center gap-1.5 tabular-nums"><Users className="size-3.5" />{server.players}</dd></div>
          <div>
            <dt>{t("server.homepage")}</dt>
            <dd className="flex min-w-0 items-center gap-2">
              <span className="truncate">{server.homepage || "—"}</span>
              {server.homepage && (
                <Button size="icon" variant="ghost" className="size-6 shrink-0" onClick={() => onOpenHomepage(server.homepage)}>
                  <ExternalLink />
                </Button>
              )}
            </dd>
          </div>
        </dl>
        <Separator />
        <div className="server-description">
          <p className="server-section-kicker">{t("server.description")}</p>
          <p className="whitespace-pre-wrap break-words">{server.description || t("common.noDescription")}</p>
        </div>
        <Button className="server-connect-button" onClick={onConnect} disabled={busy}>
          {t("server.connect")}
        </Button>
      </CardContent>
    </Card>
  )
}
