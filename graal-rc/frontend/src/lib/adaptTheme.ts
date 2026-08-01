// adaptMonacoTheme normalizes a third-party Monaco theme (from the
// brijeshb42/monaco-themes gallery, designed for standard languages) so it also
// colors every scope our GS2 grammar emits. Remote themes often lack rules for
// `storage`, `variable`, `constant.language`, `variable.language`,
// `keyword.operator`, etc.; without those, Monaco's prefix-fallback leaves those
// GS2 tokens uncolored. The adapter resolves a color for each GS2 scope by
// preferring the theme's own most-specific rule, then common TextMate variants,
// then a readable GS2 fallback palette.

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

const DARK_FALLBACKS: Record<string, string> = {
  comment: "6A9955", keyword: "C586C0", storage: "C586C0", constant: "4FC1FF",
  variable: "9CDCFE", prefix: "4EC9B0", member: "9CDCFE", flag: "C586C0",
  type: "4EC9FF", function: "DCDCAA", string: "CE9178", number: "B5CEA8",
  operator: "D4D4D4", punctuation: "D4D4D4",
}

const LIGHT_FALLBACKS: Record<string, string> = {
  comment: "008000", keyword: "0000FF", storage: "AF00DB", constant: "098658",
  variable: "001080", prefix: "267F99", member: "001080", flag: "AF00DB",
  type: "267F99", function: "795E26", string: "A31515", number: "098658",
  operator: "000000", punctuation: "000000",
}

// resolve finds an exact rule or the closest language-specific descendant.
// Parent fallback is intentionally handled by the caller's candidate list:
// otherwise a generic variable rule would mask a later variant such as
// variable.other.readwrite.js.
function resolve(rules: Map<string, Resolved>, scope: string): Resolved | null {
  const hit = rules.get(scope)
  if (hit && (hit.foreground || hit.fontStyle)) return hit

  // Gallery themes often specialize a scope for another language, for
  // example variable.other.readwrite.js. GS2 has no language suffix, so
  // accept the closest descendant when there is no exact rule.
  let descendant: Resolved | null = null
  let descendantDepth = Number.POSITIVE_INFINITY
  for (const [candidate, value] of rules) {
    if (!candidate.startsWith(scope + ".") || !(value.foreground || value.fontStyle)) continue
    const depth = candidate.split(".").length
    if (depth < descendantDepth) {
      descendant = value
      descendantDepth = depth
    }
  }
  return descendant
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
  const fallback = def.base === "vs" || def.base === "hc-light" ? LIGHT_FALLBACKS : DARK_FALLBACKS

  // Reference colors used to derive missing categories.
  const keyword = pick(rules, "keyword", "keyword.control", "keyword.other") ?? {foreground: fallback.keyword ?? fgColor}
  const constant = pick(rules, "constant", "constant.language", "constant.numeric", "constant.character", "number") ?? {foreground: fallback.constant ?? fgColor}
  // Variables: try the many token-name variants themes use. If the theme truly
  // has none, fall back to the keyword color (NOT editor.foreground) so language
  // variables like this/temp/server are visibly colored instead of blending with
  // the default text.
  const variable =
    pick(
      rules,
      "variable.language",
      "variable.other",
      "variable.parameter",
      "support.variable",
      "entity.name.variable",
      "variable",
      "identifier",
    ) ?? {foreground: fallback.variable ?? fgColor}

  const want: {token: string; from: Resolved | null}[] = [
    {token: "comment", from: pick(rules, "comment") ?? {foreground: fallback.comment}},
    {token: "keyword", from: keyword},
    {token: "keyword.control", from: pick(rules, "keyword.control") ?? keyword},
    {token: "keyword.other", from: pick(rules, "keyword.other") ?? keyword},
    {token: "keyword.operator", from: pick(rules, "keyword.operator", "operator") ?? {foreground: fallback.operator}},
    {token: "keyword.operator.array", from: pick(rules, "keyword.operator", "operator") ?? {foreground: fallback.operator}},
    {token: "storage", from: pick(rules, "storage", "storage.type", "storage.modifier") ?? {foreground: fallback.storage}},
    {token: "storage.type", from: pick(rules, "storage.type", "storage") ?? {foreground: fallback.storage}},
    {token: "storage.modifier", from: pick(rules, "storage.modifier", "storage", "storage.type") ?? {foreground: fallback.storage}},
    {token: "constant", from: constant},
    {token: "constant.language", from: pick(rules, "constant.language", "constant") ?? constant},
    {token: "constant.numeric", from: pick(rules, "constant.numeric", "number", "constant") ?? constant},
    {token: "variable", from: variable},
    {token: "variable.language", from: pick(rules, "variable.language", "variable.other.readwrite", "variable.other", "variable.parameter", "support.variable", "variable") ?? variable},
    {token: "variable.language.prefix", from: pick(rules, "variable.language.prefix", "variable.other.readwrite", "variable.language", "variable.other", "variable") ?? {foreground: fallback.prefix}},
    {token: "variable.language.member", from: pick(rules, "variable.language.member", "variable", "variable.other", "variable.other.readwrite", "variable.language") ?? {foreground: fallback.member}},
    {token: "variable.language.flag", from: pick(rules, "variable.language.flag", "variable.other.constant", "constant.language", "variable.language", "variable") ?? {foreground: fallback.flag}},
    {token: "type.identifier", from: pick(rules, "type.identifier", "entity.name.class", "entity.name.type.class", "support.class", "support.type", "type", "entity.name.type") ?? {foreground: fallback.type}},
    {token: "entity.name.function", from: pick(rules, "entity.name.function", "entity.name.function.graalscript", "function", "support.function", "entity.name") ?? {foreground: fallback.function}},
    {token: "string", from: pick(rules, "string.quoted.double.sql", "string.quoted.double", "string.quoted", "string") ?? {foreground: fallback.string}},
    {token: "string.quoted.double.sql", from: pick(rules, "string.quoted.double.sql", "string.quoted.double", "string.quoted", "string") ?? {foreground: fallback.string}},
    {token: "keyword.other.sql", from: pick(rules, "keyword.other.sql", "keyword.other", "keyword.control", "keyword") ?? {foreground: fallback.keyword}},
    {token: "constant.character.escape", from: pick(rules, "constant.character.escape", "constant") ?? {foreground: fallback.constant}},
    {token: "keyword.operator.append", from: pick(rules, "keyword.operator.append", "keyword.operator", "operator") ?? {foreground: fallback.operator}},
    {token: "punctuation", from: pick(rules, "punctuation") ?? {foreground: fallback.punctuation}},
  ]

  const adapted: ThemeRule[] = []
  for (const w of want) {
    if (w.from && (w.from.foreground || w.from.fontStyle)) {
      const rule: ThemeRule = {token: w.token}
      if (w.from.foreground) rule.foreground = w.from.foreground.replace(/^#/, "")
      if (w.from.fontStyle) rule.fontStyle = w.from.fontStyle
      adapted.push(rule)
      // Monarch appends ".graalscript" to every token emitted by the GS2
      // tokenizer. Keep the generic rule for compatibility and add the exact
      // scoped variant so custom/light themes cannot fall back to editor text.
      adapted.push({...rule, token: w.token + ".graalscript"})
    }
  }

  // Monaco theme galleries commonly encode the editor background as a rule
  // with an empty token instead of colors.editor.background.
  const colors = {...(def.colors ?? {})}
  const emptyTokenBackground = def.rules?.find((rule) => rule.token === "")?.background
  if (!colors["editor.background"] && emptyTokenBackground) {
    colors["editor.background"] = "#" + emptyTokenBackground.replace(/^#/, "")
  }

  return {
    base: def.base ?? "vs-dark",
    inherit: true,
    rules: adapted,
    colors,
  }
}
