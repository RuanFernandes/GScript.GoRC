import {useCallback, useEffect, useMemo, useRef, useState} from "react"
import {Events} from "@wailsio/runtime"
import {
  Archive,
  ArrowLeft,
  ArrowRight,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  ChevronsDown,
  ChevronsUp,
  Folder,
  FolderOpen,
  Loader2,
  RefreshCw,
  Search,
  ShieldCheck,
  XCircle,
} from "lucide-react"
import {toast} from "sonner"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {Skeleton} from "@/components/ui/skeleton"
import {useLanguage} from "@/hooks/useLanguage"
import {buildFileBrowserTree, flattenFileBrowserTree, type FileBrowserTreeNode} from "@/lib/fileBrowserTree"
import {rcService} from "@/services/rcService"
import type {FileBrowserBackupResult, FileBrowserFolder} from "@/types"

type BackupStep = "select" | "destination" | "running" | "completed" | "failed" | "cancelled"

interface BackupProgress {
  phase: "scanning" | "downloading" | "completed" | "failed" | "cancelled"
  folder?: string
  file?: string
  foldersDone: number
  foldersTotal: number
  filesDone: number
  filesTotal: number
  bytesDone: number
  bytesTotal: number
  error?: string
}

const EMPTY_PROGRESS: BackupProgress = {
  phase: "scanning",
  foldersDone: 0,
  foldersTotal: 0,
  filesDone: 0,
  filesTotal: 0,
  bytesDone: 0,
  bytesTotal: 0,
}

function humanize(bytes: number): string {
  if (bytes < 0 || !Number.isFinite(bytes)) return "—"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let value = bytes
  let index = 0
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024
    index++
  }
  return `${value >= 10 || index === 0 ? Math.round(value) : value.toFixed(1)} ${units[index]}`
}

function nodePaths(node: FileBrowserTreeNode): string[] {
  return [node.path, ...node.children.flatMap(nodePaths)]
}

function folderLabel(path: string): string {
  return path ? `${path}/` : "/"
}

function parseBackupProgress(data: string): BackupProgress | null {
  try {
    const value = JSON.parse(data) as Partial<BackupProgress>
    if (typeof value.phase !== "string") return null
    return {
      phase: value.phase as BackupProgress["phase"],
      folder: typeof value.folder === "string" ? value.folder : "",
      file: typeof value.file === "string" ? value.file : "",
      foldersDone: typeof value.foldersDone === "number" ? value.foldersDone : 0,
      foldersTotal: typeof value.foldersTotal === "number" ? value.foldersTotal : 0,
      filesDone: typeof value.filesDone === "number" ? value.filesDone : 0,
      filesTotal: typeof value.filesTotal === "number" ? value.filesTotal : 0,
      bytesDone: typeof value.bytesDone === "number" ? value.bytesDone : 0,
      bytesTotal: typeof value.bytesTotal === "number" ? value.bytesTotal : 0,
      error: typeof value.error === "string" ? value.error : "",
    }
  } catch {
    return null
  }
}

export function FileBrowserBackupWindowScreen() {
  const {t} = useLanguage()
  const [folders, setFolders] = useState<FileBrowserFolder[]>([])
  const [foldersLoaded, setFoldersLoaded] = useState(false)
  const [foldersLoading, setFoldersLoading] = useState(true)
  const [folderError, setFolderError] = useState("")
  const [folderQuery, setFolderQuery] = useState("")
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(() => new Set())
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [step, setStep] = useState<BackupStep>("select")
  const [destination, setDestination] = useState("")
  const [progress, setProgress] = useState<BackupProgress>(EMPTY_PROGRESS)
  const [result, setResult] = useState<FileBrowserBackupResult | null>(null)
  const [error, setError] = useState("")
  const initialLoadStarted = useRef(false)
  const cancelRequested = useRef(false)
  const [cancelling, setCancelling] = useState(false)

  const tree = useMemo(() => buildFileBrowserTree(folders, true), [folders])
  const allFolders = useMemo(() => flattenFileBrowserTree(tree), [tree])
  const allPaths = useMemo(() => allFolders.map((node) => node.path), [allFolders])
  const selectedFolderList = useMemo(() => [...selectedPaths].sort((a, b) => a.localeCompare(b)), [selectedPaths])
  const normalizedQuery = folderQuery.trim().toLocaleLowerCase()
  const searchResults = useMemo(
    () => normalizedQuery
      ? allFolders.filter((node) => node.path.toLocaleLowerCase().includes(normalizedQuery)).slice(0, 100)
      : [],
    [allFolders, normalizedQuery],
  )
  const allSelected = allPaths.length > 0 && allPaths.every((path) => selectedPaths.has(path))

  const snapshotFolders = useCallback(async () => {
    const list = await rcService.getFileBrowserFolders().catch(() => null)
    setFolders(list ?? [])
    setFoldersLoaded(true)
    setFoldersLoading(false)
    setFolderError("")
  }, [])

  const refreshFolders = useCallback(async () => {
    setFoldersLoading(true)
    setFolderError("")
    try {
      await rcService.fileBrowserStart()
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setFolderError(message)
      setFoldersLoading(false)
      toast.error(t("file.backupStartFailed"), {description: message})
    }
  }, [t])

  useEffect(() => {
    let active = true
    const off = Events.On("rc:evt", (event: {data: string}) => {
      try {
        const payload = JSON.parse(event.data) as {name?: string}
        if (!active) return
        if (payload.name === "rc:fbFolders") {
          void snapshotFolders()
        } else if (payload.name === "rc:fbReset") {
          setFolders([])
          setFoldersLoaded(false)
          setFoldersLoading(true)
          setSelectedPaths(new Set())
        }
      } catch {
        // Ignore malformed events and keep the current folder snapshot.
      }
    })

    if (!initialLoadStarted.current) {
      initialLoadStarted.current = true
      void rcService.getFileBrowserFolders().then((list) => {
        if (!active) return
        if (list && list.length > 0) {
          setFolders(list)
          setFoldersLoaded(true)
          setFoldersLoading(false)
          return
        }
        void refreshFolders()
      }).catch(() => {
        if (active) void refreshFolders()
      })
    }
    return () => {
      active = false
      off()
    }
  }, [refreshFolders, snapshotFolders])

  useEffect(() => {
    const off = Events.On("rc:fileBrowserBackup", (event: {data: string}) => {
      const next = parseBackupProgress(event.data)
      if (!next) return
      setProgress(next)
      if (next.phase === "failed") {
        setError(next.error || t("file.backupFailed"))
        setStep("failed")
      } else if (next.phase === "cancelled") {
        setError(next.error || "")
        setStep("cancelled")
      }
    })
    return off
  }, [t])

  useEffect(() => {
    setSelectedPaths((current) => {
      const available = new Set(allPaths)
      const next = new Set([...current].filter((path) => available.has(path)))
      return next.size === current.size ? current : next
    })
  }, [allPaths])

  const toggleFolder = (node: FileBrowserTreeNode) => {
    const paths = nodePaths(node)
    setSelectedPaths((current) => {
      const next = new Set(current)
      const checked = paths.every((path) => next.has(path))
      paths.forEach((path) => {
        if (checked) next.delete(path)
        else next.add(path)
      })
      return next
    })
  }

  const toggleAll = (checked: boolean) => {
    setSelectedPaths(checked ? new Set(allPaths) : new Set())
  }

  const chooseDestination = async () => {
    try {
      const chosen = await rcService.chooseBackupDirectory()
      if (chosen) setDestination(chosen)
    } catch (err) {
      toast.error(t("file.backupFolderFailed"), {description: String(err)})
    }
  }

  const startBackup = async () => {
    if (selectedFolderList.length === 0) {
      toast.error(t("file.backupEmptySelection"))
      return
    }
    if (!destination) {
      toast.error(t("file.backupNoDestination"))
      return
    }
    cancelRequested.current = false
    setCancelling(false)
    setError("")
    setResult(null)
    setProgress({...EMPTY_PROGRESS, foldersTotal: selectedFolderList.length})
    setStep("running")
    try {
      const backup = await rcService.backupFileBrowser(selectedFolderList, destination)
      setResult(backup)
      setStep("completed")
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setError(message)
      setStep(cancelRequested.current ? "cancelled" : "failed")
    }
  }

  const cancelBackup = async () => {
    cancelRequested.current = true
    setCancelling(true)
    try {
      await rcService.cancelFileBrowserBackup()
    } catch (err) {
      cancelRequested.current = false
      setCancelling(false)
      setError(String(err))
      toast.error(t("file.backupFailed"), {description: String(err)})
    }
  }

  const reset = () => {
    setStep("select")
    setDestination("")
    setResult(null)
    setError("")
    setProgress(EMPTY_PROGRESS)
    cancelRequested.current = false
    setCancelling(false)
  }

  return (
    <div className="bg-background flex h-full min-w-0 flex-col">
      <header className="flex flex-wrap items-center gap-3 border-b px-4 py-3 sm:px-5">
        <div className="flex size-8 shrink-0 items-center justify-center rounded-md border bg-primary/10 text-primary">
          <Archive className="size-4" />
        </div>
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-base font-semibold">{t("file.backupTitle")}</h1>
          <p className="text-muted-foreground text-xs">{t("file.backupDescription")}</p>
        </div>
        {step === "select" && (
          <Button variant="ghost" size="sm" onClick={() => void refreshFolders()} disabled={foldersLoading}>
            <RefreshCw className={foldersLoading ? "animate-spin" : undefined} />
            {t("file.refresh")}
          </Button>
        )}
      </header>

      <main className="min-h-0 flex-1 overflow-y-auto">
        <div className="mx-auto grid w-full max-w-4xl gap-4 p-4 sm:gap-5 sm:p-6">
          {step === "select" && (
            <FolderSelectionStep
              foldersLoaded={foldersLoaded}
              foldersLoading={foldersLoading}
              folderError={folderError}
              tree={tree}
              allFolders={allFolders}
              searchResults={searchResults}
              folderQuery={folderQuery}
              selectedPaths={selectedPaths}
              allSelected={allSelected}
              expanded={expanded}
              onQueryChange={setFolderQuery}
              onToggleAll={toggleAll}
              onExpandAll={() => setExpanded(new Set(allPaths))}
              onCollapseAll={() => setExpanded(new Set())}
              onToggleExpand={(path) => setExpanded((current) => {
                const next = new Set(current)
                if (next.has(path)) next.delete(path)
                else next.add(path)
                return next
              })}
              onToggleFolder={toggleFolder}
              onContinue={() => setStep("destination")}
              t={t}
            />
          )}

          {step === "destination" && (
            <DestinationStep
              selectedFolders={selectedFolderList}
              destination={destination}
              onChoose={chooseDestination}
              onBack={() => setStep("select")}
              onStart={() => void startBackup()}
              t={t}
            />
          )}

          {step === "running" && (
            <ProgressStep progress={progress} cancelRequested={cancelling} onCancel={() => void cancelBackup()} t={t} />
          )}

          {step === "completed" && result && (
            <CompletedStep result={result} onReset={reset} t={t} />
          )}

          {(step === "failed" || step === "cancelled") && (
            <OutcomeStep step={step} message={error} onRetry={() => setStep("destination")} onReset={reset} t={t} />
          )}
        </div>
      </main>
    </div>
  )
}

function FolderSelectionStep({
  foldersLoaded,
  foldersLoading,
  folderError,
  tree,
  allFolders,
  searchResults,
  folderQuery,
  selectedPaths,
  allSelected,
  expanded,
  onQueryChange,
  onToggleAll,
  onExpandAll,
  onCollapseAll,
  onToggleExpand,
  onToggleFolder,
  onContinue,
  t,
}: {
  foldersLoaded: boolean
  foldersLoading: boolean
  folderError: string
  tree: FileBrowserTreeNode[]
  allFolders: FileBrowserTreeNode[]
  searchResults: FileBrowserTreeNode[]
  folderQuery: string
  selectedPaths: Set<string>
  allSelected: boolean
  expanded: Set<string>
  onQueryChange: (value: string) => void
  onToggleAll: (checked: boolean) => void
  onExpandAll: () => void
  onCollapseAll: () => void
  onToggleExpand: (path: string) => void
  onToggleFolder: (node: FileBrowserTreeNode) => void
  onContinue: () => void
  t: (key: string, vars?: Record<string, string | number>) => string
}) {
  const searchActive = folderQuery.trim().length > 0
  const canContinue = selectedPaths.size > 0

  return (
    <>
      <div className="flex items-start gap-3 rounded-lg border border-primary/25 bg-primary/10 p-3.5">
        <ShieldCheck className="mt-0.5 size-5 shrink-0 text-primary" />
        <div className="grid gap-1">
          <p className="text-sm font-medium">{t("file.backupReadOnly")}</p>
          <p className="text-muted-foreground text-xs leading-relaxed">{t("file.backupReadOnlyDescription")}</p>
        </div>
      </div>

      <section className="overflow-hidden rounded-lg border bg-card/30">
        <div className="flex flex-wrap items-start gap-3 border-b p-4">
          <div className="min-w-0 flex-1">
            <h2 className="text-sm font-semibold">{t("file.backupSelectFolders")}</h2>
            <p className="text-muted-foreground mt-1 text-xs">{t("file.backupSelectFoldersDescription")}</p>
          </div>
          <Badge variant={canContinue ? "default" : "secondary"}>
            {t("file.backupSelectedFolders", {count: selectedPaths.size})}
          </Badge>
        </div>

        <div className="flex flex-wrap items-center gap-2 border-b p-3">
          <div className="relative min-w-[220px] flex-1">
            <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2" />
            <Input
              value={folderQuery}
              onChange={(event) => onQueryChange(event.target.value)}
              placeholder={t("file.backupSearchFolders")}
              aria-label={t("file.backupSearchFolders")}
              className="h-8 pl-8 text-xs"
            />
          </div>
          <Button variant="outline" size="sm" onClick={() => onToggleAll(!allSelected)} disabled={allFolders.length === 0}>
            <Check />
            {allSelected ? t("file.backupClearFolders") : t("file.backupSelectAllFolders")}
          </Button>
          <Button variant="ghost" size="sm" onClick={onExpandAll} disabled={allFolders.length === 0} title={t("file.backupExpandAll")} aria-label={t("file.backupExpandAll")}>
            <ChevronsDown />
          </Button>
          <Button variant="ghost" size="sm" onClick={onCollapseAll} disabled={expanded.size === 0} title={t("file.backupCollapseAll")} aria-label={t("file.backupCollapseAll")}>
            <ChevronsUp />
          </Button>
        </div>

        <div className="max-h-[min(48vh,30rem)] min-h-56 overflow-y-auto p-2 sm:p-3">
          {foldersLoading && !foldersLoaded ? (
            <div className="grid gap-2 p-2">
              {Array.from({length: 7}).map((_, index) => (
                <div key={index} className="flex items-center gap-2" style={{paddingLeft: `${(index % 3) * 18}px`}}>
                  <Skeleton className="size-4" />
                  <Skeleton className="h-4" style={{width: `${40 + ((index * 17) % 45)}%`}} />
                </div>
              ))}
            </div>
          ) : folderError ? (
            <div className="grid min-h-48 place-items-center gap-3 p-6 text-center">
              <XCircle className="text-destructive size-6" />
              <p className="text-muted-foreground max-w-md text-xs">{folderError}</p>
            </div>
          ) : !foldersLoaded || allFolders.length === 0 ? (
            <div className="grid min-h-48 place-items-center p-6 text-center">
              <p className="text-muted-foreground text-sm">{t("file.backupNoFolders")}</p>
            </div>
          ) : searchActive ? (
            searchResults.length === 0 ? (
              <p className="text-muted-foreground p-4 text-xs">{t("file.noMatchingFolders")}</p>
            ) : (
              <div className="grid gap-0.5">
                {searchResults.map((node) => (
                  <BackupFolderRow
                    key={node.path}
                    node={node}
                    selectedPaths={selectedPaths}
                    depth={0}
                    showExpand={false}
                    onToggleExpand={onToggleExpand}
                    onToggleFolder={onToggleFolder}
                    t={t}
                  />
                ))}
              </div>
            )
          ) : (
            <div className="grid gap-0.5">
              {tree.map((node) => (
                <BackupFolderNode
                  key={node.path}
                  node={node}
                  selectedPaths={selectedPaths}
                  expanded={expanded}
                  depth={0}
                  onToggleExpand={onToggleExpand}
                  onToggleFolder={onToggleFolder}
                  t={t}
                />
              ))}
            </div>
          )}
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2 border-t px-3 py-3">
          <p className="text-muted-foreground text-xs">
            {searchActive ? t("file.backupSearchCount", {count: searchResults.length}) : t("file.backupFolderCount", {count: allFolders.length})}
          </p>
          <Button onClick={onContinue} disabled={!canContinue}>
            {t("file.backupContinue")}
            <ArrowRight />
          </Button>
        </div>
      </section>
    </>
  )
}

function BackupFolderNode({
  node,
  selectedPaths,
  expanded,
  depth,
  onToggleExpand,
  onToggleFolder,
  t,
}: {
  node: FileBrowserTreeNode
  selectedPaths: Set<string>
  expanded: Set<string>
  depth: number
  onToggleExpand: (path: string) => void
  onToggleFolder: (node: FileBrowserTreeNode) => void
  t: (key: string, vars?: Record<string, string | number>) => string
}) {
  const isExpanded = expanded.has(node.path)
  return (
    <>
      <BackupFolderRow
        node={node}
        selectedPaths={selectedPaths}
        depth={depth}
        showExpand={node.children.length > 0}
        expanded={isExpanded}
        onToggleExpand={onToggleExpand}
        onToggleFolder={onToggleFolder}
        t={t}
      />
      {isExpanded && node.children.map((child) => (
        <BackupFolderNode
          key={child.path}
          node={child}
          selectedPaths={selectedPaths}
          expanded={expanded}
          depth={depth + 1}
          onToggleExpand={onToggleExpand}
          onToggleFolder={onToggleFolder}
          t={t}
        />
      ))}
    </>
  )
}

function BackupFolderRow({
  node,
  selectedPaths,
  depth,
  showExpand,
  expanded = false,
  onToggleExpand,
  onToggleFolder,
  t,
}: {
  node: FileBrowserTreeNode
  selectedPaths: Set<string>
  depth: number
  showExpand: boolean
  expanded?: boolean
  onToggleExpand: (path: string) => void
  onToggleFolder: (node: FileBrowserTreeNode) => void
  t: (key: string, vars?: Record<string, string | number>) => string
}) {
  const checkboxRef = useRef<HTMLInputElement>(null)
  const paths = useMemo(() => nodePaths(node), [node])
  const selectedCount = paths.filter((path) => selectedPaths.has(path)).length
  const checked = selectedCount === paths.length
  const mixed = selectedCount > 0 && !checked

  useEffect(() => {
    if (checkboxRef.current) checkboxRef.current.indeterminate = mixed
  }, [mixed])

  return (
    <div className="group flex min-w-0 items-center gap-1 rounded-md px-1 py-1 hover:bg-accent/60" style={{paddingLeft: `${depth * 18 + 4}px`}}>
      <button
        type="button"
        className="text-muted-foreground flex size-5 shrink-0 items-center justify-center rounded hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
        onClick={() => showExpand && onToggleExpand(node.path)}
        disabled={!showExpand}
        tabIndex={showExpand ? 0 : -1}
        title={showExpand ? (expanded ? t("file.collapse") : t("file.expand")) : undefined}
        aria-label={showExpand ? (expanded ? t("file.collapse") : t("file.expand")) : undefined}
      >
        {showExpand ? (expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />) : null}
      </button>
      <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 rounded px-1 py-0.5">
        <input
          ref={checkboxRef}
          type="checkbox"
          checked={checked}
          onChange={() => onToggleFolder(node)}
          aria-label={t("file.backupSelectFolder", {name: folderLabel(node.path)})}
          aria-checked={mixed ? "mixed" : checked}
          className="size-3.5 shrink-0 accent-primary"
        />
        {checked || mixed ? <FolderOpen className="size-4 shrink-0 text-primary" /> : <Folder className="text-muted-foreground size-4 shrink-0" />}
        <span className="min-w-0 truncate text-xs" title={folderLabel(node.path)}>{node.name}</span>
        {node.rights && <Badge variant="secondary" className="ml-auto h-4 shrink-0 px-1 text-[10px]">{node.rights}</Badge>}
      </label>
    </div>
  )
}

function DestinationStep({
  selectedFolders,
  destination,
  onChoose,
  onBack,
  onStart,
  t,
}: {
  selectedFolders: string[]
  destination: string
  onChoose: () => void
  onBack: () => void
  onStart: () => void
  t: (key: string, vars?: Record<string, string | number>) => string
}) {
  return (
    <section className="overflow-hidden rounded-lg border bg-card/30">
      <div className="border-b p-4 sm:p-5">
        <div className="mb-4 flex items-center gap-2 text-xs font-medium text-primary">
          <span className="flex size-5 items-center justify-center rounded-full bg-primary text-primary-foreground">1</span>
          <span>{t("file.backupStepFolders")}</span>
          <ArrowRight className="text-muted-foreground size-3.5" />
          <span className="flex size-5 items-center justify-center rounded-full bg-primary text-primary-foreground">2</span>
          <span>{t("file.backupStepDestination")}</span>
        </div>
        <h2 className="text-base font-semibold">{t("file.backupDestinationTitle")}</h2>
        <p className="text-muted-foreground mt-1 max-w-2xl text-sm leading-relaxed">{t("file.backupDestinationDescription")}</p>
      </div>

      <div className="grid gap-5 p-4 sm:p-5">
        <div className="grid gap-2">
          <Label htmlFor="backup-destination">{t("file.backupDestination")}</Label>
          <div className="flex flex-col gap-2 sm:flex-row">
            <Input
              id="backup-destination"
              value={destination}
              placeholder={t("file.backupDestinationPlaceholder")}
              className="min-w-0 flex-1"
              readOnly
            />
            <Button variant="outline" onClick={onChoose}>
              <FolderOpen />
              {t("file.backupChooseFolder")}
            </Button>
          </div>
        </div>

        <div className="rounded-md border bg-muted/20 p-3">
          <p className="text-muted-foreground mb-2 text-xs font-medium">{t("file.backupSelectedFoldersSummary")}</p>
          <div className="flex flex-wrap gap-1.5">
            {selectedFolders.slice(0, 8).map((folder) => <Badge key={folder} variant="secondary" className="max-w-full truncate">{folderLabel(folder)}</Badge>)}
            {selectedFolders.length > 8 && <Badge variant="outline">+{selectedFolders.length - 8}</Badge>}
          </div>
        </div>

        <div className="flex flex-wrap justify-between gap-2 border-t pt-4">
          <Button variant="ghost" onClick={onBack}>
            <ArrowLeft />
            {t("file.backupBack")}
          </Button>
          <Button onClick={onStart} disabled={!destination}>
            <Archive />
            {t("file.backupStart")}
          </Button>
        </div>
      </div>
    </section>
  )
}

function ProgressStep({
  progress,
  cancelRequested,
  onCancel,
  t,
}: {
  progress: BackupProgress
  cancelRequested: boolean
  onCancel: () => void
  t: (key: string, vars?: Record<string, string | number>) => string
}) {
  const determinate = progress.phase === "downloading" && progress.filesTotal > 0
  const percentage = determinate ? Math.min(100, Math.round((progress.filesDone / progress.filesTotal) * 100)) : 0
  const description = progress.phase === "scanning"
    ? t("file.backupScanning", {folder: folderLabel(progress.folder || "")})
    : t("file.backupDownloading", {file: progress.file || "…"})

  return (
    <section className="mx-auto w-full max-w-2xl overflow-hidden rounded-lg border bg-card/30">
      <div className="grid gap-4 p-5 sm:p-7">
        <div className="flex items-start gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
            <Loader2 className="size-5 animate-spin" />
          </div>
          <div className="min-w-0">
            <h2 className="text-base font-semibold">{t("file.backupInProgress")}</h2>
            <p className="text-muted-foreground mt-1 truncate text-sm" title={progress.file || progress.folder}>{description}</p>
          </div>
        </div>

        <div className="grid gap-2">
          <div className="h-2 overflow-hidden rounded-full bg-muted">
            <div className={`h-full rounded-full bg-primary transition-[width] duration-200 ${determinate ? "" : "w-1/3 animate-pulse"}`} style={determinate ? {width: `${percentage}%`} : undefined} />
          </div>
          <div className="flex flex-wrap justify-between gap-2 text-xs">
            <span className="text-foreground font-medium">{t("file.backupProgress", {done: progress.filesDone, total: progress.filesTotal})}</span>
            <span className="text-muted-foreground tabular-nums">{progress.bytesTotal > 0 ? `${humanize(progress.bytesDone)} / ${humanize(progress.bytesTotal)}` : humanize(progress.bytesDone)}</span>
          </div>
        </div>

        <div className="grid grid-cols-2 gap-2 text-xs sm:grid-cols-3">
          <ProgressStat label={t("file.backupFolders")} value={`${progress.foldersDone}/${progress.foldersTotal}`} />
          <ProgressStat label={t("file.backupFiles")} value={`${progress.filesDone}/${progress.filesTotal}`} />
          <ProgressStat label={t("file.backupTransferred")} value={humanize(progress.bytesDone)} />
        </div>

        <div className="flex justify-end border-t pt-4">
          <Button variant="outline" onClick={onCancel} disabled={cancelRequested}>
            <XCircle />
            {cancelRequested ? t("file.backupCancelling") : t("file.backupCancel")}
          </Button>
        </div>
      </div>
    </section>
  )
}

function ProgressStat({label, value}: {label: string; value: string}) {
  return (
    <div className="grid gap-0.5 rounded-md border bg-muted/15 p-2.5">
      <span className="text-muted-foreground text-[10px]">{label}</span>
      <span className="min-w-0 truncate font-medium tabular-nums" title={value}>{value}</span>
    </div>
  )
}

function CompletedStep({
  result,
  onReset,
  t,
}: {
  result: FileBrowserBackupResult
  onReset: () => void
  t: (key: string, vars?: Record<string, string | number>) => string
}) {
  return (
    <section className="mx-auto w-full max-w-2xl overflow-hidden rounded-lg border border-emerald-500/30 bg-card/30">
      <div className="grid gap-4 p-5 sm:p-7">
        <div className="flex items-start gap-3">
          <CheckCircle2 className="mt-0.5 size-6 shrink-0 text-emerald-400" />
          <div className="min-w-0">
            <h2 className="text-base font-semibold">{t("file.backupFinished")}</h2>
            <p className="text-muted-foreground mt-1 break-all text-sm">{t("file.backupFinishedDescription", {files: result.files, path: result.destination})}</p>
          </div>
        </div>
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          <ProgressStat label={t("file.backupFiles")} value={String(result.files)} />
          <ProgressStat label={t("file.backupTransferred")} value={humanize(result.bytes)} />
          <ProgressStat label={t("file.backupLocation")} value={result.destination} />
        </div>
        <div className="flex justify-end border-t pt-4">
          <Button onClick={onReset}>
            <Archive />
            {t("file.backupNew")}
          </Button>
        </div>
      </div>
    </section>
  )
}

function OutcomeStep({
  step,
  message,
  onRetry,
  onReset,
  t,
}: {
  step: "failed" | "cancelled"
  message: string
  onRetry: () => void
  onReset: () => void
  t: (key: string, vars?: Record<string, string | number>) => string
}) {
  const cancelled = step === "cancelled"
  return (
    <section className="mx-auto w-full max-w-2xl overflow-hidden rounded-lg border bg-card/30">
      <div className="grid gap-4 p-5 sm:p-7">
        <div className="flex items-start gap-3">
          {cancelled ? <XCircle className="mt-0.5 size-6 shrink-0 text-muted-foreground" /> : <XCircle className="text-destructive mt-0.5 size-6 shrink-0" />}
          <div className="min-w-0">
            <h2 className="text-base font-semibold">{t(cancelled ? "file.backupCancelled" : "file.backupFailed")}</h2>
            <p className="text-muted-foreground mt-1 break-words text-sm">{message || t(cancelled ? "file.backupCancelledDescription" : "file.backupFailedDescription")}</p>
          </div>
        </div>
        <div className="flex flex-wrap justify-end gap-2 border-t pt-4">
          <Button variant="ghost" onClick={onReset}>{t("file.backupBackToFolders")}</Button>
          {!cancelled && <Button variant="outline" onClick={onRetry}>{t("file.backupRetry")}</Button>}
        </div>
      </div>
    </section>
  )
}
