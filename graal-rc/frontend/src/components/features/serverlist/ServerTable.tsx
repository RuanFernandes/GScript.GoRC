// Presentational server table. Highlights the selected row and reports
// selection + double-click-to-connect through callbacks.
import {Users} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Table, TableBody, TableCell, TableHead, TableHeader, TableRow} from "@/components/ui/table"
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
  return (
    <ScrollArea className="h-full rounded-lg border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t("server.server")}</TableHead>
            <TableHead className="w-[90px]">{t("server.language")}</TableHead>
            <TableHead className="w-[70px]">{t("server.version")}</TableHead>
            <TableHead className="w-[90px] text-right">{t("server.players")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {servers.map((server, index) => {
            const {label, tier} = serverDisplay(server.name)
            const badge = tierBadge(tier)
            return (
              <TableRow
                key={`${server.name}-${index}`}
                data-state={index === selectedIndex ? "selected" : undefined}
                className={cn(busy && "opacity-60")}
                onClick={() => onSelect(index)}
                onDoubleClick={() => onConnect(index)}
              >
                <TableCell className="font-medium">
                  <div className="flex items-center gap-2">
                    <span>{label}</span>
                    {badge && <Badge variant={tier === "gold" ? "default" : "secondary"}>{badge}</Badge>}
                  </div>
                </TableCell>
                <TableCell className="text-muted-foreground">{server.language || "—"}</TableCell>
                <TableCell className="text-muted-foreground">{server.version || "—"}</TableCell>
                <TableCell className="text-right tabular-nums">
                  <span className="inline-flex items-center gap-1 justify-end">
                    <Users className="size-3.5 text-muted-foreground" />
                    {server.players}
                  </span>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </ScrollArea>
  )
}
