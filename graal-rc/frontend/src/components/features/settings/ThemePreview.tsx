import {useCallback, useEffect, useMemo, useRef, useState} from "react"
import Editor, {type BeforeMount, type OnMount} from "@monaco-editor/react"
import {Code2, Palette, Plus, Save} from "lucide-react"

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
  ["comment", "Comments"],
  ["keyword.control", "Control keywords"],
  ["keyword.other", "Other keywords"],
  ["storage", "Declarations and flags"],
  ["constant.language", "Constants"],
  ["constant.numeric", "Numbers"],
  ["variable.language", "GS2 prefixes (temp., player., client.)"],
  ["variable.language.flag", "Server flags (serverr)"],
  ["type.identifier", "Types and classes"],
  ["entity.name.function", "Functions"],
  ["string.quoted.double.sql", "Strings and SQL text"],
  ["keyword.other.sql", "SQLite keywords inside strings"],
  ["constant.character.escape", "String escapes"],
  ["keyword.operator", "Operators and append tokens (@, SPC)"],
  ["punctuation", "Punctuation"],
] as const

const DEFAULT_TOKEN_COLORS: Record<string, string> = {
  comment: "#6b7280", "keyword.control": "#c084fc", "keyword.other": "#f472b6", storage: "#f472b6",
  "constant.language": "#fbbf24", "constant.numeric": "#fbbf24", "variable.language": "#67e8f9", "variable.language.flag": "#c586c0",
  "type.identifier": "#fca5a5", "entity.name.function": "#60a5fa", "string.quoted.double.sql": "#86efac",
  "keyword.other.sql": "#facc15", "constant.character.escape": "#fb923c", "keyword.operator": "#a5b4fc", "keyword.operator.append": "#d7ba7d", punctuation: "#e6e8ed",
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

export function ThemePreview({theme, definition, fontFamily, fontSize}: {theme: string; definition?: string; fontFamily: string; fontSize: number}) {
  const {t} = useLanguage()
  const monacoRef = useRef<any>(null)
  const [ready, setReady] = useState(false)
  const activeDefinition = useMemo(() => parseDefinition(definition ?? ""), [definition])
  const beforeMount: BeforeMount = useCallback((monaco) => {
    const m = monaco as any
    registerGraalScript(m)
    ensureTheme(m, theme)
  }, [theme])
  const onMount: OnMount = useCallback((_editor, monaco) => {
    monacoRef.current = monaco
    setReady(true)
  }, [])

  useEffect(() => {
    const m = monacoRef.current
    if (!m || !ready) return
    try {
      if (activeDefinition) m.editor.defineTheme(theme, adaptMonacoTheme(activeDefinition))
      else ensureTheme(m, theme)
      m.editor.setTheme(theme)
    } catch { /* invalid custom definitions stay on the last valid theme */ }
  }, [theme, activeDefinition, ready])

  return (
    <div className="overflow-hidden rounded-lg border bg-[#111318]">
      <div className="flex items-center justify-between border-b border-white/10 px-3 py-2">
        <div className="flex items-center gap-2 text-xs text-white/70"><Code2 className="size-3.5" />{t("settings.graalScript2Preview")}</div>
        <span className="font-mono text-[10px] text-white/40">{fontSize}px · {fontFamily.split(",")[0]}</span>
      </div>
      <div className="h-56">
        <Editor
          height="100%"
          language="graalscript"
          value={PREVIEW}
          beforeMount={beforeMount}
          onMount={onMount}
          theme={theme}
          options={{readOnly: true, minimap: {enabled: false}, fontFamily, fontSize, lineNumbers: "on", folding: false, padding: {top: 12, bottom: 12}, scrollBeyondLastLine: false, overviewRulerLanes: 0}}
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
    const initial = theme ?? {key: "custom_theme", name: t("settings.newTheme"), definition: JSON.stringify({base: "vs-dark", inherit: true, colors: {"editor.background": "#111318", "editor.foreground": "#e6e8ed", "editorCursor.foreground": "#7dd3fc", "editor.selectionBackground": "#26415f"}, rules: [{token: "comment", foreground: "6b7280", fontStyle: "italic"}, {token: "keyword", foreground: "c084fc"}, {token: "string", foreground: "86efac"}, {token: "constant.numeric", foreground: "fbbf24"}, {token: "entity.name.function", foreground: "60a5fa"}]}, null, 2)}
    setName(initial.name); setKey(initial.key); setDraft(draftFromDefinition(initial.definition)); setError("")
  }, [open, theme, t])

  if (!open) return null
  const save = async () => {
    try {
      const cleanKey = (key || name).toLowerCase().replace(/^custom_/, "").replace(/[^a-z0-9]+/g, "_").replace(/^_+|_+$/g, "")
      if (!cleanKey || !name.trim()) throw new Error(t("settings.themeNameRequired"))
      setSaving(true); setError("")
      const saved = {key: `custom_${cleanKey}`, name: name.trim(), definition: definitionFromDraft(draft)}
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
              <Label htmlFor="theme-base">{t("settings.themeBase")}</Label>
              <select id="theme-base" value={draft.base} onChange={(event) => setDraft((current) => ({...current, base: event.target.value as ThemeDraft["base"]}))} className="bg-input border-input h-9 rounded-md border px-2 text-sm">
                <option value="vs-dark">Dark</option><option value="vs">Light</option><option value="hc-black">High contrast dark</option><option value="hc-light">High contrast light</option>
              </select>
            </div>
            <div className="grid gap-2 rounded-md border p-3">
              <p className="text-sm font-medium">{t("settings.editorColors")}</p>
              {Object.entries(draft.colors).map(([name, value]) => (
                <div key={name} className="flex items-center gap-2"><Label className="min-w-0 flex-1 truncate font-mono text-xs" htmlFor={`color-${name}`}>{name.replace("editor.", "")}</Label><input id={`color-${name}`} type="color" value={value} onChange={(event) => setDraft((current) => ({...current, colors: {...current.colors, [name]: event.target.value}}))} className="h-7 w-10 cursor-pointer rounded border bg-transparent p-0.5" /><span className="text-muted-foreground w-16 font-mono text-[10px]">{value}</span></div>
              ))}
            </div>
            <div className="grid gap-2 rounded-md border p-3">
              <p className="text-sm font-medium">{t("settings.graalScript2Tokens")}</p>
              <p className="text-muted-foreground text-xs">{t("settings.tokenSelectorDescription")}</p>
              <div className="grid gap-2 sm:grid-cols-2">
                {GS2_THEME_TOKENS.map(([token, label]) => {
                  const rule = draft.tokens[token]
                  return <div key={token} className="flex items-center gap-2 rounded-md bg-muted/30 p-2"><span className="min-w-0 flex-1 truncate text-xs" title={token}>{label}</span><input aria-label={`${label} color`} type="color" value={rule.foreground} onChange={(event) => setDraft((current) => ({...current, tokens: {...current.tokens, [token]: {...current.tokens[token], foreground: event.target.value}}}))} className="h-7 w-9 cursor-pointer rounded border bg-transparent p-0.5" /><select aria-label={`${label} style`} value={rule.fontStyle} onChange={(event) => setDraft((current) => ({...current, tokens: {...current.tokens, [token]: {...current.tokens[token], fontStyle: event.target.value}}}))} className="bg-input border-input h-7 w-20 rounded border px-1 text-[10px]"><option value="">Normal</option><option value="italic">Italic</option><option value="bold">Bold</option><option value="italic bold">Both</option></select></div>
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
