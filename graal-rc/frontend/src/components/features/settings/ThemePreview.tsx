import {useCallback, useEffect, useMemo, useRef, useState} from "react"
import Editor, {type BeforeMount, type OnMount} from "@monaco-editor/react"
import {Check, ChevronDown, Code2, Palette, Plus, Save} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {useLanguage} from "@/hooks/useLanguage"
import {ensureTheme, MONACO_THEME_OPTIONS} from "@/lib/monacoThemes"
import {registerGraalScript} from "@/lib/monacoGraalScript"
import {adaptMonacoTheme, type ThemeData} from "@/lib/adaptTheme"
import {rcService} from "@/services/rcService"
import type {CustomTheme} from "@/types"

const PREVIEW = `function onCreated() {
  // GraalScript2 theme preview
  temp.playerName = player.account;
  if (serverr & 0x01) {
    temp.message = "SELECT account FROM players WHERE active = 1";
    temp.message = temp.message @ SPC @ player.account;
  }
}`

function parseDefinition(definition: string): ThemeData | null {
  try {
    const parsed = JSON.parse(definition) as ThemeData
    return parsed && typeof parsed === "object" ? parsed : null
  } catch { return null }
}

type ThemeDraft = {base: "vs" | "vs-dark" | "hc-black" | "hc-light"; colors: Record<string, string>; tokens: Record<string, {foreground: string; fontStyle: string}>}

const GS2_THEME_TOKENS = [
  ["comment", "settings.token.comment"],
  ["keyword.control", "settings.token.controlKeyword"],
  ["keyword.other", "settings.token.otherKeyword"],
  ["storage.type", "settings.token.storageType"],
  ["storage.modifier", "settings.token.storageModifier"],
  ["constant.language", "settings.token.constant"],
  ["constant.numeric", "settings.token.number"],
  ["variable.language", "settings.token.legacyVariable"],
  ["variable.language.prefix", "settings.token.prefix"],
  ["variable.language.member", "settings.token.member"],
  ["variable.language.flag", "settings.token.flag"],
  ["type.identifier", "settings.token.type"],
  ["entity.name.function", "settings.token.function"],
  ["string", "settings.token.string"],
  ["string.quoted.double.sql", "settings.token.sqlString"],
  ["keyword.other.sql", "settings.token.sqlKeyword"],
  ["constant.character.escape", "settings.token.escape"],
  ["keyword.operator", "settings.token.operator"],
  ["keyword.operator.array", "settings.token.arrayOperator"],
  ["keyword.operator.append", "settings.token.append"],
  ["punctuation", "settings.token.punctuation"],
] as const

const EDITOR_COLOR_LABELS: Record<string, {label: string; description: string}> = {
  "editor.background": {label: "settings.color.editorBackground", description: "settings.color.editorBackgroundDescription"},
  "editor.foreground": {label: "settings.color.editorForeground", description: "settings.color.editorForegroundDescription"},
  "editorCursor.foreground": {label: "settings.color.cursor", description: "settings.color.cursorDescription"},
  "editor.selectionBackground": {label: "settings.color.selection", description: "settings.color.selectionDescription"},
  "editor.lineHighlightBackground": {label: "settings.color.currentLine", description: "settings.color.currentLineDescription"},
}

const DEFAULT_TOKEN_COLORS: Record<string, string> = {
  comment: "#6b7280", "keyword.control": "#c084fc", "keyword.other": "#f472b6", "storage.type": "#c084fc", "storage.modifier": "#f472b6",
  "constant.language": "#fbbf24", "constant.numeric": "#fbbf24", "variable.language": "#67e8f9", "variable.language.prefix": "#4ec9b0", "variable.language.member": "#67e8f9", "variable.language.flag": "#c586c0",
  "type.identifier": "#fca5a5", "entity.name.function": "#60a5fa", "string.quoted.double.sql": "#86efac",
  string: "#86efac", "keyword.other.sql": "#facc15", "constant.character.escape": "#fb923c", "keyword.operator": "#a5b4fc", "keyword.operator.array": "#a5b4fc", "keyword.operator.append": "#d7ba7d", punctuation: "#e6e8ed",
}

const BASE_COLOR_PRESETS: Record<ThemeDraft["base"], Record<string, string>> = {
  "vs-dark": {
    "editor.background": "#1e1e1e", "editor.foreground": "#d4d4d4", "editorCursor.foreground": "#aeafad",
    "editor.selectionBackground": "#264f78", "editor.lineHighlightBackground": "#2a2d2e",
  },
  vs: {
    "editor.background": "#ffffff", "editor.foreground": "#3b3b3b", "editorCursor.foreground": "#000000",
    "editor.selectionBackground": "#add6ff", "editor.lineHighlightBackground": "#f3f3f3",
  },
  "hc-black": {
    "editor.background": "#000000", "editor.foreground": "#ffffff", "editorCursor.foreground": "#ffffff",
    "editor.selectionBackground": "#616161", "editor.lineHighlightBackground": "#1f1f1f",
  },
  "hc-light": {
    "editor.background": "#ffffff", "editor.foreground": "#000000", "editorCursor.foreground": "#000000",
    "editor.selectionBackground": "#bde5ff", "editor.lineHighlightBackground": "#eeeeee",
  },
}

const BASE_TOKEN_PRESETS: Record<ThemeDraft["base"], Record<string, string>> = {
  "vs-dark": DEFAULT_TOKEN_COLORS,
  vs: {
    comment: "#008000", "keyword.control": "#0000ff", "keyword.other": "#af00db", "storage.type": "#267f99", "storage.modifier": "#af00db",
    "constant.language": "#0000ff", "constant.numeric": "#098658", "variable.language": "#001080", "variable.language.prefix": "#267f99", "variable.language.member": "#001080", "variable.language.flag": "#af00db",
    "type.identifier": "#267f99", "entity.name.function": "#795e26", string: "#a31515", "string.quoted.double.sql": "#a31515", "keyword.other.sql": "#0000ff", "constant.character.escape": "#ee0000",
    "keyword.operator": "#000000", "keyword.operator.array": "#000000", "keyword.operator.append": "#0000ff", punctuation: "#000000",
  },
  "hc-black": {
    comment: "#7f9f7f", "keyword.control": "#ff00ff", "keyword.other": "#ffaf00", "storage.type": "#00ffff", "storage.modifier": "#ff00ff",
    "constant.language": "#00ffff", "constant.numeric": "#ffaf00", "variable.language": "#ffffff", "variable.language.prefix": "#00ffff", "variable.language.member": "#ffffff", "variable.language.flag": "#ff00ff",
    "type.identifier": "#00ffff", "entity.name.function": "#ffff00", string: "#ffaf00", "string.quoted.double.sql": "#ffaf00", "keyword.other.sql": "#00ffff", "constant.character.escape": "#ffaf00",
    "keyword.operator": "#ffffff", "keyword.operator.array": "#ffffff", "keyword.operator.append": "#00ffff", punctuation: "#ffffff",
  },
  "hc-light": {
    comment: "#006600", "keyword.control": "#800080", "keyword.other": "#8a3c00", "storage.type": "#006666", "storage.modifier": "#800080",
    "constant.language": "#006666", "constant.numeric": "#8a3c00", "variable.language": "#000000", "variable.language.prefix": "#006666", "variable.language.member": "#000000", "variable.language.flag": "#800080",
    "type.identifier": "#006666", "entity.name.function": "#795e26", string: "#8a1f11", "string.quoted.double.sql": "#8a1f11", "keyword.other.sql": "#0000aa", "constant.character.escape": "#a31515",
    "keyword.operator": "#000000", "keyword.operator.array": "#000000", "keyword.operator.append": "#0000aa", punctuation: "#000000",
  },
}

function draftFromDefinition(definition?: string): ThemeDraft {
  const parsed = parseDefinition(definition ?? "")
  const tokens: ThemeDraft["tokens"] = {}
  for (const [token] of GS2_THEME_TOKENS) {
    const rule = parsed?.rules?.find((item) => item.token === token)
    tokens[token] = {foreground: rule?.foreground ? `#${rule.foreground.replace(/^#/, "")}` : DEFAULT_TOKEN_COLORS[token] ?? "#e6e8ed", fontStyle: rule?.fontStyle ?? ""}
  }
  return {base: parsed?.base ?? "vs-dark", colors: {"editor.background": parsed?.colors?.["editor.background"] ?? "#111318", "editor.foreground": parsed?.colors?.["editor.foreground"] ?? "#e6e8ed", "editorCursor.foreground": parsed?.colors?.["editorCursor.foreground"] ?? "#7dd3fc", "editor.selectionBackground": parsed?.colors?.["editor.selectionBackground"] ?? "#26415f", "editor.lineHighlightBackground": parsed?.colors?.["editor.lineHighlightBackground"] ?? "#1b2029"}, tokens}
}

function definitionFromDraft(draft: ThemeDraft): string {
  return JSON.stringify({base: draft.base, inherit: true, colors: draft.colors, rules: Object.entries(draft.tokens).map(([token, rule]) => ({token, foreground: rule.foreground.replace(/^#/, ""), ...(rule.fontStyle ? {fontStyle: rule.fontStyle} : {})}))}, null, 2)
}

function themeVersion(value: string): string {
  let hash = 2166136261
  for (let index = 0; index < value.length; index += 1) {
    hash ^= value.charCodeAt(index)
    hash = Math.imul(hash, 16777619)
  }
  return (hash >>> 0).toString(16)
}

function ThemeChoiceMenu({value, options, onChange, ariaLabel}: {value: string; options: {value: string; label: string; description?: string}[]; onChange: (value: string) => void; ariaLabel: string}) {
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const selected = options.find((option) => option.value === value) ?? options[0]

  useEffect(() => {
    if (!open) return
    const close = (event: MouseEvent) => {
      if (ref.current && !ref.current.contains(event.target as Node)) setOpen(false)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", close)
    document.addEventListener("keydown", escape)
    return () => {
      document.removeEventListener("mousedown", close)
      document.removeEventListener("keydown", escape)
    }
  }, [open])

  return (
    <div ref={ref} className={"relative " + (open ? "z-[100]" : "z-20")}>
      <button type="button" aria-label={ariaLabel} aria-expanded={open} onClick={() => setOpen((current) => !current)} className="bg-input border-input focus-visible:ring-ring flex h-9 w-full items-center justify-between rounded-md border px-2 text-left text-sm focus-visible:ring-2 focus-visible:outline-none">
        <span className="min-w-0 truncate">{selected?.label}</span>
        <ChevronDown className="text-muted-foreground size-4 shrink-0" />
      </button>
      {open && (
        <div className="bg-popover text-popover-foreground absolute left-0 top-full mt-1 w-full min-w-56 overflow-hidden rounded-md border shadow-lg opacity-100">
          {options.map((option) => (
            <button key={option.value} type="button" onClick={() => { onChange(option.value); setOpen(false) }} className={"flex w-full items-start gap-2 px-3 py-2 text-left hover:bg-accent " + (option.value === value ? "bg-accent" : "")}>
              <Check className={"mt-0.5 size-4 shrink-0 " + (option.value === value ? "opacity-100" : "opacity-0")} />
              <span className="min-w-0">
                <span className="block text-sm">{option.label}</span>
                {option.description && <span className="text-muted-foreground mt-0.5 block text-[11px] leading-snug">{option.description}</span>}
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

export function ThemePreview({theme, definition, fontFamily, fontSize}: {theme: string; definition?: string; fontFamily: string; fontSize: number}) {
  const {t} = useLanguage()
  const monacoRef = useRef<any>(null)
  const [ready, setReady] = useState(false)
  const activeDefinition = useMemo(() => parseDefinition(definition ?? ""), [definition])
  // Monaco validates theme names. Keep preview themes internal and only use
  // lowercase letters, digits and hyphens so a user-provided key can never
  // make the editor crash during `defineTheme`.
  const activeTheme = useMemo(() => activeDefinition ? `preview-${themeVersion(definition ?? "")}` : theme, [theme, definition, activeDefinition])
  const beforeMount: BeforeMount = useCallback((monaco) => {
    const m = monaco as any
    registerGraalScript(m)
    // The editor applies its `theme` during initialization. Defining the
    // preview theme only in the effect below is too late: Monaco can already
    // have painted the editor with its default `vs` theme, which is especially
    // visible when the selected base is dark.
    if (activeDefinition) {
      m.editor.defineTheme(activeTheme, adaptMonacoTheme(activeDefinition))
    } else {
      ensureTheme(m, theme)
    }
  }, [activeDefinition, activeTheme, theme])
  const onMount: OnMount = useCallback((_editor, monaco) => {
    monacoRef.current = monaco
    setReady(true)
  }, [])

  useEffect(() => {
    const m = monacoRef.current
    if (!m || !ready) return
    try {
      if (activeDefinition) {
        m.editor.defineTheme(activeTheme, adaptMonacoTheme(activeDefinition))
        m.editor.setTheme(activeTheme)
      } else {
        ensureTheme(m, theme)
        m.editor.setTheme(theme)
      }
    } catch { /* invalid custom definitions stay on the last valid theme */ }
  }, [theme, activeTheme, activeDefinition, ready])

  return (
    <div className="overflow-hidden rounded-lg border bg-[#111318]">
      <div className="flex items-center justify-between border-b border-white/10 px-3 py-2">
        <div className="flex items-center gap-2 text-xs text-white/70"><Code2 className="size-3.5" />{t("settings.graalScript2Preview")}</div>
        <span className="font-mono text-[10px] text-white/40">{fontSize}px · {fontFamily.split(",")[0]}</span>
      </div>
      <div className="h-56">
        <Editor
          key={activeTheme}
          height="100%"
          language="graalscript"
          value={PREVIEW}
          beforeMount={beforeMount}
          onMount={onMount}
          theme={activeTheme}
          options={{readOnly: true, minimap: {enabled: false}, fontFamily, fontSize, fontLigatures: true, lineNumbers: "on", folding: false, padding: {top: 12, bottom: 12}, scrollBeyondLastLine: false, overviewRulerLanes: 0}}
        />
      </div>
    </div>
  )
}

export function CustomThemeDialog({open, theme, onClose, onSaved}: {open: boolean; theme?: CustomTheme; onClose: () => void; onSaved: (theme: CustomTheme) => void}) {
  const {t} = useLanguage()
  const [name, setName] = useState("")
  const [key, setKey] = useState("")
  const [draft, setDraft] = useState<ThemeDraft>(() => draftFromDefinition())
  const [error, setError] = useState("")
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    const initial = theme ?? {key: "custom-theme", name: t("settings.newTheme"), definition: JSON.stringify({base: "vs-dark", inherit: true, colors: {"editor.background": "#111318", "editor.foreground": "#e6e8ed", "editorCursor.foreground": "#7dd3fc", "editor.selectionBackground": "#26415f"}, rules: [{token: "comment", foreground: "6b7280", fontStyle: "italic"}, {token: "keyword", foreground: "c084fc"}, {token: "string", foreground: "86efac"}, {token: "constant.numeric", foreground: "fbbf24"}, {token: "entity.name.function", foreground: "60a5fa"}]}, null, 2)}
    setName(initial.name); setKey(initial.key); setDraft(draftFromDefinition(initial.definition)); setError("")
  }, [open, theme, t])

  if (!open) return null
  const save = async () => {
    try {
      const cleanKey = (key || name).toLowerCase().replace(/^custom[-_]/, "").replace(/[^a-z0-9]+/g, "-").replace(/^-+|-+$/g, "")
      if (!cleanKey || !name.trim()) throw new Error(t("settings.themeNameRequired"))
      setSaving(true); setError("")
      const saved = {key: `custom-${cleanKey}`, name: name.trim(), definition: definitionFromDraft(draft)}
      await rcService.saveCustomTheme(saved)
      onSaved(saved)
      onClose()
    } catch (err) { setError(err instanceof Error ? err.message : t("settings.invalidTheme")) }
    finally { setSaving(false) }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
      <div className="bg-background flex max-h-[92vh] w-full max-w-4xl flex-col overflow-hidden rounded-xl border shadow-xl">
        <div className="flex items-start justify-between border-b px-5 py-4">
          <div><h2 className="flex items-center gap-2 text-lg font-semibold"><Palette className="text-primary size-5" />{theme ? t("settings.editTheme") : t("settings.newTheme")}</h2><p className="text-muted-foreground mt-1 text-xs">{t("settings.themeEditorDescription")}</p></div>
          <Button variant="ghost" size="icon" onClick={onClose} aria-label={t("common.close")}>×</Button>
        </div>
        <div className="grid min-h-0 gap-4 overflow-y-auto p-5 md:grid-cols-[220px_1fr]">
          <div className="grid content-start gap-4">
            <div className="grid gap-2"><Label htmlFor="theme-name">{t("settings.themeName")}</Label><Input id="theme-name" value={name} onChange={(e) => setName(e.target.value)} /></div>
            <div className="grid gap-2"><Label htmlFor="theme-key">{t("settings.themeKey")}</Label><Input id="theme-key" value={key} onChange={(e) => setKey(e.target.value)} /><p className="text-muted-foreground text-[11px]">{t("settings.themeKeyDescription")}</p></div>
            <div className="rounded-md border bg-muted/30 p-3 text-xs leading-relaxed"><p className="font-medium">{t("settings.graalScript2Tokens")}</p><p className="text-muted-foreground mt-1">{t("settings.graalScript2TokensDescription")}</p></div>
          </div>
          <div className="grid gap-3">
            <div className="grid gap-2 rounded-md border p-3">
              <Label>{t("settings.themeBase")}</Label>
              <ThemeChoiceMenu
                value={draft.base}
                ariaLabel={t("settings.themeBase")}
                onChange={(value) => setDraft((current) => {
                  const base = value as ThemeDraft["base"]
                  const tokenColors = BASE_TOKEN_PRESETS[base]
                  const tokens = Object.fromEntries(Object.entries(current.tokens).map(([token, rule]) => [token, {...rule, foreground: tokenColors[token] ?? rule.foreground}]))
                  return {...current, base, colors: {...current.colors, ...BASE_COLOR_PRESETS[base]}, tokens}
                })}
                options={[
                  {value: "vs-dark", label: t("settings.baseDark"), description: t("settings.baseDarkDescription")},
                  {value: "vs", label: t("settings.baseLight"), description: t("settings.baseLightDescription")},
                  {value: "hc-black", label: t("settings.baseHighContrastDark"), description: t("settings.baseHighContrastDarkDescription")},
                  {value: "hc-light", label: t("settings.baseHighContrastLight"), description: t("settings.baseHighContrastLightDescription")},
                ]}
              />
            </div>
            <div className="grid gap-2 rounded-md border p-3">
              <p className="text-sm font-medium">{t("settings.editorColors")}</p>
              {Object.entries(draft.colors).map(([name, value]) => (
                <div key={name} className="flex items-center gap-2">
                  <div className="min-w-0 flex-1">
                    <Label className="block truncate text-xs" htmlFor={"color-" + name}>{t(EDITOR_COLOR_LABELS[name]?.label ?? name)}</Label>
                    <p className="text-muted-foreground truncate text-[10px]">{t(EDITOR_COLOR_LABELS[name]?.description ?? "settings.color.genericDescription")}</p>
                  </div>
                  <input id={"color-" + name} aria-label={t(EDITOR_COLOR_LABELS[name]?.label ?? name)} type="color" value={value} onChange={(event) => setDraft((current) => ({...current, colors: {...current.colors, [name]: event.target.value}}))} className="h-7 w-10 cursor-pointer rounded border bg-transparent p-0.5" />
                  <span className="text-muted-foreground w-16 font-mono text-[10px]">{value}</span>
                </div>
              ))}
            </div>
            <div className="grid gap-2 rounded-md border p-3">
              <p className="text-sm font-medium">{t("settings.graalScript2Tokens")}</p>
              <p className="text-muted-foreground text-xs">{t("settings.tokenSelectorDescription")}</p>
              <div className="grid gap-2 sm:grid-cols-2">
                {GS2_THEME_TOKENS.map(([token, label]) => {
                  const rule = draft.tokens[token]
                  const translatedLabel = t(label)
                  return <div key={token} className="flex items-center gap-2 rounded-md bg-muted/30 p-2">
                    <span className="min-w-0 flex-1 truncate text-xs" title={token}>{translatedLabel}</span>
                    <input aria-label={translatedLabel} type="color" value={rule.foreground} onChange={(event) => setDraft((current) => ({...current, tokens: {...current.tokens, [token]: {...current.tokens[token], foreground: event.target.value}}}))} className="h-7 w-9 cursor-pointer rounded border bg-transparent p-0.5" />
                    <div className="w-28 shrink-0">
                      <ThemeChoiceMenu
                        value={rule.fontStyle}
                        ariaLabel={t("settings.tokenStyleFor", {token: translatedLabel})}
                        onChange={(fontStyle) => setDraft((current) => ({...current, tokens: {...current.tokens, [token]: {...current.tokens[token], fontStyle}}}))}
                        options={[
                          {value: "", label: t("settings.styleNormal")},
                          {value: "italic", label: t("settings.styleItalic")},
                          {value: "bold", label: t("settings.styleBold")},
                          {value: "italic bold", label: t("settings.styleItalicBold")},
                        ]}
                      />
                    </div>
                  </div>
                })}
              </div>
            </div>
            <ThemePreview theme={key || "custom_preview"} definition={definitionFromDraft(draft)} fontFamily="Consolas, monospace" fontSize={13} />
          </div>
        </div>
        {error && <p className="text-destructive border-t px-5 py-2 text-xs">{error}</p>}
        <div className="flex justify-end gap-2 border-t px-5 py-3"><Button variant="outline" onClick={onClose}>{t("common.cancel")}</Button><Button onClick={save} disabled={saving}><Save className="size-4" />{t("common.save")}</Button></div>
      </div>
    </div>
  )
}

export function NewThemeButton({onClick}: {onClick: () => void}) {
  const {t} = useLanguage()
  return <Button variant="outline" size="sm" onClick={onClick}><Plus className="size-4" />{t("settings.newTheme")}</Button>
}
