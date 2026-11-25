// Listing snapshots are asynchronous and can finish out of order when folders
// change quickly. Only the newest one may be shown, otherwise the folder label
// (which Upload targets) and the listed files can disagree.
export interface LatestRequest {
  /** Loads a value and applies it unless a newer request or reset followed. */
  run<T>(load: () => Promise<T>, apply: (value: T) => void): Promise<void>
  /** Drops every request still in flight. */
  invalidate(): void
}

export function createLatestRequest(): LatestRequest {
  let latest = 0
  return {
    async run(load, apply) {
      const request = ++latest
      const value = await load()
      if (request === latest) apply(value)
    },
    invalidate() {
      latest++
    },
  }
}
