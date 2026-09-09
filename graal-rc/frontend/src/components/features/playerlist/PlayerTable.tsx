// PlayerTable renders the active server's players (rc_get_players) as a
// compact, grouped data list. Players with an empty level are staff (admins)
// and group under "Admins"; the rest under "Players" — the same split used by
// the reference TPlayerList::refresh. Each row keeps the server-facing
// identities visible: nickname, account, community name, and level.
import {useState} from "react"
import {ChevronDown, ChevronRight, MessageSquare, Shield, Users} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {PlayerContextMenu} from "./PlayerContextMenu"
import type {PlayerEditKind} from "./PlayerContextMenu"
import {Skeleton} from "@/components/ui/skeleton"
import {parsePlayerTag} from "@/lib/playerTag"
import {displayPlayerValue, playerHue, playerInitials} from "@/lib/playerIdentity"
import type {Player} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

export type {PlayerEditKind} from "./PlayerContextMenu"

interface PlayerTableProps {
  players: Player[]
  unreadById: Record<number, number>
  canBanPlayers: boolean
  loading?: boolean
  emptyMessage?: string
  onPM: (player: Player) => void
  selectedIds?: Set<number>
  onSelect?: (player: Player) => void
  onToggleSelection?: (player: Player) => void
  onEdit: (player: Player, kind: PlayerEditKind) => void
}

interface PlayerColumnLabels {
  nickname: string
  account: string
  communityName: string
  level: string
  id: string
}

interface GroupProps {
  label: string
  icon: typeof Users
  rows: Player[]
  unreadById: Record<number, number>
  onPM: (player: Player) => void
  onContext: (event: React.MouseEvent, player: Player) => void
  selectedIds: Set<number>
  onSelect?: (player: Player) => void
  onToggleSelection?: (player: Player) => void
  selectLabel: string
  privateMessageLabel: string
  columnLabels: PlayerColumnLabels
  selectionEnabled: boolean
  defaultOpen?: boolean
}

interface PlayerRowProps {
  player: Player
  unread: number
  onPM: (player: Player) => void
  onContext: (event: React.MouseEvent, player: Player) => void
  selected: boolean
  onSelect?: (player: Player) => void
  onToggleSelection?: (player: Player) => void
  selectLabel: string
  privateMessageLabel: string
}

function PlayerRow({player, unread, onPM, onContext, selected, onSelect, onToggleSelection, selectLabel, privateMessageLabel}: PlayerRowProps) {
  const tag = parsePlayerTag(player.level)
  const account = displayPlayerValue(player.account)
  const nicknameValue = displayPlayerValue(player.nick)
  const nickname = nicknameValue === "—" ? account : nicknameValue
  const communityName = displayPlayerValue(player.communityName)
  const hue = playerHue(nickname)

  return (
    <div
      className={`player-list-grid player-list-row group ${selected ? "player-list-row-selected" : ""}`}
      role="button"
      tabIndex={0}
      aria-pressed={selected}
      onClick={() => onSelect?.(player)}
      onKeyDown={(event) => {
        if ((event.key === "Enter" || event.key === " ") && onSelect) {
          event.preventDefault()
          onSelect(player)
        }
      }}
      onContextMenu={(event) => onContext(event, player)}
    >
      {onToggleSelection ? (
        <input
          type="checkbox"
          checked={selected}
          onChange={() => onToggleSelection(player)}
          onClick={(event) => event.stopPropagation()}
          aria-label={`${selectLabel} ${nickname}`}
          className="size-4 shrink-0 accent-[var(--primary)]"
        />
      ) : (
        <span aria-hidden="true" className="player-list-checkbox-slot" />
      )}
      <div className="player-list-identity">
        <div className="relative shrink-0">
          <div
            className="text-primary-foreground flex size-9 items-center justify-center rounded-[0.7rem] text-xs font-semibold shadow-sm"
            style={{background: `hsl(${hue} 55% 42%)`}}
          >
            {playerInitials(nickname)}
          </div>
          <span className="bg-emerald-500 absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full ring-2 ring-background" />
        </div>
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-medium">{nickname}</span>
            {unread > 0 && (
              <Badge variant="destructive" className="h-4 px-1 text-[10px] leading-none">
                {unread}
              </Badge>
            )}
          </div>
        </div>
      </div>
      <div className="player-list-cell" title={account}>
        <span className="player-list-value player-list-mono">{account}</span>
      </div>
      <div className="player-list-cell" title={communityName}>
        <span className={`player-list-value ${communityName === "—" ? "text-muted-foreground/60" : ""}`}>{communityName}</span>
      </div>
      <div className="player-list-cell player-list-level">
        {tag.flag ? (
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <Badge variant="secondary" className="font-mono text-[10px]">
              {tag.flag}
            </Badge>
            <span className="player-list-value">{tag.value}</span>
          </span>
        ) : (
          <span className="player-list-value text-muted-foreground">{tag.value || "—"}</span>
        )}
      </div>
      <span className="player-list-id text-muted-foreground font-mono text-xs tabular-nums" title={`ID ${player.id}`}>
        {player.id}
      </span>
      <Button
        variant="ghost"
        size="icon"
        className={`player-list-pm size-8 shrink-0 transition-opacity group-hover:opacity-100 group-focus-within:opacity-100 ${selected ? "opacity-100" : "opacity-0"}`}
        onClick={() => onPM(player)}
        aria-label={`${privateMessageLabel} ${nickname}`}
      >
        <MessageSquare className="size-4" />
      </Button>
    </div>
  )
}

function ColumnHeader({labels, selectionEnabled}: {labels: PlayerColumnLabels; selectionEnabled: boolean}) {
  return (
    <div className="player-list-grid player-list-column-header" role="row">
      {selectionEnabled ? <span aria-hidden="true" /> : <span aria-hidden="true" className="player-list-checkbox-slot" />}
      <span>{labels.nickname}</span>
      <span>{labels.account}</span>
      <span>{labels.communityName}</span>
      <span className="player-list-level">{labels.level}</span>
      <span className="text-right">{labels.id}</span>
      <span aria-hidden="true" />
    </div>
  )
}

function Group({label, icon: Icon, rows, unreadById, onPM, onContext, selectedIds, onSelect, onToggleSelection, selectLabel, privateMessageLabel, columnLabels, selectionEnabled, defaultOpen = true}: GroupProps) {
  const [open, setOpen] = useState(defaultOpen)
  const totalUnread = rows.reduce((sum, player) => sum + (unreadById[player.id] ?? 0), 0)
  const Chevron = open ? ChevronDown : ChevronRight

  return (
    <section className="min-w-0">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        className="text-muted-foreground hover:bg-accent/40 flex w-full items-center gap-1.5 rounded-md px-1.5 py-1.5 text-xs font-medium uppercase tracking-wide"
      >
        <Chevron className="size-3.5" />
        <Icon className="size-3.5" />
        <span>{label}</span>
        <span className="text-muted-foreground/70">({rows.length})</span>
        {totalUnread > 0 && (
          <Badge variant="destructive" className="h-4 px-1 text-[10px] leading-none">
            {totalUnread}
          </Badge>
        )}
      </button>
      {open && (
        <>
          <ColumnHeader labels={columnLabels} selectionEnabled={selectionEnabled} />
          <div className="flex flex-col gap-1">
            {rows.map((player) => (
              <PlayerRow
                key={`${player.account}-${player.id}`}
                player={player}
                unread={unreadById[player.id] ?? 0}
                onPM={onPM}
                onContext={onContext}
                selected={selectedIds.has(player.id)}
                onSelect={onSelect}
                onToggleSelection={onToggleSelection}
                selectLabel={selectLabel}
                privateMessageLabel={privateMessageLabel}
              />
            ))}
          </div>
        </>
      )}
    </section>
  )
}

export function PlayerTable({players, unreadById, canBanPlayers, loading, emptyMessage, onPM, selectedIds = new Set<number>(), onSelect, onToggleSelection, onEdit}: PlayerTableProps) {
  const {t} = useLanguage()
  const [menu, setMenu] = useState<{x: number; y: number; player: Player} | null>(null)

  const openContext = (event: React.MouseEvent, player: Player) => {
    event.preventDefault()
    setMenu({x: event.clientX, y: event.clientY, player})
  }

  if (players.length === 0) {
    if (loading) {
      // Initial fetch: mirror the row layout with pulsing placeholders instead
      // of the misleading "No players online." final-state message.
      return (
        <div className="flex flex-col gap-1">
          {Array.from({length: 6}).map((_, index) => (
            <div key={index} className="player-list-grid player-list-row">
              <Skeleton className="size-4" />
              <div className="player-list-identity">
                <Skeleton className="size-9 shrink-0 rounded-[0.7rem]" />
                <Skeleton className="h-3.5 w-24" />
              </div>
              <Skeleton className="h-3 w-20" />
              <Skeleton className="h-3 w-24" />
              <Skeleton className="player-list-level h-3 w-12" />
              <Skeleton className="h-3 w-6" />
              <Skeleton className="size-8" />
            </div>
          ))}
        </div>
      )
    }
    return (
      <div className="text-muted-foreground flex flex-col items-center justify-center gap-2 py-16 text-sm">
        <Users className="size-8 opacity-40" />
        {emptyMessage ?? t("player.noPlayers")}
      </div>
    )
  }

  const admins = players.filter((player) => !player.level)
  const regular = players.filter((player) => !!player.level)
  const columnLabels: PlayerColumnLabels = {
    nickname: t("player.nickname"),
    account: t("player.account"),
    communityName: t("player.communityName"),
    level: t("player.level"),
    id: t("player.id"),
  }
  const groupProps = {
    unreadById,
    onPM,
    onContext: openContext,
    selectedIds,
    onSelect,
    onToggleSelection,
    selectLabel: t("player.selectPlayer", {name: ""}).trim(),
    privateMessageLabel: t("player.privateMessageFor", {name: ""}).trim(),
    columnLabels,
    selectionEnabled: Boolean(onToggleSelection),
  }


  return (
    <>
      <div className="flex flex-col gap-3">
        {admins.length > 0 && <Group {...groupProps} label={t("player.admins")} icon={Shield} rows={admins} />}
        <Group {...groupProps} label={t("player.players")} icon={Users} rows={regular} />
      </div>

      {menu && (
        <PlayerContextMenu
          player={menu.player}
          anchor={{x: menu.x, y: menu.y}}
          canBanPlayers={canBanPlayers}
          onPM={onPM}
          onEdit={onEdit}
          onClose={() => setMenu(null)}
        />
      )}
    </>
  )
}
