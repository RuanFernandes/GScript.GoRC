// SettingsWindowScreen is the content of the external Settings window (opened
// via App.OpenSettings, URL "/#settings"). Two sections: Coding (Monaco theme,
// font family, font size) and Chat (the existing color/log settings via
// ChatSettingsFields). Both persist to localStorage.
import {useCallback, useEffect, useRef, useState} from "react"
import {Events} from "@wailsio/runtime"
import {Check, ChevronDown, Code2, FolderDown, Languages, MessageSquareText} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {ThemeSelect} from "@/components/features/settings/ThemeSelect"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {ChatSettingsFields} from "@/components/features/chat/ChatSettingsFields"
import {useChatSettings} from "@/hooks/useChatSettings"
import {useCodingSettings} from "@/hooks/useCodingSettings"
import {rcService} from "@/services/rcService"
import type {ChatSettings, FileBrowserConfig} from "@/types"
import {useLanguage, type Language} from "@/hooks/useLanguage"
import {CustomThemeDialog, NewThemeButton, ThemePreview} from "@/components/features/settings/ThemePreview"
import type {CustomTheme} from "@/types"

export function SettingsWindowScreen() {
  const coding = useCodingSettings()
  const chat = useChatSettings()
  const {language, setLanguage, t} = useLanguage()

  return (
    <div className="bg-background flex h-svh flex-col">
      <Tabs defaultValue="coding" orientation="vertical" className="flex min-h-0 flex-1 flex-row gap-0">
        <TabsList aria-label="Settings sections" className="h-auto w-48 shrink-0 flex-col items-stretch justify-start gap-1 rounded-none border-b-0 border-r bg-muted/20 p-3">
          <TabsTrigger value="coding" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <Code2 />{t("settings.coding")}
          </TabsTrigger>
          <TabsTrigger value="chat" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <MessageSquareText />{t("settings.chat")}
          </TabsTrigger>
          <TabsTrigger value="files" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <FolderDown />{t("settings.files")}
          </TabsTrigger>
          <TabsTrigger value="language" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <Languages />{t("settings.language")}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="coding" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <CodingSection
            theme={coding.settings.theme}
            fontFamily={coding.settings.fontFamily}
            fontSize={coding.settings.fontSize}
            onChange={coding.update}
            onReset={coding.reset}
            t={t}
          />
        </TabsContent>
        <TabsContent value="chat" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <ChatSection
            settings={chat.settings}
            onChange={chat.update}
            onReset={chat.reset}
            t={t}
          />
        </TabsContent>
        <TabsContent value="files" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <FilesSection t={t} />
        </TabsContent>
        <TabsContent value="language" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <LanguageSection language={language} onChange={setLanguage} t={t} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

// FilesSection configures the required downloads folder (without it, downloads
// in the File Browser are disabled). Persists via the backend and stays in sync
// across windows via the raw rc:fbConfig event.
function FilesSection({t}: {t: (key: string) => string}) {
  const [dir, setDir] = useState("")

  useEffect(() => {
    rcService
      .getFileBrowserConfig()
      .then((c) => setDir(c.downloadDir ?? ""))
      .catch(() => {})
    const off = Events.On("rc:fbConfig", (e: {data: string}) => {
      try {
        const c = JSON.parse(e.data) as FileBrowserConfig
        setDir(c.downloadDir ?? "")
      } catch {
        // ignore
      }
    })
    return () => {
      off()
    }
  }, [])

  const browse = async () => {
    const chosen = await rcService.chooseDirectory()
    if (chosen) {
      await rcService.setFileBrowserConfig(chosen)
      setDir(chosen)
    }
  }

  const clear = async () => {
    await rcService.setFileBrowserConfig("")
    setDir("")
  }

  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.files")} description={t("settings.filesDescription")} />
      <div className="overflow-hidden rounded-lg border bg-card/40">
        <div className="border-b px-4 py-3">
          <p className="text-sm font-medium">{t("settings.downloads")}</p>
          <p className="text-muted-foreground mt-1 text-xs">{t("settings.downloadsDescription")}</p>
        </div>
        <div className="grid gap-3 p-4">
          <Label htmlFor="dl-dir">{t("settings.downloadsFolder")}</Label>
          <Input id="dl-dir" value={dir} readOnly placeholder={`${t("settings.notSet")} — downloads disabled`} />
          <div className="flex gap-2">
            <Button variant="outline" onClick={browse}>{t("settings.browse")}</Button>
            <Button variant="ghost" onClick={clear} disabled={!dir}>{t("settings.clear")}</Button>
          </div>
        </div>
      </div>
    </div>
  )
}

function CodingSection({
  theme,
  fontFamily,
  fontSize,
  onChange,
  onReset,
  t,
}: {
  theme: string
  fontFamily: string
  fontSize: number
  onChange: (patch: {theme?: string; fontFamily?: string; fontSize?: number}) => void
  onReset: () => void
  t: (key: string) => string
}) {
  // Installed system fonts for the font autocomplete. Fetched once; if it fails
  // the input still works as free text.
  const [fonts, setFonts] = useState<string[]>([])
  useEffect(() => {
    rcService
      .listFonts()
      .then((list) => {
        if (list) setFonts(list)
      })
      .catch(() => {})
  }, [])

  // Active remote theme name (if any), so the Theme dropdown can show it.
  const [remoteName, setRemoteName] = useState<string>("")
  const [remoteDefinition, setRemoteDefinition] = useState<string | undefined>(undefined)
  const [customThemes, setCustomThemes] = useState<CustomTheme[]>([])
  const [themeDialog, setThemeDialog] = useState<{open: boolean; theme?: CustomTheme}>({open: false})
  useEffect(() => {
    rcService
      .getRemoteTheme()
      .then((rt) => {
        if (rt?.name) setRemoteName(rt.name)
        if (rt?.definition) setRemoteDefinition(rt.definition)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    rcService.getCustomThemes().then((themes) => setCustomThemes(themes ?? [])).catch(() => {})
  }, [])

  const activeCustom = customThemes.find((item) => item.key === theme)

  const activateRemote = useCallback(async (name: string, definition: string) => {
    await rcService.saveRemoteTheme(name, definition)
    setRemoteName(name)
    setRemoteDefinition(definition)
    onChange({theme: "remoteTheme"})
  }, [onChange])

  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.coding")} description={t("settings.codingDescription")} />
      <div className="overflow-hidden rounded-lg border bg-card/40">
        <div className="border-b px-4 py-3">
          <p className="text-sm font-medium">{t("settings.editorAppearance")}</p>
          <p className="text-muted-foreground mt-1 text-xs">{t("settings.editorAppearanceDescription")}</p>
        </div>
        <div className="grid gap-4 p-4">
          <ThemePreview
            key={`settings-preview-${theme}-${themeDialog.open ? "dialog-open" : "dialog-closed"}`}
            theme={theme}
            definition={activeCustom?.definition ?? (theme === "remoteTheme" ? remoteDefinition : undefined)}
            fontFamily={fontFamily}
            fontSize={fontSize}
          />
          <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:items-center">
            <Label htmlFor="theme">{t("settings.theme")}</Label>
            <ThemeSelect
              value={theme}
              onChange={(k) => onChange({theme: k})}
              customOptions={customThemes.map((item) => ({key: item.key, label: `${item.name} · ${t("settings.customTheme")}`}))}
              extraOption={
                theme === "remoteTheme" && remoteName
                  ? {key: "remoteTheme", label: `Remote: ${remoteName}`}
                  : undefined
              }
            />
            <div className="flex flex-wrap gap-2">
              <NewThemeButton onClick={() => setThemeDialog({open: true})} />
              {activeCustom && <Button variant="ghost" size="sm" onClick={() => setThemeDialog({open: true, theme: activeCustom})}>{t("settings.editTheme")}</Button>}
            </div>
          </div>

      <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:items-start">
        <Label htmlFor="remote" className="pt-2">
          {t("settings.remoteTheme")}
        </Label>
        <RemoteThemePicker onActivate={activateRemote} />
      </div>

      <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:items-center">
        <Label htmlFor="font">{t("settings.fontFamily")}</Label>
        <Input
          id="font"
          list="system-fonts"
          value={fontFamily}
          onChange={(e) => onChange({fontFamily: e.target.value})}
          placeholder={t("settings.fontPlaceholder")}
        />
        <datalist id="system-fonts">
          {fonts.map((f) => (
            <option key={f} value={f} />
          ))}
        </datalist>
      </div>

      <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:items-center">
        <Label htmlFor="size">{t("settings.fontSize")}</Label>
        <div className="flex items-center gap-3">
          <input
            id="size"
            type="range"
            min={8}
            max={32}
            value={fontSize}
            onChange={(e) => onChange({fontSize: Number(e.target.value)})}
            className="flex-1"
          />
          <span className="text-muted-foreground w-8 text-sm tabular-nums">{fontSize}</span>
        </div>
      </div>

        </div>
      </div>
      <div className="flex justify-end">
        <Button variant="ghost" onClick={onReset}>{t("settings.reset")}</Button>
      </div>
      <CustomThemeDialog
        open={themeDialog.open}
        theme={themeDialog.theme}
        onClose={() => setThemeDialog({open: false})}
        onSaved={(saved) => {
          setCustomThemes((current) => [...current.filter((item) => item.key !== saved.key), saved])
          onChange({theme: saved.key})
        }}
      />
    </div>
  )
}

const THEMELIST_URL =
  "https://cdn.jsdelivr.net/gh/brijeshb42/monaco-themes@master/themes/themelist.json"
const THEMES_BASE =
  "https://cdn.jsdelivr.net/gh/brijeshb42/monaco-themes@master/themes/"

// RemoteThemePicker is an autocomplete over the brijeshb42/monaco-themes gallery
// (~80 themes). On pick it fetches the theme JSON, caches it via the backend
// (so it works offline afterwards), and activates it.
function RemoteThemePicker({
  onActivate,
}: {
  onActivate: (name: string, definition: string) => void | Promise<void>
}) {
  const {t} = useLanguage()
  const [names, setNames] = useState<string[]>([])
  const [value, setValue] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    fetch(THEMELIST_URL)
      .then((r) => r.json() as Promise<Record<string, string>>)
      .then((map) => setNames(Object.values(map).sort((a, b) => a.localeCompare(b))))
      .catch(() => setError(t("settings.galleryError")))
  }, [])

  const apply = useCallback(async () => {
    const name = value.trim()
    if (!name) return
    setBusy(true)
    setError("")
    try {
      // themelist.json maps {slug: displayName}; the theme files are named by
      // displayName (e.g. "Blackboard.json", with spaces), so fetch directly.
      const definition = await fetch(THEMES_BASE + encodeURIComponent(name) + ".json").then((r) => {
        if (!r.ok) throw new Error(String(r.status))
        return r.text()
      })
      await onActivate(name, definition)
    } catch {
      setError(`Couldn't fetch "${name}" (online? exact name?)`)
    } finally {
      setBusy(false)
    }
  }, [value, onActivate])

  return (
    <div className="grid gap-1.5">
      <div className="flex gap-2">
        <Input
          list="remote-themes"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") apply()
          }}
          placeholder={t("settings.gallerySearch")}
        />
        <datalist id="remote-themes">
          {names.map((n) => (
            <option key={n} value={n} />
          ))}
        </datalist>
        <Button onClick={apply} disabled={busy || !value.trim()}>
          {busy ? "…" : t("settings.apply")}
        </Button>
      </div>
      {error && <p className="text-destructive text-xs">{error}</p>}
      {names.length > 0 && (
        <p className="text-muted-foreground text-xs">{names.length} themes in gallery.</p>
      )}
    </div>
  )
}

function ChatSection({
  settings,
  onChange,
  onReset,
  t,
}: {
  settings: ChatSettings
  onChange: (patch: Partial<ChatSettings>) => void
  onReset: () => void
  t: (key: string) => string
}) {
  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.chat")} description={t("settings.chatDescription")} />
      <div className="overflow-hidden rounded-lg border bg-card/40 p-4">
      <ChatSettingsFields
        settings={settings}
        onChange={onChange}
        onBrowse={async () => {
          const dir = await rcService.chooseDirectory()
          if (dir) onChange({logDir: dir})
        }}
        onBrowsePm={async () => {
          const dir = await rcService.chooseDirectory()
          if (dir) onChange({pmLogDir: dir})
        }}
      />
      </div>
      <div className="flex justify-end">
        <Button variant="ghost" onClick={onReset}>{t("settings.reset")}</Button>
      </div>
    </div>
  )
}

function LanguageSection({language, onChange, t}: {language: Language; onChange: (language: Language) => void; t: (key: string) => string}) {
  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.language")} description={t("language.description")} />
      <div className="border-border bg-card/40 rounded-lg border p-4">
        <div className="grid gap-2 sm:grid-cols-[180px_1fr] sm:items-center">
          <Label htmlFor="language">{t("language.title")}</Label>
          <LanguagePicker language={language} onChange={onChange} />
        </div>
        <p className="text-muted-foreground mt-3 text-xs">{t("language.saved")}</p>
      </div>
    </div>
  )
}

const languageOptions: Array<{value: Language; label: string; region: string}> = [
  {value: "pt-BR", label: "Português (Brasil)", region: "PT-BR"},
  {value: "en", label: "English", region: "EN-US"},
  {value: "es", label: "Español", region: "ES-ES"},
]

function LanguagePicker({language, onChange}: {language: Language; onChange: (language: Language) => void}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const selected = languageOptions.find((option) => option.value === language) ?? languageOptions[0]

  useEffect(() => {
    if (!open) return
    const closeOnOutsideClick = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", closeOnOutsideClick)
    document.addEventListener("keydown", closeOnEscape)
    return () => {
      document.removeEventListener("mousedown", closeOnOutsideClick)
      document.removeEventListener("keydown", closeOnEscape)
    }
  }, [open])

  const choose = (value: Language) => {
    onChange(value)
    setOpen(false)
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        id="language"
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="border-input bg-input/30 hover:bg-input/50 flex h-10 w-full items-center justify-between rounded-md border px-3 text-left text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="flex min-w-0 items-center gap-3">
          <span className="bg-primary/12 text-primary flex size-6 shrink-0 items-center justify-center rounded text-[10px] font-bold tracking-wide">{selected.region.slice(0, 2)}</span>
          <span className="truncate">{selected.label}</span>
        </span>
        <ChevronDown className={`text-muted-foreground size-4 shrink-0 transition-transform ${open ? "rotate-180" : ""}`} />
      </button>
      {open && (
        <div role="listbox" aria-label="Language options" className="border-border bg-popover text-popover-foreground absolute inset-x-0 top-[calc(100%+6px)] z-50 overflow-hidden rounded-md border p-1 shadow-lg">
          {languageOptions.map((option) => {
            const active = option.value === language
            return (
              <button
                key={option.value}
                type="button"
                role="option"
                aria-selected={active}
                onClick={() => choose(option.value)}
                className={`flex w-full items-center justify-between rounded px-2.5 py-2 text-left text-sm transition-colors ${active ? "bg-accent text-accent-foreground" : "hover:bg-accent/70"}`}
              >
                <span className="flex items-center gap-3">
                  <span className={`flex size-6 items-center justify-center rounded text-[10px] font-bold tracking-wide ${active ? "bg-primary/15 text-primary" : "bg-muted text-muted-foreground"}`}>{option.region.slice(0, 2)}</span>
                  <span>
                    <span className="block">{option.label}</span>
                    <span className="text-muted-foreground block text-[11px]">{option.region}</span>
                  </span>
                </span>
                {active && <Check className="text-primary size-4" />}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

function SectionHeading({title, description}: {title: string; description: string}) {
  return (
    <div>
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      <p className="text-muted-foreground mt-1 text-sm">{description}</p>
    </div>
  )
}
