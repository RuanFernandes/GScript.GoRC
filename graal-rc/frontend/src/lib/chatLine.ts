// Shared chat-line helpers used by both the rendered ChatLine and the .txt
// log formatter so the on-screen and logged formats stay in sync.
import type {ChatMessage} from "@/types"

// hhmm formats an epoch ms timestamp as HH:MM (24h, local time).
export function hhmm(ts: number): string {
  const d = new Date(ts)
  const h = String(d.getHours()).padStart(2, "0")
  const m = String(d.getMinutes()).padStart(2, "0")
  return `${h}:${m}`
}

// sourceTag returns the [RC]/[NC]/[IRC] bracket for a message, or "" for system.
export function sourceTag(source: ChatMessage["source"]): string {
  if (source === "rc") return "[RC]"
  if (source === "nc") return "[NC]"
  if (source === "irc") return "[IRC]"
  return ""
}

// formatLogLine renders a message as a single color-less line for the .txt log:
//   [HH:MM] [RC] <text>
export function formatLogLine(m: ChatMessage): string {
  const tag = sourceTag(m.source)
  return `[${hhmm(m.ts)}]${tag ? " " + tag : ""} ${m.text}`
}
