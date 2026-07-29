// PlayerTable renders the active server's players (rc_get_players) as a grouped
// avatar list, prettier than the reference C++ tree view. Players with an empty
// level are staff (admins) and group under "Admins"; the rest under "Players" —
// same split TPlayerList::refresh uses. Each row exposes a PM action and shows
// an unread badge when an inbound PM is pending for that id. Right-click opens
// the admin context menu (PM / Edit Rights / Edit Access / Edit Attributes /
// Edit Comments), mirroring the reference client's right-click tree menu.
import {useEffect, useLayoutEffect, useRef, useState} from "react"
import {ChevronDown, ChevronRight, History, MessageSquare, ScrollText, Shield, SquareUser, Users, Wand2} from "lucide-react"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Skeleton} from "@/components/ui/skeleton"
import {parsePlayerTag} from "@/lib/playerTag"
import type {Player} from "@/types"

export type PlayerEditKind = "rights" | "ban" | "attrs" | "comments" | "banhistory" | "staffactivity"

interface PlayerTableProps {
  players: Player[]
  unreadById: Record<number, number>
  loading?: boolean
  onPM: (player: Player) => void
  onEdit: (player: Player, kind: PlayerEditKind) => void
}

interface GroupProps {
  label: string
  icon: typeof Users
  rows: Player[]
  unreadById: Record<number, number>
  onPM: (player: Player) => void
  onContext: (e: React.MouseEvent, player: Player) => void
  defaultOpen?: boolean
}

function initials(name: string): string {
  const clean = name.replace(/[^\p{L}\p{N} ]/gu, "").trim()
  if (!clean) return "?"
  const parts = clean.split(/\s+/).slice(0, 2)
  return parts.map((p) => p[0]?.toUpperCase() ?? "").join("") || clean[0]!.toUpperCase()
}

// Deterministic avatar hue from the nick so the same player keeps the same color.
function hueFor(name: string): number {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 360
  return h
}

function PlayerRow({player, unread, onPM, onContext}: {player: Player; unread: number; onPM: (p: Player) => void; onContext: (e: React.MouseEvent, p: Player) => void}) {
  const tag = parsePlayerTag(player.level)
  const nick = player.nick || player.account
  const hue = hueFor(nick)
  return (
    <div
      className="group hover:bg-accent/50 flex items-center gap-3 rounded-lg px-2.5 py-2"
      onContextMenu={(e) => onContext(e, player)}
    >
      <div className="relative shrink-0">
        <div
          className="text-primary-foreground flex size-9 items-center justify-center rounded-full text-xs font-semibold shadow-sm"
          style={{background: `linear-gradient(135deg, hsl(${hue} 65% 45%), hsl(${(hue + 40) % 360} 70% 38%))`}}
        >
          {initials(nick)}
        </div>
        <span className="bg-emerald-500 absolute -right-0.5 -bottom-0.5 size-2.5 rounded-full ring-2 ring-background" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{nick}</span>
          {unread > 0 && (
            <Badge variant="destructive" className="h-4 px-1 text-[10px] leading-none">
              {unread}
            </Badge>
          )}
        </div>
        <span className="text-muted-foreground block truncate text-xs">{player.account}</span>
      </div>
      <div className="hidden min-w-0 flex-1 sm:block">
        {tag.flag ? (
          <span className="inline-flex items-center gap-1.5">
            <Badge variant="secondary" className="font-mono text-[10px]">
              {tag.flag}
            </Badge>
            <span className="text-muted-foreground truncate text-xs">{tag.value}</span>
          </span>
        ) : (
          <span className="text-muted-foreground truncate text-xs">{tag.value || "—"}</span>
        )}
      </div>
      <span className="text-muted-foreground w-10 shrink-0 text-right font-mono text-xs tabular-nums">
        {player.id}
      </span>
      <Button
        variant="ghost"
        size="icon"
        className="size-8 shrink-0 opacity-0 transition-opacity group-hover:opacity-100 data-[state=on]:opacity-100"
        onClick={() => onPM(player)}
        aria-label={`Private message ${nick}`}
      >
        <MessageSquare className="size-4" />
      </Button>
    </div>
  )
}

function Group({label, icon: Icon, rows, unreadById, onPM, onContext, defaultOpen = true}: GroupProps) {
  const [open, setOpen] = useState(defaultOpen)
  const totalUnread = rows.reduce((sum, p) => sum + (unreadById[p.id] ?? 0), 0)
  const Chevron = open ? ChevronDown : ChevronRight
  return (
    <section className="min-w-0">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
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
        <div className="flex flex-col gap-0.5">
          {rows.map((p) => (
            <PlayerRow key={`${p.account}-${p.id}`} player={p} unread={unreadById[p.id] ?? 0} onPM={onPM} onContext={onContext} />
          ))}
        </div>
      )}
    </section>
  )
}

export function PlayerTable({players, unreadById, loading, onPM, onEdit}: PlayerTableProps) {
  const [menu, setMenu] = useState<{x: number; y: number; player: Player} | null>(null)
  const menuRef = useRef<HTMLDivElement>(null)
  // Clamp the menu inside the viewport so a right-click near a window edge
  // doesn't clip it (the player list often sits in a small child window).
  const [menuPos, setMenuPos] = useState<{left: number; top: number}>({left: 0, top: 0})

  useLayoutEffect(() => {
    if (!menu) return
    const el = menuRef.current
    if (!el) return
    const MARGIN = 8
    setMenuPos({
      left: Math.min(menu.x, window.innerWidth - el.offsetWidth - MARGIN),
      top: Math.min(menu.y, window.innerHeight - el.offsetHeight - MARGIN),
    })
  }, [menu])

  // Close the context menu on any outside click / escape / scroll.
  useEffect(() => {
    if (!menu) return
    const close = () => setMenu(null)
    window.addEventListener("click", close)
    window.addEventListener("contextmenu", close, true)
    window.addEventListener("scroll", close, true)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setMenu(null)
    }
    window.addEventListener("keydown", onKey)
    return () => {
      window.removeEventListener("click", close)
      window.removeEventListener("contextmenu", close, true)
      window.removeEventListener("scroll", close, true)
      window.removeEventListener("keydown", onKey)
    }
  }, [menu])

  const openContext = (e: React.MouseEvent, player: Player) => {
    e.preventDefault()
    setMenu({x: e.clientX, y: e.clientY, player})
  }

  if (players.length === 0) {
    if (loading) {
      // Initial fetch: mirror the row layout with pulsing placeholders instead
      // of the misleading "No players online." final-state message.
      return (
        <div className="flex flex-col gap-0.5">
          {Array.from({length: 6}).map((_, i) => (
            <div key={i} className="flex items-center gap-3 rounded-lg px-2.5 py-2">
              <Skeleton className="size-9 shrink-0 rounded-full" />
              <div className="flex-1 space-y-1.5">
                <Skeleton className="h-3.5 w-32" />
                <Skeleton className="h-3 w-24" />
              </div>
              <Skeleton className="hidden h-3 w-20 sm:block" />
              <Skeleton className="h-3 w-10" />
            </div>
          ))}
        </div>
      )
    }
    return (
      <div className="text-muted-foreground flex flex-col items-center justify-center gap-2 py-16 text-sm">
        <Users className="size-8 opacity-40" />
        No players online.
      </div>
    )
  }
  const admins = players.filter((p) => !p.level)
  const regular = players.filter((p) => !!p.level)

  const items: {label: string; icon: typeof Users; kind?: PlayerEditKind; pm?: boolean}[] = [
    {label: "Private Message", icon: MessageSquare, pm: true},
    {label: "Edit Rights", icon: Shield, kind: "rights"},
    {label: "Edit Access (Ban)", icon: Wand2, kind: "ban"},
    {label: "Edit Attributes", icon: SquareUser, kind: "attrs"},
    {label: "Edit Comments", icon: ScrollText, kind: "comments"},
    {label: "Ban History", icon: History, kind: "banhistory"},
    {label: "Staff Activity", icon: History, kind: "staffactivity"},
  ]

  return (
    <>
      <div className="flex flex-col gap-3">
        {admins.length > 0 && (
          <Group label="Admins" icon={Shield} rows={admins} unreadById={unreadById} onPM={onPM} onContext={openContext} />
        )}
        <Group label="Players" icon={Users} rows={regular} unreadById={unreadById} onPM={onPM} onContext={openContext} />
      </div>

      {menu && (
        <div
          ref={menuRef}
          className="bg-popover text-popover-foreground fixed z-50 min-w-[180px] overflow-hidden rounded-md border py-1 text-sm shadow-xl"
          style={{left: menuPos.left, top: menuPos.top}}
          onClick={(e) => e.stopPropagation()}
        >
          <div className="text-muted-foreground truncate border-b px-2.5 py-1 text-xs">
            {menu.player.nick || menu.player.account} · <span className="font-mono">{menu.player.account}</span>
          </div>
          {items.map((it) => {
            const Icon = it.icon
            return (
              <button
                key={it.label}
                type="button"
                className="hover:bg-accent flex w-full items-center gap-2 px-2.5 py-1.5 text-left"
                onClick={() => {
                  if (it.pm) onPM(menu.player)
                  else if (it.kind) onEdit(menu.player, it.kind)
                  setMenu(null)
                }}
              >
                <Icon className="size-4" />
                {it.label}
              </button>
            )
          })}
        </div>
      )}
    </>
  )
}
