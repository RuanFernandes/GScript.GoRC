// Player-list tag parser.
//
// The reference client renders some player/server strings with a single-letter
// status prefix (e.g. "U ServerName", "H Testbed3d") and leaves at least "H"
// unparsed — showing the raw token. This parser splits a leading uppercase flag
// from the rest so we can render the flag as a badge and the value cleanly.
//
// The exact meaning of each flag letter is confirmed against live data; for now
// any single uppercase letter followed by a space is treated as a flag.

export interface PlayerTag {
  raw: string
  flag?: string
  value: string
}

const FLAG_RE = /^([A-Z])\s+(.+)$/

export function parsePlayerTag(input: string | undefined | null): PlayerTag {
  const s = input ?? ""
  const m = FLAG_RE.exec(s)
  if (m) return {raw: s, flag: m[1], value: m[2]}
  return {raw: s, value: s}
}
