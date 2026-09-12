// File Browser downloads can emit one progress message for every received
// chunk. Keep the log readable by treating progress and completion messages
// for the same file as one logical entry.

type TransferLogMessage = {
  direction: "download" | "upload"
  kind: "progress" | "complete"
  key: string
  bytes?: number
}

const receivedChunkPattern = /^\s*Received chunk:\s*\d+\s*\/\s*\d+\s+bytes\s+for\s+(.+?)\s*$/i
const fileDownloadedPattern = /^\s*File downloaded:\s*(.+?)\s*$/i
const uploadingBigFilePattern = /^\s*Uploading big file\s+(.+?)(?:\s+size\s+(\d+))?\.{0,3}\s*$/i
const uploadedBigFilePattern = /^\s*Uploaded big file\s+(.+?)(?:\.)?\s*$/i
const bigfileStartedPattern = /^\s*Bigfile transfer started:\s*(.+?)\s*$/i
const fileCompletePattern = /^\s*File complete:\s*(.+?)\s*$/i

const transferMessagePatterns = [
  receivedChunkPattern,
  fileDownloadedPattern,
  bigfileStartedPattern,
  fileCompletePattern,
]

function normalizeTransferPath(path: string): string {
  return path.trim().replaceAll("\\", "/").replace(/^\/+|\/+$/g, "").toLocaleLowerCase()
}

function extractTransferPath(message: string): string | null {
  for (const pattern of transferMessagePatterns) {
    const match = pattern.exec(message)
    if (match) return normalizeTransferPath(match[1])
  }
  return null
}

function transferPathsMatch(receivedPath: string, previewPath: string): boolean {
  if (!receivedPath || !previewPath) return false
  return (
    receivedPath === previewPath ||
    receivedPath.endsWith(`/${previewPath}`) ||
    previewPath.endsWith(`/${receivedPath}`)
  )
}

// Thumbnail previews use the same native transfer channel as user downloads.
// The reference client hides protocol messages for those known preview paths,
// while keeping messages for explicit downloads visible.
export function isPreviewTransferMessage(message: string, previewPaths: Iterable<string>): boolean {
  const receivedPath = extractTransferPath(message)
  if (!receivedPath) return false

  for (const previewPath of previewPaths) {
    if (transferPathsMatch(receivedPath, normalizeTransferPath(previewPath))) return true
  }
  return false
}

function parseTransferLogMessage(message: string): TransferLogMessage | null {
  const progress = receivedChunkPattern.exec(message)
  if (progress) {
    return {direction: "download", kind: "progress", key: normalizeTransferPath(progress[1])}
  }

  const complete = fileDownloadedPattern.exec(message)
  if (complete) {
    return {direction: "download", kind: "complete", key: normalizeTransferPath(complete[1])}
  }

  const uploading = uploadingBigFilePattern.exec(message)
  if (uploading) {
    const bytes = uploading[2] === undefined ? undefined : Number(uploading[2])
    return {
      direction: "upload",
      kind: "progress",
      key: normalizeTransferPath(uploading[1]),
      ...(bytes === undefined || !Number.isSafeInteger(bytes) ? {} : {bytes}),
    }
  }

  const uploaded = uploadedBigFilePattern.exec(message)
  if (uploaded) {
    return {direction: "upload", kind: "complete", key: normalizeTransferPath(uploaded[1])}
  }

  return null
}

// mergeFileBrowserMessage appends ordinary messages and keeps one progress line
// per transfer. Upload progress may arrive out of order, so retain its highest
// reported byte count; late progress after a completion is ignored.
export function mergeFileBrowserMessage(messages: string[], incoming: string, maxMessages = 200): string[] {
  const message = incoming.trim()
  if (!message) return messages
  const limit = Math.max(1, Math.floor(maxMessages))

  const parsed = parseTransferLogMessage(message)
  if (!parsed) {
    return appendWithinLimit(messages, message, limit)
  }

  const existingIndex = messages.findIndex((candidate) => {
    const candidateParsed = parseTransferLogMessage(candidate)
    return candidateParsed?.direction === parsed.direction && candidateParsed.key === parsed.key
  })

  if (existingIndex >= 0) {
    const existing = parseTransferLogMessage(messages[existingIndex])
    if (parsed.kind === "progress" && existing?.kind === "complete") return messages
    if (
      parsed.direction === "upload" &&
      parsed.kind === "progress" &&
      existing?.kind === "progress" &&
      existing.bytes !== undefined &&
      (parsed.bytes === undefined || parsed.bytes < existing.bytes)
    ) {
      return messages
    }

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
