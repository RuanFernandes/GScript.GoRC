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

function displayValue(value: string | undefined | null): string {
  return value?.trim() || "—"
}

export function PlayerInspector({player, canBanPlayers, onPM, onEdit, onClose}: PlayerInspectorProps) {
  const {t} = useLanguage()

  // The inspector is contextual. Keeping it out of the DOM until a row is
  // selected prevents an empty child panel from taking over the small window.
  if (!player) return null

  const nickname = displayValue(player.nick) === "—" ? displayValue(player.account) : displayValue(player.nick)
  const account = displayValue(player.account)
  const communityName = displayValue(player.communityName)
  const level = displayValue(player.level)
  const action = (kind: PlayerEditKind) => onEdit(player, kind)

  return (
    <aside className="player-inspector flex w-full shrink-0 flex-col gap-3 rounded-lg border p-3 lg:w-72" aria-label={`${nickname} details`}>
      <div className="flex min-w-0 items-center gap-2.5">
        <div className="bg-primary text-primary-foreground flex size-9 shrink-0 items-center justify-center rounded-[0.7rem]">
          <UserRound className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <h2 className="truncate text-sm font-semibold">{nickname}</h2>
            <Badge variant="outline" className="shrink-0 font-mono text-[10px]">#{player.id}</Badge>
          </div>
          <p className="text-muted-foreground truncate text-[11px]">{t("player.detailsHint")}</p>
        </div>
        <Button
          variant="ghost"
          size="icon"
          className="size-7 shrink-0"
          onClick={onClose}
          aria-label={t("common.close")}
          title={t("common.close")}
        >
          <X className="size-4" />
        </Button>
      </div>

      <dl className="player-inspector-meta">
        <div>
          <dt>{t("player.account")}</dt>
          <dd className="font-mono">{account}</dd>
        </div>
        <div>
          <dt>{t("player.communityName")}</dt>
          <dd>{communityName}</dd>
        </div>
        <div>
          <dt>{t("player.level")}</dt>
          <dd className="font-mono">{level}</dd>
        </div>
      </dl>

      <div className="grid gap-1.5">
        <p className="text-muted-foreground text-[11px] font-medium uppercase tracking-wide">{t("player.moderationActions")}</p>
        <Button size="sm" className="justify-start" onClick={() => onPM(player)}>
          <MessageSquare className="size-4" />
          {t("player.privateMessage")}
        </Button>
        <div className="grid grid-cols-2 gap-1.5">
          <Button variant="outline" size="sm" className="justify-start truncate" onClick={() => action("rights")} title={t("player.rights")}>
            <Shield className="size-3.5" />
            {t("player.rights")}
          </Button>
          <Button variant="outline" size="sm" className="justify-start truncate" onClick={() => action("ban")} disabled={!canBanPlayers} title={!canBanPlayers ? t("player.banPlayersRightRequired") : t("player.access")}>
            <Wand2 className="size-3.5" />
            {t("player.access")}
          </Button>
          <Button variant="outline" size="sm" className="justify-start truncate" onClick={() => action("attrs")} title={t("player.attributes")}>
            <SquareUser className="size-3.5" />
            {t("player.attributes")}
          </Button>
          <Button variant="outline" size="sm" className="justify-start truncate" onClick={() => action("comments")} title={t("player.comments")}>
            <ScrollText className="size-3.5" />
            {t("player.comments")}
          </Button>
          <Button variant="outline" size="sm" className="justify-start truncate" onClick={() => action("banhistory")} disabled={!canBanPlayers} title={!canBanPlayers ? t("player.banPlayersRightRequired") : t("player.banHistory")}>
            <History className="size-3.5" />
            {t("player.banHistory")}
          </Button>
          <Button variant="outline" size="sm" className="justify-start truncate" onClick={() => action("staffactivity")} title={t("player.staffActivity")}>
            <History className="size-3.5" />
            {t("player.staffActivity")}
          </Button>
        </div>
      </div>

      <p className="text-muted-foreground mt-auto border-t pt-2 text-[11px]">{t("player.actionsUseServerPermissions")}</p>
    </aside>
  )
}
