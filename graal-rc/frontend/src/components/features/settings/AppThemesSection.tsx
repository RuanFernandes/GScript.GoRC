import {useMemo, useState, type CSSProperties} from "react"
import {Check, Moon, Palette, Plus, RotateCcw, Save, Sun, Trash2, X} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {useAppTheme} from "@/hooks/useAppTheme"
import {
  APP_THEME_GROUPS,
  APP_THEME_FIELDS,
  BUILT_IN_APP_THEMES,
  isValidCssColor,
  toColorInputValue,
  type AppThemeColorKey,
  type AppThemeMode,
  type EditableAppTheme,
} from "@/lib/appThemes"

type Translator = (key: string, vars?: Record<string, string | number>) => string

export function AppThemesSection({t}: {t: Translator}) {
  const {activeKey, activeTheme, themes, loaded, select, save, remove, reset} = useAppTheme()
  const [draft, setDraft] = useState<EditableAppTheme | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  const customThemes = useMemo(
    () => themes.filter((theme) => !BUILT_IN_APP_THEMES.some((builtIn) => builtIn.key === theme.key)),
    [themes],
  )
  const activeIsBuiltIn = BUILT_IN_APP_THEMES.some((theme) => theme.key === activeTheme.key)

  const startEdit = (theme: EditableAppTheme, copy: boolean) => {
    const key = copy ? nextCustomKey(theme.name, themes.map((item) => item.key)) : theme.key
    setDraft({
      key,
      name: copy ? `${theme.name} ${t("settings.themeCopySuffix")}` : theme.name,
      mode: theme.mode,
      colors: {...theme.colors},
    })
    setError("")
  }

  const startNew = () => {
    const key = nextCustomKey(t("settings.newTheme"), themes.map((item) => item.key))
    setDraft({
      key,
      name: t("settings.newTheme"),
      mode: activeTheme.mode,
      colors: {...activeTheme.colors},
    })
    setError("")
  }

  const selectTheme = async (key: string) => {
    if (busy || key === activeKey) return
    setBusy(true)
    setError("")
    try {
      await select(key)
      setDraft(null)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  const saveDraft = async () => {
    if (!draft) return
    const validationError = validateDraft(draft, themes, activeKey, t)
    if (validationError) {
      setError(validationError)
      return
    }
    setBusy(true)
    setError("")
    try {
      await save(draft)
      setDraft(null)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  const deleteActive = async () => {
    if (activeIsBuiltIn || busy) return
    if (typeof window !== "undefined" && !window.confirm(t("settings.themeDeleteConfirm", {name: activeTheme.name}))) return
    setBusy(true)
    setError("")
    try {
      await remove(activeTheme.key)
      setDraft(null)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  const resetTheme = async () => {
    if (busy) return
    setBusy(true)
    setError("")
    try {
      await reset()
      setDraft(null)
    } catch (cause) {
      setError(errorMessage(cause))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mx-auto grid max-w-5xl gap-5">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="text-primary mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.16em]"><Palette className="size-4" />{t("settings.themes")}</div>
          <h2 className="text-lg font-semibold tracking-tight">{t("settings.themesTitle")}</h2>
          <p className="text-muted-foreground mt-1 max-w-2xl text-sm">{t("settings.themesDescription")}</p>
        </div>
        <Button onClick={startNew} disabled={!loaded || busy}><Plus />{t("settings.newTheme")}</Button>
      </div>

      <div className="grid min-w-0 gap-4 xl:grid-cols-[minmax(14rem,0.75fr)_minmax(0,1.6fr)]">
        <section className="bg-card/40 min-w-0 overflow-hidden rounded-lg border">
          <div className="border-b px-4 py-3">
            <p className="text-sm font-medium">{t("settings.chooseTheme")}</p>
            <p className="text-muted-foreground mt-1 text-xs">{t("settings.chooseThemeDescription")}</p>
          </div>
          <div className="grid gap-1.5 p-2">
            {themes.map((theme) => (
              <ThemeOption key={theme.key} theme={theme} active={theme.key === activeKey} disabled={busy} onChoose={() => void selectTheme(theme.key)} />
            ))}
          </div>
          {customThemes.length === 0 && <p className="text-muted-foreground border-t px-4 py-3 text-xs">{t("settings.noCustomThemes")}</p>}
        </section>

        <section className="bg-card/40 min-w-0 overflow-hidden rounded-lg border">
          <div className="flex flex-wrap items-start justify-between gap-3 border-b px-4 py-3">
            <div>
              <p className="text-sm font-medium">{activeTheme.name}</p>
              <p className="text-muted-foreground mt-1 text-xs">{activeIsBuiltIn ? t("settings.builtInTheme") : t("settings.customTheme")}</p>
            </div>
            <div className="flex flex-wrap gap-2">
              {draft ? (
                <>
                  <Button variant="ghost" size="sm" onClick={() => {setDraft(null); setError("")}} disabled={busy}><X />{t("common.cancel")}</Button>
                  <Button size="sm" onClick={() => void saveDraft()} disabled={busy}><Save />{busy ? t("settings.savingTheme") : t("settings.saveTheme")}</Button>
                </>
              ) : (
                <>
                  <Button variant="outline" size="sm" onClick={() => startEdit(activeTheme, activeIsBuiltIn)} disabled={busy}><Palette />{activeIsBuiltIn ? t("settings.customizeTheme") : t("settings.editTheme")}</Button>
                  {!activeIsBuiltIn && <Button variant="ghost" size="sm" onClick={() => void deleteActive()} disabled={busy}><Trash2 />{t("common.delete")}</Button>}
                </>
              )}
            </div>
          </div>

          <div className="grid gap-4 p-4">
            <ThemePreview theme={draft ?? activeTheme} />
            {draft ? <ThemeEditor draft={draft} onChange={setDraft} t={t} /> : <p className="text-muted-foreground text-xs">{t("settings.themeEditHint")}</p>}
            {error && <p className="text-destructive text-xs">{error}</p>}
          </div>
        </section>
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="text-muted-foreground text-xs">{t("settings.themeScope")}</p>
        <Button variant="ghost" size="sm" onClick={() => void resetTheme()} disabled={busy || activeKey === "default-dark"}><RotateCcw />{t("settings.reset")}</Button>
      </div>
    </div>
  )
}

function ThemeOption({theme, active, disabled, onChoose}: {theme: EditableAppTheme; active: boolean; disabled: boolean; onChoose: () => void}) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onChoose}
      disabled={disabled}
      className={`group flex min-w-0 items-center gap-3 rounded-md border px-3 py-2 text-left transition-colors ${active ? "border-primary bg-accent text-accent-foreground" : "border-transparent hover:border-border hover:bg-accent/60"}`}
    >
      <div className="flex shrink-0 gap-0.5 overflow-hidden rounded border border-black/15 p-0.5 dark:border-white/15">
        {["background", "primary", "accent", "card", "foreground"].map((key) => <span key={key} className="size-3.5" style={{backgroundColor: theme.colors[key as AppThemeColorKey]}} />)}
      </div>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-sm font-medium">{theme.name}</span>
        <span className="text-muted-foreground flex items-center gap-1 text-[11px]">{theme.mode === "dark" ? <Moon className="size-3" /> : <Sun className="size-3" />}{theme.mode}</span>
      </span>
      {active && <Check className="text-primary size-4 shrink-0" />}
    </button>
  )
}

function ThemePreview({theme}: {theme: EditableAppTheme}) {
  const style = {} as CSSProperties & Record<string, string>
  for (const field of APP_THEME_FIELDS) style[field.cssVariable] = theme.colors[field.key]
  return (
    <div style={style} className="grid gap-3 rounded-md border border-border bg-background p-4 text-foreground">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <p className="text-sm font-medium">Graal Remote Control</p>
          <p className="text-xs text-muted-foreground">{theme.name}</p>
        </div>
        <span className="rounded-full border border-border bg-muted px-2 py-1 text-[10px] font-medium text-muted-foreground">{theme.mode}</span>
      </div>
      <div className="grid gap-2 sm:grid-cols-[1fr_auto_auto] sm:items-center">
        <div className="rounded border border-border bg-card px-3 py-2 text-xs text-card-foreground">Preview of surfaces, text and controls</div>
        <button type="button" className="rounded-md bg-primary px-3 py-2 text-xs font-medium text-primary-foreground">Primary</button>
        <button type="button" className="rounded-md border border-border bg-secondary px-3 py-2 text-xs font-medium text-secondary-foreground">Secondary</button>
      </div>
      <div className="flex flex-wrap gap-1.5">
        {(["chart1", "chart2", "chart3", "chart4", "chart5"] as AppThemeColorKey[]).map((key) => <span key={key} className="h-1.5 flex-1 rounded-full" style={{backgroundColor: theme.colors[key]}} />)}
      </div>
    </div>
  )
}

function ThemeEditor({draft, onChange, t}: {draft: EditableAppTheme; onChange: (theme: EditableAppTheme) => void; t: Translator}) {
  const update = (patch: Partial<EditableAppTheme>) => onChange({...draft, ...patch})
  const updateColor = (key: AppThemeColorKey, value: string) => onChange({...draft, colors: {...draft.colors, [key]: value}})

  return (
    <div className="grid gap-4 rounded-md border border-dashed border-border p-3">
      <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
        <div className="grid gap-1.5">
          <Label htmlFor="app-theme-name">{t("settings.themeName")}</Label>
          <Input id="app-theme-name" value={draft.name} onChange={(event) => update({name: event.target.value})} maxLength={80} />
        </div>
        <div className="grid gap-1.5">
          <Label htmlFor="app-theme-key">{t("settings.themeKey")}</Label>
          <Input id="app-theme-key" value={draft.key} onChange={(event) => update({key: event.target.value.toLowerCase()})} maxLength={64} />
        </div>
        <div className="grid gap-1.5">
          <span className="text-sm font-medium">{t("settings.themeMode")}</span>
          <div className="flex h-9 rounded-md border border-input p-0.5">
            {(["dark", "light"] as AppThemeMode[]).map((mode) => (
              <button key={mode} type="button" aria-pressed={draft.mode === mode} onClick={() => update({mode})} className={`flex items-center gap-1 rounded px-2 text-xs ${draft.mode === mode ? "bg-accent text-accent-foreground" : "text-muted-foreground"}`}>
                {mode === "dark" ? <Moon className="size-3" /> : <Sun className="size-3" />}{mode}
              </button>
            ))}
          </div>
        </div>
      </div>
      <div className="grid gap-4 md:grid-cols-2">
        {APP_THEME_GROUPS.map((group) => (
          <div key={group.key} className="grid content-start gap-2 rounded-md border border-border/70 p-3">
            <div>
              <p className="text-sm font-medium">{t(group.labelKey)}</p>
              <p className="text-muted-foreground mt-0.5 text-[11px]">{t("settings.themeColorGroupDescription")}</p>
            </div>
            <div className="grid gap-2">
              {group.fields.map((field) => <ThemeColorField key={field.key} field={field} value={draft.colors[field.key]} onChange={(value) => updateColor(field.key, value)} t={t} />)}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function ThemeColorField({field, value, onChange, t}: {field: {key: AppThemeColorKey; labelKey: string}; value: string; onChange: (value: string) => void; t: Translator}) {
  const valid = isValidCssColor(value)
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
      <Label htmlFor={`app-theme-${field.key}`} className="min-w-0 truncate text-xs">{t(field.labelKey)}</Label>
      <div className="flex items-center gap-1.5">
        <input aria-label={t(field.labelKey)} type="color" value={toColorInputValue(value)} onChange={(event) => onChange(event.target.value)} className="size-8 cursor-pointer rounded border border-input bg-transparent p-0.5" />
        <Input id={`app-theme-${field.key}`} value={value} onChange={(event) => onChange(event.target.value)} className={`h-8 w-28 font-mono text-xs ${valid ? "" : "border-destructive"}`} spellCheck={false} />
      </div>
    </div>
  )
}

function nextCustomKey(name: string, existing: string[]): string {
  const base = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "") || "theme"
  const prefix = base.startsWith("custom-") ? base : `custom-${base}`
  if (!existing.includes(prefix)) return prefix
  let index = 2
  while (existing.includes(`${prefix}-${index}`)) index += 1
  return `${prefix}-${index}`
}

function validateDraft(draft: EditableAppTheme, themes: EditableAppTheme[], existingKey: string, t: Translator): string {
  if (!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(draft.key)) return t("settings.themeKeyInvalid")
  if (!draft.name.trim()) return t("settings.themeNameRequired")
  if (BUILT_IN_APP_THEMES.some((theme) => theme.key === draft.key)) return t("settings.themeKeyReserved")
  const duplicate = themes.find((theme) => theme.key === draft.key && theme.key !== existingKey)
  if (duplicate) return t("settings.themeKeyExists")
  const invalidField = APP_THEME_FIELDS.find((field) => !isValidCssColor(draft.colors[field.key]))
  return invalidField ? t("settings.themeColorInvalid", {name: t(invalidField.labelKey)}) : ""
}

function errorMessage(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}
