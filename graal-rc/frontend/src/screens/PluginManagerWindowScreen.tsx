import {useCallback, useEffect, useMemo, useRef, useState} from "react"
import {Events} from "@wailsio/runtime"
import Editor, {type BeforeMount} from "@monaco-editor/react"
import {AlertCircle, AlertTriangle, BookOpen, CheckCircle2, Code2, Download, Eye, FileJson, FolderOpen, Hammer, Loader2, Play, Plus, Power, Puzzle, RefreshCw, Save, ShieldCheck, Terminal, Trash2, X} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {rcService} from "@/services/rcService"
import {useCodingSettings} from "@/hooks/useCodingSettings"
import {useLanguage} from "@/hooks/useLanguage"
import type {PluginBuildResult, PluginInfo, PluginLogEntry} from "@/plugins/types"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"

function pluginLanguageForFile(path: string): string {
  const lower = path.toLowerCase()
  if (lower.endsWith(".json")) return "json"
  if (lower.endsWith(".ts") || lower.endsWith(".tsx")) return "typescript"
  if (lower.endsWith(".js") || lower.endsWith(".jsx") || lower.endsWith(".mjs") || lower.endsWith(".cjs")) return "javascript"
  if (lower.endsWith(".css")) return "css"
  if (lower.endsWith(".scss")) return "scss"
  if (lower.endsWith(".html")) return "html"
  if (lower.endsWith(".md") || lower.endsWith(".markdown")) return "markdown"
  if (lower.endsWith(".xml")) return "xml"
  return "plaintext"
}

function isPluginEditorFile(path: string, main = ""): boolean {
  const lower = path.toLowerCase().replaceAll("\\", "/")
  const normalizedMain = main.toLowerCase().replaceAll("\\", "/")
  if (lower === normalizedMain || lower.startsWith("dist/") || lower.endsWith(".d.ts")) return false
  return lower === "manifest.json" || /\.(?:json|ts|tsx|js|jsx|mjs|cjs|css|scss|html|md|markdown|xml)$/.test(lower)
}

function stripPluginDeclarationReference(content: string): string {
  return content.replace(/^\s*\/\/\/\s*<reference\s+path=["']\.\/plugin\.d\.ts["']\s*\/?>\s*\r?\n?/m, "")
}

function pluginEditorTheme(theme: string): "vs" | "vs-dark" | "hc-black" | "hc-light" {
  // Coding themes are tuned for the GraalScript2 grammar. Plugin source uses
  // Monaco's native JavaScript/TypeScript tokenization, so custom GS2 themes
  // must never be registered here. Keep only Monaco's neutral built-in themes.
  if (theme === "vs" || theme === "hc-black" || theme === "hc-light") return theme
  return "vs-dark"
}

type PluginMonaco = Parameters<BeforeMount>[0]

function configurePluginLanguageService(monaco: PluginMonaco, declarations: string): void {
  // Keep the JavaScript/TypeScript worker in sync with models created by the
  // file tabs. Without eager sync Monaco falls back to Monarch-only coloring:
  // keywords and strings are colored, but identifiers/functions remain plain.
  const typescript = (monaco.languages as typeof monaco.languages & {
    typescript?: {
      javascriptDefaults?: {
        setEagerModelSync(enabled: boolean): void
        addExtraLib?(content: string, filePath?: string): void
        setCompilerOptions?(options: {target?: number; lib?: string[]; noUnusedParameters?: boolean; noUnusedLocals?: boolean}): void
      }
      typescriptDefaults?: {
        setEagerModelSync(enabled: boolean): void
        addExtraLib?(content: string, filePath?: string): void
        setCompilerOptions?(options: {target?: number; lib?: string[]; noUnusedParameters?: boolean; noUnusedLocals?: boolean}): void
      }
    }
  }).typescript
  typescript?.javascriptDefaults?.setEagerModelSync(true)
  typescript?.typescriptDefaults?.setEagerModelSync(true)
  // The browser's deprecated DOM `Plugin` global otherwise wins over the
  // GoRC SDK declaration. Plugin source is capability-based and does not
  // expose arbitrary DOM types, so use the standard ES library only.
  // Monaco resolves explicit libraries by their bundled file names. Using the
  // short TypeScript CLI names (for example "es2022") silently drops Promise.
  // This ES-only library keeps the DOM Plugin global out while preserving the
  // standard runtime types used by async plugin code.
  const compilerOptions = {target: 99, lib: ["lib.es2022.d.ts"], noUnusedParameters: false, noUnusedLocals: false}
  typescript?.javascriptDefaults?.setCompilerOptions?.(compilerOptions)
  typescript?.typescriptDefaults?.setCompilerOptions?.(compilerOptions)
  if (declarations) {
    let source = declarations.replace(/body\?: string/g, "body?: PluginHttpBody")
    if (!source.includes("type PluginHttpBody")) source = `type PluginHttpBody = string | Record<string, unknown> | unknown[] | number | boolean | null\n\n${source}`
    if (!source.includes("declare const console")) source += `\n\ndeclare const console: { log(...data: unknown[]): void; info(...data: unknown[]): void; warn(...data: unknown[]): void; error(...data: unknown[]): void }`
    for (const path of ["file:///gorc-plugin/plugin.d.ts", "file:///gorc-plugin/src/plugin.d.ts"]) {
      typescript?.javascriptDefaults?.addExtraLib?.(source, path)
      typescript?.typescriptDefaults?.addExtraLib?.(source, path)
    }
  }
}

export function PluginManagerWindowScreen() {
  const {t} = useLanguage()
  const [plugins, setPlugins] = useState<PluginInfo[]>([])
  const [directory, setDirectory] = useState("")
  const [busy, setBusy] = useState<string | null>(null)
  const [showCreate, setShowCreate] = useState(false)
  const [name, setName] = useState("")
  const [error, setError] = useState("")
  const [selectedId, setSelectedId] = useState("")

  const refresh = useCallback(async () => {
    const [list, path] = await Promise.all([rcService.getPlugins(), rcService.getPluginDirectory()])
    setPlugins(list ?? [])
    setDirectory(path ?? "")
    setSelectedId(current => current || list?.[0]?.manifest.id || "")
  }, [])

  useEffect(() => {
    void refresh()
    const off = Events.On("plugin:list", () => void refresh())
    return off
  }, [refresh])

  const toggle = async (plugin: PluginInfo) => {
    setBusy(plugin.manifest.id)
    try { await rcService.setPluginEnabled(plugin.manifest.id, !plugin.enabled); await refresh() } finally { setBusy(null) }
  }

  const approve = async (plugin: PluginInfo) => {
    setBusy(plugin.manifest.id)
    try { await rcService.approvePluginPermissions(plugin.manifest.id, plugin.manifest.permissions ?? {}); await refresh() } finally { setBusy(null) }
  }

  const create = async () => {
    if (!name.trim()) return
    setBusy("create")
    setError("")
    try {
      await rcService.createPluginTemplate(name.trim())
      setName("")
      setShowCreate(false)
      await refresh()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
    } finally { setBusy(null) }
  }

  return (
    <div className="bg-background flex h-svh min-h-0 flex-col">
      <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b px-5 py-4 sm:px-7">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border bg-primary/10 text-primary"><Puzzle className="h-4 w-4" /></div>
          <div className="min-w-0">
            <h1 className="truncate text-lg font-semibold">{t("settings.pluginManagerTitle")}</h1>
            <p className="text-muted-foreground truncate text-xs">{directory || t("settings.pluginDirectoryUnavailable")}</p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => void rcService.openPluginDocumentation()}><BookOpen />{t("settings.pluginDocumentation")}</Button>
          <Button variant="outline" size="sm" onClick={() => void rcService.openPluginsFolder()}><FolderOpen />{t("settings.openPluginsFolder")}</Button>
          <Button variant="outline" size="sm" onClick={() => setShowCreate(value => !value)}><Plus />{t("settings.newPlugin")}</Button>
          <Button variant="ghost" size="icon" aria-label={t("settings.refreshPlugins")} onClick={() => void rcService.refreshPlugins().then(refresh)}><RefreshCw /></Button>
        </div>
      </header>

      {showCreate && (
        <div className="border-b bg-muted/20 px-5 py-4 sm:px-7">
          <div className="mx-auto flex max-w-3xl flex-col gap-3 sm:flex-row sm:items-end">
            <div className="grid flex-1 gap-1.5">
              <label htmlFor="new-plugin-name" className="text-xs font-medium">{t("settings.pluginName")}</label>
              <Input id="new-plugin-name" autoFocus value={name} onChange={event => setName(event.target.value)} placeholder={t("settings.pluginNamePlaceholder")} onKeyDown={event => { if (event.key === "Enter") void create() }} />
            </div>
            <Button onClick={() => void create()} disabled={busy === "create" || !name.trim()}><Plus />{t("settings.createPlugin")}</Button>
            <Button variant="ghost" size="icon" aria-label={t("common.cancel")} onClick={() => { setShowCreate(false); setError("") }}><X /></Button>
          </div>
          {error && <p className="mx-auto mt-2 max-w-3xl text-xs text-destructive">{error}</p>}
          <p className="text-muted-foreground mx-auto mt-2 max-w-3xl text-xs">{t("settings.createPluginDescription")}</p>
        </div>
      )}

      <main className="grid min-h-0 flex-1 lg:grid-cols-[minmax(280px,360px)_minmax(0,1fr)]">
        <aside className="min-h-0 overflow-y-auto border-b lg:border-b-0 lg:border-r">
          <div className="flex items-center justify-between border-b px-5 py-3">
            <div><h2 className="text-sm font-medium">{t("settings.installedPlugins")}</h2><p className="text-muted-foreground mt-0.5 text-xs">{plugins.length} · {plugins.filter(plugin => plugin.enabled).length} {t("settings.activePlugins")}</p></div>
            <Button variant="ghost" size="icon" aria-label={t("settings.refreshPlugins")} onClick={() => void refresh()}><RefreshCw /></Button>
          </div>
          {plugins.length === 0 ? <p className="text-muted-foreground p-5 text-sm">{t("settings.noPlugins")}</p> : <div className="divide-y">
            {plugins.map(plugin => <PluginListItem key={plugin.manifest.id} plugin={plugin} selected={plugin.manifest.id === selectedId} busy={busy === plugin.manifest.id} t={t} onSelect={() => setSelectedId(plugin.manifest.id)} onToggle={() => void toggle(plugin)} onApprove={() => void approve(plugin)} />)}
          </div>}
        </aside>
        <section className="min-h-0 overflow-y-auto px-5 py-5 sm:px-8 sm:py-7">
          <div className="mx-auto max-w-5xl">
            {plugins.find(plugin => plugin.manifest.id === selectedId) ? <PluginDevTools plugin={plugins.find(plugin => plugin.manifest.id === selectedId)!} t={t} /> : <PluginWorkspaceEmptyState t={t} />}
          </div>
        </section>
      </main>
    </div>
  )
}

function PluginWorkspaceEmptyState({t}: {t: (key: string) => string}) {
  return (
    <div className="flex min-h-[520px] items-center justify-center">
      <div className="max-w-md text-center">
        <div className="mx-auto flex size-12 items-center justify-center rounded-xl border bg-primary/10 text-primary"><BookOpen className="size-5" /></div>
        <h2 className="mt-5 text-lg font-semibold">{t("settings.pluginWorkspaceEmptyTitle")}</h2>
        <p className="text-muted-foreground mt-2 text-sm leading-6">{t("settings.pluginWorkspaceEmptyDescription")}</p>
        <Button className="mt-5" onClick={() => void rcService.openPluginDocumentation()}><BookOpen />{t("settings.pluginDocumentation")}</Button>
      </div>
    </div>
  )
}

function PluginListItem({plugin, selected, busy, t, onSelect, onToggle, onApprove}: {plugin: PluginInfo; selected: boolean; busy: boolean; t: (key: string) => string; onSelect: () => void; onToggle: () => void; onApprove: () => void}) {
  const needsApproval = (plugin.manifest.permissions?.events?.length ?? 0) > (plugin.approvedEvents?.length ?? 0) || (plugin.manifest.permissions?.apis?.length ?? 0) > (plugin.approvedApis?.length ?? 0) || (plugin.manifest.permissions?.network?.length ?? 0) > (plugin.approvedHosts?.length ?? 0) || (plugin.manifest.permissions?.plugins?.length ?? 0) > (plugin.approvedPlugins?.length ?? 0) || (plugin.manifest.permissions?.files?.read?.length ?? 0) > (plugin.approvedFileRead?.length ?? 0) || (plugin.manifest.permissions?.files?.write?.length ?? 0) > (plugin.approvedFileWrite?.length ?? 0)
  const statusIcon = plugin.status === "ready"
    ? <CheckCircle2 className="size-4 text-emerald-400" />
    : plugin.status === "invalid"
      ? <AlertTriangle className="size-4 text-destructive" />
      : plugin.status === "disabled"
        ? <span className="size-2 rounded-full bg-muted-foreground/60" />
        : <AlertCircle className="size-4 text-muted-foreground" />
  return (
    <div className={`grid gap-3 p-4 transition-colors ${selected ? "bg-accent/70" : "hover:bg-muted/30"}`}>
      <button type="button" onClick={onSelect} aria-current={selected ? "true" : undefined} className="grid min-w-0 gap-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-ring">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <p className="truncate text-sm font-semibold">{plugin.manifest.name}</p>
              <span className="shrink-0 rounded border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">v{plugin.manifest.version}</span>
            </div>
            <p className="text-muted-foreground mt-1 truncate font-mono text-[11px]">{plugin.manifest.id}</p>
          </div>
          <span className="flex shrink-0 items-center gap-1.5 text-[11px] text-muted-foreground">
            {statusIcon}<span>{plugin.status}</span>
          </span>
        </div>
        {plugin.manifest.description && <p className="text-muted-foreground line-clamp-2 text-xs leading-5">{plugin.manifest.description}</p>}
      </button>
      {plugin.error && <div className="flex gap-2 rounded-md border border-destructive/30 bg-destructive/10 px-2.5 py-2 text-xs leading-5 text-destructive"><AlertTriangle className="mt-0.5 size-3.5 shrink-0" /><span>{plugin.error}</span></div>}
      <div className="flex flex-wrap items-center gap-2">
        <button
          type="button"
          aria-pressed={plugin.enabled}
          disabled={busy || plugin.status === "invalid"}
          onClick={onToggle}
          className={`inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 ${plugin.enabled ? "border-emerald-500/35 bg-emerald-500/10 text-emerald-300 hover:bg-emerald-500/15" : "border-border bg-background text-muted-foreground hover:bg-accent hover:text-foreground"}`}
        >
          {busy ? <Loader2 className="size-3.5 animate-spin" /> : <Power className="size-3.5" />}
          <span>{plugin.enabled ? t("settings.enabled") : t("settings.disabled")}</span>
        </button>
        {needsApproval && <Button variant="outline" size="sm" disabled={busy} onClick={onApprove}><ShieldCheck />{t("settings.approvePermissions")}</Button>}
      </div>
    </div>
  )
}

type PluginOperation = "loading" | "idle" | "saving" | "compiling" | "reloading" | "saved" | "compiled" | "reloaded" | "error"

function isPluginOperationBusy(operation: PluginOperation): boolean {
  return operation === "loading" || operation === "saving" || operation === "compiling" || operation === "reloading"
}

function PluginActionStatus({operation, dirty, lastSavedAt, t}: {operation: PluginOperation; dirty: boolean; lastSavedAt: number | null; t: (key: string) => string}) {
  const status = operation === "loading"
    ? {label: t("settings.pluginWorkspaceLoading"), tone: "text-sky-300", icon: <Loader2 className="size-3.5 animate-spin" />}
    : operation === "saving"
    ? {label: t("settings.pluginSaving"), tone: "text-sky-300", icon: <Loader2 className="size-3.5 animate-spin" />}
    : operation === "compiling"
      ? {label: t("settings.pluginCompiling"), tone: "text-sky-300", icon: <Loader2 className="size-3.5 animate-spin" />}
      : operation === "reloading"
        ? {label: t("settings.pluginReloading"), tone: "text-sky-300", icon: <Loader2 className="size-3.5 animate-spin" />}
        : operation === "error"
          ? {label: t("settings.pluginOperationFailed"), tone: "text-destructive", icon: <AlertCircle className="size-3.5" />}
          : dirty
            ? {label: t("settings.unsavedPluginChanges"), tone: "text-amber-300", icon: <AlertTriangle className="size-3.5" />}
            : operation === "saved"
              ? {label: t("settings.pluginSaved"), tone: "text-emerald-300", icon: <CheckCircle2 className="size-3.5" />}
              : operation === "compiled"
                ? {label: t("settings.pluginCompiled"), tone: "text-emerald-300", icon: <CheckCircle2 className="size-3.5" />}
                : operation === "reloaded"
                  ? {label: t("settings.pluginReloaded"), tone: "text-emerald-300", icon: <CheckCircle2 className="size-3.5" />}
                  : {label: t("settings.pluginReady"), tone: "text-muted-foreground", icon: <CheckCircle2 className="size-3.5" />}
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
      <span className={`inline-flex items-center gap-1.5 font-medium ${status.tone}`}>{status.icon}{status.label}</span>
      {lastSavedAt && !dirty && <span className="text-muted-foreground">{t("settings.pluginLastSaved")} {new Date(lastSavedAt).toLocaleTimeString()}</span>}
      {dirty && <span className="text-muted-foreground">{t("settings.pluginSaveRequired")}</span>}
    </div>
  )
}

function PluginDevTools({plugin, t}: {plugin: PluginInfo; t: (key: string) => string}) {
  const {settings} = useCodingSettings()
  const [files, setFiles] = useState<string[]>([])
  const [activeFile, setActiveFile] = useState("")
  const [content, setContent] = useState<Record<string, string>>({})
  const [logs, setLogs] = useState<PluginLogEntry[]>([])
  const [build, setBuild] = useState<PluginBuildResult | null>(null)
  const [savedContent, setSavedContent] = useState<Record<string, string>>({})
  const [operation, setOperation] = useState<PluginOperation>("loading")
  const [hotReload, setHotReload] = useState(true)
  const [lastSavedAt, setLastSavedAt] = useState<number | null>(null)
  const [error, setError] = useState("")
  const [workspaceLoaded, setWorkspaceLoaded] = useState(false)

  const dirty = useMemo(() => {
    const paths = new Set([...Object.keys(content), ...Object.keys(savedContent)])
    return [...paths].some(path => content[path] !== savedContent[path])
  }, [content, savedContent])
  const busy = isPluginOperationBusy(operation)

  const refreshLogs = useCallback(async () => setLogs((await rcService.getPluginLogs(plugin.manifest.id)) ?? []), [plugin.manifest.id])
  const load = useCallback(async () => {
    setError("")
    setOperation("loading")
    setWorkspaceLoaded(false)
    try {
      const discovered = (await rcService.getPluginFiles(plugin.manifest.id)) ?? []
      setFiles(discovered)
      const loaded: Record<string, string> = {}
      for (const path of discovered) {
        if (isPluginEditorFile(path, plugin.manifest.main) || path === plugin.manifest.main || /\.d\.ts$/i.test(path)) {
          const file = await rcService.readPluginFile(plugin.manifest.id, path)
          if (file) loaded[path] = /\.(?:ts|tsx)$/i.test(path) ? stripPluginDeclarationReference(file.content) : file.content
        }
      }
      setContent(loaded)
      setSavedContent(loaded)
      setBuild(null)
      setLastSavedAt(null)
      const editable = discovered.filter(path => isPluginEditorFile(path, plugin.manifest.main))
      const preferred = editable.find(path => /(?:^|\/)index\.(?:ts|tsx)$/i.test(path)) ?? editable.find(path => path.toLowerCase() === "manifest.json") ?? editable[0] ?? ""
      setActiveFile(preferred)
      await refreshLogs()
      setWorkspaceLoaded(true)
      setOperation("idle")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      setOperation("error")
    }
  }, [plugin.manifest.id, plugin.manifest.main, refreshLogs])

  useEffect(() => { void load() }, [load])
  useEffect(() => {
    const off = Events.On("plugin:logs", event => { if (String(event.data) === plugin.manifest.id) void refreshLogs() })
    return off
  }, [plugin.manifest.id, refreshLogs])

  const persist = async (snapshot: Record<string, string>) => {
    for (const [path, value] of Object.entries(snapshot)) await rcService.writePluginFile(plugin.manifest.id, path, value)
    setSavedContent(snapshot)
    setLastSavedAt(Date.now())
  }

  const compilePlugin = async (sourceContent = content): Promise<boolean> => {
    setOperation("compiling")
    setError("")
    try {
      const snapshot = {...sourceContent}
      await persist(snapshot)
      let nextContent = snapshot
      const sourcePath =
        files.find(path => /(?:^|\/)index\.(?:ts|tsx)$/i.test(path) && !/\.d\.ts$/i.test(path)) ??
        files.find(path => /\.(?:ts|tsx)$/i.test(path) && !/\.d\.ts$/i.test(path))
      if (sourcePath && snapshot[sourcePath] !== undefined) {
        const ts = await import("typescript")
        const transpiled = ts.transpileModule(snapshot[sourcePath], {compilerOptions: {target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, sourceMap: false}})
        nextContent = {...snapshot, [plugin.manifest.main]: transpiled.outputText}
        await rcService.writePluginFile(plugin.manifest.id, plugin.manifest.main, transpiled.outputText)
      }
      const result = await rcService.buildPlugin(plugin.manifest.id)
      setContent(nextContent)
      setSavedContent(nextContent)
      setBuild(result)
      setLastSavedAt(Date.now())
      if (!result?.success) {
        setError(result?.message ?? t("settings.pluginCompileFailed"))
        setOperation("error")
        return false
      }
      await refreshLogs()
      setOperation("compiled")
      return true
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      setOperation("error")
      return false
    }
  }

  const reload = async () => {
    if (busy) return
    setOperation("reloading")
    setError("")
    try {
      await rcService.reloadPlugin(plugin.manifest.id)
      await refreshLogs()
      setOperation("reloaded")
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      setOperation("error")
    }
  }

  const save = async () => {
    if (!dirty || busy) return
    setOperation("saving")
    setError("")
    try {
      const snapshot = {...content}
      await persist(snapshot)
      setBuild(null)
      setOperation("saved")
      if (hotReload && await compilePlugin(snapshot)) await reload()
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : String(cause))
      setOperation("error")
    }
  }

  const buildPlugin = async () => {
    if (busy) return
    await compilePlugin()
  }

  const openFile = async (path: string) => {
    setActiveFile(path)
    if (content[path] !== undefined) return
    const file = await rcService.readPluginFile(plugin.manifest.id, path)
    if (file) {
      setContent(current => ({...current, [path]: file.content}))
      setSavedContent(current => ({...current, [path]: file.content}))
    }
  }

  const languageForFile = pluginLanguageForFile(activeFile)
  const activeMonacoTheme = pluginEditorTheme(settings.theme)
  const monacoRef = useRef<PluginMonaco | null>(null)
  const declarations = useMemo(() => {
    const declarationPath = Object.keys(content).find(path => /\.d\.ts$/i.test(path))
    return declarationPath ? content[declarationPath] : ""
  }, [content])
  const configureLanguageService: BeforeMount = useCallback((monaco) => {
    monacoRef.current = monaco
    configurePluginLanguageService(monaco, declarations)
  }, [declarations])
  useEffect(() => {
    if (monacoRef.current) configurePluginLanguageService(monacoRef.current, declarations)
  }, [declarations])
  const preview = useMemo(() => content[plugin.manifest.main] ?? "", [content, plugin.manifest.main])

  return (
    <div className="grid min-h-[620px] gap-4">
      <div className="rounded-lg border bg-card/40 p-4">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0"><div className="flex flex-wrap items-center gap-2"><h2 className="text-xl font-semibold">{plugin.manifest.name}</h2><span className="text-muted-foreground font-mono text-[11px]">{plugin.manifest.id}</span></div><p className="text-muted-foreground mt-1 max-w-2xl text-sm">{t("settings.pluginDevTools")}</p></div>
          <div className="flex flex-wrap gap-2">
            <Button variant="default" size="sm" disabled={busy || !dirty} onClick={() => void save()}>{operation === "saving" ? <Loader2 className="animate-spin" /> : <Save />}{operation === "saving" ? t("settings.pluginSaving") : t("settings.savePlugin")}</Button>
            <Button variant="outline" size="sm" disabled={busy} onClick={() => void buildPlugin()}>{operation === "compiling" ? <Loader2 className="animate-spin" /> : <Hammer />}{operation === "compiling" ? t("settings.pluginCompiling") : t("settings.buildPlugin")}</Button>
            <Button variant="outline" size="sm" disabled={busy} onClick={() => void reload()}>{operation === "reloading" ? <Loader2 className="animate-spin" /> : <Play />}{operation === "reloading" ? t("settings.pluginReloading") : t("settings.reloadPlugin")}</Button>
            <Button variant="outline" size="sm" disabled={busy} onClick={() => void rcService.exportPlugin(plugin.manifest.id)}><Download />{t("settings.exportPlugin")}</Button>
          </div>
        </div>
        <div className="mt-4 border-t pt-3"><PluginActionStatus operation={operation} dirty={dirty} lastSavedAt={lastSavedAt} t={t} /></div>
      </div>
      {error && <div role="alert" className="flex gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-xs leading-5 text-destructive"><AlertCircle className="mt-0.5 size-4 shrink-0" /><span>{error}</span></div>}
      <Tabs defaultValue="editor" className="grid min-h-0 gap-3">
        <TabsList className="w-fit"><TabsTrigger value="editor"><Code2 />{t("settings.pluginEditor")}</TabsTrigger><TabsTrigger value="runtime"><Terminal />{t("settings.pluginConsole")}</TabsTrigger><TabsTrigger value="preview"><Eye />{t("settings.pluginPreview")}</TabsTrigger></TabsList>
        <TabsContent value="editor" className="mt-0 grid min-h-0 gap-3">
          <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border bg-muted/20 px-3 py-2"><div className="flex min-w-0 flex-1 flex-wrap gap-1">{files.filter(path => isPluginEditorFile(path, plugin.manifest.main)).map(path => <Button key={path} variant={path === activeFile ? "secondary" : "ghost"} size="sm" onClick={() => void openFile(path)}>{path === "manifest.json" ? <FileJson /> : <Code2 />}{path}</Button>)}</div><button type="button" aria-pressed={hotReload} onClick={() => setHotReload(value => !value)} className={`inline-flex h-8 items-center gap-2 rounded-md border px-2.5 text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${hotReload ? "border-sky-500/35 bg-sky-500/10 text-sky-300" : "border-border text-muted-foreground hover:bg-accent hover:text-foreground"}`}><span className={`size-1.5 rounded-full ${hotReload ? "bg-sky-300" : "bg-muted-foreground/60"}`} />{t("settings.hotReload")}</button></div>
          <div className="min-h-[440px] overflow-hidden rounded-md border">{workspaceLoaded && activeFile ? <Editor height="55vh" theme={activeMonacoTheme} language={languageForFile} path={activeFile} value={content[activeFile] ?? ""} beforeMount={configureLanguageService} onChange={value => setContent(current => ({...current, [activeFile]: value ?? ""}))} options={{minimap: {enabled: false}, automaticLayout: true, fontFamily: settings.fontFamily, fontSize: settings.fontSize, fontLigatures: true, fixedOverflowWidgets: true, hover: {above: false}, scrollBeyondLastLine: false, wordWrap: "on", "semanticHighlighting.enabled": true}} /> : <div className="text-muted-foreground flex h-[55vh] items-center justify-center text-sm">{t("settings.pluginWorkspaceLoading")}</div>}</div>
          <div className="flex items-center justify-between text-xs"><span className="text-muted-foreground">{activeFile || t("settings.noPluginFile")}{dirty ? ` · ${t("settings.unsavedPluginChanges")}` : ""}</span>{build && <span className={build.success ? "text-emerald-500" : "text-destructive"}>{build.message}</span>}</div>
        </TabsContent>
        <TabsContent value="runtime" className="mt-0 grid gap-3">
          <div className="flex items-center justify-between rounded-md border bg-muted/20 px-3 py-2"><div><p className="text-sm font-medium">{t("settings.pluginConsole")}</p><p className="text-muted-foreground text-xs">{t("settings.pluginConsoleDescription")}</p></div><Button variant="ghost" size="sm" onClick={() => void rcService.clearPluginLogs(plugin.manifest.id).then(refreshLogs)}><Trash2 />{t("settings.clearPluginLogs")}</Button></div>
          <pre className="min-h-[420px] overflow-auto rounded-md border bg-black/30 p-4 font-mono text-xs leading-5">{logs.length === 0 ? <span className="text-muted-foreground">{t("settings.noPluginLogs")}</span> : logs.map((entry, index) => <div key={`${entry.timestamp}-${index}`}><span className="text-muted-foreground">{new Date(entry.timestamp).toLocaleTimeString()}</span> <span className={entry.level === "error" ? "text-red-400" : entry.level === "warn" ? "text-amber-400" : "text-emerald-400"}>[{entry.level}]</span> {entry.message}</div>)}</pre>
        </TabsContent>
        <TabsContent value="preview" className="mt-0 grid gap-3"><div className="rounded-md border bg-muted/20 px-4 py-3"><div className="flex items-center gap-2"><Eye className="h-4 w-4 text-primary" /><p className="text-sm font-medium">{t("settings.pluginPreview")}</p></div><p className="text-muted-foreground mt-1 text-xs">{t("settings.pluginPreviewDescription")}</p></div><iframe title={t("settings.pluginPreview")} sandbox="allow-scripts" className="min-h-[420px] w-full rounded-md border bg-background" srcDoc={createPluginPreviewDocument(plugin.manifest.name, preview, t("settings.pluginPreviewRuntime"))} /></TabsContent>
      </Tabs>
    </div>
  )
}

function createPluginPreviewDocument(name: string, bundle: string, runtimeText: string): string {
  return `<!doctype html><html><body style="font-family:system-ui;padding:24px;background:#111;color:#eee"><h2>${escapeHtml(name)}</h2><p style="color:#aaa">${escapeHtml(runtimeText)}</p><div id="plugin-root" style="border:1px solid #333;border-radius:8px;padding:16px;margin-top:20px">Loading plugin…</div><script>
    const root = document.getElementById('plugin-root');
    let pluginDefinition;
    let pluginInstance;
    const Plugin = class {
      constructor() { pluginInstance = this; if (api) this.__attach(api); }
      __attach(api) {
        this.api = api;
        this.events = api.events;
        this.commands = api.commands;
        this.panels = api.panels;
        this.storage = api.storage;
        this.secrets = api.secrets;
        this.network = api.network;
        this.sockets = api.sockets;
        this.plugins = api.plugins;
        this.express = api.express;
        this.fileBrowser = api.fileBrowser;
        this.ui = api.ui;
        this.notifications = api.notifications;
        this.monaco = api.monaco;
        this.nc = api.nc;
        this.automation = api.automation;
        this.actions = api.actions;
      }
    };
    globalThis.registerGorcPlugin = definition => { pluginDefinition = definition; };
    const api = {id:'preview',events:{on:()=>()=>{}},commands:{register:(command, handler)=>{root.innerHTML='<strong>'+command.label+'</strong><p style="color:#aaa">Command registered successfully.</p><button id="run-plugin-command" style="margin-top:12px;padding:8px 12px">Run command</button><pre id="plugin-command-result" style="color:#aaa"></pre>'; document.getElementById('run-plugin-command')?.addEventListener('click',()=>Promise.resolve(handler?.([])).then(()=>{const result=document.getElementById('plugin-command-result'); if(result) result.textContent='Callback executed';}).catch(error=>{const result=document.getElementById('plugin-command-result'); if(result) result.textContent=String(error);})); return ()=>{};}},panels:{register:panel=>{root.innerHTML=panel.html || '<strong>'+panel.title+'</strong>'; return ()=>{};}},storage:{get:async()=>null,set:async()=>{},delete:async()=>{}},secrets:{get:async()=>null,set:async()=>{},delete:async()=>{}},network:{request:async()=>({status:204,headers:{},body:''})},sockets:{connect:async()=>({id:'preview',url:'wss://preview',readyState:'OPEN',on:()=>()=>{},send:async()=>{},close:async()=>{}})},plugins:{list:async()=>[],on:()=>()=>{},send:async()=>{},call:async()=>({}),expose:()=>()=>{}},express:{listen:async()=>({baseUrl:'http://127.0.0.1:0/plugins/preview',token:'preview',get:async()=>async()=>{},post:async()=>async()=>{},put:async()=>async()=>{},patch:async()=>async()=>{},delete:async()=>async()=>{}})},fileBrowser:{readText:async()=>({path:'',name:'',extension:'',size:0,revision:'',content:''}),writeText:async()=>({path:'',name:'',extension:'',size:0,revision:'',content:''}),editors:{register:()=>async()=>{}}},ui:{windows:{open:async()=>({id:'preview',update:async()=>{},close:async()=>{}})},onAction:()=>()=>{},onClosed:()=>()=>{}},notifications:{show:notification=>{root.innerHTML='<strong>'+String(notification.title||'Plugin notification')+'</strong><p style="color:#aaa">'+String(notification.message||'')+'</p>';},info:(message,title)=>{root.textContent=(title?title+': ':'')+message;},success:(message,title)=>{root.textContent=(title?title+': ':'')+message;},warning:(message,title)=>{root.textContent=(title?title+': ':'')+message;},error:(message,title)=>{root.textContent=(title?title+': ':'')+message;}},monaco:{languages:{register:async()=>async()=>{}},diagnostics:{register:()=>async()=>{}},completions:{register:()=>async()=>{}}},nc:{readWeapon:async()=>({}),readClass:async()=>({}),readNPC:async()=>({}),readNPCFlags:async()=>({}),readNPCAttributes:async()=>({}),list:async()=>({weapons:[],classes:[],npcs:[]}),saveWeapon:async()=>{},saveClass:async()=>{},saveNPC:async()=>{},saveNPCFlags:async()=>{},createWeapon:async()=>{},deleteWeapon:async()=>{},createClass:async()=>{},deleteClass:async()=>{},createNPC:async()=>{},deleteNPC:async()=>{},resetNPC:async()=>{}},automation:{timeout:()=>()=>{},interval:()=>()=>{},debounce:handler=>handler,retry:async operation=>operation()},actions:{pm:{send:async()=>{},},admin:{send:async()=>{}},rc:{execute:async()=>{}},nc:{saveWeapon:async()=>{},saveClass:async()=>{},saveNPC:async()=>{}}}};
    try {
      const module = {exports:{}};
      new Function('api', 'Plugin', 'registerGorcPlugin', 'module', 'exports', ${JSON.stringify(bundle)})(api, Plugin, globalThis.registerGorcPlugin, module, module.exports);
      const exported = module.exports && Object.prototype.hasOwnProperty.call(module.exports, 'default') ? module.exports.default : module.exports;
      if (!pluginInstance && typeof exported === 'function') pluginInstance = new exported();
      if (!pluginInstance && exported && typeof exported === 'object' && typeof exported.onLoad === 'function') pluginDefinition = exported;
      const lifecycle = pluginInstance || pluginDefinition;
      if (!lifecycle) throw new Error('Plugin bundle must instantiate Plugin or register a definition');
      if (pluginInstance) pluginInstance.__attach(api);
      Promise.resolve(lifecycle.onLoad?.(api)).then(()=>lifecycle.onStart?.()).catch(error=>{root.textContent=String(error); root.style.color='#f87171';});
    } catch (error) { root.textContent=String(error); root.style.color='#f87171'; }
  <\/script></body></html>`
}

function escapeHtml(value: string): string { return value.replace(/[&<>'"]/g, character => ({"&":"&amp;","<":"&lt;",">":"&gt;","'":"&#39;","\"":"&quot;"}[character] ?? character)) }
