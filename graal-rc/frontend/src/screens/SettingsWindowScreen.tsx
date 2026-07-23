// SettingsWindowScreen is the content of the external Settings window (opened
// via App.OpenSettings, URL "/#settings"). Two sections: Coding (Monaco theme,
// font family, font size) and Chat (the existing color/log settings via
// ChatSettingsFields). Both persist to localStorage.
import {useCallback, useEffect, useState} from "react"
import {Events} from "@wailsio/runtime"

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

export function SettingsWindowScreen() {
  const coding = useCodingSettings()
  const chat = useChatSettings()

  return (
    <div className="bg-background flex h-svh flex-col">
      <header className="border-b px-4 py-2.5">
        <h1 className="text-base font-semibold">Settings</h1>
      </header>
      <Tabs defaultValue="coding" className="flex min-h-0 flex-1 flex-col p-3">
        <TabsList>
          <TabsTrigger value="coding">Coding</TabsTrigger>
          <TabsTrigger value="chat">Chat</TabsTrigger>
          <TabsTrigger value="files">Files</TabsTrigger>
        </TabsList>
        <TabsContent value="coding" className="mt-4 min-h-0 flex-1 overflow-y-auto">
          <CodingSection
            theme={coding.settings.theme}
            fontFamily={coding.settings.fontFamily}
            fontSize={coding.settings.fontSize}
            onChange={coding.update}
            onReset={coding.reset}
          />
        </TabsContent>
        <TabsContent value="chat" className="mt-4 min-h-0 flex-1 overflow-y-auto">
          <ChatSection
            settings={chat.settings}
            onChange={chat.update}
            onReset={chat.reset}
          />
        </TabsContent>
        <TabsContent value="files" className="mt-4 min-h-0 flex-1 overflow-y-auto">
          <FilesSection />
        </TabsContent>
      </Tabs>
    </div>
  )
}

// FilesSection configures the required downloads folder (without it, downloads
// in the File Browser are disabled). Persists via the backend and stays in sync
// across windows via the raw rc:fbConfig event.
function FilesSection() {
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
    <div className="grid gap-4">
      <div className="grid grid-cols-[140px_1fr] items-center gap-2">
        <Label htmlFor="dl-dir">Downloads folder</Label>
        <Input id="dl-dir" value={dir} readOnly placeholder="Not set — downloads disabled" />
      </div>
      <p className="text-muted-foreground text-xs">
        Downloaded files are saved here. Downloads are blocked until a folder is set.
      </p>
      <div className="flex gap-2">
        <Button variant="outline" onClick={browse}>
          Browse…
        </Button>
        <Button variant="ghost" onClick={clear} disabled={!dir}>
          Clear
        </Button>
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
}: {
  theme: string
  fontFamily: string
  fontSize: number
  onChange: (patch: {theme?: string; fontFamily?: string; fontSize?: number}) => void
  onReset: () => void
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
  useEffect(() => {
    rcService
      .getRemoteTheme()
      .then((rt) => {
        if (rt?.name) setRemoteName(rt.name)
      })
      .catch(() => {})
  }, [])

  const activateRemote = useCallback(async (name: string, definition: string) => {
    await rcService.saveRemoteTheme(name, definition)
    setRemoteName(name)
    onChange({theme: "remoteTheme"})
  }, [onChange])

  return (
    <div className="grid gap-4">
      <div className="grid grid-cols-[140px_1fr] items-center gap-2">
        <Label htmlFor="theme">Theme</Label>
        <ThemeSelect
          value={theme}
          onChange={(k) => onChange({theme: k})}
          extraOption={
            theme === "remoteTheme" && remoteName
              ? {key: "remoteTheme", label: `Remote: ${remoteName}`}
              : undefined
          }
        />
      </div>

      <div className="grid grid-cols-[140px_1fr] items-start gap-2">
        <Label htmlFor="remote" className="pt-2">
          Remote theme
        </Label>
        <RemoteThemePicker onActivate={activateRemote} />
      </div>

      <div className="grid grid-cols-[140px_1fr] items-center gap-2">
        <Label htmlFor="font">Font family</Label>
        <Input
          id="font"
          list="system-fonts"
          value={fontFamily}
          onChange={(e) => onChange({fontFamily: e.target.value})}
          placeholder="Pick or type a font…"
        />
        <datalist id="system-fonts">
          {fonts.map((f) => (
            <option key={f} value={f} />
          ))}
        </datalist>
      </div>

      <div className="grid grid-cols-[140px_1fr] items-center gap-2">
        <Label htmlFor="size">Font size</Label>
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

      <div>
        <Button variant="ghost" onClick={onReset}>
          Reset defaults
        </Button>
      </div>
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
  const [names, setNames] = useState<string[]>([])
  const [value, setValue] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    fetch(THEMELIST_URL)
      .then((r) => r.json() as Promise<Record<string, string>>)
      .then((map) => setNames(Object.values(map).sort((a, b) => a.localeCompare(b))))
      .catch(() => setError("Couldn't load theme gallery (online?)"))
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
          placeholder="Search gallery (e.g. Solarized, Dracula)…"
        />
        <datalist id="remote-themes">
          {names.map((n) => (
            <option key={n} value={n} />
          ))}
        </datalist>
        <Button onClick={apply} disabled={busy || !value.trim()}>
          {busy ? "…" : "Apply"}
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
}: {
  settings: ChatSettings
  onChange: (patch: Partial<ChatSettings>) => void
  onReset: () => void
}) {
  return (
    <div className="grid gap-4">
      <ChatSettingsFields
        settings={settings}
        onChange={onChange}
        onBrowse={async () => {
          const dir = await rcService.chooseDirectory()
          if (dir) onChange({logDir: dir})
        }}
      />
      <div>
        <Button variant="ghost" onClick={onReset}>
          Reset defaults
        </Button>
      </div>
    </div>
  )
}
