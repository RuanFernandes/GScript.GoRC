import {useCallback, useEffect, useMemo, useState} from "react"
import Editor from "@monaco-editor/react"
import {ArrowDownToLine, FolderPlus, Loader2, Pencil, Plus, Save, Search, Trash2, Upload, X} from "lucide-react"
import {toast} from "sonner"

import {ConfirmDialog} from "@/components/ConfirmDialog"
import {ScriptGalleryAuthPanel} from "@/components/ScriptGalleryAuthPanel"
import {Badge} from "@/components/ui/badge"
import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {useCodingSettings} from "@/hooks/useCodingSettings"
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

type BusyAction = "load" | "createProject" | "saveProject" | "deleteProject" | "upload" | "saveScript" | "deleteScript" | null

export function ScriptGalleryDialog({
  open,
  scriptType,
  currentName,
  currentContent,
  onClose,
  onInsert,
}: ScriptGalleryDialogProps) {
  const {t} = useLanguage()
  const {settings} = useCodingSettings()
  const [projects, setProjects] = useState<ScriptGalleryProject[]>([])
  const [authState, setAuthState] = useState<ScriptGalleryAuthState | null>(null)
  const [authLoading, setAuthLoading] = useState(false)
  const [selectedProjectID, setSelectedProjectID] = useState<string | null>(null)
  const [selectedScriptID, setSelectedScriptID] = useState<string | null>(null)
  const [selectedScript, setSelectedScript] = useState<ScriptGalleryScript | null>(null)
  const [query, setQuery] = useState("")
  const [loading, setLoading] = useState(false)
  const [loadingScript, setLoadingScript] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busyAction, setBusyAction] = useState<BusyAction>(null)
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
  const [scriptContent, setScriptContent] = useState<string | null>(null)
  const [confirmDelete, setConfirmDelete] = useState<"project" | "script" | null>(null)

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
      if (event.key === "Escape" && !busyAction) onClose()
    }
    document.body.style.overflow = "hidden"
    window.addEventListener("keydown", handleKeyDown)
    return () => {
      document.body.style.overflow = previousOverflow
      window.removeEventListener("keydown", handleKeyDown)
    }
  }, [busyAction, onClose, open])

  useEffect(() => {
    if (!selectedProject) {
      setSelectedScriptID(null)
      setSelectedScript(null)
      setScriptContent(null)
      setEditingProject(false)
      return
    }
    setProjectName(selectedProject.name)
    setProjectDescription(selectedProject.description)
    setProjectVisibility(selectedProject.visibility || "public")
    if (!selectedScripts.some((script) => script.id === selectedScriptID)) {
      setSelectedScriptID(null)
      setSelectedScript(null)
      setScriptContent(null)
      setEditingScript(false)
    }
  }, [selectedProject, selectedScriptID, selectedScripts])

  const selectProject = useCallback((project: ScriptGalleryProject) => {
    setSelectedProjectID(project.id)
    setSelectedScriptID(null)
    setSelectedScript(null)
    setScriptContent(null)
    setEditingProject(false)
    setEditingScript(false)
  }, [])

  const selectScript = useCallback(async (script: ScriptGalleryScript) => {
    setSelectedScriptID(script.id)
    setSelectedScript(script)
    setScriptName(script.name)
    setScriptContent(null)
    setEditingScript(false)
    setLoadingScript(true)
    try {
      const detail = await rcService.getScriptGalleryScript(script.id)
      setSelectedScript(detail)
      setScriptName(detail.name)
      setScriptContent(detail.content)
    } catch {
      setSelectedScript(null)
      setScriptContent(null)
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
      setScriptContent(null)
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
      setScriptContent(null)
      setConfirmDelete(null)
      toast.success(t("gallery.deleted"))
    } catch {
      toast.error(t("gallery.deleteFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [selectedProject, t])

  const uploadCurrentScript = useCallback(async () => {
    if (!authState?.authenticated) {
      toast.error(t("gallery.loginRequired"))
      return
    }
    if (!selectedProject) {
      toast.error(t("gallery.selectProject"))
      return
    }
    const name = currentName.trim()
    if (!name) {
      toast.error(t("gallery.scriptRequired"))
      return
    }
    setBusyAction("upload")
    try {
      const script = await rcService.uploadScriptGalleryScript(selectedProject.id, scriptType, name, currentContent)
      setProjects((previous) => previous.map((project) => project.id === selectedProject.id ? addScriptToProject(project, script) : project))
      setSelectedScriptID(script.id)
      setSelectedScript(script)
      setScriptName(script.name)
      setScriptContent(script.content)
      toast.success(t("gallery.uploaded"))
    } catch {
      toast.error(t("gallery.uploadFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [authState?.authenticated, currentContent, currentName, scriptType, selectedProject, t])

  const saveScript = useCallback(async () => {
    if (!selectedScript || scriptContent === null || !scriptName.trim()) {
      toast.error(t("gallery.scriptRequired"))
      return
    }
    setBusyAction("saveScript")
    try {
      const updated = await rcService.updateScriptGalleryScript(selectedScript.id, scriptName.trim(), scriptContent)
      setProjects((previous) => previous.map((project) => replaceScriptInProject(project, updated)))
      setSelectedScript(updated)
      setScriptName(updated.name)
      setEditingScript(false)
      toast.success(t("gallery.saved"))
    } catch {
      toast.error(t("gallery.saveFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [scriptContent, scriptName, selectedScript, t])

  const deleteScript = useCallback(async () => {
    if (!selectedScript) return
    setBusyAction("deleteScript")
    try {
      await rcService.deleteScriptGalleryScript(selectedScript.id)
      setProjects((previous) => previous.map((project) => removeScriptFromProject(project, selectedScript.id)))
      setSelectedScriptID(null)
      setSelectedScript(null)
      setScriptContent(null)
      setConfirmDelete(null)
      toast.success(t("gallery.deleted"))
    } catch {
      toast.error(t("gallery.deleteFailed"))
    } finally {
      setBusyAction(null)
    }
  }, [selectedScript, t])

  if (!open) return null

  const dialogBusy = busyAction !== null || loadingScript
  const typeLabel = t(`gallery.type.${scriptType}`)
  const scriptCount = selectedProject ? totalScriptCount(selectedProject) : 0

  return (
    <>
      <div
        className="fixed inset-0 z-50 flex items-center justify-center bg-black/65 p-4"
        onMouseDown={(event) => {
          if (event.target === event.currentTarget && !dialogBusy) onClose()
        }}
      >
        <section
          role="dialog"
          aria-modal="true"
          aria-labelledby="script-gallery-title"
          className="bg-background flex h-[min(760px,calc(100svh-2rem))] w-full max-w-6xl flex-col overflow-hidden rounded-xl border shadow-2xl"
        >
          <header className="flex shrink-0 items-center gap-3 border-b px-5 py-3">
            <div className="min-w-0 flex-1">
              <h2 id="script-gallery-title" className="text-base font-semibold">{t("gallery.title")}</h2>
              <p className="text-muted-foreground truncate text-xs">{t("gallery.description", {type: typeLabel})}</p>
            </div>
            <Badge variant="outline">{typeLabel}</Badge>
            <Button variant="ghost" size="icon" className="size-8" onClick={onClose} disabled={dialogBusy} aria-label={t("common.close")}>
              <X className="size-4" />
            </Button>
          </header>
          <ScriptGalleryAuthPanel state={authState} loading={authLoading} onChanged={handleAuthChanged} />

          <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
            <aside className="flex min-h-0 w-full shrink-0 flex-col border-b lg:w-72 lg:border-r lg:border-b-0">
              <div className="space-y-2 border-b p-3">
                <div className="flex items-center justify-between gap-2">
                  <Label className="text-xs uppercase tracking-wide text-muted-foreground">{t("gallery.projects")}</Label>
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
                <div className="relative">
                  <Search className="text-muted-foreground pointer-events-none absolute left-2.5 top-2.5 size-4" />
                  <Input value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("gallery.search")} className="pl-8" aria-label={t("gallery.search")} />
                </div>
              </div>
              {showCreateProject && (
                <div className="space-y-2 border-b bg-muted/10 p-3">
                  <Label htmlFor="gallery-new-project-name">{t("gallery.projectName")}</Label>
                  <Input id="gallery-new-project-name" value={newProjectName} onChange={(event) => setNewProjectName(event.target.value)} maxLength={120} autoFocus />
                  <Label htmlFor="gallery-new-project-description">{t("gallery.projectDescription")}</Label>
                  <textarea id="gallery-new-project-description" value={newProjectDescription} onChange={(event) => setNewProjectDescription(event.target.value)} maxLength={1000} className="min-h-16 w-full resize-y rounded-md border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring" />
                  <Label htmlFor="gallery-new-project-visibility">{t("gallery.visibility")}</Label>
                  <select id="gallery-new-project-visibility" value={newProjectVisibility} onChange={(event) => setNewProjectVisibility(event.target.value)} className="h-9 w-full rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring">
                    <option value="public">{t("gallery.visibilityPublic")}</option>
                    <option value="private">{t("gallery.visibilityPrivate")}</option>
                  </select>
                  <Button className="w-full" size="sm" onClick={() => void createProject()} disabled={dialogBusy}>
                    {busyAction === "createProject" ? <Loader2 className="animate-spin" /> : <Plus />}{t("gallery.createProject")}
                  </Button>
                </div>
              )}
              <div className="min-h-0 flex-1 overflow-y-auto p-2">
                {loading ? (
                  <div className="text-muted-foreground flex items-center justify-center gap-2 p-6 text-xs"><Loader2 className="size-4 animate-spin" />{t("gallery.loading")}</div>
                ) : error ? (
                  <div className="text-destructive p-4 text-center text-xs">{error}</div>
                ) : projects.length === 0 ? (
                  <div className="text-muted-foreground p-5 text-center text-xs">{t("gallery.noProjects")}</div>
                ) : (
                  <div className="space-y-1">
                    {projects.map((project) => {
                      const active = project.id === selectedProjectID
                      const count = scriptsForType(project, scriptType).length
                      return (
                        <button
                          key={project.id}
                          type="button"
                          onClick={() => selectProject(project)}
                          className={`w-full rounded-lg border px-3 py-2 text-left transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none ${active ? "border-primary bg-primary/10" : "border-transparent hover:border-border hover:bg-muted/50"}`}
                        >
                          <div className="flex items-start gap-2">
                            <span className="mt-1.5 size-2 shrink-0 rounded-full bg-primary/70" />
                            <span className="min-w-0 flex-1">
                              <span className="block truncate text-sm font-medium">{project.name}</span>
                              <span className="text-muted-foreground mt-0.5 block truncate text-[11px]">{project.description || t("gallery.noDescription")}</span>
                            </span>
                            <span className="flex shrink-0 items-center gap-1">
                              <Badge variant="outline" className="text-[10px]">{project.visibility === "private" ? t("gallery.visibilityPrivate") : t("gallery.visibilityPublic")}</Badge>
                              <Badge variant="secondary" className="text-[10px]">{count}</Badge>
                            </span>
                          </div>
                        </button>
                      )
                    })}
                  </div>
                )}
              </div>
            </aside>

            <section className="flex min-h-0 min-w-0 flex-1 flex-col">
              {!selectedProject ? (
                <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-3 p-8 text-center text-sm">
                  <FolderPlus className="size-8 opacity-50" />
                  <p>{t("gallery.selectProject")}</p>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => {
                      if (!authState?.authenticated) {
                        toast.error(t("gallery.loginRequired"))
                        return
                      }
                      setShowCreateProject(true)
                    }}
                    disabled={dialogBusy}
                  >
                    <Plus className="size-4" />{t("gallery.newProject")}
                  </Button>
                </div>
              ) : (
                <>
                  <div className="shrink-0 space-y-3 border-b p-4">
                    {editingProject ? (
                      <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,1.2fr)_minmax(120px,0.6fr)_auto] md:items-end">
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
                      <div className="flex flex-wrap items-start gap-3">
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-2">
                            <h3 className="truncate text-lg font-semibold">{selectedProject.name}</h3>
                            {selectedProject.owned ? <Badge variant="outline">{t("gallery.yourProject")}</Badge> : <Badge variant="secondary">{selectedProject.owner}</Badge>}
                            <Badge variant="outline">{selectedProject.visibility === "private" ? t("gallery.visibilityPrivate") : t("gallery.visibilityPublic")}</Badge>
                          </div>
                          <p className="text-muted-foreground mt-1 text-xs">{selectedProject.description || t("gallery.noDescription")}</p>
                          <p className="text-muted-foreground mt-2 text-[11px]">{t("gallery.projectContents", {count: scriptCount})}</p>
                        </div>
                        {selectedProject.owned && (
                          <div className="flex flex-wrap gap-2">
                            <Button variant="outline" size="sm" onClick={() => setEditingProject(true)} disabled={dialogBusy}><Pencil className="size-4" />{t("gallery.editProject")}</Button>
                            <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={() => setConfirmDelete("project")} disabled={dialogBusy}><Trash2 className="size-4" />{t("gallery.deleteProject")}</Button>
                          </div>
                        )}
                      </div>
                    )}
                  </div>

                  <div className="grid min-h-0 flex-1 grid-cols-1 xl:grid-cols-[260px_minmax(0,1fr)]">
                    <div className="min-h-0 border-b p-3 xl:border-r xl:border-b-0">
                      <div className="mb-2 flex items-center justify-between gap-2">
                        <Label className="text-xs uppercase tracking-wide text-muted-foreground">{t("gallery.scripts")}</Label>
                        <Badge variant="secondary">{selectedScripts.length}</Badge>
                      </div>
                      {selectedProject.owned && (
                        <Button className="mb-3 w-full" size="sm" onClick={() => void uploadCurrentScript()} disabled={dialogBusy}>
                          {busyAction === "upload" ? <Loader2 className="animate-spin" /> : <Upload className="size-4" />}{t("gallery.uploadCurrent")}
                        </Button>
                      )}
                      <div className="max-h-48 space-y-1 overflow-y-auto xl:max-h-none xl:h-[calc(100%_-_4.5rem)]">
                        {selectedScripts.length === 0 ? (
                          <p className="text-muted-foreground p-3 text-center text-xs">{t("gallery.noScripts", {type: typeLabel})}</p>
                        ) : selectedScripts.map((script) => (
                          <button
                            key={script.id}
                            type="button"
                            onClick={() => void selectScript(script)}
                            className={`w-full rounded-md border px-2.5 py-2 text-left text-xs transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none ${script.id === selectedScriptID ? "border-primary bg-primary/10" : "border-transparent hover:border-border hover:bg-muted/50"}`}
                          >
                            <span className="block truncate font-medium">{script.name}</span>
                            <span className="text-muted-foreground mt-1 block text-[10px]">{formatDate(script.updatedAt)}</span>
                          </button>
                        ))}
                      </div>
                    </div>

                    <div className="flex min-h-0 min-w-0 flex-col p-3">
                      {!selectedScript ? (
                        <div className="text-muted-foreground flex h-full items-center justify-center p-6 text-center text-xs">{t("gallery.selectScript")}</div>
                      ) : (
                        <>
                          <div className="mb-3 flex flex-wrap items-center gap-2">
                            {editingScript ? (
                              <Input value={scriptName} onChange={(event) => setScriptName(event.target.value)} maxLength={160} className="h-8 max-w-sm" aria-label={t("gallery.scriptName")} />
                            ) : <h4 className="min-w-0 flex-1 truncate text-sm font-semibold">{selectedScript.name}</h4>}
                            {loadingScript && <Loader2 className="text-muted-foreground size-4 animate-spin" />}
                            {selectedProject.owned && !editingScript && (
                              <Button variant="outline" size="sm" onClick={() => setEditingScript(true)} disabled={dialogBusy}><Pencil className="size-4" />{t("gallery.editScript")}</Button>
                            )}
                            {selectedProject.owned && editingScript && (
                              <Button size="sm" onClick={() => void saveScript()} disabled={dialogBusy}><Save className="size-4" />{t("gallery.saveScript")}</Button>
                            )}
                            {selectedProject.owned && editingScript && (
                              <Button variant="ghost" size="sm" onClick={() => { setEditingScript(false); setScriptName(selectedScript.name); setScriptContent(selectedScript.content) }} disabled={dialogBusy}>{t("common.cancel")}</Button>
                            )}
                            {selectedProject.owned && <Button variant="ghost" size="icon" className="size-8 text-destructive hover:text-destructive" onClick={() => setConfirmDelete("script")} disabled={dialogBusy} aria-label={t("gallery.deleteScript")}><Trash2 className="size-4" /></Button>}
                          </div>
                          <div className="min-h-[240px] min-w-0 flex-1 overflow-hidden rounded-md border">
                            {loadingScript || scriptContent === null ? (
                              <div className="text-muted-foreground flex h-full items-center justify-center gap-2 text-xs"><Loader2 className="size-4 animate-spin" />{t("gallery.loading")}</div>
                            ) : (
                              <Editor
                                height="100%"
                                theme={settings.theme}
                                language="graalscript"
                                value={scriptContent}
                                onChange={(value) => setScriptContent(value ?? "")}
                                options={{
                                  readOnly: !editingScript,
                                  minimap: {enabled: false},
                                  automaticLayout: true,
                                  scrollBeyondLastLine: false,
                                  fontFamily: settings.fontFamily,
                                  fontSize: settings.fontSize,
                                  tabSize: settings.tabSize,
                                  fontLigatures: true,
                                  fixedOverflowWidgets: true,
                                }}
                              />
                            )}
                          </div>
                          <div className="mt-3 flex flex-wrap justify-between gap-2">
                            <p className="text-muted-foreground text-[11px]">{selectedProject.owner ? `${t("gallery.owner")}: ${selectedProject.owner}` : ""}</p>
                            <div className="flex gap-2">
                              <Button variant="outline" size="sm" onClick={() => onInsert(scriptContent ?? "")} disabled={dialogBusy || scriptContent === null}><ArrowDownToLine className="size-4" />{t("gallery.useInEditor")}</Button>
                            </div>
                          </div>
                        </>
                      )}
                    </div>
                  </div>
                </>
              )}
            </section>
          </div>
        </section>
      </div>

      <ConfirmDialog
        open={confirmDelete !== null}
        title={confirmDelete === "project" ? t("gallery.deleteProjectTitle") : t("gallery.deleteScriptTitle")}
        description={confirmDelete === "project" ? t("gallery.deleteProjectDescription") : t("gallery.deleteScriptDescription")}
        confirmLabel={confirmDelete === "project" ? t("gallery.deleteProject") : t("gallery.deleteScript")}
        destructive
        busy={busyAction === "deleteProject" || busyAction === "deleteScript"}
        onCancel={() => setConfirmDelete(null)}
        onConfirm={() => void (confirmDelete === "project" ? deleteProject() : deleteScript())}
      />
    </>
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
  const replace = (scripts: ScriptGalleryScript[]) => (scripts ?? []).map((candidate) => candidate.id === script.id ? script : candidate)
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

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString()
}
