// FileBrowserWindowScreen is the content of the external File Browser window
// (opened via App.OpenFileBrowser, URL "/#files"). It browses the server's
// remote filesystem with a recursive folder tree (left) and the current
// folder's files (right), and supports download (to the configured downloads
// folder or Save-As), upload (drag-in or native picker), rename, delete, and
// move. Server messages stream into a log.
//
// Folder tree model mirrors the reference client (TFileBrowserTree::addFolder):
// rc_filebrowser_start returns ALL accessible folders as flat glob patterns
// with full paths (e.g. "levels/*", "scripts/sub/*"). The tree is built
// client-side by splitting on "/" and stripping the "*" wildcard. Navigating a
// folder calls rc_filebrowser_cd with the cleaned path + trailing slash
// ("levels/"); passing the raw pattern does nothing (the bug in v1).
import {useEffect, useMemo, useRef, useState} from "react"
import {
  ChevronDown,
  ChevronRight,
  ChevronUp,
  Folder,
  FolderOpen,
  HardDriveDownload,
  Home,
  RefreshCw,
  Upload,
} from "lucide-react"
import {toast} from "sonner"

import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {ContextMenu} from "@/components/ContextMenu"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {ScrollArea} from "@/components/ui/scroll-area"
import {Skeleton} from "@/components/ui/skeleton"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import {useFileBrowser} from "@/hooks/useFileBrowser"
import {rcService} from "@/services/rcService"
import type {FileBrowserEntry, FileBrowserFolder} from "@/types"

// humanize turns a byte count into a compact human-readable size.
function humanize(bytes: number): string {
  if (!bytes || bytes < 0) return "—"
  const units = ["B", "KB", "MB", "GB"]
  let v = bytes
  let i = 0
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

// formatDate turns a grclib epoch (seconds) into a locale string, "" when 0.
function formatDate(ts: number): string {
  if (!ts) return "—"
  return new Date(ts * 1000).toLocaleString()
}

// basename is the last path segment (used for the file name column).
function basename(path: string): string {
  const i = path.lastIndexOf("/")
  return i >= 0 ? path.slice(i + 1) : path
}

// globMatch tests whether a filename matches a simple glob (only '*' wildcard).
// e.g. globMatch("tb_hello.txt", "tb_*") → true.
function globMatch(name: string, glob: string): boolean {
  if (!glob) return true
  const re = "^" + glob.replace(/[.+^${}()|[\]\\]/g, "\\$&").replace(/\*/g, ".*") + "$"
  return new RegExp(re).test(name)
}

// trimFolder normalizes a server folder path for comparison: strip a trailing
// slash so "levels/" and "levels" compare equal.
function trimFolder(folder: string): string {
  return folder.replace(/\/+$/, "")
}

// TreeNode is one node of the recursive folder tree, built from the flat
// pattern list the server returns at start.
interface TreeNode {
  name: string // display label, e.g. "levels/"
  path: string // full cleaned folder path (no trailing slash), e.g. "levels/users/x/other"
  rights?: string // access rights for this folder
  globs?: string[] // file-prefix globs accessible here (e.g. "tb_*") — not folders
  children: TreeNode[]
}

// buildTree turns the server's access patterns into a folder tree. Each pattern
// is "<folder/path>/<fileglob>" (e.g. "levels/users/x/other/tb_*"): the LAST
// segment is a file-prefix glob when it contains '*', NOT a folder — it's the
// access filter for the folder, so it is stripped. The remaining path is the
// folder. Rights + glob attach to the folder leaf.
function buildTree(folders: FileBrowserFolder[]): TreeNode[] {
  const root: TreeNode[] = []
  for (const f of folders) {
    const segs = (f.pattern ?? "").split("/").filter(Boolean)
    // Drop a trailing file glob (segment with '*') — it filters files in the
    // folder, it is not a folder itself.
    let glob = ""
    if (segs.length > 0 && segs[segs.length - 1].includes("*")) {
      glob = segs.pop() as string
    }
    const folderPath = segs.join("/")
    if (!folderPath) continue // glob-only (root-level file access): nothing to nest
    const parts = folderPath.split("/")
    let level = root
    let acc = ""
    parts.forEach((part, i) => {
      acc = acc ? acc + "/" + part : part
      let node = level.find((n) => n.path === acc)
      if (!node) {
        node = {name: part + "/", path: acc, children: []}
        level.push(node)
      }
      if (i === parts.length - 1) {
        if (f.rights) node.rights = f.rights
        if (glob) {
          node.globs = node.globs ?? []
          if (!node.globs.includes(glob)) node.globs.push(glob)
        }
      }
      level = node.children
    })
  }
  // Stable sort: folders with children first, then alphabetical.
  const sortNodes = (nodes: TreeNode[]) => {
    nodes.sort((a, b) =>
      a.children.length === 0 === (b.children.length === 0)
        ? a.name.localeCompare(b.name)
        : a.children.length === 0
          ? 1
          : -1,
    )
    nodes.forEach((n) => sortNodes(n.children))
  }
  sortNodes(root)
  return root
}

export function FileBrowserWindowScreen() {
  const fb = useFileBrowser(rcService)
  const [dragging, setDragging] = useState(false)
  const [query, setQuery] = useState("")

  // Expanded node paths (by cleaned path). Default-expand the top level so the
  // folder structure is visible immediately.
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [autoExpandedRoot, setAutoExpandedRoot] = useState(false)

  // Dialog state for rename / delete / move.
  const [renameTarget, setRenameTarget] = useState<FileBrowserEntry | null>(null)
  const [renameValue, setRenameValue] = useState("")
  const [deleteTarget, setDeleteTarget] = useState<FileBrowserEntry | null>(null)
  const [moveTarget, setMoveTarget] = useState<FileBrowserEntry | null>(null)
  const [moveDest, setMoveDest] = useState("")
  const [moveName, setMoveName] = useState("")

  // Selected file (single-click highlight) + right-click context menu target.
  const [selected, setSelected] = useState<string | null>(null)
  const [ctx, setCtx] = useState<{x: number; y: number; entry: FileBrowserEntry} | null>(null)

  const tree = useMemo(() => buildTree(fb.folders), [fb.folders])

  // Flatten the folder tree into move destinations: cleaned paths + their file
  // globs (for rights validation). Uses the TREE (glob-stripped) paths, so the
  // phantom tb_/ from raw patterns never appears as a destination.
  const destFolders = useMemo(() => {
    const out: {path: string; globs?: string[]}[] = []
    const walk = (nodes: TreeNode[]) => {
      for (const n of nodes) {
        out.push({path: n.path, globs: n.globs})
        walk(n.children)
      }
    }
    walk(tree)
    return out
  }, [tree])

  // Live file filter for the current folder (case-insensitive name match).
  const filteredFiles = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return fb.files
    return fb.files.filter((f) => basename(f.path).toLowerCase().includes(q))
  }, [fb.files, query])

  // Auto-expand the top-level nodes once the folder list first arrives.
  useEffect(() => {
    if (!autoExpandedRoot && tree.length > 0) {
      setExpanded((prev) => {
        const next = new Set(prev)
        tree.forEach((n) => next.add(n.path))
        return next
      })
      setAutoExpandedRoot(true)
    }
  }, [tree, autoExpandedRoot])

  const currentClean = trimFolder(fb.currentFolder)

  const toggle = (path: string) =>
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(path)) next.delete(path)
      else next.add(path)
      return next
    })

  // cdPath navigates a folder node: cleaned path + trailing slash (matches the
  // reference onFolderSelected, which appends "/" before rc_filebrowser_cd).
  const cdPath = (path: string) => fb.cd(path ? path + "/" : "")

  const openRename = (entry: FileBrowserEntry) => {
    setRenameTarget(entry)
    setRenameValue(basename(entry.path))
  }

  const doRename = async () => {
    const target = renameTarget
    const value = renameValue.trim()
    if (!target || !value) return
    setRenameTarget(null)
    await fb.rename(target, value)
  }

  const openMove = (entry: FileBrowserEntry) => {
    setMoveTarget(entry)
    setMoveDest(destFolders[0]?.path ?? "")
    setMoveName(basename(entry.path))
  }

  const doMove = async () => {
    const target = moveTarget
    const dest = moveDest
    const name = moveName.trim()
    if (!target || !dest) return
    setMoveTarget(null)
    await fb.move(target, dest, name)
  }

  // Rights validation for the move dialog: a file name must match at least one
  // of the destination folder's file globs (e.g. tb_*). No globs = broad access.
  const moveSelected = destFolders.find((d) => d.path === moveDest)
  const moveGlobs = moveSelected?.globs ?? []
  const nameOk = moveGlobs.length === 0 || moveGlobs.some((g) => globMatch(moveName, g))
  const moveValid = !!moveDest && moveName.trim().length > 0 && nameOk

  const doDelete = async () => {
    const target = deleteTarget
    if (!target) return
    setDeleteTarget(null)
    await fb.remove(target)
  }

  // Double-click a file: download + open by type (media / text / db / download).
  const openFile = async (entry: FileBrowserEntry) => {
    if (entry.isDirectory) {
      fb.cd(entry.path)
      return
    }
    try {
      const kind = await rcService.openRemoteFile(entry.path)
      if (kind === "media") toast.success("Opened in default app")
      else if (kind === "text") toast.success("Opened in editor")
      else if (kind === "database") toast.success("Opened SQLite explorer")
    } catch (err) {
      toast.error("Could not open file", {description: String(err)})
    }
  }

  // Force-open any file as text in the Monaco editor (even binary).
  const openAsText = async (entry: FileBrowserEntry) => {
    try {
      await rcService.openRemoteFileAsText(entry.path)
      toast.success("Opened as text")
    } catch (err) {
      toast.error("Could not open file", {description: String(err)})
    }
  }

  // Build the right-click context menu items for an entry.
  const contextItems = (entry: FileBrowserEntry) => {
    const items = []
    if (!entry.isDirectory) {
      items.push({label: "Open", onSelect: () => openFile(entry)})
      items.push({label: "Open as Text", onSelect: () => openAsText(entry)})
      items.push({separator: true, label: "", onSelect: () => {}})
      items.push({label: "Download", disabled: !hasDownloadDir, onSelect: () => fb.download(entry)})
      items.push({label: "Save As…", onSelect: () => fb.download(entry, true)})
      items.push({label: "Move…", onSelect: () => openMove(entry)})
      items.push({separator: true, label: "", onSelect: () => {}})
    } else {
      items.push({label: "Open", onSelect: () => fb.cd(entry.path)})
    }
    items.push({label: "Rename", onSelect: () => openRename(entry)})
    items.push({label: "Delete", danger: true, onSelect: () => setDeleteTarget(entry)})
    return items
  }

  const onDrop = async (e: React.DragEvent) => {
    e.preventDefault()
    setDragging(false)
    if (e.dataTransfer.files?.length) await fb.uploadFiles(e.dataTransfer.files)
  }

  const hasDownloadDir = !!fb.config.downloadDir

  return (
    <div className="bg-background flex h-svh flex-col">
      {/* Toolbar: breadcrumb + actions */}
      <header className="flex flex-wrap items-center gap-2 border-b px-3 py-2">
        <Button variant="ghost" size="sm" onClick={() => fb.cd("")} title="Root">
          <Home />
        </Button>
        <Breadcrumb path={fb.currentFolder} onNavigate={fb.cd} />
        <div className="ml-auto flex items-center gap-2">
          {fb.maxUpload > 0 && (
            <span className="text-muted-foreground hidden text-xs sm:inline">
              Max upload {humanize(fb.maxUpload)}
            </span>
          )}
          <Button variant="outline" size="sm" onClick={fb.uploadViaDialog}>
            <Upload />
            Upload
          </Button>
          <Button variant="outline" size="sm" onClick={fb.refresh} disabled={fb.loading}>
            <RefreshCw className={fb.loading ? "animate-spin" : undefined} />
            Refresh
          </Button>
        </div>
      </header>

      {/* Search: live-filter files in the current folder. */}
      <div className="flex items-center gap-2 border-b px-3 py-1.5">
        <Input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder={`Search in ${fb.currentFolder || "root"}…`}
          className="h-8 text-sm"
        />
        {query && (
          <span className="text-muted-foreground shrink-0 text-xs">
            {filteredFiles.length}/{fb.files.length}
          </span>
        )}
      </div>

      {!hasDownloadDir && (
        <div className="border-b bg-amber-500/10 px-3 py-1.5 text-xs text-amber-200">
          No downloads folder set — downloads are disabled. Set one in Settings → Files.
        </div>
      )}

      {/* Main: folder tree | files */}
      <div className="flex min-h-0 flex-1">
        <aside className="w-60 shrink-0 border-r">
          <ScrollArea className="h-full">
            <div className="p-1.5">
              {!fb.loaded ? (
                <div className="flex flex-col gap-1.5 p-1">
                  {Array.from({length: 8}).map((_, i) => (
                    <div key={i} className="flex items-center gap-1.5" style={{paddingLeft: `${(i % 3) * 12}px`}}>
                      <Skeleton className="size-3.5" />
                      <Skeleton className="h-3.5" style={{width: `${45 + ((i * 29) % 40)}%`}} />
                    </div>
                  ))}
                </div>
              ) : tree.length === 0 ? (
                <p className="text-muted-foreground p-2 text-xs">No folders.</p>
              ) : (
                tree.map((node) => (
                  <FolderNode
                    key={node.path}
                    node={node}
                    depth={0}
                    expanded={expanded}
                    current={currentClean}
                    onToggle={toggle}
                    onOpen={cdPath}
                  />
                ))
              )}
            </div>
          </ScrollArea>
        </aside>

        <section
          className="relative min-w-0 flex-1"
          onDragOver={(e) => {
            e.preventDefault()
            setDragging(true)
          }}
          onDragLeave={(e) => {
            if (e.currentTarget === e.target) setDragging(false)
          }}
          onDrop={onDrop}
        >
          <FileTable
            files={filteredFiles}
            loaded={fb.loaded}
            selected={selected}
            onSelect={(path) => setSelected(path)}
            onContextMenu={(entry, x, y) => setCtx({x, y, entry})}
            onOpen={openFile}
          />
          {dragging && (
            <div className="bg-primary/10 pointer-events-none absolute inset-0 flex items-center justify-center border-2 border-dashed">
              <div className="text-primary flex flex-col items-center gap-1 text-sm font-medium">
                <Upload />
                Drop to upload to {fb.currentFolder || "root"}
              </div>
            </div>
          )}
        </section>
      </div>

      {/* Server message log — always visible (status / errors / diagnostics). */}
      <div className="h-32 shrink-0 border-t">
        <ScrollArea className="h-full">
          <div className="space-y-0.5 p-2 font-mono text-xs">
            {fb.messages.length === 0 ? (
              <p className="text-muted-foreground">No messages.</p>
            ) : (
              fb.messages.map((m, i) => (
                <div key={i} className="text-muted-foreground">
                  {m}
                </div>
              ))
            )}
          </div>
        </ScrollArea>
      </div>

      {/* Rename dialog */}
      <AlertDialog open={renameTarget !== null} onOpenChange={(v) => !v && setRenameTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Rename</AlertDialogTitle>
            <AlertDialogDescription>Enter a new name for this file.</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="rename-value">Name</Label>
            <Input
              id="rename-value"
              value={renameValue}
              autoFocus
              onChange={(e) => setRenameValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") doRename()
              }}
            />
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={doRename} disabled={!renameValue.trim()}>
              Rename
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Move dialog */}
      <AlertDialog open={moveTarget !== null} onOpenChange={(v) => !v && setMoveTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Move “{moveTarget ? basename(moveTarget.path) : ""}”</AlertDialogTitle>
            <AlertDialogDescription>Choose a destination folder.</AlertDialogDescription>
          </AlertDialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="move-dest">Destination folder</Label>
            <FolderAutocomplete
              value={moveDest}
              options={destFolders.map((d) => d.path)}
              onChange={setMoveDest}
            />
            <Label htmlFor="move-name">File name in destination</Label>
            <Input
              id="move-name"
              value={moveName}
              onChange={(e) => setMoveName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && moveValid) doMove()
              }}
            />
            {moveGlobs.length > 0 && (
              <p className="text-xs text-muted-foreground">
                You have write rights to files in this folder matching: {moveGlobs.join(", ")}
              </p>
            )}
            {!nameOk && moveName.trim().length > 0 && (
              <p className="text-xs text-destructive">
                Please fix your new file name to match your rights.
              </p>
            )}
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={doMove} disabled={!moveValid}>
              Move
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Delete confirm */}
      <AlertDialog open={deleteTarget !== null} onOpenChange={(v) => !v && setDeleteTarget(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete “{deleteTarget ? basename(deleteTarget.path) : ""}”?</AlertDialogTitle>
            <AlertDialogDescription>
              This permanently removes it from the server. This cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={doDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {ctx && (
        <ContextMenu x={ctx.x} y={ctx.y} items={contextItems(ctx.entry)} onClose={() => setCtx(null)} />
      )}
    </div>
  )
}

// Breadcrumb renders the current-folder path as clickable segments.
function Breadcrumb({path, onNavigate}: {path: string; onNavigate: (folder: string) => void}) {
  const clean = trimFolder(path)
  const parts = clean.split("/").filter(Boolean)
  return (
    <nav className="text-muted-foreground flex min-w-0 items-center gap-0.5 text-sm">
      <span className="text-foreground/80">/</span>
      {parts.length === 0 && <span className="text-muted-foreground">(root)</span>}
      {parts.map((part, i) => {
        const p = parts.slice(0, i + 1).join("/")
        const last = i === parts.length - 1
        return (
          <span key={p} className="flex min-w-0 items-center gap-0.5">
            <ChevronRight className="h-3.5 w-3.5 shrink-0" />
            {last ? (
              <span className="text-foreground truncate font-medium">{part}</span>
            ) : (
              <button className="hover:text-foreground truncate hover:underline" onClick={() => onNavigate(p + "/")}>
                {part}
              </button>
            )}
          </span>
        )
      })}
    </nav>
  )
}

// FolderAutocomplete is a type-to-filter folder picker (dark). Native <select>
// popups render light in WebView2 and show every folder at once; this filters as
// you type and selects on click / Enter.
function FolderAutocomplete({
  value,
  options,
  onChange,
}: {
  value: string
  options: string[]
  onChange: (v: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [draft, setDraft] = useState<string | null>(null)
  const ref = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false)
    }
    window.addEventListener("mousedown", onDown)
    window.addEventListener("keydown", onKey)
    return () => {
      window.removeEventListener("mousedown", onDown)
      window.removeEventListener("keydown", onKey)
    }
  }, [open])

  const shown = draft ?? value
  const q = shown.toLowerCase()
  const matches = options.filter((o) => o.toLowerCase().includes(q)).slice(0, 50)

  // On blur, resolve whatever was typed to a real folder: exact match first,
  // else a single case-insensitive match, else keep the typed text (validation /
  // the server reject an invalid path). So typing a full path without clicking a
  // suggestion still commits it.
  const commit = () => {
    const text = draft
    setDraft(null)
    setOpen(false)
    if (text == null) return
    const t = text.trim()
    if (!t) return
    let match = options.find((o) => o === t)
    if (!match) {
      const ci = options.filter((o) => o.toLowerCase() === t.toLowerCase())
      if (ci.length === 1) match = ci[0]
    }
    onChange(match ?? t)
  }

  return (
    <div ref={ref} className="relative">
      <input
        className="border-input bg-input focus-visible:ring-ring h-9 w-full rounded-md border px-3 text-sm"
        value={shown}
        placeholder="Type to search folders…"
        onChange={(e) => {
          setDraft(e.target.value)
          setOpen(true)
        }}
        onFocus={() => setOpen(true)}
        onBlur={commit}
        onKeyDown={(e) => {
          if (e.key === "Enter" && matches[0] !== undefined) {
            onChange(matches[0])
            setDraft(null)
            setOpen(false)
          } else if (e.key === "Enter") {
            commit()
          }
        }}
      />
      {open && (
        <ul
          className="bg-popover absolute z-20 mt-1 max-h-64 w-full overflow-auto rounded-md border shadow-lg"
          // Prevent blur when clicking a suggestion so its onClick fires.
          onMouseDown={(e) => e.preventDefault()}
        >
          {matches.length === 0 && (
            <li className="text-muted-foreground px-3 py-2 text-sm">No matching folders</li>
          )}
          {matches.map((o) => (
            <li key={o || "__root__"}>
              <button
                type="button"
                className={`hover:bg-accent flex w-full items-center px-3 py-1.5 text-left text-sm ${
                  o === value ? "font-medium text-foreground" : ""
                }`}
                onClick={() => {
                  onChange(o)
                  setDraft(null)
                  setOpen(false)
                }}
              >
                <span className="truncate">{o || "(root)"}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

// FolderNode renders one tree node + its children recursively.
function FolderNode({
  node,
  depth,
  expanded,
  current,
  onToggle,
  onOpen,
}: {
  node: TreeNode
  depth: number
  expanded: Set<string>
  current: string
  onToggle: (path: string) => void
  onOpen: (path: string) => void
}) {
  const isOpen = expanded.has(node.path)
  const hasChildren = node.children.length > 0
  const active = current === node.path
  return (
    <div>
      <div
        className={`group flex items-center gap-1 rounded px-1 py-1 text-sm hover:bg-accent ${
          active ? "bg-accent" : ""
        }`}
        style={{paddingLeft: depth * 12 + 4}}
      >
        <button
          className="text-muted-foreground flex h-4 w-4 shrink-0 items-center justify-center hover:text-foreground"
          onClick={(e) => {
            e.stopPropagation()
            if (hasChildren) onToggle(node.path)
          }}
          title={hasChildren ? (isOpen ? "Collapse" : "Expand") : ""}
        >
          {hasChildren ? (
            isOpen ? (
              <ChevronDown className="h-3.5 w-3.5" />
            ) : (
              <ChevronRight className="h-3.5 w-3.5" />
            )
          ) : null}
        </button>
        <button
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
          onClick={() => onOpen(node.path)}
          title={
            node.path +
            "/" +
            (node.globs && node.globs.length ? `  (files: ${node.globs.join(", ")})` : "")
          }
        >
          {active ? (
            <FolderOpen className="h-4 w-4 shrink-0 text-sky-400" />
          ) : (
            <Folder className="h-4 w-4 shrink-0 text-muted-foreground" />
          )}
          <span className="truncate">{node.name}</span>
          {node.rights && (
            <Badge variant="secondary" className="ml-auto h-4 px-1 text-[10px]">
              {node.rights}
            </Badge>
          )}
        </button>
      </div>
      {isOpen &&
        node.children.map((child) => (
          <FolderNode
            key={child.path}
            node={child}
            depth={depth + 1}
            expanded={expanded}
            current={current}
            onToggle={onToggle}
            onOpen={onOpen}
          />
        ))}
    </div>
  )
}

// SortKey is which column the file table is sorted by.
type SortKey = "name" | "size" | "modified" | "rights"
type SortDir = "asc" | "desc"

// Column widths (px) for the resizable file-table columns.
const DEFAULT_WIDTHS: Record<SortKey, number> = {
  name: 320,
  size: 84,
  modified: 168,
  rights: 84,
}
const MIN_WIDTH = 48

// FileTable renders the right-pane file listing: fixed layout (cells truncate
// instead of overflowing), drag-to-resize column borders, and click-to-sort
// headers (Name / Size / Modified / Rights). Row interactions:
//   - left click: select (highlight)
//   - right click: context menu (built by the parent)
//   - double click: open by type (parent)
function FileTable({
  files,
  loaded,
  selected,
  onSelect,
  onContextMenu,
  onOpen,
}: {
  files: FileBrowserEntry[]
  loaded?: boolean
  selected: string | null
  onSelect: (path: string) => void
  onContextMenu: (entry: FileBrowserEntry, x: number, y: number) => void
  onOpen: (entry: FileBrowserEntry) => void
}) {
  const [widths, setWidths] = useState({...DEFAULT_WIDTHS})
  const [sort, setSort] = useState<{key: SortKey; dir: SortDir}>({key: "name", dir: "asc"})

  const sorted = useMemo(() => {
    const dir = sort.dir === "asc" ? 1 : -1
    const value = (e: FileBrowserEntry): string | number => {
      switch (sort.key) {
        case "name":
          return basename(e.path).toLowerCase()
        case "size":
          return e.size
        case "modified":
          return e.modified
        case "rights":
          return e.rights ?? ""
      }
    }
    return [...files].sort((a, b) => {
      const va = value(a)
      const vb = value(b)
      if (va < vb) return -1 * dir
      if (va > vb) return 1 * dir
      return 0
    })
  }, [files, sort])

  const toggleSort = (key: SortKey) =>
    setSort((cur) => (cur.key === key ? {key, dir: cur.dir === "asc" ? "desc" : "asc"} : {key, dir: "asc"}))

  // Drag a column border to resize that column.
  const startResize = (key: keyof typeof widths, e: React.MouseEvent) => {
    e.preventDefault()
    e.stopPropagation()
    const startX = e.clientX
    const startW = widths[key]
    const onMove = (ev: MouseEvent) => {
      const next = Math.max(MIN_WIDTH, startW + (ev.clientX - startX))
      setWidths((w) => ({...w, [key]: next}))
    }
    const onUp = () => {
      window.removeEventListener("mousemove", onMove)
      window.removeEventListener("mouseup", onUp)
    }
    window.addEventListener("mousemove", onMove)
    window.addEventListener("mouseup", onUp)
  }

  if (files.length === 0) {
    if (!loaded) {
      // Initial load: mirror a few file rows instead of the premature "Empty
      // folder." final-state message.
      return (
        <div className="flex flex-col gap-1 p-2">
          {Array.from({length: 7}).map((_, i) => (
            <div key={i} className="flex items-center gap-2 py-1">
              <Skeleton className="size-4" />
              <Skeleton className="h-4 flex-1" style={{maxWidth: `${40 + ((i * 37) % 45)}%`}} />
              <Skeleton className="h-4 w-16" />
              <Skeleton className="h-4 w-20" />
            </div>
          ))}
        </div>
      )
    }
    return (
      <div className="text-muted-foreground flex h-full items-center justify-center text-sm">
        Empty folder.
      </div>
    )
  }

  const colWidth = (k: keyof typeof widths) => `${widths[k]}px`
  const SortIcon = ({k}: {k: SortKey}) =>
    sort.key === k ? (
      sort.dir === "asc" ? (
        <ChevronDown className="h-3 w-3" />
      ) : (
        <ChevronUp className="h-3 w-3" />
      )
    ) : null

  return (
    <ScrollArea className="h-full">
      <table className="table-fixed w-full text-sm">
        <colgroup>
          <col style={{width: colWidth("name")}} />
          <col style={{width: colWidth("size")}} />
          <col style={{width: colWidth("modified")}} />
          <col style={{width: colWidth("rights")}} />
        </colgroup>
        <thead className="bg-muted/40 sticky top-0">
          <tr className="text-muted-foreground select-none text-xs">
            <Th label="Name" active={sort.key === "name"} onClick={() => toggleSort("name")} icon={<SortIcon k="name" />} onResize={(e) => startResize("name", e)} />
            <Th label="Size" align="right" active={sort.key === "size"} onClick={() => toggleSort("size")} icon={<SortIcon k="size" />} onResize={(e) => startResize("size", e)} />
            <Th label="Modified" active={sort.key === "modified"} onClick={() => toggleSort("modified")} icon={<SortIcon k="modified" />} onResize={(e) => startResize("modified", e)} />
            <Th label="Rights" align="right" active={sort.key === "rights"} onClick={() => toggleSort("rights")} icon={<SortIcon k="rights" />} onResize={(e) => startResize("rights", e)} />
          </tr>
        </thead>
        <tbody>
          {sorted.map((entry) => (
            <tr
              key={entry.path}
              className={`cursor-default border-b border-white/5 last:border-0 ${
                selected === entry.path ? "bg-accent/70" : "hover:bg-accent/50"
              }`}
              onClick={() => onSelect(entry.path)}
              onDoubleClick={() => onOpen(entry)}
              onContextMenu={(e) => {
                e.preventDefault()
                onContextMenu(entry, e.clientX, e.clientY)
              }}
            >
              <td className="overflow-hidden px-3 py-1.5">
                <div className="flex min-w-0 items-center gap-2 text-left" title={basename(entry.path)}>
                  {entry.isDirectory ? (
                    <Folder className="h-4 w-4 shrink-0 text-sky-400" />
                  ) : (
                    <HardDriveDownload className="text-muted-foreground h-4 w-4 shrink-0" />
                  )}
                  <span className="truncate">{basename(entry.path)}</span>
                </div>
              </td>
              <td className="text-muted-foreground overflow-hidden px-3 py-1.5 text-right tabular-nums">
                <span className="block truncate">{entry.isDirectory ? "—" : humanize(entry.size)}</span>
              </td>
              <td className="text-muted-foreground overflow-hidden px-3 py-1.5 text-xs">
                <span className="block truncate">{formatDate(entry.modified)}</span>
              </td>
              <td className="overflow-hidden px-3 py-1.5 text-right">
                {entry.rights && (
                  <Badge variant="secondary" className="text-[10px]">
                    {entry.rights}
                  </Badge>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </ScrollArea>
  )
}

// Th is a sortable, resizable table header cell. Click the label to sort; drag
// the right edge handle to resize.
function Th({
  label,
  align = "left",
  active,
  onClick,
  icon,
  onResize,
}: {
  label: string
  align?: "left" | "right"
  active?: boolean
  onClick?: () => void
  icon?: React.ReactNode
  onResize?: (e: React.MouseEvent) => void
}) {
  return (
    <th className="relative px-3 py-2 font-medium">
      {onClick ? (
        <button
          className={`flex w-full items-center gap-1 hover:text-foreground ${align === "right" ? "justify-end" : ""} ${
            active ? "text-foreground" : ""
          }`}
          onClick={onClick}
        >
          {align === "right" && icon}
          <span>{label}</span>
          {align === "left" && icon}
        </button>
      ) : (
        <div className={align === "right" ? "text-right" : ""}>{label}</div>
      )}
      {onResize && (
        <span
          className="absolute right-0 top-0 h-full w-1.5 cursor-col-resize hover:bg-primary/40"
          onMouseDown={onResize}
        />
      )}
    </th>
  )
}
