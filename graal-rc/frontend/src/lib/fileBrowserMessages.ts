// File Browser downloads can emit one progress message for every received
// chunk. Keep the log readable by treating progress and completion messages
// for the same file as one logical entry.

type DownloadLogMessage = {
  kind: "progress" | "complete"
  key: string
}

const receivedChunkPattern = /^\s*Received chunk:\s*\d+\s*\/\s*\d+\s+bytes\s+for\s+(.+?)\s*$/i
const fileDownloadedPattern = /^\s*File downloaded:\s*(.+?)\s*$/i

function parseDownloadMessage(message: string): DownloadLogMessage | null {
  const progress = receivedChunkPattern.exec(message)
  if (progress) {
    return {kind: "progress", key: progress[1].trim().toLowerCase()}
  }

  const complete = fileDownloadedPattern.exec(message)
  if (complete) {
    return {kind: "complete", key: complete[1].trim().toLowerCase()}
  }

  return null
}

// mergeFileBrowserMessage appends ordinary messages, replaces a file's
// previous chunk-progress line with the newest one, and replaces that line
// with the final "File downloaded" message. A late chunk after completion is
// ignored so an old callback cannot bring the noisy progress line back.
export function mergeFileBrowserMessage(messages: string[], incoming: string, maxMessages = 200): string[] {
  const message = incoming.trim()
  if (!message) return messages
  const limit = Math.max(1, Math.floor(maxMessages))

  const parsed = parseDownloadMessage(message)
  if (!parsed) {
    return appendWithinLimit(messages, message, limit)
  }

  const existingIndex = messages.findIndex((candidate) => {
    const candidateParsed = parseDownloadMessage(candidate)
    return candidateParsed?.key === parsed.key
  })

  if (existingIndex >= 0) {
    const existing = parseDownloadMessage(messages[existingIndex])
    if (parsed.kind === "progress" && existing?.kind === "complete") return messages

    const next = messages.slice()
    next[existingIndex] = message
    return next
  }

  return appendWithinLimit(messages, message, limit)
}

function appendWithinLimit(messages: string[], message: string, limit: number): string[] {
  const keepCount = limit - 1
  return [...(keepCount > 0 ? messages.slice(-keepCount) : []), message]
}
