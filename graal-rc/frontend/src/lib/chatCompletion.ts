// Chat input completion: what to suggest, how to rank it, and what each key
// does while the suggestion list is open. It follows the VS Code suggest
// widget. No React or app imports, so node --test can load it directly.

export interface CompletionPlayer {
  account: string
  nick?: string
}

export interface CompletionItem {
  kind: "command" | "player"
  // What gets inserted: the command name without the slash, or the account.
  value: string
  label: string
  // Indices into label (and detail) of the characters that matched.
  labelMatches: number[]
  detail?: string
  detailMatches: number[]
  takesAccount: boolean
}

const ACCOUNT_COMMANDS = new Set([
  "playerinfo", "open", "openrights", "opencomments", "openaccess", "openacc",
  "openprofile", "disconnect", "reset", "staffactivity",
])

// Mirrors the commands parsed client-side in useChat.send, plus the server
// commands worth suggesting. The order here is the order of equal matches.
const COMMANDS = [
  "clear", "refreshlsp", "help", "optionshelp", "stats", "playerinfo", "open",
  "openrights", "opencomments", "openaccess", "openacc", "openprofile",
  "disconnect", "reset", "localbans", "staffactivity", "find", "finddef",
  "global", "updatelevel", "refreshfilesystem", "clientstats", "npcstart",
  "npckill", "reloadscriptlibs", "loadlang", "savenpcs", "clearnpcs", "npc",
  "style", "listscriptlogfunctions", "functionprofilestart",
  "functionprofilestop", "functionprofileshow", "scripthelp", "scriptscan",
  "memstats", "activeobjects", "showstaticvarlinks", "countnoclassnpcs",
  "clearnoclassnpcs", "npcshutdown", "scripthelp2",
]

const EXACT = 0
const PREFIX = 1
const SUBSTRING = 2
const SUBSEQUENCE = 3

interface Match {
  tier: number
  // Lower is better within a tier.
  cost: number
  indices: number[]
}

const range = (start: number, length: number) => Array.from({length}, (_, i) => start + i)

// matchText finds query as a contiguous piece of candidate, ignoring case.
function matchText(query: string, candidate: string): Match | null {
  const q = query.toLowerCase()
  const c = candidate.toLowerCase()
  if (!q) return {tier: PREFIX, cost: 0, indices: []}
  const at = c.indexOf(q)
  if (at < 0) return null
  const tier = at > 0 ? SUBSTRING : c.length === q.length ? EXACT : PREFIX
  return {tier, cost: at, indices: range(at, q.length)}
}

// matchSubsequence accepts the query's characters spread over the candidate. It
// has to start at the first letter, otherwise almost anything matches.
function matchSubsequence(query: string, candidate: string): Match | null {
  const q = query.toLowerCase()
  const c = candidate.toLowerCase()
  if (!q || q[0] !== c[0]) return null
  const indices: number[] = []
  for (let i = 0; i < c.length && indices.length < q.length; i++) {
    if (c[i] === q[indices.length]) indices.push(i)
  }
  if (indices.length < q.length) return null
  return {tier: SUBSEQUENCE, cost: indices[indices.length - 1], indices}
}

const compareMatches = (a: Match, b: Match) => a.tier - b.tier || (a.tier === PREFIX ? 0 : a.cost - b.cost)

// Splits "/cmd rest" into the two parts; rest is undefined until a space is typed.
function parse(text: string): {command: string; rest: string | undefined} | null {
  const m = text.match(/^\/(\S*)(?:\s+(.*))?$/)
  return m ? {command: m[1], rest: m[2]} : null
}

function commandItems(query: string, pluginCommands: string[]): CompletionItem[] {
  return [...new Set([...COMMANDS, ...pluginCommands])]
    .flatMap((name) => {
      const match = matchText(query, name) ?? matchSubsequence(query, name)
      return match ? [{name, match}] : []
    })
    .sort((a, b) => compareMatches(a.match, b.match))
    .map(({name, match}) => ({
      kind: "command",
      value: name,
      label: `/${name}`,
      labelMatches: match.indices.map((i) => i + 1),
      detailMatches: [],
      takesAccount: ACCOUNT_COMMANDS.has(name.toLowerCase()),
    }))
}

function playerItems(query: string, players: CompletionPlayer[]): CompletionItem[] {
  const seen = new Set<string>()
  const found: {item: CompletionItem; tier: number}[] = []
  for (const {account, nick} of players) {
    const key = account.toLowerCase()
    if (!account || seen.has(key)) continue
    // "/disconnect account reason": past the account there is nothing to complete.
    if (query.toLowerCase().startsWith(`${key} `)) return []
    const byAccount = matchText(query, account)
    const detail = nick && nick.toLowerCase() !== key ? nick : undefined
    const byNick = detail ? matchText(query, detail) : null
    if (!byAccount && !byNick) continue
    seen.add(key)
    found.push({
      tier: Math.min(byAccount?.tier ?? SUBSEQUENCE, byNick?.tier ?? SUBSEQUENCE),
      item: {
        kind: "player",
        value: account,
        label: account,
        labelMatches: byAccount?.indices ?? [],
        detail,
        detailMatches: byNick?.indices ?? [],
        takesAccount: false,
      },
    })
  }
  return found
    .sort((a, b) => a.tier - b.tier || a.item.value.toLowerCase().localeCompare(b.item.value.toLowerCase()))
    .map(({item}) => item)
}

export function completionItems(text: string, players: CompletionPlayer[], pluginCommands: string[]): CompletionItem[] {
  const parsed = parse(text)
  if (!parsed) return []
  if (parsed.rest === undefined) return commandItems(parsed.command, pluginCommands)
  return ACCOUNT_COMMANDS.has(parsed.command.toLowerCase()) ? playerItems(parsed.rest, players) : []
}

export function applyCompletion(text: string, item: CompletionItem): string {
  if (item.kind === "command") return `/${item.value}${item.takesAccount ? " " : ""}`
  return `/${parse(text)?.command ?? ""} ${item.value}`
}

export function keepsOpenAfterAccept(item: CompletionItem): boolean {
  return item.takesAccount
}

// defaultSelection returns -1 for "nothing selected". An empty account argument
// selects nothing, so Enter can't pick a player the user never looked for.
export function defaultSelection(text: string, items: CompletionItem[]): number {
  return items.length === 0 || /^\/\S+\s+$/.test(text) ? -1 : 0
}

export interface CompletionKey {
  key: string
  shift: boolean
  ctrl: boolean
  alt: boolean
  meta: boolean
  composing: boolean
}

export type CompletionKeyAction =
  | {type: "move"; index: number}
  | {type: "accept"; index: number}
  | {type: "open"}
  | {type: "close"}
  // pass leaves the key to the input; close also hides the list.
  | {type: "pass"; close?: boolean}

const PAGE = 8

export function completionKey(
  press: CompletionKey,
  state: {open: boolean; text: string; items: CompletionItem[]; selected: number},
): CompletionKeyAction {
  const {key} = press
  const {open, text, items, selected} = state
  const pass: CompletionKeyAction = {type: "pass"}
  if (press.composing || press.alt || press.meta || press.shift || items.length === 0) return pass
  if (!open) {
    const reopen = (key === "Tab" && !press.ctrl) || (key === " " && press.ctrl)
    return reopen ? {type: "open"} : pass
  }
  if (press.ctrl) return pass

  const last = items.length - 1
  switch (key) {
    case "ArrowDown":
      return {type: "move", index: selected >= last ? 0 : selected + 1}
    case "ArrowUp":
      return {type: "move", index: selected <= 0 ? last : selected - 1}
    case "PageDown":
      return {type: "move", index: Math.min(last, Math.max(selected, 0) + PAGE)}
    case "PageUp":
      return {type: "move", index: Math.max(0, selected - PAGE)}
    case "Escape":
      return {type: "close"}
    case "Tab":
    case "Enter": {
      if (key === "Enter" && selected < 0) return {type: "pass", close: true}
      const index = Math.max(selected, 0)
      // As in VS Code: when accepting changes nothing, Enter keeps its normal
      // job (sending the line).
      if (applyCompletion(text, items[index]).toLowerCase() === text.toLowerCase()) {
        return key === "Enter" ? {type: "pass", close: true} : {type: "close"}
      }
      return {type: "accept", index}
    }
    default:
      return pass
  }
}

// highlightParts splits a string into runs of matched and unmatched characters.
export function highlightParts(text: string, matches: number[]): {text: string; match: boolean}[] {
  const hit = new Set(matches)
  const parts: {text: string; match: boolean}[] = []
  for (let i = 0; i < text.length; i++) {
    const match = hit.has(i)
    const previous = parts[parts.length - 1]
    if (previous && previous.match === match) previous.text += text[i]
    else parts.push({text: text[i], match})
  }
  return parts
}
