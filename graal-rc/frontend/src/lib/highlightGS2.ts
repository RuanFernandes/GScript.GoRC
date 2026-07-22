// highlightGS2 is a lightweight standalone GS2 tokenizer (no Monaco) used to
// colorize code snippets in the chat (e.g. the /scripthelp2 tooltip example),
// since Monaco only loads in editor windows. The rules mirror the Monarch
// grammar in lib/monacoGraalScript.ts; colors are a vs-dark-ish palette.

export interface GsToken {
  text: string
  type: string
}

// Ordered rules: each anchored at the current position (first match wins).
const RULES: {re: RegExp; type: string}[] = [
  {re: /^\/\/.*/, type: "comment"},
  {re: /^\/\*[\s\S]*?\*\//, type: "comment"},
  {re: /^"(?:[^"\\]|\\.)*"/, type: "string"},
  {re: /^"(?:[^"]|\\.)*"?/, type: "string"},
  {re: /^0[xX][0-9a-fA-F]+/, type: "number"},
  {re: /^\d+(?:\.\d+)?/, type: "number"},
  {re: /^\b(break|case|continue|default|do|else|elseif|for|if|in|return|switch|while|with)\b/, type: "keyword"},
  {re: /^\b(function|import|public|private|const|enum|new|datablock)\b/, type: "storage"},
  {re: /^\b(true|false|nil|null|NULL|pi|timevar2)\b/, type: "constant"},
  {re: /^\b(this|thiso|temp|server|serverr|client|clientr|player|name)\b/, type: "variable"},
  {re: /^[a-zA-Z_][a-zA-Z0-9_]*(?=\s*\()/, type: "function"},
  {re: /^[a-zA-Z_][a-zA-Z0-9_]*/, type: "identifier"},
  {re: /^\s+/, type: "space"},
  {re: /^[-~^@/%|=+*!?&<>\[\]{}();:,.]/, type: "punct"},
  {re: /^./, type: "text"},
]

export function tokenizeGS2(code: string): GsToken[] {
  const out: GsToken[] = []
  let rest = code
  let guard = 0
  while (rest.length > 0 && guard++ < 100000) {
    let matched = false
    for (const rule of RULES) {
      const m = rule.re.exec(rest)
      if (m && m[0].length > 0) {
        out.push({text: m[0], type: rule.type})
        rest = rest.slice(m[0].length)
        matched = true
        break
      }
    }
    if (!matched) {
      out.push({text: rest[0], type: "text"})
      rest = rest.slice(1)
    }
  }
  return out
}

// TOKEN_COLORS maps a token type to a hex color (no leading #).
export const TOKEN_COLORS: Record<string, string> = {
  comment: "6a9955",
  string: "ce9178",
  number: "b5cea8",
  keyword: "569cd6",
  storage: "569cd6",
  constant: "4fc1ff",
  variable: "9cdcfe",
  function: "dcdcaa",
  identifier: "d4d4d4",
  punct: "d4d4d4",
  space: "d4d4d4",
  text: "d4d4d4",
}
