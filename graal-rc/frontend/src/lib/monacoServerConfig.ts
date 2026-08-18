// serverconfig — a Monarch language for the server-side text configs (Server
// Options / Folder Config / Server Flags / NPC Flags). It handles two shapes:
//   - k=v documents (Options/Flags/NPC Flags): keys before `=`, values after,
//     comma-separated arrays (commas colored as delimiter), # comments and
//     [Header] sections (Options).
//   - type-path documents (Folder Config): a leading type word (file/body/
//     sword/...) then whitespace then a server path, each colored distinctly.

// Minimal monaco.languages surface (monaco-editor is a transitive dep).
interface MonacoLanguageAPI {
  languages: {
    register(language: {id: string}): void
    setMonarchTokensProvider(languageId: string, provider: unknown): void
  }
}

let registered = false

// Server option names may contain spaces (for example, "guild_Global Admin").
// Keep the complete key together so the folder-config rule does not mistake
// the first word for a type token.
export const SERVER_CONFIG_KEY_PATTERN = /[A-Za-z0-9_.\-]+(?:[ \t]+[A-Za-z0-9_.\-]+)*(?=\s*=)/

// registerServerConfig registers the serverconfig language + Monarch tokenizer
// once per monaco instance. Idempotent. Wrapped so a monarch compile hiccup can
// never crash the editor mount (the editor would silently fall back to plain
// text rather than render a grey panel).
export function registerServerConfig(monaco: MonacoLanguageAPI): void {
  if (registered) return
  try {
    monaco.languages.register({id: "serverconfig"})
    monaco.languages.setMonarchTokensProvider(
      "serverconfig",
      {
        defaultToken: "",
        tokenPostfix: "",
        ignoreCase: false,
        tokenizer: {
          root: [
            // # line comments.
            [/#.*$/, "comment"],
            // [Header] sections (Server Options).
            [/\[[^\]]*\]/, "type.identifier"],
            // k=v key: identifiers and spaces immediately before an = (dots/
            // dashes allowed, e.g. guild_Global Admin).
            [SERVER_CONFIG_KEY_PATTERN, "keyword"],
            // Folder-config type: a leading word followed by whitespace then a
            // path (file/body/head/sword/shield/...).
            [/[A-Za-z0-9_.\-]+(?=\s+[^#\n])/, "type.identifier"],
            // = separates key and value.
            [/=/, "delimiter"],
            // commas in array values (k=a,b,c) — colored as delimiter (white).
            [/,/, "delimiter"],
            // value / path token: a run with no whitespace / comma / = / # / [].
            [/[^\s#,=\[\]]+/, "string"],
            [/\s+/, "white"],
          ],
        },
      },
    )
  } catch {
    // best-effort: leave serverconfig unregistered; Editor falls back to plain
  }
  registered = true
}
