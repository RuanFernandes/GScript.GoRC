import {useCallback, useEffect, useMemo, useState} from "react"
import {Copy, FileCode2, Files, FolderOpen, FolderPlus, Globe2, Loader2, LockKeyhole, Pencil, Plus, RefreshCw, Save, Search, Trash2, Upload, X} from "lucide-react"
import {toast} from "sonner"

import {ConfirmDialog} from "@/components/ConfirmDialog"
import {ScriptGalleryAuthPanel} from "@/components/ScriptGalleryAuthPanel"
import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {useLanguage} from "@/hooks/useLanguage"
import {rcService} from "@/services/rcService"
import type {ScriptGalleryAuthState, ScriptGalleryProject, ScriptGalleryScript} from "@/types"

export type ScriptGalleryType = "weapon" | "class" | "npc"

interface ScriptGalleryDialogProps {
  open: boolean
  scriptType: ScriptGalleryType
  currentName: string
  currentContent: string
  onClose: () => void
  onInsert: (content: string) => void
}

type BusyAction = "createProject" | "saveProject" | "deleteProject" | "upload" | "updateCurrent" | "saveScript" | "deleteScript" | null
type ConfirmAction = "copyToCurrent" | "uploadCurrent" | "updateCurrent" | "deleteProject" | "deleteScript" | null

export function ScriptGalleryDialog({
  open,
  scriptType,
  currentName,
  currentContent,
  onClose,
  onInsert,
}: ScriptGalleryDialogProps) {
  const {t} = useLanguage()
  const [projects, setProjects] = useState<ScriptGalleryProject[]>([])
  const [authState, setAuthState] = useState<ScriptGalleryAuthState | null>(null)
  const [authLoading, setAuthLoading] = useState(false)
  const [selectedProjectID, setSelectedProjectID] = useState<string | null>(null)
  const [selectedScriptID, setSelectedScriptID] = useState<string | null>(null)
  const [selectedScript, setSelectedScript] = useState<ScriptGalleryScript | null>(null)
  const [selectedScriptContent, setSelectedScriptContent] = useState<string | null>(null)
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(false)
  const [loadingScript, setLoadingScript] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busyAction, setBusyAction] = useState<BusyAction>(null)
  const [confirmAction, setConfirmAction] = useState<ConfirmAction>(null)
  const [showCreateProject, setShowCreateProject] = useState(false)
  const [newProjectName, setNewProjectName] = useState("")
  const [newProjectDescription, setNewProjectDescription] = useState("")
  const [newProjectVisibility, setNewProjectVisibility] = useState("public")
  const [editingProject, setEditingProject] = useState(false)
  const [projectName, setProjectName] = useState("")
  const [projectDescription, setProjectDescription] = useState("")
  const [projectVisibility, setProjectVisibility] = useState("public")
  const [editingScript, setEditingScript] = useState(false)
  const [scriptName, setScriptName] = useState("")

  const selectedProject = useMemo(
    () => projects.find((project) => project.id === selectedProjectID) ?? null,
    [projects, selectedProjectID],
  )
  const selectedScripts = useMemo(
    () => scriptsForType(selectedProject, scriptType),
    [selectedProject, scriptType],
  )

  const loadAuthState = useCallback(async () => {
    setAuthLoading(true)
    try {
      setAuthState(await rcService.getScriptGalleryAuthState())
    } catch {
      setAuthState(null)
    } finally {
      setAuthLoading(false)
    }
  }, [])

  const loadProjects = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const next = await rcService.getScriptGalleryProjects(scriptType, query) ?? []
      setProjects(next)
      setSelectedProjectID((previous) => next.some((project) => project.id === previous) ? previous : next[0]?.id ?? null)
      setSelectedScriptID((previous) => next.some((project) => scriptsForType(project, scriptType).some((script) => script.id === previous)) ? previous : null)
    } catch {
      setError(t("gallery.loadFailed"))
    } finally {
      setLoading(false)
    }
  }, [query, scriptType, t])

  useEffect(() => {
    if (!open) return
    const timer = window.setTimeout(() => void loadProjects(), query ? 220 : 0)
    return () => window.clearTimeout(timer)
  }, [loadProjects, open, query])

  useEffect(() => {
    if (open) void loadAuthState()
  }, [loadAuthState, open])

  const handleAuthChanged = useCallback(async () => {
    await loadAuthState()
    await loadProjects()
  }, [loadAuthState, loadProjects])

  useEffect(() => {
    if (!open) return
    const previousOverflow = document.body.style.overflow
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !busyAction && !confirmAction) onClose()
    }
    document.body.style.overflow = "hidden"
    window.addEventListener("keydown", handleKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      window.removeEventListener("keydown", handleKeyDown)
    }
  }, [busyAction, confirmAction, onClose, open])

  useEffect(() => {
    if (!selectedProject) {
      setSelectedScriptID(null)
      setSelectedScript(null)
      setSelectedScriptContent(null)
      setEditingProject(false)
      setEditingScript(false)
      return
    }
    setProjectName(selectedProject.name)
    setProjectDescription(selectedProject.description)
    setProjectVisibility(selectedProject.visibility || "public")
    if (!selectedScripts.some((script) => script.id === selectedScriptID)) {
      setSelectedScriptID(null)
      setSelectedScript(null)
      setSelectedScriptContent(null)
      setEditingScript(false)
    }
  }, [selectedProject, selectedScriptID, selectedScripts])

  const selectProject = useCallback((project: ScriptGalleryProject) => {
    setSelectedProjectID(project.id)
    setSelectedScriptID(null)
    setSelectedScript(null)
    setSelectedScriptContent(null)
    setEditingProject(false)
    setEditingScript(false)
  }, [])

  const selectScript = useCallback(async (script: ScriptGalleryScript) => {
    setSelectedScriptID(script.id)
    setSelectedScript(script)
    setSelectedScriptContent(null)
    setScriptName(script.name)
    setEditingScript(false)
    setLoadingScript(true)
    try {
      const detail = await rcService.getScriptGalleryScript(script.id)
      setSelectedScript(detail)
      setSelectedScriptContent(detail.content ?? "")
      setScriptName(detail.name)
    } catch {
      setSelectedScript(null)
      setSelectedScriptContent(null)
      toast.error(t("gallery.loadFailed"))
    } finally {
      setLoadingScript(false)
    }
  }, [t])

  const createProject = useCallback(async () => {
    if (!authState?.authenticated) {
      toast.error(t("gallery.loginRequired"))
      return
    }
    if (!newProjectName.trim()) {
      toast.error(t("gallery.projectRequired"))
      return
    }
    setBusyAction("createProject")
    try {
      const project = await rcService.createScriptGalleryProject(newProjectName.trim(), newProjectDescription.trim(), newProjectVisibility)
      setProjects((previous) => [project, ...previous.filter((candidate) => candidate.id !== project.id)])
      setSelectedProjectID(project.id)
      setSelectedScriptID(null)
      setSelectedScript(null)
      setSelectedScriptContent(null)
      setNewProjectName("")
      setNewProjectDescription("")
      setNewProjectVisibility("public")
      setShowCreateProject(false)
      toast.success(t("gallery.created"))
    } catch {
      toast.error(t("gallery.createFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [authState?.authenticated, newProjectDescription, newProjectName, newProjectVisibility, t])

  const saveProject = useCallback(async () => {
    if (!selectedProject || !projectName.trim()) {
      toast.error(t("gallery.projectRequired"))
      return
    }
    setBusyAction("saveProject")
    try {
      const updated = await rcService.updateScriptGalleryProject(selectedProject.id, projectName.trim(), projectDescription.trim(), projectVisibility)
      setProjects((previous) => previous.map((project) => project.id === updated.id ? updated : project))
      setEditingProject(false)
      toast.success(t("gallery.saved"))
    } catch {
      toast.error(t("gallery.saveFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [projectDescription, projectName, projectVisibility, selectedProject, t])

  const deleteProject = useCallback(async () => {
    if (!selectedProject) return
    setBusyAction("deleteProject")
    try {
      await rcService.deleteScriptGalleryProject(selectedProject.id)
      setProjects((previous) => previous.filter((project) => project.id !== selectedProject.id))
      setSelectedProjectID(null)
      setSelectedScriptID(null)
      setSelectedScript(null)
      setSelectedScriptContent(null)
      setConfirmAction(null)
      toast.success(t("gallery.deleted"))
    } catch {
      toast.error(t("gallery.deleteFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [selectedProject, t])

  const uploadCurrentScript = useCallback(async () => {
    if (!selectedProject) return
    setBusyAction("upload")
    try {
      const script = await rcService.uploadScriptGalleryScript(selectedProject.id, scriptType, currentName.trim(), currentContent)
      setProjects((previous) => previous.map((project) => project.id === selectedProject.id ? addScriptToProject(project, script) : project))
      setSelectedScriptID(script.id)
      setSelectedScript(script)
      setSelectedScriptContent(script.content ?? currentContent)
      setScriptName(script.name)
      setConfirmAction(null)
      toast.success(t("gallery.added"))
    } catch {
      toast.error(t("gallery.uploadFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [currentContent, currentName, scriptType, selectedProject, t])

  const updateCurrentScript = useCallback(async () => {
    if (!selectedScript) return
    setBusyAction("updateCurrent")
    try {
      const updated = await rcService.updateScriptGalleryScript(selectedScript.id, currentName.trim() || selectedScript.name, currentContent)
      setProjects((previous) => previous.map((project) => replaceScriptInProject(project, updated)))
      setSelectedScript(updated)
      setSelectedScriptContent(updated.content ?? currentContent)
      setScriptName(updated.name)
      setConfirmAction(null)
      toast.success(t("gallery.updated"))
    } catch {
      toast.error(t("gallery.saveFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [currentContent, currentName, selectedScript, t])

  const saveScript = useCallback(async () => {
    if (!selectedScript || selectedScriptContent === null || !scriptName.trim()) {
      toast.error(t("gallery.scriptRequired"))
      return
    }
    setBusyAction("saveScript")
    try {
      const updated = await rcService.updateScriptGalleryScript(selectedScript.id, scriptName.trim(), selectedScriptContent)
      setProjects((previous) => previous.map((project) => replaceScriptInProject(project, updated)))
      setSelectedScript(updated)
      setSelectedScriptContent(updated.content ?? "")
      setEditingScript(false)
      toast.success(t("gallery.saved"))
    } catch {
      toast.error(t("gallery.saveFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [scriptName, selectedScript, selectedScriptContent, t])

  const deleteScript = useCallback(async () => {
    if (!selectedScript) return
    setBusyAction("deleteScript")
    try {
      await rcService.deleteScriptGalleryScript(selectedScript.id)
      setProjects((previous) => previous.map((project) => removeScriptFromProject(project, selectedScript.id)))
      setSelectedScriptID(null)
      setSelectedScript(null)
      setSelectedScriptContent(null)
      setConfirmAction(null)
      toast.success(t("gallery.deleted"))
    } catch {
      toast.error(t("gallery.deleteFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [selectedScript, t])

  const requestUploadCurrent = useCallback(() => {
    if (!authState?.authenticated) {
      toast.error(t("gallery.loginRequired"))
      return
    }
    if (!selectedProject) {
      toast.error(t("gallery.selectProject"))
      return
    }
    if (!selectedProject.owned) {
      toast.error(t("gallery.loginRequired"))
      return
    }
    if (!currentName.trim()) {
      toast.error(t("gallery.scriptRequired"))
      return
    }
    setConfirmAction("uploadCurrent")
  }, [authState?.authenticated, currentName, selectedProject, t])

  const requestUpdateCurrent = useCallback(() => {
    if (!authState?.authenticated || !selectedProject?.owned) {
      toast.error(t("gallery.loginRequired"))
      return
    }
    if (!selectedScript) {
      toast.error(t("gallery.selectScript"))
      return
    }
    if (!currentName.trim()) {
      toast.error(t("gallery.scriptRequired"))
      return
    }
    setConfirmAction("updateCurrent")
  }, [authState?.authenticated, currentName, selectedProject, selectedScript, t])

  const requestCopyToCurrent = useCallback(() => {
    if (!selectedScript || selectedScriptContent === null) {
      toast.error(t("gallery.loading"))
      return
    }
    setConfirmAction("copyToCurrent")
  }, [selectedScript, selectedScriptContent, t])

  if (!open) return null

  const dialogBusy = busyAction !== null || loadingScript
  const typeLabel = t(`gallery.type.${scriptType}`)
  const scriptCount = selectedProject ? totalScriptCount(selectedProject) : 0
  const confirmTitle = confirmAction === "deleteProject"
    ? t("gallery.deleteProjectTitle")
    : confirmAction === "deleteScript"
      ? t("gallery.deleteScriptTitle")
      : t("gallery.confirmTitle")
  const confirmDescription = confirmAction === "deleteProject"
    ? t("gallery.deleteProjectDescription")
    : confirmAction === "deleteScript"
      ? t("gallery.deleteScriptDescription")
      : confirmAction === "copyToCurrent"
        ? t("gallery.copyDescription")
        : confirmAction === "updateCurrent"
          ? t("gallery.updateDescription")
          : t("gallery.addDescription")
  const confirmLabel = confirmAction === "deleteProject"
    ? t("gallery.deleteProject")
    : confirmAction === "deleteScript"
      ? t("gallery.deleteScript")
      : confirmAction === "copyToCurrent"
        ? t("gallery.copyToCurrent")
        : confirmAction === "updateCurrent"
          ? t("gallery.updateCurrent")
          : t("gallery.addCurrent")
  const confirmIsDestructive = confirmAction === "deleteProject" || confirmAction === "deleteScript" || confirmAction === "copyToCurrent"

  return (
    <>
      <div
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-4"
        onMouseDown={(event) => {
          if (event.target === event.currentTarget && !dialogBusy && !confirmAction) onClose()
        }}
      >
        <section
          role="dialog"
          aria-modal="true"
          aria-labelledby="script-gallery-title"
          className="bg-background flex h-[min(820px,calc(100svh-2rem))] w-full max-w-6xl flex-col overflow-hidden rounded-xl border shadow-2xl"
        >
          <header className="flex shrink-0 items-center gap-3 border-b px-5 py-3.5">
            <div className="grid size-9 shrink-0 place-items-center rounded-lg border border-primary/30 bg-primary/10 text-primary">
              <FolderOpen className="size-4" />
            </div>
            <div className="min-w-0 flex-1">
              <h2 id="script-gallery-title" className="text-base font-semibold">{t("gallery.title")}</h2>
              <p className="text-muted-foreground truncate text-xs">{t("gallery.description", {type: typeLabel})}</p>
            </div>
            <Badge variant="outline">{typeLabel}</Badge>
            <Button variant="ghost" size="icon" className="size-8" onClick={onClose} disabled={dialogBusy || Boolean(confirmAction)} aria-label={t("common.close")}>
              <X className="size-4" />
            </Button>
          </header>

          <ScriptGalleryAuthPanel state={authState} loading={authLoading} onChanged={handleAuthChanged} />

          <div className="flex min-h-0 flex-1 flex-col">
            <div className="flex shrink-0 flex-wrap items-center gap-3 border-b bg-muted/10 px-5 py-3">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <Files className="text-primary size-4" />
                  <h3 className="text-sm font-semibold">{t("gallery.projects")}</h3>
                  <Badge variant="secondary" className="text-[10px]">{projects.length}</Badge>
                </div>
                <p className="text-muted-foreground mt-0.5 text-[11px]">{t("gallery.projectShelf", {type: typeLabel})}</p>
              </div>
              <div className="relative w-full sm:w-64">
                <Search className="text-muted-foreground pointer-events-none absolute left-2.5 top-2.5 size-4" />
                <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("gallery.search")} className="h-9 pl-8" aria-label={t("gallery.search")} />
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  if (!authState?.authenticated) {
                    toast.error(t("gallery.loginRequired"))
                    return
                  }
                  setShowCreateProject((value) => !value)
                }}
                disabled={dialogBusy}
              >
                <FolderPlus className="size-4" />{t("gallery.newProject")}
              </Button>
            </div>

            {showCreateProject && (
              <div className="grid shrink-0 gap-3 border-b bg-muted/5 px-5 py-4 md:grid-cols-[1fr_1.4fr_150px_auto] md:items-end">
                <div className="space-y-1">
                  <Label htmlFor="gallery-new-project-name">{t("gallery.projectName")}</Label>
                  <Input id="gallery-new-project-name" value={newProjectName} onChange={(event) => setNewProjectName(event.target.value)} maxLength={120} autoFocus />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="gallery-new-project-description">{t("gallery.projectDescription")}</Label>
                  <Input id="gallery-new-project-description" value={newProjectDescription} onChange={(event) => setNewProjectDescription(event.target.value)} maxLength={1000} />
                </div>
                <div className="space-y-1">
                  <Label htmlFor="gallery-new-project-visibility">{t("gallery.visibility")}</Label>
                  <select id="gallery-new-project-visibility" value={newProjectVisibility} onChange={(event) => setNewProjectVisibility(event.target.value)} className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">
                    <option value="public">{t("gallery.visibilityPublic")}</option>
                    <option value="private">{t("gallery.visibilityPrivate")}</option>
                  </select>
                </div>
                <Button size="sm" onClick={() => void createProject()} disabled={dialogBusy}>
                  {busyAction === "createProject" ? <Loader2 className="animate-spin" /> : <Plus />}{t("gallery.createProject")}
                </Button>
              </div>
            )}

            <div className="min-h-0 flex-1 overflow-y-auto">
              {loading ? (
                <div className="grid gap-3 p-5 sm:grid-cols-2 xl:grid-cols-3">
                  {[1, 2, 3].map((item) => <div key={item} className="h-32 animate-pulse rounded-lg border bg-muted/20" />)}
                </div>
              ) : error ? (
                <div className="flex min-h-56 items-center justify-center p-6 text-center">
                  <div className="max-w-sm space-y-3">
                    <p className="text-destructive text-sm">{error}</p>
                    <Button variant="outline" size="sm" onClick={() => void loadProjects()}><RefreshCw className="size-4" />{t("gallery.retry")}</Button>
                  </div>
                </div>
              ) : projects.length === 0 ? (
                <div className="flex min-h-64 flex-col items-center justify-center gap-3 p-8 text-center">
                  <div className="grid size-12 place-items-center rounded-xl border border-dashed text-muted-foreground">
                    <FolderOpen className="size-5" />
                  </div>
                  <div>
                    <h3 className="text-sm font-semibold">{t("gallery.noProjects")}</h3>
                    <p className="text-muted-foreground mt-1 max-w-sm text-xs">{t("gallery.noProjectsHint", {type: typeLabel})}</p>
                  </div>
                  <Button variant="outline" size="sm" onClick={() => setShowCreateProject(true)} disabled={dialogBusy || !authState?.authenticated}>
                    <Plus className="size-4" />{t("gallery.newProject")}
                  </Button>
                </div>
              ) : (
                <>
                  <div className="grid gap-3 p-5 sm:grid-cols-2 xl:grid-cols-3">
                    {projects.map((project) => {
                      const active = project.id === selectedProjectID
                      const count = scriptsForType(project, scriptType).length
                      return (
                        <button
                          key={project.id}
                          type="button"
                          aria-pressed={active}
                          onClick={() => selectProject(project)}
                          className={`group min-w-0 rounded-lg border p-4 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${active ? "border-primary bg-primary/10" : "border-border/70 bg-card hover:border-primary/50 hover:bg-muted/30"}`}
                        >
                          <div className="flex items-start gap-3">
                            <div className={`grid size-9 shrink-0 place-items-center rounded-lg border ${active ? "border-primary/40 bg-primary/15 text-primary" : "border-border bg-muted/30 text-muted-foreground group-hover:text-primary"}`}>
                              <FolderOpen className="size-4" />
                            </div>
                            <div className="min-w-0 flex-1">
                              <h4 className="truncate text-sm font-semibold">{project.name}</h4>
                              <p className="text-muted-foreground mt-1 line-clamp-2 min-h-8 text-xs">{project.description || t("gallery.noDescription")}</p>
                            </div>
                          </div>
                          <div className="text-muted-foreground mt-4 flex flex-wrap items-center gap-2 text-[11px]">
                            <Badge variant="outline" className="gap-1 text-[10px]">
                              {project.visibility === "private" ? <LockKeyhole className="size-3" /> : <Globe2 className="size-3" />}
                              {project.visibility === "private" ? t("gallery.visibilityPrivate") : t("gallery.visibilityPublic")}
                            </Badge>
                            <span className="inline-flex items-center gap-1"><FileCode2 className="size-3" />{count} {t("gallery.typeScripts", {type: typeLabel})}</span>
                            <span className="ml-auto truncate">{project.owned ? t("gallery.yourProject") : project.owner}</span>
                          </div>
                        </button>
                      )
                    })}
                  </div>

                  {selectedProject && (
                    <section className="border-t bg-muted/5">
                      <div className="border-b px-5 py-4">
                        {editingProject ? (
                          <div className="grid gap-3 md:grid-cols-[1fr_1.3fr_150px_auto] md:items-end">
                            <div className="space-y-1">
                              <Label htmlFor="gallery-project-name">{t("gallery.projectName")}</Label>
                              <Input id="gallery-project-name" value={projectName} onChange={(event) => setProjectName(event.target.value)} maxLength={120} />
                            </div>
                            <div className="space-y-1">
                              <Label htmlFor="gallery-project-description">{t("gallery.projectDescription")}</Label>
                              <Input id="gallery-project-description" value={projectDescription} onChange={(event) => setProjectDescription(event.target.value)} maxLength={1000} />
                            </div>
                            <div className="space-y-1">
                              <Label htmlFor="gallery-project-visibility">{t("gallery.visibility")}</Label>
                              <select id="gallery-project-visibility" value={projectVisibility} onChange={(event) => setProjectVisibility(event.target.value)} className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">
                                <option value="public">{t("gallery.visibilityPublic")}</option>
                                <option value="private">{t("gallery.visibilityPrivate")}</option>
                              </select>
                            </div>
                            <div className="flex gap-2">
                              <Button size="sm" onClick={() => void saveProject()} disabled={dialogBusy}><Save className="size-4" />{t("gallery.saveProject")}</Button>
                              <Button variant="ghost" size="sm" onClick={() => { setEditingProject(false); setProjectName(selectedProject.name); setProjectDescription(selectedProject.description); setProjectVisibility(selectedProject.visibility || "public") }} disabled={dialogBusy}>{t("common.cancel")}</Button>
                            </div>
                          </div>
                        ) : (
                          <div className="flex flex-wrap items-start gap-4">
                            <div className="grid size-11 shrink-0 place-items-center rounded-xl border border-primary/30 bg-primary/10 text-primary">
                              <FolderOpen className="size-5" />
                            </div>
                            <div className="min-w-0 flex-1">
                              <div className="flex flex-wrap items-center gap-2">
                                <h3 className="truncate text-lg font-semibold">{selectedProject.name}</h3>
                                {selectedProject.owned ? <Badge variant="outline">{t("gallery.yourProject")}</Badge> : <Badge variant="secondary">{selectedProject.owner}</Badge>}
                                <Badge variant="outline" className="gap-1">
                                  {selectedProject.visibility === "private" ? <LockKeyhole className="size-3" /> : <Globe2 className="size-3" />}
                                  {selectedProject.visibility === "private" ? t("gallery.visibilityPrivate") : t("gallery.visibilityPublic")}
                                </Badge>
                              </div>
                              <p className="text-muted-foreground mt-1 text-xs">{selectedProject.description || t("gallery.noDescription")}</p>
                              <div className="text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px]">
                                <span className="inline-flex items-center gap-1"><Files className="size-3" />{t("gallery.projectContents", {count: scriptCount})}</span>
                                <span>{t("gallery.updatedAt", {date: formatDate(selectedProject.updatedAt)})}</span>
                              </div>
                            </div>
                            {selectedProject.owned && (
                              <div className="flex flex-wrap gap-2">
                                <Button variant="outline" size="sm" onClick={() => setEditingProject(true)} disabled={dialogBusy}><Pencil className="size-4" />{t("gallery.editProject")}</Button>
                                <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={() => setConfirmAction("deleteProject")} disabled={dialogBusy}><Trash2 className="size-4" />{t("gallery.deleteProject")}</Button>
                              </div>
                            )}
                          </div>
                        )}
                      </div>

                      <div className="px-5 py-4">
                        <div className="mb-3 flex flex-wrap items-center gap-2">
                          <div className="min-w-0 flex-1">
                            <div className="flex items-center gap-2">
                              <FileCode2 className="text-primary size-4" />
                              <h4 className="text-sm font-semibold">{t("gallery.scripts")}</h4>
                              <Badge variant="secondary" className="text-[10px]">{selectedScripts.length}</Badge>
                            </div>
                            <p className="text-muted-foreground mt-0.5 text-[11px]">{t("gallery.scriptShelf", {type: typeLabel})}</p>
                          </div>
                          {selectedProject.owned && (
                            <div className="flex flex-wrap gap-2">
                              <Button variant="outline" size="sm" onClick={requestUploadCurrent} disabled={dialogBusy}><Upload className="size-4" />{t("gallery.addCurrent")}</Button>
                              {selectedScript && <Button size="sm" onClick={requestUpdateCurrent} disabled={dialogBusy}><RefreshCw className="size-4" />{t("gallery.updateCurrent")}</Button>}
                            </div>
                          )}
                        </div>

                        {selectedScripts.length === 0 ? (
                          <div className="rounded-lg border border-dashed p-8 text-center">
                            <FileCode2 className="text-muted-foreground mx-auto size-6" />
                            <p className="mt-2 text-sm font-medium">{t("gallery.noScripts", {type: typeLabel})}</p>
                            <p className="text-muted-foreground mt-1 text-xs">{t("gallery.noScriptsHint")}</p>
                          </div>
                        ) : (
                          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                            {selectedScripts.map((script) => {
                              const active = script.id === selectedScriptID
                              return (
                                <button
                                  key={script.id}
                                  type="button"
                                  aria-pressed={active}
                                  onClick={() => void selectScript(script)}
                                  className={`min-w-0 rounded-lg border p-3 text-left transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${active ? "border-primary bg-primary/10" : "border-border/70 bg-card hover:border-primary/50 hover:bg-muted/30"}`}
                                >
                                  <div className="flex items-start gap-2.5">
                                    <FileCode2 className={`mt-0.5 size-4 shrink-0 ${active ? "text-primary" : "text-muted-foreground"}`} />
                                    <div className="min-w-0 flex-1">
                                      <h5 className="truncate text-xs font-semibold">{script.name}</h5>
                                      <p className="text-muted-foreground mt-1 text-[10px]">{formatDate(script.updatedAt)}</p>
                                    </div>
                                    {active && <Badge variant="outline" className="text-[10px]">{t("gallery.selected")}</Badge>}
                                  </div>
                                </button>
                              )
                            })}
                          </div>
                        )}

                        {selectedScript && (
                          <div className="mt-4 rounded-lg border bg-background/70 p-4">
                            <div className="flex flex-wrap items-start gap-3">
                              <div className="grid size-9 shrink-0 place-items-center rounded-lg border bg-muted/30 text-primary">
                                <FileCode2 className="size-4" />
                              </div>
                              <div className="min-w-0 flex-1">
                                {editingScript ? (
                                  <Input value={scriptName} onChange={(event) => setScriptName(event.target.value)} maxLength={160} className="h-8 max-w-sm" aria-label={t("gallery.scriptName")} />
                                ) : <h5 className="truncate text-sm font-semibold">{selectedScript.name}</h5>}
                                <p className="text-muted-foreground mt-1 text-[11px]">{t("gallery.selectedScript")}</p>
                              </div>
                              <div className="flex flex-wrap gap-2">
                                <Button variant="outline" size="sm" onClick={requestCopyToCurrent} disabled={dialogBusy || selectedScriptContent === null}><Copy className="size-4" />{t("gallery.copyToCurrent")}</Button>
                                {selectedProject.owned && !editingScript && <Button variant="ghost" size="sm" onClick={() => setEditingScript(true)} disabled={dialogBusy}><Pencil className="size-4" />{t("gallery.renameScript")}</Button>}
                                {selectedProject.owned && editingScript && <Button size="sm" onClick={() => void saveScript()} disabled={dialogBusy || selectedScriptContent === null}><Save className="size-4" />{t("gallery.saveScript")}</Button>}
                                {selectedProject.owned && editingScript && <Button variant="ghost" size="sm" onClick={() => { setEditingScript(false); setScriptName(selectedScript.name) }} disabled={dialogBusy}>{t("common.cancel")}</Button>}
                                {selectedProject.owned && <Button variant="ghost" size="icon" className="size-8 text-destructive hover:text-destructive" onClick={() => setConfirmAction("deleteScript")} disabled={dialogBusy} aria-label={t("gallery.deleteScript")}><Trash2 className="size-4" /></Button>}
                              </div>
                            </div>

                            {loadingScript ? (
                              <div className="text-muted-foreground mt-4 flex items-center gap-2 text-xs"><Loader2 className="size-4 animate-spin" />{t("gallery.loading")}</div>
                            ) : selectedScriptContent !== null ? (
                              <div className="mt-4 grid gap-2 sm:grid-cols-3">
                                <ScriptStat label={t("gallery.scriptType")} value={typeLabel} />
                                <ScriptStat label={t("gallery.scriptLines")} value={String(countLines(selectedScriptContent))} />
                                <ScriptStat label={t("gallery.scriptUpdated")} value={formatDate(selectedScript.updatedAt)} />
                              </div>
                            ) : null}
                          </div>
                        )}
                      </div>
                    </section>
                  )}
                </>
              )}
            </div>
          </div>
        </section>
      </div>

      <ConfirmDialog
        open={confirmAction !== null}
        title={confirmTitle}
        description={confirmDescription}
        confirmLabel={confirmLabel}
        destructive={confirmIsDestructive}
        busy={busyAction !== null}
        onCancel={() => setConfirmAction(null)}
        onConfirm={() => {
          if (confirmAction === "copyToCurrent") {
            if (selectedScriptContent !== null) {
              onInsert(selectedScriptContent)
              setConfirmAction(null)
              toast.success(t("gallery.copied"))
            }
          } else if (confirmAction === "uploadCurrent") {
            void uploadCurrentScript()
          } else if (confirmAction === "updateCurrent") {
            void updateCurrentScript()
          } else if (confirmAction === "deleteProject") {
            void deleteProject()
          } else if (confirmAction === "deleteScript") {
            void deleteScript()
          }
        }}
      />
    </>
  )
}

function ScriptStat({label, value}: {label: string; value: string}) {
  return (
    <div className="rounded-md border bg-muted/20 px-3 py-2">
      <p className="text-muted-foreground text-[10px] uppercase tracking-wide">{label}</p>
      <p className="mt-1 truncate text-xs font-medium">{value}</p>
    </div>
  )
}

function scriptsForType(project: ScriptGalleryProject | null, type: ScriptGalleryType): ScriptGalleryScript[] {
  if (!project) return []
  if (type === "weapon") return project.weaponScripts ?? []
  if (type === "class") return project.classScripts ?? []
  return project.npcScripts ?? []
}

function addScriptToProject(project: ScriptGalleryProject, script: ScriptGalleryScript): ScriptGalleryProject {
  if (script.type === "weapon") return {...project, weaponScripts: [...(project.weaponScripts ?? []), script]}
  if (script.type === "class") return {...project, classScripts: [...(project.classScripts ?? []), script]}
  return {...project, npcScripts: [...(project.npcScripts ?? []), script]}
}

function replaceScriptInProject(project: ScriptGalleryProject, script: ScriptGalleryScript): ScriptGalleryProject {
  const replace = (scripts: ScriptGalleryScript[] | null | undefined) => (scripts ?? []).map((candidate) => candidate.id === script.id ? script : candidate)
  return {
    ...project,
    weaponScripts: replace(project.weaponScripts),
    classScripts: replace(project.classScripts),
    npcScripts: replace(project.npcScripts),
  }
}

function removeScriptFromProject(project: ScriptGalleryProject, scriptID: string): ScriptGalleryProject {
  return {
    ...project,
    weaponScripts: (project.weaponScripts ?? []).filter((script) => script.id !== scriptID),
    classScripts: (project.classScripts ?? []).filter((script) => script.id !== scriptID),
    npcScripts: (project.npcScripts ?? []).filter((script) => script.id !== scriptID),
  }
}

function totalScriptCount(project: ScriptGalleryProject): number {
  return (project.weaponScripts?.length ?? 0) + (project.classScripts?.length ?? 0) + (project.npcScripts?.length ?? 0)
}

function countLines(value: string): number {
  return value.length === 0 ? 0 : value.split(/\r?\n/).length
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString()
}
