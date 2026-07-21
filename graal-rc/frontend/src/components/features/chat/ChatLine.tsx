// ChatLine renders a single chat message with the reference-client layout:
//   [HH:MM] [RC]|[NC] <speaker>: <content>
// Colors come from the user's ChatSettings. The speaker is the text before the
// first colon (colored); the rest is content. system lines render gray/italic
// with no prefix tag.
import {hhmm, sourceTag} from "@/lib/chatLine"
import type {ChatMessage, ChatSettings} from "@/types"

// Splits a chat line for coloring. IRC has no guaranteed speaker framing — the
// server may send "Account: msg", "Player says hi", or bare text — so we only
// color a speaker when the content itself contains a colon: everything before
// the FIRST colon is the speaker, the rest is content. No colon => plain
// content. A leading "<> " (grclib's empty-source privmsg framing on echoes) is
// stripped first so it doesn't leak into the speaker.
function splitSpeaker(text: string): {speaker: string; content: string} | null {
  const t = text.startsWith("<> ") ? text.slice(3) : text
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
}

export function ChatLine({message, settings}: ChatLineProps) {
  if (message.source === "system") {
    return <span className="text-muted-foreground italic">{message.text}</span>
  }

  // The reference shows a source tag ([RC]/[NC]/[IRC]) only in the main server
  // tab; inside a dedicated IRC channel tab the tag is redundant, so omit it.
  const showTag = message.channel === ""
  const split = splitSpeaker(message.text)

  return (
    <span>
      <span style={{color: settings.timestamp}}>[{hhmm(message.ts)}]</span>{" "}
      {showTag && (
        <>
          <span style={{color: prefixColor(message.source, settings)}}>{sourceTag(message.source)}</span>{" "}
        </>
      )}
      {split ? (
        <>
          <span style={{color: settings.speaker}}>{split.speaker}</span>
          <span style={{color: settings.content}}>:{split.content}</span>
        </>
      ) : (
        <span style={{color: settings.content}}>{message.text}</span>
      )}
    </span>
  )
}
