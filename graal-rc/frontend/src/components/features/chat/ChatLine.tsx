// ChatLine renders a single chat message with the reference-client layout:
//   [HH:MM] [RC]|[NC] <speaker>: <content>
// Colors come from the user's ChatSettings. The speaker is the text before the
// first colon (colored); the rest is content. system lines render gray/italic
// with no prefix tag.
import {Fragment, type ReactNode} from "react"
import {hhmm, sourceTag} from "@/lib/chatLine"
import {PlayerMentionText, type PlayerMentionTranslator} from "@/components/features/chat/PlayerMention"
import type {PlayerMentionMatcher} from "@/lib/playerMentions"
import type {ChatMessage, ChatSettings, Player} from "@/types"

const URL_TOKEN_PATTERN = /https?:\/\/[^\s<>"']+/gi

function trimURLPunctuation(rawURL: string): {url: string; suffix: string} {
  let url = rawURL
  let suffix = ""

  while (/[.,!?;:]+$/.test(url)) {
    suffix = `${url.slice(-1)}${suffix}`
    url = url.slice(0, -1)
  }

  for (const [opening, closing] of [["(", ")"], ["[", "]"], ["{", "}"]] as const) {
    while (url.endsWith(closing) && countCharacter(url, closing) > countCharacter(url, opening)) {
      suffix = `${closing}${suffix}`
      url = url.slice(0, -1)
    }
  }

  return {url, suffix}
}

function countCharacter(value: string, character: string): number {
  return [...value].reduce((count, current) => count + (current === character ? 1 : 0), 0)
}

function validWebURL(value: string): string | null {
  try {
    const parsed = new URL(value)
    if (parsed.protocol !== "http:" && parsed.protocol !== "https:") return null
    if (!parsed.hostname) return null
    return value
  } catch {
    return null
  }
}

interface InteractiveTextProps {
  text: string
  onOpenLink: (url: string) => void
  translate: PlayerMentionTranslator
  playerMatcher?: PlayerMentionMatcher
  onPlayerContext?: (player: Player, x: number, y: number) => void
}

function LinkifiedText({text, onOpenLink, translate, playerMatcher, onPlayerContext}: InteractiveTextProps): ReactNode {
  const parts: ReactNode[] = []
  let cursor = 0

  for (const match of text.matchAll(URL_TOKEN_PATTERN)) {
    const rawURL = match[0]
    const start = match.index ?? 0
    const end = start + rawURL.length
    const trimmed = trimURLPunctuation(rawURL)
    const safeURL = validWebURL(trimmed.url)

    if (start > cursor) {
      parts.push(
        <PlayerMentionText
          key={`text-${cursor}-${start}`}
          text={text.slice(cursor, start)}
          matcher={playerMatcher}
          translate={translate}
          onOpenContext={onPlayerContext}
        />,
      )
    }
    if (safeURL) {
      parts.push(
        <a
          key={`${start}-${safeURL}`}
          href={safeURL}
          className="text-primary underline decoration-primary/60 underline-offset-2 hover:opacity-80"
          onClick={(event) => {
            event.preventDefault()
            onOpenLink(safeURL)
          }}
        >
          {trimmed.url}
        </a>,
        trimmed.suffix && <Fragment key={`suffix-${start}`}>{trimmed.suffix}</Fragment>,
      )
    } else {
      parts.push(
        <PlayerMentionText
          key={`invalid-url-${start}`}
          text={rawURL}
          matcher={playerMatcher}
          translate={translate}
          onOpenContext={onPlayerContext}
        />,
      )
    }
    cursor = end
  }

  if (cursor < text.length) {
    parts.push(
      <PlayerMentionText
        key={`text-${cursor}-end`}
        text={text.slice(cursor)}
        matcher={playerMatcher}
        translate={translate}
        onOpenContext={onPlayerContext}
      />,
    )
  }

  return <>{parts}</>
}

// Splits a chat line for coloring. IRC has no guaranteed speaker framing — the
// server may send "Account: msg", "Player says hi", or bare text — so we only
// color a speaker when the content itself contains a colon: everything before
// the FIRST colon is the speaker, the rest is content. No colon => plain
// content. A leading "<> " (grclib's empty-source privmsg framing on echoes) is
// stripped first so it doesn't leak into the speaker.
function splitSpeaker(text: string): {speaker: string; content: string} | null {
  const t = text.startsWith("<> ") ? text.slice(3) : text
  if (/^https?:\/\//i.test(t)) return null
  const i = t.indexOf(":")
  if (i <= 0) return null
  return {speaker: t.slice(0, i), content: t.slice(i + 1)}
}

function prefixColor(source: ChatMessage["source"], s: ChatSettings): string {
  if (source === "nc") return s.ncPrefix
  if (source === "irc") return s.ircPrefix
  return s.rcPrefix
}

interface ChatLineProps {
  message: ChatMessage
  settings: ChatSettings
  repeatLabel?: string
  onOpenLink: (url: string) => void
  translate: PlayerMentionTranslator
  playerMatcher?: PlayerMentionMatcher
  onPlayerContext?: (player: Player, x: number, y: number) => void
}

export function ChatLine({message, settings, repeatLabel, onOpenLink, translate, playerMatcher, onPlayerContext}: ChatLineProps) {
  if (message.source === "system") {
    return (
      <span className="text-muted-foreground italic">
        {repeatLabel && <span className="mr-1 not-italic" title={repeatLabel}>{repeatLabel}</span>}
        <LinkifiedText text={message.text} onOpenLink={onOpenLink} translate={translate} playerMatcher={playerMatcher} onPlayerContext={onPlayerContext} />
      </span>
    )
  }

  // The reference shows a source tag ([RC]/[NC]/[IRC]) only in the main server
  // tab; inside a dedicated IRC channel tab the tag is redundant, so omit it.
  const showTag = message.channel === ""
  const split = splitSpeaker(message.text)

  return (
    <span
      className={message.mentionTarget ? "inline-block max-w-full align-middle rounded-md bg-amber-400/10 px-2 py-1 ring-1 ring-inset ring-amber-300/35" : undefined}
      data-chat-mention={message.mentionTarget || undefined}
      title={message.mentionTarget ? `Ping para @${message.mentionTarget}` : undefined}
    >
      {repeatLabel && <span className="text-muted-foreground mr-1" title={repeatLabel}>{repeatLabel}</span>}
      <span style={{color: settings.timestamp}}>[{hhmm(message.ts)}]</span>{" "}
      {showTag && (
        <>
          <span style={{color: prefixColor(message.source, settings)}}>{sourceTag(message.source)}</span>{" "}
        </>
      )}
      {split ? (
        <>
          <span style={{color: settings.speaker}}>
            <PlayerMentionText text={split.speaker} matcher={playerMatcher} translate={translate} onOpenContext={onPlayerContext} />
          </span>
          <span style={{color: settings.content}}>
            :<LinkifiedText text={split.content} onOpenLink={onOpenLink} translate={translate} playerMatcher={playerMatcher} onPlayerContext={onPlayerContext} />
          </span>
        </>
      ) : (
        <span style={{color: settings.content}}>
          <LinkifiedText text={message.text} onOpenLink={onOpenLink} translate={translate} playerMatcher={playerMatcher} onPlayerContext={onPlayerContext} />
        </span>
      )}
    </span>
  )
}
