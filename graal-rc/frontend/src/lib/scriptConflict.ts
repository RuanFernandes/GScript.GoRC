// Resolution of a sync conflict from inside a script editor. When the editor
// has unsaved text, "local" is that text; the synced file on disk (item.local)
// only stands for the local side of a clean editor.
import type {SyncReviewItem} from "../types"

export type ConflictChoice = "local" | "server" | "merge"

export function localConflictText(item: SyncReviewItem, buffer: string, baseline: string): string {
  return buffer !== baseline ? buffer : item.local ?? ""
}

// mergeEdit is null until the user types in the merge box. Until then the box
// follows the local side, so it never holds a stale copy of the editor.
export function mergeBoxValue(mergeEdit: string | null, item: SyncReviewItem, buffer: string, baseline: string): string {
  return mergeEdit ?? localConflictText(item, buffer, baseline)
}

interface ResolveOptions {
  choice: ConflictChoice
  mergeEdit: string | null
  // getConflict returns the newest conflict event for this editor.
  getConflict: () => SyncReviewItem | null
  getBuffer: () => string
  getBaseline: () => string
  resolve: (choice: ConflictChoice, mergeContent: string) => Promise<void>
  getPair: () => Promise<{local?: string; server?: string}>
}

export type ConflictResolution =
  | {status: "none"}
  | {status: "empty"}
  | {
    status: "resolved"
    applied: string
    // False when the user typed during the request: the editor must be kept.
    replaceBuffer: boolean
    // A conflict that is still open after this resolution.
    remaining: SyncReviewItem | null
  }

export async function resolveScriptConflict(options: ResolveOptions): Promise<ConflictResolution> {
  const requested = options.getConflict()
  if (!requested) return {status: "none"}
  const buffer = options.getBuffer()
  const baseline = options.getBaseline()

  let choice = options.choice
  let sent = ""
  if (choice === "merge") {
    sent = mergeBoxValue(options.mergeEdit, requested, buffer, baseline)
  } else if (choice === "local" && buffer !== baseline) {
    // The backend's "local" is the disk file, so send the buffer explicitly.
    choice = "merge"
    sent = buffer
  }
  if (choice === "merge" && !sent) return {status: "empty"}

  await options.resolve(choice, sent)

  let applied = choice === "merge" ? sent : (choice === "server" ? requested.server : requested.local) ?? ""
  let remaining: SyncReviewItem | null = null
  const newer = options.getConflict()
  if (newer && newer !== requested) {
    // The server changed again during the request. The event alone does not
    // tell whether the backend consumed it or raised it afterwards.
    const pair = await options.getPair().catch(() => newer)
    if (pair.local || pair.server) {
      remaining = {...(options.getConflict() ?? newer), local: pair.local ?? "", server: pair.server ?? ""}
    } else if (choice === "server") {
      applied = newer.server ?? ""
    }
  }
  return {status: "resolved", applied, replaceBuffer: options.getBuffer() === buffer, remaining}
}
