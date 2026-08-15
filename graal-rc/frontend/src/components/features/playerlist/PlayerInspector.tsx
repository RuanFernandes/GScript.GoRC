import {History, MessageSquare, ScrollText, Shield, SquareUser, UserRound, Wand2, X} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import type {Player} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"
import type {PlayerEditKind} from "./PlayerTable"

interface PlayerInspectorProps {
  player: Player | null
  canBanPlayers: boolean
  onPM: (player: Player) => void
  onEdit: (player: Player, kind: PlayerEditKind) => void
  onClose: () => void
}

export function PlayerInspector({player, canBanPlayers, onPM, onEdit, onClose}: PlayerInspectorProps) {
  const {t} = useLanguage()

  if (!player) {
    return (
      <aside className="border-border bg-card flex min-h-52 w-full shrink-0 flex-col items-center justify-center gap-2 rounded-lg border p-5 text-center lg:w-72">
        <UserRound className="text-muted-foreground size-7" />
        <p className="text-sm font-medium">{t("player.selectTitle")}</p>
        <p className="text-muted-foreground max-w-56 text-xs">{t("player.selectDescription")}</p>
      </aside>
    )
  }

  const nickname = player.nick || player.account
  const action = (kind: PlayerEditKind) => onEdit(player, kind)

  return (
    <aside className="border-border bg-card flex min-h-52 w-full shrink-0 flex-col gap-4 rounded-lg border p-4 lg:w-72">
      <div className="flex items-start gap-3">
        <div className="bg-primary text-primary-foreground flex size-10 shrink-0 items-center justify-center rounded-full">
          <UserRound className="size-5" />
        </div>
        <div className="min-w-0">
          <h2 className="truncate text-sm font-semibold">{nickname}</h2>
          <p className="text-muted-foreground truncate font-mono text-xs">{player.account}</p>
          <div className="mt-1 flex flex-wrap gap-1.5">
            <Badge variant="outline">ID {player.id}</Badge>
            {player.level && <Badge variant="secondary">{player.level}</Badge>}
          </div>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="ml-auto size-7 shrink-0"
          onClick={onClose}
          aria-label={t("common.close")}
          title={t("common.close")}
        >
          <X className="size-4" />
        </Button>
      </div>

      <div className="grid gap-1.5">
        <p className="text-muted-foreground text-[11px] font-medium uppercase tracking-wide">{t("player.moderationActions")}</p>
        <Button size="sm" className="justify-start" onClick={() => onPM(player)}><MessageSquare className="size-4" />{t("player.privateMessage")}</Button>
        <div className="grid grid-cols-2 gap-1.5">
          <Button variant="outline" size="sm" className="justify-start" onClick={() => action("rights")}><Shield className="size-3.5" />{t("player.rights")}</Button>
          <Button variant="outline" size="sm" className="justify-start" onClick={() => action("ban")} disabled={!canBanPlayers} title={!canBanPlayers ? t("player.banPlayersRightRequired") : undefined}><Wand2 className="size-3.5" />{t("player.access")}</Button>
          <Button variant="outline" size="sm" className="justify-start" onClick={() => action("attrs")}><SquareUser className="size-3.5" />{t("player.attributes")}</Button>
          <Button variant="outline" size="sm" className="justify-start" onClick={() => action("comments")}><ScrollText className="size-3.5" />{t("player.comments")}</Button>
          <Button variant="outline" size="sm" className="justify-start" onClick={() => action("banhistory")} disabled={!canBanPlayers} title={!canBanPlayers ? t("player.banPlayersRightRequired") : undefined}><History className="size-3.5" />{t("player.banHistory")}</Button>
          <Button variant="outline" size="sm" className="justify-start" onClick={() => action("staffactivity")}><History className="size-3.5" />{t("player.staffActivity")}</Button>
        </div>
      </div>

      <p className="text-muted-foreground mt-auto border-t pt-3 text-[11px]">{t("player.actionsUseServerPermissions")}</p>
    </aside>
  )
}
