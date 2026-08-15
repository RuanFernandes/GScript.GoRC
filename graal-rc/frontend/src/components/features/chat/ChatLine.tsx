// ChatLine renders a single chat message with the reference-client layout:
//   [HH:MM] [RC]|[NC] <speaker>: <content>
// Colors come from the user's ChatSettings. The speaker is the text before the
// first colon (colored); the rest is content. system lines render gray/italic
// with no prefix tag.
import type {ReactNode} from "react"
import {hhmm, sourceTag} from "@/lib/chatLine"
import type {ChatMessage, ChatSettings} from "@/types"

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

function LinkifiedText({text, onOpenLink}: {text: string; onOpenLink: (url: string) => void}): ReactNode {
  const parts: ReactNode[] = []
  let cursor = 0

  for (const match of text.matchAll(URL_TOKEN_PATTERN)) {
    const rawURL = match[0]
    const start = match.index ?? 0
    const end = start + rawURL.length
    const trimmed = trimURLPunctuation(rawURL)
    const safeURL = validWebURL(trimmed.url)

    parts.push(text.slice(cursor, start))
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
        trimmed.suffix,
      )
    } else {
      parts.push(rawURL)
    }
    cursor = end
  }

  parts.push(text.slice(cursor))
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
}

export function ChatLine({message, settings, repeatLabel, onOpenLink}: ChatLineProps) {
  if (message.source === "system") {
    return <span className="text-muted-foreground italic">{repeatLabel && <span className="mr-1 not-italic" title={repeatLabel}>{repeatLabel}</span>}<LinkifiedText text={message.text} onOpenLink={onOpenLink} /></span>
  }

  // The reference shows a source tag ([RC]/[NC]/[IRC]) only in the main server
  // tab; inside a dedicated IRC channel tab the tag is redundant, so omit it.
  const showTag = message.channel === ""
  const split = splitSpeaker(message.text)

  return (
    <span>
      {repeatLabel && <span className="text-muted-foreground mr-1" title={repeatLabel}>{repeatLabel}</span>}
      <span style={{color: settings.timestamp}}>[{hhmm(message.ts)}]</span>{" "}
      {showTag && (
        <>
          <span style={{color: prefixColor(message.source, settings)}}>{sourceTag(message.source)}</span>{" "}
        </>
      )}
      {split ? (
        <>
          <span style={{color: settings.speaker}}>{split.speaker}</span>
          <span style={{color: settings.content}}>:</span>
          <LinkifiedText text={split.content} onOpenLink={onOpenLink} />
        </>
      ) : (
        <span style={{color: settings.content}}><LinkifiedText text={message.text} onOpenLink={onOpenLink} /></span>
      )}
    </span>
  )
}
