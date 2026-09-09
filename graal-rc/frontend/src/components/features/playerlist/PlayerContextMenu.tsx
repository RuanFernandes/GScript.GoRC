import {useEffect, useLayoutEffect, useRef, useState} from "react"
import {Copy, History, MessageSquare, ScrollText, Shield, SquareUser, Wand2} from "lucide-react"

import type {Player} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"
import {copyTextToClipboard} from "@/lib/privateMessage"
import {toast} from "sonner"

export type PlayerEditKind = "rights" | "ban" | "attrs" | "comments" | "banhistory" | "staffactivity"

interface PlayerContextMenuProps {
  player: Player
  anchor: {x: number; y: number}
  canBanPlayers: boolean
  onPM: (player: Player) => void
  onEdit: (player: Player, kind: PlayerEditKind) => void
  onClose: () => void
}

const MENU_MARGIN = 8

export function PlayerContextMenu({player, anchor, canBanPlayers, onPM, onEdit, onClose}: PlayerContextMenuProps) {
  const {t} = useLanguage()
  const menuRef = useRef<HTMLDivElement>(null)
  const [position, setPosition] = useState(anchor)

  useLayoutEffect(() => {
    const element = menuRef.current
    if (!element) return

    setPosition({
      x: Math.max(MENU_MARGIN, Math.min(anchor.x, window.innerWidth - element.offsetWidth - MENU_MARGIN)),
      y: Math.max(MENU_MARGIN, Math.min(anchor.y, window.innerHeight - element.offsetHeight - MENU_MARGIN)),
    })
  }, [anchor])

  useEffect(() => {
    const closeOnPointerDown = (event: PointerEvent) => {
      const target = event.target
      if (target instanceof Node && !menuRef.current?.contains(target)) onClose()
    }
    const closeOnContextMenu = () => onClose()
    const closeOnKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose()
    }

    window.addEventListener("pointerdown", closeOnPointerDown)
    window.addEventListener("contextmenu", closeOnContextMenu, true)
    window.addEventListener("scroll", onClose, true)
    window.addEventListener("keydown", closeOnKeyDown)
    return () => {
      window.removeEventListener("pointerdown", closeOnPointerDown)
      window.removeEventListener("contextmenu", closeOnContextMenu, true)
      window.removeEventListener("scroll", onClose, true)
      window.removeEventListener("keydown", closeOnKeyDown)
    }
  }, [onClose])

  const copyAccountName = async () => {
    const account = player.account.trim()
    if (!account) {
      toast.error(t("player.noAccount"))
      return
    }

    try {
      await copyTextToClipboard(account)
      toast.success(t("player.accountNameCopied"))
    } catch {
      toast.error(t("player.accountNameCopyFailed"))
    }
  }

  const items: Array<{label: string; icon: typeof MessageSquare; kind?: PlayerEditKind; pm?: boolean; copyAccountName?: boolean; requiresBanPlayers?: boolean}> = [
    {label: t("player.copyAccountName"), icon: Copy, copyAccountName: true},
    {label: t("player.privateMessage"), icon: MessageSquare, pm: true},
    {label: t("player.rights"), icon: Shield, kind: "rights"},
    {label: t("player.access"), icon: Wand2, kind: "ban", requiresBanPlayers: true},
    {label: t("player.attributes"), icon: SquareUser, kind: "attrs"},
    {label: t("player.comments"), icon: ScrollText, kind: "comments"},
    {label: t("player.banHistory"), icon: History, kind: "banhistory", requiresBanPlayers: true},
    {label: t("player.staffActivity"), icon: History, kind: "staffactivity"},
  ]

  return (
    <div
      ref={menuRef}
      className="bg-popover text-popover-foreground fixed z-50 min-w-[180px] overflow-hidden rounded-md border py-1 text-sm shadow-xl"
      style={{left: position.x, top: position.y}}
      onPointerDown={(event) => event.stopPropagation()}
      onClick={(event) => event.stopPropagation()}
    >
      <div className="text-muted-foreground truncate border-b px-2.5 py-1 text-xs">
        {player.nick || player.account} · <span className="font-mono">{player.account}</span>
        {player.communityName && <> · {player.communityName}</>}
      </div>
      {items.map((item) => {
        if (item.requiresBanPlayers && !canBanPlayers) return null
        const Icon = item.icon
        return (
          <button
            key={item.label}
            type="button"
            className="hover:bg-accent disabled:text-muted-foreground disabled:pointer-events-none disabled:opacity-50 flex w-full items-center gap-2 px-2.5 py-1.5 text-left"
            disabled={item.copyAccountName && !player.account.trim()}
            onClick={() => {
              if (item.copyAccountName) void copyAccountName()
              else if (item.pm) onPM(player)
              else if (item.kind) onEdit(player, item.kind)
              onClose()
            }}
          >
            <Icon className="size-4" />
            {item.label}
          </button>
        )
      })}
    </div>
  )
}
