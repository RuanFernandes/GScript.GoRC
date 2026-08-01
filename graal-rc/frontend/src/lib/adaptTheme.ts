// adaptMonacoTheme normalizes a third-party Monaco theme (from the
// brijeshb42/monaco-themes gallery, designed for standard languages) so it also
// colors every scope our GS2 grammar emits. Remote themes often lack rules for
// `storage`, `variable`, `constant.language`, `variable.language`,
// `keyword.operator`, etc.; without those, Monaco's prefix-fallback leaves those
// GS2 tokens uncolored. The adapter resolves a color for each GS2 scope by
// preferring the theme's own most-specific rule, then neighbouring categories,
// then the editor foreground — so the result matches the theme's palette while
// filling the gaps.

interface ThemeRule {
  token?: string
  foreground?: string
  background?: string
  fontStyle?: string
}

export interface ThemeData {
  base?: "vs" | "vs-dark" | "hc-black" | "hc-light"
  inherit?: boolean
  rules?: ThemeRule[]
  colors?: Record<string, string>
}

interface Resolved {
  foreground?: string
  fontStyle?: string
}

// resolve finds the theme's most-specific rule covering `scope` by walking scope
// prefixes (longest first): storage.modifier -> storage -> "". Returns the first
// match's foreground/fontStyle, or null.
function resolve(rules: Map<string, Resolved>, scope: string): Resolved | null {
  let s = scope
  for (;;) {
    const hit = rules.get(s)
    if (hit && (hit.foreground || hit.fontStyle)) return hit
    const i = s.lastIndexOf(".")
    if (i < 0) break
    s = s.slice(0, i)
  }
  return null
}

// pick returns the first non-empty resolve among candidates.
function pick(rules: Map<string, Resolved>, ...candidates: string[]): Resolved | null {
  for (const c of candidates) {
    const r = resolve(rules, c)
    if (r) return r
  }
  return null
}

// adaptMonacoTheme returns a theme definition whose rules explicitly color every
// GS2 scope, derived from the input theme's existing palette.
export function adaptMonacoTheme(def: ThemeData): ThemeData {
  const rules = new Map<string, Resolved>()
  for (const r of def.rules ?? []) {
    if (r.token) rules.set(r.token, {foreground: r.foreground, fontStyle: r.fontStyle})
  }
  const fgColor = def.colors?.["editor.foreground"]?.replace(/^#/, "")

  // Reference colors used to derive missing categories.
  const keyword = pick(rules, "keyword", "keyword.control", "keyword.other") ?? {foreground: fgColor}
  const constant = pick(rules, "constant", "constant.language", "constant.numeric", "number") ?? keyword
  // Variables: try the many token-name variants themes use. If the theme truly
  // has none, fall back to the keyword color (NOT editor.foreground) so language
  // variables like this/temp/server are visibly colored instead of blending with
  // the default text.
  const variable =
    pick(
      rules,
      "variable",
      "variable.language",
      "variable.other",
      "variable.parameter",
      "support.variable",
      "entity.name.variable",
      "identifier",
    ) ?? keyword

  const want: {token: string; from: Resolved | null}[] = [
    {token: "comment", from: pick(rules, "comment")},
    {token: "keyword", from: keyword},
    {token: "keyword.control", from: pick(rules, "keyword.control") ?? keyword},
    {token: "keyword.other", from: pick(rules, "keyword.other") ?? keyword},
    {token: "keyword.operator", from: pick(rules, "keyword.operator", "operator") ?? keyword},
    {token: "keyword.operator.array", from: pick(rules, "keyword.operator", "operator") ?? keyword},
    {token: "storage", from: pick(rules, "storage", "storage.type", "storage.modifier") ?? keyword},
    {token: "storage.type", from: pick(rules, "storage.type", "storage") ?? keyword},
    {token: "storage.modifier", from: pick(rules, "storage.modifier", "storage", "storage.type") ?? keyword},
    {token: "constant", from: constant},
    {token: "constant.language", from: pick(rules, "constant.language", "constant") ?? constant},
    {token: "constant.numeric", from: pick(rules, "constant.numeric", "number", "constant") ?? constant},
    {token: "variable", from: variable},
    {token: "variable.language", from: pick(rules, "variable.language", "variable", "variable.other", "variable.parameter", "support.variable") ?? variable},
    {token: "variable.language.flag", from: pick(rules, "variable.language.flag", "variable.language", "variable") ?? variable},
    {token: "type.identifier", from: pick(rules, "type.identifier", "type", "entity.name.type") ?? variable},
    {token: "entity.name.function", from: pick(rules, "entity.name.function", "entity.name.function.graalscript", "function", "support.function", "entity.name") ?? {foreground: fgColor}},
    {token: "string", from: pick(rules, "string.quoted.double.sql", "string")},
    {token: "string.quoted.double.sql", from: pick(rules, "string.quoted.double.sql", "string")},
    {token: "keyword.other.sql", from: pick(rules, "keyword.other.sql", "keyword.other", "keyword") ?? keyword},
    {token: "constant.character.escape", from: pick(rules, "constant.character.escape", "constant") ?? constant},
    {token: "keyword.operator.append", from: pick(rules, "keyword.operator.append", "keyword.operator", "operator") ?? keyword},
    {token: "punctuation", from: pick(rules, "punctuation") ?? {foreground: fgColor}},
  ]

  const adapted: ThemeRule[] = []
  for (const w of want) {
    if (w.from && (w.from.foreground || w.from.fontStyle)) {
      const rule: ThemeRule = {token: w.token}
      if (w.from.foreground) rule.foreground = w.from.foreground.replace(/^#/, "")
      if (w.from.fontStyle) rule.fontStyle = w.from.fontStyle
      adapted.push(rule)
    }
  }

  return {
    base: def.base ?? "vs-dark",
    inherit: true,
    rules: adapted,
    colors: def.colors ?? {},
  }
}
