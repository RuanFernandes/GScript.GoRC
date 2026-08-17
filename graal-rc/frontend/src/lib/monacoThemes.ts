import {adaptMonacoTheme} from "@/lib/adaptTheme"
import {BUILT_IN_APP_THEMES, type EditableAppTheme} from "@/lib/appThemes"

// Monaco theme catalog for the Coding settings. Built-in themes (vs, vs-dark,
// hc-black, hc-light) need no definition; custom themes (monokai, darcula,
// night-owl) are registered via monaco.editor.defineTheme on first editor
// mount. Types kept
// loose (the monaco-editor package is a transitive dep of @monaco-editor/react,
// not a direct one) to avoid a direct type import.

// A minimal mirror of monaco.editor.IStandaloneThemeData.
interface StandaloneThemeData {
  base: "vs" | "vs-dark" | "hc-black" | "hc-light"
  inherit: boolean
  rules: {token: string; foreground?: string; fontStyle?: string}[]
  colors: Record<string, string>
}

// Minimal mirror of the monaco namespace surface we use.
interface MonacoLike {
  editor: {
    defineTheme(name: string, data: StandaloneThemeData): void
  }
}

export interface ThemeOption {
  key: string
  label: string
  // Custom themes that must be defineTheme'd before use; built-ins omit this.
  define?: StandaloneThemeData
}

// Monaco only accepts letters, digits and hyphens in a theme name. Custom
// themes created by older releases used underscores, so map persisted keys to
// a safe runtime name without changing their storage key.
export function toMonacoThemeName(key: string): string {
  return key.replace(/[^a-z0-9-]/gi, "-")
}

const baseTheme = (base: StandaloneThemeData["base"]): StandaloneThemeData => ({
  base,
  inherit: true,
  rules: [],
  colors: {},
})

type GraalScriptThemeKey = "default-dark" | "default-light" | "midnight-blue" | "forest-terminal" | "warm-paper"

type GraalScriptSyntaxPalette = {
  comment: string
  controlKeyword: string
  otherKeyword: string
  storage: string
  constant: string
  number: string
  variable: string
  prefix: string
  member: string
  flag: string
  type: string
  function: string
  string: string
  sqlKeyword: string
  escape: string
  operator: string
  append: string
  punctuation: string
}

// These semantic colors are deliberately separate from the RC chrome tokens:
// the same roles stay distinguishable in a GS script even when a surface color
// is edited in a future custom application theme.
const GS_SYNTAX_PALETTES: Record<GraalScriptThemeKey, GraalScriptSyntaxPalette> = {
  "default-dark": {
    comment: "#8f9aa7",
    controlKeyword: "#c7a0ff",
    otherKeyword: "#f3b76e",
    storage: "#ff8db5",
    constant: "#f5d06f",
    number: "#f5d06f",
    variable: "#78dce8",
    prefix: "#67d2a7",
    member: "#e7eaf0",
    flag: "#ff9d9d",
    type: "#79b8ff",
    function: "#a8e6cf",
    string: "#8fe3b0",
    sqlKeyword: "#f6d365",
    escape: "#ffad66",
    operator: "#c5b9ff",
    append: "#e5c07b",
    punctuation: "#fafafa",
  },
  "default-light": {
    comment: "#6b7280",
    controlKeyword: "#6d28d9",
    otherKeyword: "#a21caf",
    storage: "#0f766e",
    constant: "#956d18",
    number: "#995c16",
    variable: "#0f3a66",
    prefix: "#0f766e",
    member: "#252525",
    flag: "#9d174d",
    type: "#236b73",
    function: "#795e26",
    string: "#9b2c1f",
    sqlKeyword: "#1d4ed8",
    escape: "#c2410c",
    operator: "#374151",
    append: "#1d4ed8",
    punctuation: "#252525",
  },
  "midnight-blue": {
    comment: "#8297b2",
    controlKeyword: "#d8a8ff",
    otherKeyword: "#ff9fba",
    storage: "#8ac8ff",
    constant: "#f6c86e",
    number: "#f6c86e",
    variable: "#b8e5ff",
    prefix: "#71d4bb",
    member: "#e6f0ff",
    flag: "#ff9b9b",
    type: "#8ac8ff",
    function: "#a7e8ca",
    string: "#a6e3a1",
    sqlKeyword: "#ffd166",
    escape: "#ffb86b",
    operator: "#b7c9ff",
    append: "#f2c572",
    punctuation: "#dce8f8",
  },
  "forest-terminal": {
    comment: "#73a78c",
    controlKeyword: "#ff9b9b",
    otherKeyword: "#e8c171",
    storage: "#77e0b1",
    constant: "#f0c674",
    number: "#f0c674",
    variable: "#b8f5d1",
    prefix: "#77e0b1",
    member: "#e4fff0",
    flag: "#ff8f8f",
    type: "#78c7e8",
    function: "#a5e8b7",
    string: "#9fe6a0",
    sqlKeyword: "#f5d76e",
    escape: "#ffae6b",
    operator: "#9cd9c0",
    append: "#e0c0ff",
    punctuation: "#d9f3e6",
  },
  "warm-paper": {
    comment: "#7d6e63",
    controlKeyword: "#8c3d62",
    otherKeyword: "#9a4d2e",
    storage: "#236c68",
    constant: "#956d18",
    number: "#995c16",
    variable: "#334e68",
    prefix: "#438c73",
    member: "#342f2a",
    flag: "#9c2f2f",
    type: "#236c68",
    function: "#7a4e26",
    string: "#7a3e2a",
    sqlKeyword: "#7a4d00",
    escape: "#a04a17",
    operator: "#5b556e",
    append: "#6b4d9a",
    punctuation: "#342f2a",
  },
}

function addAlpha(color: string, alpha: string): string {
  return /^#[0-9a-f]{6}$/i.test(color) ? `${color}${alpha}` : color
}

function graalScriptRules(palette: GraalScriptSyntaxPalette): StandaloneThemeData["rules"] {
  const rules = [
    ["identifier", palette.member],
    ["comment", palette.comment],
    ["comment.line.double-slash", palette.comment],
    ["comment.block", palette.comment],
    ["comment.block.documentation", palette.comment],
    ["keyword", palette.controlKeyword],
    ["keyword.control", palette.controlKeyword],
    ["keyword.other", palette.otherKeyword],
    ["storage", palette.storage],
    ["storage.type", palette.storage],
    ["storage.modifier", palette.storage],
    ["constant", palette.constant],
    ["constant.language", palette.constant],
    ["constant.numeric", palette.number],
    ["variable", palette.variable],
    ["variable.language", palette.variable],
    ["variable.language.prefix", palette.prefix],
    ["variable.language.member", palette.member],
    ["variable.language.flag", palette.flag],
    ["type.identifier", palette.type],
    ["entity.name.function", palette.function],
    ["function", palette.function],
    ["string", palette.string],
    ["string.quoted.double.sql", palette.string],
    ["keyword.other.sql", palette.sqlKeyword],
    ["constant.character.escape", palette.escape],
    ["keyword.operator", palette.operator],
    ["keyword.operator.array", palette.operator],
    ["keyword.operator.append", palette.append],
    ["punctuation", palette.punctuation],
  ] as const

  return rules.map(([token, foreground]) => ({token, foreground: foreground.replace(/^#/, "")}))
}

function graalScriptTheme(appTheme: EditableAppTheme, syntax: GraalScriptSyntaxPalette): StandaloneThemeData {
  const colors = appTheme.colors
  const primary = colors.sidebarPrimary || colors.primary
  const surface = colors.card || colors.background
  const border = colors.border || colors.windowBorder
  const selection = addAlpha(primary, "66")
  const inactiveSelection = addAlpha(primary, "3d")
  const lineHighlight = addAlpha(appTheme.mode === "light" ? colors.secondary : surface, "b8")

  return {
    base: appTheme.mode === "light" ? "vs" : "vs-dark",
    inherit: true,
    rules: graalScriptRules(syntax),
    colors: {
      "editor.background": colors.background,
      "editor.foreground": colors.foreground,
      "editorGutter.background": colors.background,
      "editorCursor.foreground": colors.serverAccent || primary,
      "editor.selectionBackground": selection,
      "editor.inactiveSelectionBackground": inactiveSelection,
      "editor.selectionHighlightBackground": addAlpha(primary, "38"),
      "editor.lineHighlightBackground": lineHighlight,
      "editorLineNumber.foreground": addAlpha(colors.mutedForeground, "cc"),
      "editorLineNumber.activeForeground": primary,
      "editorIndentGuide.background": addAlpha(border, "99"),
      "editorIndentGuide.activeBackground": addAlpha(primary, "aa"),
      "editorWhitespace.foreground": addAlpha(colors.mutedForeground, "99"),
      "editorBracketMatch.background": addAlpha(colors.accent, "cc"),
      "editorBracketMatch.border": colors.ring,
      "editor.findMatchBackground": addAlpha(colors.chart3, "aa"),
      "editor.findMatchHighlightBackground": addAlpha(colors.chart3, "55"),
      "editorOverviewRuler.border": border,
      "editorOverviewRuler.findMatchForeground": colors.chart3,
      "editorOverviewRuler.errorForeground": colors.destructive,
      "editorOverviewRuler.warningForeground": colors.chart3,
      "editorOverviewRuler.infoForeground": colors.chart2,
      "editorError.foreground": colors.destructive,
      "editorWarning.foreground": colors.chart3,
      "editorInfo.foreground": colors.chart2,
      "editorWidget.background": colors.popover,
      "editorWidget.foreground": colors.popoverForeground,
      "editorWidget.border": border,
      "editorSuggestWidget.background": colors.popover,
      "editorSuggestWidget.foreground": colors.popoverForeground,
      "editorSuggestWidget.border": border,
      "editorSuggestWidget.selectedBackground": colors.accent,
      "peekViewEditor.background": colors.background,
      "peekViewResult.background": colors.card,
      "peekViewTitle.background": colors.sidebar,
      "peekViewBorder": border,
      "scrollbarSlider.background": addAlpha(colors.secondary, "aa"),
      "scrollbarSlider.hoverBackground": addAlpha(primary, "99"),
      "scrollbarSlider.activeBackground": primary,
      "minimap.background": colors.background,
      "minimap.selectionHighlight": addAlpha(primary, "aa"),
    },
  }
}

export const GRAAL_SCRIPT_THEME_OPTIONS: ThemeOption[] = BUILT_IN_APP_THEMES.map((appTheme) => ({
  key: `gs-${appTheme.key}`,
  label: `GraalScript · ${appTheme.name}`,
  define: graalScriptTheme(appTheme, GS_SYNTAX_PALETTES[appTheme.key as GraalScriptThemeKey]),
}))

// monokai/darcula/one-dark-pro/night-owl evoked via base vs-dark + token rules
// covering the common highlights.
export const MONACO_THEME_OPTIONS: ThemeOption[] = [
  {key: "vs-dark", label: "Dark (VS)", define: baseTheme("vs-dark")},
  {key: "vs", label: "Light (VS)", define: baseTheme("vs")},
  {key: "hc-black", label: "Dark High Contrast", define: baseTheme("hc-black")},
  {key: "hc-light", label: "Light High Contrast", define: baseTheme("hc-light")},
  ...GRAAL_SCRIPT_THEME_OPTIONS,
  {
    key: "monokai",
    label: "Monokai",
    define: {
      base: "vs-dark",
      inherit: true,
      rules: [
        {token: "comment", foreground: "75715e", fontStyle: "italic"},
        {token: "keyword", foreground: "f92672"},
        {token: "keyword.control", foreground: "f92672"},
        {token: "keyword.other", foreground: "f92672"},
        {token: "storage", foreground: "f92672"},
        {token: "storage.type", foreground: "f92672"},
        {token: "storage.modifier", foreground: "f92672"},
        {token: "constant", foreground: "ae81ff"},
        {token: "constant.numeric", foreground: "ae81ff"},
        {token: "variable", foreground: "f8f8f2"},
        {token: "variable.language", foreground: "fd971f", fontStyle: "italic"},
        {token: "variable.language.prefix", foreground: "fd971f", fontStyle: "italic"},
        {token: "variable.language.member", foreground: "f8f8f2"},
        {token: "variable.language.flag", foreground: "fd971f", fontStyle: "italic"},
        {token: "keyword.operator", foreground: "f92672"},
        {token: "keyword.operator.append", foreground: "f92672"},
        {token: "string", foreground: "e6db74"},
        {token: "string.quoted.double.sql", foreground: "e6db74"},
        {token: "keyword.other.sql", foreground: "a6e22e"},
        {token: "constant.character.escape", foreground: "ae81ff"},
        {token: "number", foreground: "ae81ff"},
        {token: "function", foreground: "a6e22e"},
        {token: "entity.name.function", foreground: "a6e22e"},
        {token: "type.identifier", foreground: "a6e22e"},
        {token: "punctuation", foreground: "f8f8f2"},
      ],
      colors: {
        "editor.background": "#272822",
        "editor.foreground": "#f8f8f2",
      },
    },
  },
  {
    key: "darcula",
    label: "Darcula",
    define: {
      base: "vs-dark",
      inherit: true,
      rules: [
        {token: "comment", foreground: "808080", fontStyle: "italic"},
        {token: "keyword", foreground: "cc7832"},
        {token: "keyword.control", foreground: "cc7832"},
        {token: "keyword.other", foreground: "cc7832"},
        {token: "storage", foreground: "cc7832"},
        {token: "storage.type", foreground: "cc7832"},
        {token: "storage.modifier", foreground: "cc7832"},
        {token: "constant", foreground: "9876aa"},
        {token: "constant.numeric", foreground: "6897bb"},
        {token: "variable", foreground: "a9b7c6"},
        {token: "variable.language", foreground: "cc7832", fontStyle: "italic"},
        {token: "variable.language.prefix", foreground: "cc7832", fontStyle: "italic"},
        {token: "variable.language.member", foreground: "a9b7c6"},
        {token: "variable.language.flag", foreground: "cc7832", fontStyle: "italic"},
        {token: "keyword.operator", foreground: "a9b7c6"},
        {token: "keyword.operator.append", foreground: "a9b7c6"},
        {token: "string", foreground: "6a8759"},
        {token: "string.quoted.double.sql", foreground: "6a8759"},
        {token: "keyword.other.sql", foreground: "cc7832"},
        {token: "constant.character.escape", foreground: "9876aa"},
        {token: "number", foreground: "6897bb"},
        {token: "function", foreground: "ffc66d"},
        {token: "entity.name.function", foreground: "ffc66d"},
        {token: "type.identifier", foreground: "ffc66d"},
        {token: "punctuation", foreground: "a9b7c6"},
      ],
      colors: {
        "editor.background": "#2b2b2b",
        "editor.foreground": "#a9b7c6",
      },
    },
  },
  {
    key: "one-dark-pro",
    label: "One Dark Pro",
    define: {
      base: "vs-dark",
      inherit: true,
      rules: [
        {token: "comment", foreground: "7f848e", fontStyle: "italic"},
        {token: "keyword", foreground: "c678dd"},
        {token: "keyword.control", foreground: "c678dd"},
        {token: "keyword.operator", foreground: "56b6c2"},
        {token: "storage", foreground: "c678dd"},
        {token: "storage.type", foreground: "c678dd"},
        {token: "storage.modifier", foreground: "c678dd"},
        {token: "constant", foreground: "d19a66"},
        {token: "constant.language", foreground: "56b6c2"},
        {token: "constant.numeric", foreground: "d19a66"},
        {token: "variable", foreground: "e06c75"},
        {token: "variable.language", foreground: "e06c75", fontStyle: "italic"},
        {token: "variable.language.prefix", foreground: "e06c75", fontStyle: "italic"},
        {token: "variable.language.member", foreground: "abb2bf"},
        {token: "variable.language.flag", foreground: "e06c75", fontStyle: "italic"},
        {token: "string", foreground: "98c379"},
        {token: "number", foreground: "d19a66"},
        {token: "function", foreground: "61afef"},
        {token: "entity.name.function", foreground: "61afef"},
        {token: "entity.name.type", foreground: "e5c07b"},
        {token: "support.type", foreground: "e5c07b"},
        {token: "punctuation", foreground: "abb2bf"},
      ],
      colors: {
        "editor.background": "#282c34",
        "editor.foreground": "#abb2bf",
        "editorLineNumber.foreground": "#495162",
        "editor.selectionBackground": "#3e4451",
        "editor.lineHighlightBackground": "#2c313c",
        "editorCursor.foreground": "#528bff",
        "editorWhitespace.foreground": "#3b4048",
      },
    },
  },
  {
    key: "night-owl",
    label: "NightOwl",
    define: {
      base: "vs-dark",
      inherit: true,
      rules: [
        {token: "comment", foreground: "637777", fontStyle: "italic"},
        {token: "keyword", foreground: "c792ea"},
        {token: "keyword.control", foreground: "c792ea"},
        {token: "keyword.other", foreground: "c792ea"},
        {token: "storage", foreground: "c792ea"},
        {token: "storage.type", foreground: "c792ea"},
        {token: "storage.modifier", foreground: "c792ea"},
        {token: "constant", foreground: "82aaff"},
        {token: "constant.language", foreground: "82aaff"},
        {token: "constant.numeric", foreground: "f78c6c"},
        {token: "variable", foreground: "d6deeb"},
        {token: "variable.language", foreground: "c792ea", fontStyle: "italic"},
        {token: "variable.language.prefix", foreground: "7fdbca", fontStyle: "italic"},
        {token: "variable.language.member", foreground: "addb67"},
        {token: "variable.language.flag", foreground: "c792ea", fontStyle: "italic"},
        {token: "keyword.operator", foreground: "7fdbca"},
        {token: "keyword.operator.append", foreground: "7fdbca"},
        {token: "string", foreground: "ecc48d"},
        {token: "string.quoted.double.sql", foreground: "ecc48d"},
        {token: "keyword.other.sql", foreground: "c792ea"},
        {token: "constant.character.escape", foreground: "7fdbca"},
        {token: "number", foreground: "f78c6c"},
        {token: "function", foreground: "82aaff"},
        {token: "entity.name.function", foreground: "82aaff"},
        {token: "type.identifier", foreground: "addb67"},
        {token: "punctuation", foreground: "d6deeb"},
      ],
      colors: {
        "editor.background": "#011627",
        "editor.foreground": "#d6deeb",
        "editorLineNumber.foreground": "#4b6479",
        "editorLineNumber.activeForeground": "#c5e4fd",
        "editorCursor.foreground": "#80a4c2",
        "editor.selectionBackground": "#1d3b53",
        "editor.inactiveSelectionBackground": "#1d3b53",
        "editor.lineHighlightBackground": "#0b2942",
        "editorIndentGuide.background": "#122d42",
        "editorIndentGuide.activeBackground": "#2c4b63",
        "editorWhitespace.foreground": "#234d70",
        "editorOverviewRuler.border": "#011627",
        "editorBracketMatch.background": "#1d3b53",
        "editorBracketMatch.border": "#5ca7d8",
        "editor.findMatchBackground": "#5ca7d8",
        "editor.findMatchHighlightBackground": "#1d3b53",
        "editorWidget.background": "#0b2942",
        "editorWidget.border": "#1d3b53",
        "peekViewEditor.background": "#0b2942",
        "peekViewResult.background": "#0b2942",
        "peekViewTitle.background": "#0b2942",
        "scrollbarSlider.background": "#1d3b53",
        "scrollbarSlider.hoverBackground": "#2c4b63",
        "scrollbarSlider.activeBackground": "#5ca7d8",
        "minimap.background": "#011627",
      },
    },
  },
]

// ensureTheme registers any custom theme on the given Monaco instance. Idempotent.
export function ensureTheme(monacoInstance: MonacoLike, key: string): void {
  const opt = MONACO_THEME_OPTIONS.find((o) => o.key === key)
  if (opt?.define) {
    // Normalize local themes through the same GS2 adapter used by remote and
    // user-created themes. This keeps functions, prefixes, SQL, operators and
    // punctuation colored even when a base theme omits those scopes.
    monacoInstance.editor.defineTheme(key, adaptMonacoTheme(opt.define) as StandaloneThemeData)
  }
}
