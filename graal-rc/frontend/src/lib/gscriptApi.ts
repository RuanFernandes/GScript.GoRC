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

// loadGsFunctions returns the full function list, fetching + caching on first
// call. Concurrent callers share the in-flight fetch.
export async function loadGsFunctions(): Promise<GsFunction[]> {
  if (cache) return cache
  if (inflight) return inflight
  inflight = (async () => {
    try {
      const raw = localStorage.getItem(CACHE_KEY)
      if (raw) {
        const parsed = JSON.parse(raw) as {ts?: number; entries?: GsFunction[]}
        if (parsed.entries?.length) {
          cache = parsed.entries
          return cache
        }
      }
    } catch {
      // ignore malformed cache
    }
    const obj = await fetch(API_URL).then((r) => {
      if (!r.ok) throw new Error(`gscript.dev ${r.status}`)
      return r.json() as Promise<Record<string, GsFunction>>
    })
    const entries = Object.values(obj)
    cache = entries
    try {
      localStorage.setItem(CACHE_KEY, JSON.stringify({ts: Date.now(), entries}))
    } catch {
      // ignore quota
    }
    return entries
  })()
  try {
    return await inflight
  } finally {
    inflight = null
  }
}

// searchFunctions returns entries whose name contains the query (case-insensitive),
// capped so a broad search doesn't flood the chat.
export function searchFunctions(entries: GsFunction[], query: string, limit = 30): GsFunction[] {
  const q = query.toLowerCase()
  return entries.filter((e) => e.name.toLowerCase().includes(q)).slice(0, limit)
}
