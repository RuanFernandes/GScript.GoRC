import {Fragment, useCallback, useEffect, useLayoutEffect, useRef, useState, type MouseEvent as ReactMouseEvent, type ReactNode, type RefObject} from "react"
import {createPortal} from "react-dom"

import {Badge} from "@/components/ui/badge"
import {displayPlayerValue, playerDisplayName, playerHue, playerInitials} from "@/lib/playerIdentity"
import type {PlayerMentionMatcher} from "@/lib/playerMentions"
import type {Player} from "@/types"

export type PlayerMentionTranslator = (key: string, vars?: Record<string, string | number>) => string

interface PlayerMentionProps {
  player: Player
  identity: "account" | "communityName"
  translate: PlayerMentionTranslator
  onOpenContext?: (player: Player, x: number, y: number) => void
  children: ReactNode
}

interface HoverCardPosition {
  left: number
  top: number
}

const HOVER_GAP = 8
const HOVER_MARGIN = 10
const CLOSE_DELAY = 120

function contextPoint(event: ReactMouseEvent<HTMLButtonElement>, element: HTMLElement): {x: number; y: number} {
  const rect = element.getBoundingClientRect()
  return {
    x: event.clientX || rect.left + rect.width / 2,
    y: event.clientY || rect.bottom,
  }
}

function PlayerHoverCard({player, position, cardRef, translate, onMouseEnter, onMouseLeave}: {player: Player; position: HoverCardPosition; cardRef: RefObject<HTMLDivElement | null>; translate: PlayerMentionTranslator; onMouseEnter: () => void; onMouseLeave: () => void}) {
  const name = playerDisplayName(player)
  const hue = playerHue(name)

  return createPortal(
    <div
      ref={cardRef}
      data-player-hover-card="true"
      className="bg-popover text-popover-foreground pointer-events-auto fixed z-50 w-64 max-w-[calc(100vw-1.25rem)] max-h-[min(18rem,calc(100vh-1.25rem))] overflow-y-auto rounded-lg border p-3 text-xs shadow-xl"
      style={{left: position.left, top: position.top}}
      role="tooltip"
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
    >
      <div className="flex min-w-0 items-center gap-2.5">
        <div
          className="text-primary-foreground flex size-9 shrink-0 items-center justify-center rounded-[0.7rem] text-xs font-semibold shadow-sm"
          style={{background: `hsl(${hue} 55% 42%)`}}
        >
          {playerInitials(name)}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            <p className="truncate font-semibold">{name}</p>
            <Badge variant="secondary" className="h-4 shrink-0 gap-1 px-1.5 text-[10px]">
              <span className="size-1.5 rounded-full bg-emerald-500" aria-hidden="true" />
              {translate("player.online")}
            </Badge>
          </div>
          <p className="text-muted-foreground truncate">{translate("player.mentionDetected")}</p>
        </div>
      </div>
      <dl className="mt-3 grid grid-cols-2 gap-1.5">
        <div className="min-w-0 rounded-md border px-2 py-1.5">
          <dt className="text-muted-foreground text-[10px] uppercase tracking-wide">{translate("player.account")}</dt>
          <dd className="truncate font-mono">{displayPlayerValue(player.account)}</dd>
        </div>
        <div className="min-w-0 rounded-md border px-2 py-1.5">
          <dt className="text-muted-foreground text-[10px] uppercase tracking-wide">{translate("player.communityName")}</dt>
          <dd className="truncate">{displayPlayerValue(player.communityName)}</dd>
        </div>
        <div className="min-w-0 rounded-md border px-2 py-1.5">
          <dt className="text-muted-foreground text-[10px] uppercase tracking-wide">{translate("player.level")}</dt>
          <dd className="truncate font-mono">{displayPlayerValue(player.level)}</dd>
        </div>
        <div className="min-w-0 rounded-md border px-2 py-1.5">
          <dt className="text-muted-foreground text-[10px] uppercase tracking-wide">{translate("player.id")}</dt>
          <dd className="truncate font-mono">{player.id}</dd>
        </div>
      </dl>
      <p className="text-muted-foreground mt-2 border-t pt-2 text-[10px]">{translate("player.mentionHint")}</p>
    </div>,
    document.body,
  )
}

export function PlayerMention({player, identity, translate, onOpenContext, children}: PlayerMentionProps) {
  const triggerRef = useRef<HTMLButtonElement>(null)
  const cardRef = useRef<HTMLDivElement>(null)
  const closeTimer = useRef<number | null>(null)
  const [hoverOpen, setHoverOpen] = useState(false)
  const [position, setPosition] = useState<HoverCardPosition>({left: HOVER_MARGIN, top: HOVER_MARGIN})

  const cancelClose = useCallback(() => {
    if (closeTimer.current !== null) {
      window.clearTimeout(closeTimer.current)
      closeTimer.current = null
    }
  }, [])

  const scheduleClose = useCallback(() => {
    cancelClose()
    closeTimer.current = window.setTimeout(() => {
      closeTimer.current = null
      setHoverOpen(false)
    }, CLOSE_DELAY)
  }, [cancelClose])

  const reposition = useCallback(() => {
    const trigger = triggerRef.current
    if (!trigger) return
    const card = cardRef.current
    const rect = trigger.getBoundingClientRect()
    const width = card?.offsetWidth ?? 256
    const height = card?.offsetHeight ?? 190
    const left = Math.max(HOVER_MARGIN, Math.min(rect.left, window.innerWidth - width - HOVER_MARGIN))
    const below = rect.bottom + HOVER_GAP
    const top = below + height <= window.innerHeight - HOVER_MARGIN
      ? below
      : Math.max(HOVER_MARGIN, Math.min(rect.top - height - HOVER_GAP, window.innerHeight - height - HOVER_MARGIN))
    setPosition({left, top})
  }, [])

  useLayoutEffect(() => {
    if (!hoverOpen) return
    reposition()
    window.addEventListener("resize", reposition)
    window.addEventListener("scroll", reposition, true)
    return () => {
      window.removeEventListener("resize", reposition)
      window.removeEventListener("scroll", reposition, true)
    }
  }, [hoverOpen, reposition])

  useEffect(() => () => cancelClose(), [cancelClose])

  const openHover = () => {
    cancelClose()
    setHoverOpen(true)
  }

  const openContext = (event: ReactMouseEvent<HTMLButtonElement>) => {
    if (!onOpenContext) return
    event.preventDefault()
    event.stopPropagation()
    const trigger = triggerRef.current
    if (trigger) {
      const point = contextPoint(event, trigger)
      onOpenContext(player, point.x, point.y)
    }
    setHoverOpen(false)
  }

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        className="text-inherit inline cursor-help appearance-none rounded-sm border-0 bg-transparent p-0 align-baseline underline decoration-dotted underline-offset-2 outline-none hover:decoration-solid focus-visible:ring-2 focus-visible:ring-ring"
        aria-label={`${playerDisplayName(player)} · ${identity === "account" ? translate("player.account") : translate("player.communityName")}`}
        onMouseEnter={openHover}
        onMouseLeave={scheduleClose}
        onFocus={openHover}
        onBlur={scheduleClose}
        onClick={onOpenContext ? openContext : undefined}
        onContextMenu={onOpenContext ? openContext : undefined}
      >
        {children}
      </button>
      {hoverOpen && typeof document !== "undefined" && (
        <PlayerHoverCard
          player={player}
          position={position}
          cardRef={cardRef}
          translate={translate}
          onMouseEnter={cancelClose}
          onMouseLeave={scheduleClose}
        />
      )}
    </>
  )
}

interface PlayerMentionTextProps {
  text: string
  matcher?: PlayerMentionMatcher
  translate: PlayerMentionTranslator
  onOpenContext?: (player: Player, x: number, y: number) => void
}

export function PlayerMentionText({text, matcher, translate, onOpenContext}: PlayerMentionTextProps) {
  const matches = matcher?.find(text) ?? []
  if (matches.length === 0) return <>{text}</>

  const parts: ReactNode[] = []
  let cursor = 0
  matches.forEach((match, index) => {
    if (match.start > cursor) parts.push(<Fragment key={`text-${cursor}-${index}`}>{text.slice(cursor, match.start)}</Fragment>)
    parts.push(
      <PlayerMention
        key={`mention-${match.start}-${match.end}`}
        player={match.player}
        identity={match.identity}
        translate={translate}
        onOpenContext={onOpenContext}
      >
        {text.slice(match.start, match.end)}
      </PlayerMention>,
    )
    cursor = match.end
  })
  if (cursor < text.length) parts.push(<Fragment key={`text-${cursor}-end`}>{text.slice(cursor)}</Fragment>)
  return <>{parts}</>
}
