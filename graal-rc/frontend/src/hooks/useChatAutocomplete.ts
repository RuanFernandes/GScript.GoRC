import {useRef} from "react"

import type {Player} from "@/types"

// useChatAutocomplete powers Tab completion of chat commands, terminal-style:
// no popup — press Tab to complete/cycle the current token, Shift+Tab to cycle
// backward. Two phases, inferred from the typed text:
//
//   1. Command name — while the input is "/<partial>" with no space yet, Tab
//      completes against the known client-side commands (e.g. "/openr" →
//      "/openrights "). Account-taking commands get a trailing space so the next
//      Tab lands straight in the argument phase.
//   2. Account argument — once an account-taking command has a space ("/openrights
//      ru"), Tab completes/cycles player accounts from the playerlist, matching by
//      account OR nick prefix but always inserting the canonical account string
//      (that's what the editors resolve against).
//
// Cycling is keyed off the last text we produced: pressing Tab again on that
// exact text advances to the next match; any edit (typing, space, backspace)
// starts a fresh completion from the new text, so the cycle never desyncs.

interface CmdDef {
  name: string
  arg: "account" | "none"
}

// Mirrors the commands parsed client-side in useChat.send. Kept here (not
// imported) because that file interleaves them with server routing — a flat
// registry is all the completer needs.
const COMMANDS: CmdDef[] = [
	{name: "clear", arg: "none"},
	{name: "help", arg: "none"},
	{name: "optionshelp", arg: "none"},
	{name: "stats", arg: "none"},
	{name: "playerinfo", arg: "account"},
	{name: "open", arg: "account"},
	{name: "openrights", arg: "account"},
	{name: "opencomments", arg: "account"},
	{name: "openaccess", arg: "account"},
	{name: "openacc", arg: "account"},
	{name: "openprofile", arg: "account"},
	{name: "disconnect", arg: "account"},
	{name: "reset", arg: "account"},
	{name: "localbans", arg: "none"},
	{name: "staffactivity", arg: "account"},
	{name: "find", arg: "none"},
	{name: "finddef", arg: "none"},
	{name: "global", arg: "none"},
	{name: "updatelevel", arg: "none"},
	{name: "clientstats", arg: "none"},
	{name: "npcstart", arg: "none"},
	{name: "npckill", arg: "none"},
	{name: "reloadscriptlibs", arg: "none"},
	{name: "loadlang", arg: "none"},
	{name: "savenpcs", arg: "none"},
	{name: "clearnpcs", arg: "none"},
	{name: "npc", arg: "none"},
	{name: "style", arg: "none"},
	{name: "listscriptlogfunctions", arg: "none"},
	{name: "functionprofilestart", arg: "none"},
	{name: "functionprofilestop", arg: "none"},
	{name: "functionprofileshow", arg: "none"},
	{name: "scripthelp", arg: "none"},
	{name: "scriptscan", arg: "none"},
	{name: "memstats", arg: "none"},
	{name: "activeobjects", arg: "none"},
	{name: "showstaticvarlinks", arg: "none"},
	{name: "countnoclassnpcs", arg: "none"},
	{name: "clearnoclassnpcs", arg: "none"},
	{name: "npcshutdown", arg: "none"},
	{name: "scripthelp2", arg: "none"},
]

const cmdByName = (name: string): CmdDef | undefined =>
  COMMANDS.find((c) => c.name === name.toLowerCase())

interface Ctx {
  matches: string[]
  index: number
}

// Splits "/cmd rest" into [cmd, rest]; rest is undefined when no space is typed
// yet (command-name phase) and "" when a space exists but the arg is empty.
const parse = (text: string): [string, string | undefined] => {
  const m = text.match(/^\/(\S*)(?:\s+(.*))?$/)
  return m ? [m[1], m[2]] : ["", undefined]
}

export function useChatAutocomplete(players: Player[]) {
  const ctx = useRef<Ctx | null>(null)
  // The exact text we produced on the last Tab. Used to tell "Tab again to cycle"
  // apart from "Tab on freshly typed text".
  const lastSet = useRef<string | null>(null)

  const build = (text: string): Ctx | null => {
    const [cmdRaw, rest] = parse(text)
    // Command-name phase: no space yet.
    if (rest === undefined) {
      const pfx = cmdRaw.toLowerCase()
      const matches = COMMANDS.filter((c) => c.name.startsWith(pfx)).map((c) => c.name)
      return matches.length ? {matches, index: 0} : null
    }
    // Argument phase: only account-taking commands complete from the playerlist.
    const def = cmdByName(cmdRaw)
    if (!def || def.arg !== "account") return null
    const pfx = rest.toLowerCase()
    const seen = new Set<string>()
    const matches: string[] = []
    for (const p of players) {
      if (!p.account) continue
      const acc = p.account.toLowerCase()
      const hit = acc.startsWith(pfx) || (!!p.nick && p.nick.toLowerCase().startsWith(pfx))
      if (hit && !seen.has(acc)) {
        seen.add(acc)
        matches.push(p.account)
      }
    }
    matches.sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase()))
    return matches.length ? {matches, index: 0} : null
  }

  // Renders a match back into full input text for the current phase.
  const render = (text: string, value: string): string => {
    const [cmdRaw, rest] = parse(text)
    if (rest === undefined) {
      // Command name: re-add the slash; trailing space nudges into arg phase for
      // account-taking commands.
      const def = cmdByName(value)
      return "/" + value + (def && def.arg === "account" ? " " : "")
    }
    return "/" + cmdRaw + " " + value
  }

  // complete attempts a Tab completion on `text`. Returns true (and updates text
  // via setText) when it produced something; false when nothing matched so the
  // caller can leave default key handling alone. `backward` reverses the cycle.
  const complete = (
    text: string,
    setText: (v: string) => void,
    backward: boolean
  ): boolean => {
    if (!text.startsWith("/")) {
      ctx.current = null
      lastSet.current = null
      return false
    }

    const len = ctx.current?.matches.length ?? 0
    // Continuation: Tab pressed again on the exact text we just produced →
    // advance the cycle instead of re-parsing (the completed text would
    // otherwise look like a fresh, single-match prefix).
    if (ctx.current && len > 1 && text === lastSet.current) {
      const c = ctx.current
      c.index = backward ? (c.index - 1 + c.matches.length) % c.matches.length : (c.index + 1) % c.matches.length
      const produced = render(text, c.matches[c.index])
      setText(produced)
      lastSet.current = produced
      return true
    }

    const fresh = build(text)
    if (!fresh) {
      // Don't clobber the cycle context on a dead prefix (so a transient empty
      // match list doesn't kill an in-progress cycle) — but do remember we
      // produced nothing here.
      lastSet.current = null
      return false
    }
    fresh.index = backward ? fresh.matches.length - 1 : 0
    ctx.current = fresh
    const produced = render(text, fresh.matches[fresh.index])
    setText(produced)
    lastSet.current = produced
    return true
  }

  // suggest returns the single best completion for the raw typed text, rendered
  // as full input, for inline "ghost text" preview (what Tab would insert).
  // Only returned when it extends the typed text as a strict prefix
  // (case-insensitive) — the ghost overlay relies on the suggestion starting
  // where the caret is, so a nick-only account match (whose account doesn't
  // share the typed prefix even ignoring case) is hidden from the preview (Tab
  // still completes it). Case-insensitive so typing a lowercase "r" still ghosts
  // an account "Ruan"; Tab then inserts the canonical casing.
  const suggest = (text: string): string | null => {
    if (!text.startsWith("/")) return null
    const fresh = build(text)
    if (!fresh) return null
    const produced = render(text, fresh.matches[0])
    return produced.length > text.length && produced.toLowerCase().startsWith(text.toLowerCase())
      ? produced
      : null
  }

  // options exposes the current completion set for the inline suggestion
  // palette. It uses the same matcher as Tab, so the visual list and keyboard
  // completion can never disagree.
  const options = (text: string): string[] => {
    if (!text.startsWith("/")) return []
    return build(text)?.matches ?? []
  }

  const select = (text: string, value: string, setText: (v: string) => void) => {
    const produced = render(text, value)
    setText(produced)
    lastSet.current = produced
    ctx.current = {matches: options(text), index: Math.max(0, options(text).indexOf(value))}
  }

  return {complete, suggest, options, select}
}
