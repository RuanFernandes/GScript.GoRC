// GS2 (GraalScript) language definition for Monaco — a faithful 1:1 port of the
// graalscript.tmLanguage.json grammar to a Monarch tokenizer. Token names mirror
// the tmLanguage scope names exactly (tokenPostfix appends ".graalscript"), so
// Monaco's theme prefix-fallback colors them under the standard rules.
// Rule order matches the grammar's `patterns`/repository includes so precedence
// (sql-strings shadow strings, keywords before function-call ids) is identical.

// Minimal monaco.languages surface (monaco-editor is a transitive dep).
interface MonacoLanguageAPI {
  languages: {
    register(language: {id: string}): void
    setMonarchTokensProvider(languageId: string, provider: unknown): void
    setLanguageConfiguration(languageId: string, config: unknown): void
  }
}

let registered = false

// registerGraalScript registers the GS2 language + Monarch tokenizer once per
// monaco instance. Idempotent.
export function registerGraalScript(monaco: MonacoLanguageAPI): void {
  if (registered) return
  registered = true
  monaco.languages.register({id: "graalscript"})

  monaco.languages.setMonarchTokensProvider(
    "graalscript",
    {
      defaultToken: "",
      tokenPostfix: ".graalscript",
      // Order mirrors the grammar's top-level patterns: sql-strings (shadows
      // plain strings since both begin with "), comments, numbers, keywords,
      // function-call identifiers, then operators/punctuation last.
      tokenizer: {
        root: [
          [/"/, "string.quoted.double.sql", "@sqlString"],
          [/\/\/.*$/, "comment.line.double-slash"],
          [/\/\*\*(?=[^*])/, "comment.block.documentation", "@docComment"],
          [/\/\*/, "comment.block", "@blockComment"],
          [/\b([0-9]+|0[xX][0-9a-fA-F]+)\b/, "constant.numeric"],
          [/\b(break|case|continue|default|do|else|elseif|for|if|in|return|switch|while|with)\b/, "keyword.control"],
          [/\b(import)\b/, "storage.type"],
          [/\b(public|private|const|enum|function)\b/, "storage.modifier"],
          [/\b(new|datablock)\b/, "keyword.other"],
          [/\b(true|false|nil|null|NULL|pi|timevar2)\b/, "constant.language"],
          // Keep the GS2 namespace/prefix separate from the member name so
          // themes can style `temp.` independently from `playerName`.
          // Dynamic GS2 properties use this.(@expr) and this.("name").
          // Enter a dedicated state before the regular member-access rule so
          // the opening parenthesis, @, and expression remain one property
          // access instead of falling back to root tokens.
          [/\b(?:temp|server|client|clientr|player|this|thiso)(?=\.\s*\()/, "variable.language.prefix", "@dynamicMemberAccess"],
          [/\b(?:temp|server|client|clientr|player|this|thiso)(?=\.)/, "variable.language.prefix", "@memberAccess"],
          [/\b(serverr)\b/, "variable.language.flag"],
          [/\b(name)\b/, "variable.language"],
          // ClassName::method() — the identifier before `::` is the object/class,
          // colored as a type, and `::` itself colored as an operator. Outside
          // strings by virtue of the root state (sql-strings shadow plain strings).
          [/\b[a-zA-Z_][a-zA-Z0-9_]*(?=::)/, "type.identifier"],
          [/::/, "keyword.operator"],
          [/\b[a-zA-Z_][a-zA-Z0-9_]*\s*(?=\()/, "entity.name.function"],
          // Consume the complete identifier before tokenizing operators or
          // numbers, so names such as p1, p2, and file1 stay one token.
          [/\b[a-zA-Z_][a-zA-Z0-9_]*/, "identifier"],
          [/@/, "keyword.operator.append"],
          [/\bSPC\b/, "keyword.operator.append"],
          [/[-~^/%|=+*!?&<>]/, "keyword.operator"],
          [/[\[\]]/, "keyword.operator.array"],
          [/[{}();:,.]/, "punctuation"],
        ],
        memberAccess: [
          [/\./, "punctuation"],
          [/[a-zA-Z_][a-zA-Z0-9_]*/, "variable.language.member", "@pop"],
          [/./, "@pop"],
        ],
        dynamicMemberAccess: [
          [/\s+/, ""],
          [/\./, "punctuation"],
          [/\(/, "punctuation"],
          [/@/, "keyword.operator.append"],
          [/\b(?:temp|server|client|clientr|player|this|thiso)(?=\.)/, "variable.language.prefix"],
          [/"/, "string.quoted.double.sql", "@sqlString"],
          [/[a-zA-Z_][a-zA-Z0-9_]*/, "variable.language.member"],
          [/[-~^/%|=+*!?&<>]/, "keyword.operator"],
          [/[\[\]]/, "keyword.operator.array"],
          [/\)/, "punctuation", "@pop"],
          [/[{};:,.]/, "punctuation"],
          [/./, ""],
        ],
        // "..." string with SQL keywords highlighted inside (grammar:
        // repository.sql-strings). Because this state shadows plain strings,
        // every double-quoted string gets SQL-keyword highlighting.
        // The catch-all is split into a word-run + single-char rule (NOT a
        // greedy [^"]+) so every keyword is tested at its own start position —
        // otherwise a greedy run after the first keyword swallows the rest
        // (FROM/WHERE) and they never get recolored, regardless of whitespace.
        sqlString: [
          [
            /\b(SELECT|INSERT|UPDATE|DELETE|CREATE|TABLE|FROM|WHERE|VALUES|SET|INTO|AND|OR|NOT|NULL|IS|AS|ON|JOIN|LEFT|RIGHT|INNER|OUTER|GROUP|BY|ORDER|LIMIT|OFFSET|DISTINCT|COUNT|AVG|SUM|MIN|MAX|PRIMARY|KEY|DEFAULT|INT|TEXT)\b/,
            "keyword.other.sql",
          ],
          [/\\./, "constant.character.escape"],
          [/"/, "string.quoted.double.sql", "@pop"],
          [/[a-zA-Z_][a-zA-Z0-9_]*/, "string.quoted.double.sql"],
          [/./, "string.quoted.double.sql"],
        ],
        // /** ... */ doc comment with leading-asterisk line highlighting.
        docComment: [
          [/^\s*\*(?: |$).*$/, "comment.block"],
          [/\*\//, "comment.block", "@pop"],
          [/[^*]+/, "comment.block.documentation"],
          [/[\/*]/, "comment.block.documentation"],
        ],
        // /* ... */ block comment.
        blockComment: [
          [/[^\/*]+/, "comment.block"],
          [/\*\//, "comment.block", "@pop"],
          [/[\/*]/, "comment.block"],
        ],
      },
    },
  )

  // Bracket/quote auto-closing config for GS2.
  monaco.languages.setLanguageConfiguration("graalscript", {
    comments: {lineComment: "//", blockComment: ["/*", "*/"]},
    brackets: [
      ["{", "}"],
      ["[", "]"],
      ["(", ")"],
    ],
    autoClosingPairs: [
      {open: "{", close: "}"},
      {open: "[", close: "]"},
      {open: "(", close: ")"},
      {open: '"', close: '"', notIn: ["string"]},
    ],
    surroundingPairs: [
      {open: "{", close: "}"},
      {open: "[", close: "]"},
      {open: "(", close: ")"},
      {open: '"', close: '"'},
    ],
  })
}
