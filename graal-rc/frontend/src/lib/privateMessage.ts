// PM payloads from older native libraries can still contain Graal's
// comma-text representation. Keep this decoder small and tolerant so the UI
// remains correct while users update the native runtime independently.
export function normalizePrivateMessageText(value: string): string {
  const text = value.replace(/\r\n?/g, "\n").trim()
  if (!text) return ""

  const jsonCandidate = text.startsWith("[") && text.endsWith(",") ? text.slice(0, -1).trimEnd() : text
  try {
    const parsed: unknown = JSON.parse(jsonCandidate)
    if (Array.isArray(parsed) && parsed.every((line) => typeof line === "string")) {
      return (parsed as string[]).join("\n").trim()
    }
  } catch {
    // Fall through to the unwrapped comma-text decoder.
  }

  const lines = decodeCommaText(text)
  return lines ? lines.join("\n").trim() : text
}

const unsafeMarkupPatterns = [
  /<\s*\/?\s*(?:script|iframe|object|embed|svg|math|style|link|base|meta|form|input|img|video|audio|source|template)\b/i,
  /(?:java|vb)script\s*:/i,
  /(?:^|[\s"'])on[a-z][a-z0-9_-]*\s*=/i,
  /\bsrcdoc\s*=/i,
  /\bdata\s*:\s*text\/html/i,
]

// PMs are rendered as React text nodes, never as HTML. This predicate is for
// blocking clearly executable markup before it is sent to the server.
export function containsUnsafePrivateMessageMarkup(value: string): boolean {
  return unsafeMarkupPatterns.some((pattern) => pattern.test(value))
}

export async function copyTextToClipboard(value: string): Promise<void> {
  if (typeof navigator !== "undefined" && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return
    } catch {
      // Fall back to the legacy text-only path for restricted WebViews.
    }
  }

  if (typeof document === "undefined" || !document.body) {
    throw new Error("Clipboard is unavailable")
  }

  const textarea = document.createElement("textarea")
  textarea.value = value
  textarea.setAttribute("readonly", "")
  textarea.setAttribute("aria-hidden", "true")
  textarea.style.position = "fixed"
  textarea.style.opacity = "0"
  document.body.appendChild(textarea)
  textarea.select()
  const copied = document.execCommand("copy")
  textarea.remove()
  if (!copied) throw new Error("Clipboard copy failed")
}

function decodeCommaText(value: string): string[] | null {
  let text = value.trim()
  if (!text.startsWith('"')) return null
  const hasTrailingSeparator = text.endsWith(",")
  if (!hasTrailingSeparator && !text.includes('",')) return null
  if (hasTrailingSeparator) text = text.slice(0, -1).trimEnd()

  const lines: string[] = []
  let cursor = 0
  while (cursor < text.length) {
    let line = ""
    if (text[cursor] === '"') {
      cursor += 1
      let closed = false
      while (cursor < text.length) {
        const char = text[cursor]
        if (char === '"') {
          if (text[cursor + 1] === '"') {
            line += '"'
            cursor += 2
            continue
          }
          cursor += 1
          closed = true
          break
        }
        if (char === "\\" && text[cursor + 1] === "\\") {
          line += "\\"
          cursor += 2
          continue
        }
        line += char
        cursor += 1
      }
      if (!closed) return null
    } else {
      const start = cursor
      while (cursor < text.length && text[cursor] !== ",") cursor += 1
      line = text.slice(start, cursor)
    }

    lines.push(line)
    if (cursor === text.length) break
    if (text[cursor] !== ",") return null
    cursor += 1
    if (cursor === text.length) break
  }

  return lines.length > 0 ? lines : null
}
