// PlayerTable renders the active server's players (rc_get_players) as a
// compact, grouped data list. Players with an empty level are staff (admins)
// and group under "Admins"; the rest under "Players" — the same split used by
// the reference TPlayerList::refresh. Each row keeps the server-facing
// identities visible: nickname, account, community name, and level.
import {useEffect, useLayoutEffect, useRef, useState} from "react"
import {ChevronDown, ChevronRight, History, MessageSquare, ScrollText, Shield, SquareUser, Users, Wand2} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Skeleton} from "@/components/ui/skeleton"
import {parsePlayerTag} from "@/lib/playerTag"
import type {Player} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"

export type PlayerEditKind = "rights" | "ban" | "attrs" | "comments" | "banhistory" | "staffactivity"

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

function initials(name: string): string {
  const clean = name.replace(/[^\p{L}\p{N} ]/gu, "").trim()
  if (!clean) return "?"
  const parts = clean.split(/\s+/).slice(0, 2)
  return parts.map((part) => part[0]?.toUpperCase() ?? "").join("") || clean[0]!.toUpperCase()
}

// Deterministic avatar hue from the nick so the same player keeps the same color.
function hueFor(name: string): number {
  let hue = 0
  for (let i = 0; i < name.length; i++) hue = (hue * 31 + name.charCodeAt(i)) % 360
  return hue
}

function displayValue(value: string | undefined | null): string {
  return value?.trim() || "—"
}

function PlayerRow({player, unread, onPM, onContext, selected, onSelect, onToggleSelection, selectLabel, privateMessageLabel}: PlayerRowProps) {
  const tag = parsePlayerTag(player.level)
  const account = displayValue(player.account)
  const nicknameValue = displayValue(player.nick)
  const nickname = nicknameValue === "—" ? account : nicknameValue
  const communityName = displayValue(player.communityName)
  const hue = hueFor(nickname)

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
            {initials(nickname)}
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
  const menuRef = useRef<HTMLDivElement>(null)
  // Clamp the menu inside the viewport so a right-click near a window edge
  // doesn't clip it (the player list often sits in a small child window).
  const [menuPos, setMenuPos] = useState<{left: number; top: number}>({left: 0, top: 0})

  useLayoutEffect(() => {
    if (!menu) return
    const element = menuRef.current
    if (!element) return
    const margin = 8
    setMenuPos({
      left: Math.max(margin, Math.min(menu.x, window.innerWidth - element.offsetWidth - margin)),
      top: Math.max(margin, Math.min(menu.y, window.innerHeight - element.offsetHeight - margin)),
    })
  }, [menu])

  // Close the context menu on any outside click / escape / scroll.
  useEffect(() => {
    if (!menu) return
    const close = () => setMenu(null)
    window.addEventListener("click", close)
    window.addEventListener("contextmenu", close, true)
    window.addEventListener("scroll", close, true)
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setMenu(null)
    }
    window.addEventListener("keydown", onKey)
    return () => {
      window.removeEventListener("click", close)
      window.removeEventListener("contextmenu", close, true)
      window.removeEventListener("scroll", close, true)
      window.removeEventListener("keydown", onKey)
    }
  }, [menu])

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

  const items: {label: string; icon: typeof Users; kind?: PlayerEditKind; pm?: boolean; requiresBanPlayers?: boolean}[] = [
    {label: t("player.privateMessage"), icon: MessageSquare, pm: true},
    {label: t("player.rights"), icon: Shield, kind: "rights"},
    {label: t("player.access"), icon: Wand2, kind: "ban", requiresBanPlayers: true},
    {label: t("player.attributes"), icon: SquareUser, kind: "attrs"},
    {label: t("player.comments"), icon: ScrollText, kind: "comments"},
    {label: t("player.banHistory"), icon: History, kind: "banhistory", requiresBanPlayers: true},
    {label: t("player.staffActivity"), icon: History, kind: "staffactivity"},
  ]

  return (
    <>
      <div className="flex flex-col gap-3">
        {admins.length > 0 && <Group {...groupProps} label={t("player.admins")} icon={Shield} rows={admins} />}
        <Group {...groupProps} label={t("player.players")} icon={Users} rows={regular} />
      </div>

      {menu && (
        <div
          ref={menuRef}
          className="bg-popover text-popover-foreground fixed z-50 min-w-[180px] overflow-hidden rounded-md border py-1 text-sm shadow-xl"
          style={{left: menuPos.left, top: menuPos.top}}
          onClick={(event) => event.stopPropagation()}
        >
          <div className="text-muted-foreground truncate border-b px-2.5 py-1 text-xs">
            {menu.player.nick || menu.player.account} · <span className="font-mono">{menu.player.account}</span>
            {menu.player.communityName && <> · {menu.player.communityName}</>}
          </div>
          {items.map((item) => {
            if (item.requiresBanPlayers && !canBanPlayers) return null
            const Icon = item.icon
            return (
              <button
                key={item.label}
                type="button"
                className="hover:bg-accent flex w-full items-center gap-2 px-2.5 py-1.5 text-left"
                onClick={() => {
                  if (item.pm) onPM(menu.player)
                  else if (item.kind) onEdit(menu.player, item.kind)
                  setMenu(null)
                }}
              >
                <Icon className="size-4" />
                {item.label}
              </button>
            )
          })}
        </div>
      )}
    </>
  )
}
