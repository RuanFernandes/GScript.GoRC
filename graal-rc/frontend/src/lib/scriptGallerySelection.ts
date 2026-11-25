// Keeps the Script Gallery detail in step with the highlighted script.
export function createSelectionTracker() {
  let version = 0
  const watchVersion = (mine: number) => () => mine === version
  return {
    // change records a new selection; every earlier check turns false.
    change: () => watchVersion(++version),
    // watch returns a check that stays true until the selection changes.
    watch: () => watchVersion(version),
  }
}

interface LoadSelectedScriptOptions<T extends {id: string}> {
  id: string
  tracker: ReturnType<typeof createSelectionTracker>
  fetch: () => PromiseLike<T>
  onLoaded: (detail: T) => void
  onError: () => void
  onSettled: () => void
}

// loadSelectedScript fetches the detail of a newly selected script. Nothing is
// reported once the selection has moved on.
export async function loadSelectedScript<T extends {id: string}>(options: LoadSelectedScriptOptions<T>): Promise<void> {
  const isCurrent = options.tracker.change()
  let detail: T | null = null
  try {
    detail = await options.fetch()
  } catch {
    detail = null
  }
  if (!isCurrent()) return
  if (detail && detail.id === options.id) options.onLoaded(detail)
  else options.onError()
  options.onSettled()
}

// scriptForSelection is the script that actions may use: the loaded one, only
// while it is the highlighted one.
export function scriptForSelection<T extends {id: string}>(selectedID: string | null, script: T | null): T | null {
  return script && script.id === selectedID ? script : null
}
