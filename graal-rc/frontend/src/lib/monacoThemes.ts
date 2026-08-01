import {adaptMonacoTheme} from "@/lib/adaptTheme"

// Monaco theme catalog for the Coding settings. Built-in themes (vs, vs-dark,
// hc-black, hc-light) need no definition; custom themes (monokai, darcula) are
// registered via monaco.editor.defineTheme on first editor mount. Types kept
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

// monokai/darcula/one-dark-pro evoked via base vs-dark + token rules covering
// the common highlights.
export const MONACO_THEME_OPTIONS: ThemeOption[] = [
  {key: "vs-dark", label: "Dark (VS)", define: baseTheme("vs-dark")},
  {key: "vs", label: "Light (VS)", define: baseTheme("vs")},
  {key: "hc-black", label: "Dark High Contrast", define: baseTheme("hc-black")},
  {key: "hc-light", label: "Light High Contrast", define: baseTheme("hc-light")},
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
