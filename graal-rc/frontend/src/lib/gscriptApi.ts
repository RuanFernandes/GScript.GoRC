// gscriptApi loads + caches the GScript function reference from
// https://api.gscript.dev/ (an object {funcName: entry}, ~2100 functions). The
// payload is large (~600KB) so it is fetched once per session and cached in
// localStorage; subsequent /scripthelp calls read from memory.

const API_URL = "https://api.gscript.dev/"
const CACHE_KEY = "graal-rc:gscriptApi"

export interface GsFunction {
  name: string
  type: string
  params: string[]
  returns: string
  scope: string
  description: string
  example: string
}

let cache: GsFunction[] | null = null
let inflight: Promise<GsFunction[]> | null = null
let cacheGeneration = 0

async function fetchGsFunctions(): Promise<GsFunction[]> {
  const obj = await fetch(API_URL).then((r) => {
    if (!r.ok) throw new Error(`gscript.dev ${r.status}`)
    return r.json() as Promise<Record<string, GsFunction>>
  })
  return Object.values(obj)
}

function persistGsFunctions(entries: GsFunction[]): void {
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify({ts: Date.now(), entries}))
  } catch {
    // ignore quota
  }
}

// loadGsFunctions returns the full function list, fetching + caching on first
// call. Concurrent callers share the in-flight fetch.
export async function loadGsFunctions(): Promise<GsFunction[]> {
  if (cache) return cache
  if (inflight) return inflight
  const generation = cacheGeneration
  const request = (async () => {
    try {
      const raw = localStorage.getItem(CACHE_KEY)
      if (raw) {
        const parsed = JSON.parse(raw) as {ts?: number; entries?: GsFunction[]}
        if (parsed.entries?.length) {
          if (generation === cacheGeneration) cache = parsed.entries
          return parsed.entries
        }
      }
    } catch {
      // ignore malformed cache
    }
    const entries = await fetchGsFunctions()
    if (generation === cacheGeneration) {
      cache = entries
      persistGsFunctions(entries)
    }
    return entries
  })()
  inflight = request
  try {
    return await request
  } finally {
    if (inflight === request) inflight = null
  }
}

export async function refreshGsFunctions(): Promise<GsFunction[]> {
  const generation = ++cacheGeneration
  cache = null
  try {
    localStorage.removeItem(CACHE_KEY)
  } catch {
    // ignore storage failures
  }

  const request = fetchGsFunctions()
  inflight = request
  try {
    const entries = await request
    if (generation === cacheGeneration) {
      cache = entries
      persistGsFunctions(entries)
    }
    return entries
  } finally {
    if (inflight === request) inflight = null
  }
}

// searchFunctions returns entries whose name contains the query (case-insensitive),
// capped so a broad search doesn't flood the chat.
export function searchFunctions(entries: GsFunction[], query: string, limit = 30): GsFunction[] {
  const q = query.toLowerCase()
  return entries.filter((e) => e.name.toLowerCase().includes(q)).slice(0, limit)
}
