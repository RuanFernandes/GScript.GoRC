// Presentational details panel for the currently selected server.
import {ExternalLink} from "lucide-react"

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
      <Card className="h-full">
        <CardContent className="text-muted-foreground flex h-full items-center justify-center py-10 text-sm">
          {t("server.selectDetails")}
        </CardContent>
      </Card>
    )
  }

  const {label, tier} = serverDisplay(server.name)
  const badge = tierBadge(tier)

  return (
    <Card className="h-full">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-lg">
          {label}
          {badge && <Badge variant={tier === "gold" ? "default" : "secondary"}>{badge}</Badge>}
        </CardTitle>
      </CardHeader>
      <CardContent className="grid gap-4 text-sm">
        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
          <dt className="text-muted-foreground">{t("server.language")}</dt>
          <dd>{server.language || "—"}</dd>
          <dt className="text-muted-foreground">{t("server.version")}</dt>
          <dd>{server.version || "—"}</dd>
          <dt className="text-muted-foreground">{t("server.players")}</dt>
          <dd className="tabular-nums">{server.players}</dd>
          <dt className="text-muted-foreground">{t("server.homepage")}</dt>
          <dd className="flex items-center gap-2">
            <span className="truncate">{server.homepage || "—"}</span>
            {server.homepage && (
              <Button size="icon" variant="ghost" className="size-6" onClick={() => onOpenHomepage(server.homepage)}>
                <ExternalLink />
              </Button>
            )}
          </dd>
        </dl>
        <Separator />
        <div>
          <p className="text-muted-foreground mb-1">{t("server.description")}</p>
          <p className="whitespace-pre-wrap break-words">{server.description || t("common.noDescription")}</p>
        </div>
        <Button onClick={onConnect} disabled={busy}>
          {t("server.connect")}
        </Button>
      </CardContent>
    </Card>
  )
}
