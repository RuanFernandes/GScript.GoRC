// useFileBrowser drives the File Browser window: it holds the current folder +
// file lists, the server-message log, and the configured downloads folder, and
// exposes the actions (refresh / cd / download / upload / rename / delete /
// move). grclib pushes folder/file readiness + server messages on the uniform
// rc:evt channel; the downloads folder arrives via the raw rc:fbConfig event.
//
// The folders/files callbacks only carry a count — the actual data is fetched
// via getFileBrowserFolders/Files (a snapshot of the DLL cache), exactly as the
// reference C++ client does (TFileBrowser.cpp).
import {useCallback, useEffect, useRef, useState} from "react"
import {Events} from "@wailsio/runtime"
import {toast} from "sonner"

import type {RcService} from "@/services/rcService"
import type {FileBrowserConfig, FileBrowserEntry, FileBrowserFolder} from "@/types"
import {useLanguage} from "@/hooks/useLanguage"
import {isPreviewTransferMessage, mergeFileBrowserMessage} from "@/lib/fileBrowserMessages"
import {fileBrowserFileExists, fileBrowserUploadPath} from "@/lib/fileBrowserUpload"

type Evt = {seq: number; name: string; data: unknown[]}

export interface UseFileBrowserResult {
  folders: FileBrowserFolder[]
  files: FileBrowserEntry[]
  currentFolder: string
  messages: string[]
  config: FileBrowserConfig
  maxUpload: number
  loading: boolean
  loaded: boolean
  refresh: () => Promise<void>
  cd: (folder: string) => Promise<void>
  download: (entry: FileBrowserEntry, saveAs?: boolean, notify?: boolean) => Promise<boolean>
  uploadFiles: (files: FileList | File[]) => Promise<void>
  uploadDroppedFiles: (paths: string[], remoteFolder: string) => Promise<void>
  rename: (entry: FileBrowserEntry, newName: string) => Promise<void>
  remove: (entry: FileBrowserEntry) => Promise<boolean>
  move: (entry: FileBrowserEntry, destFolder: string, newName?: string) => Promise<boolean>
  setThumbnailPreviewPaths: (paths: readonly string[]) => void
}

function normalizeFileBrowserConfig(config?: Partial<FileBrowserConfig> | null): FileBrowserConfig {
  return {
    downloadDir: config?.downloadDir ?? "",
    showImageThumbnails: config?.showImageThumbnails === true,
  }
}

// joinPath composes a folder + name into the path grclib expects for uploads.
// The remote tree is /-rooted; the current folder may be "" (root).
function joinPath(folder: string, name: string): string {
  if (!folder) return name
  if (folder.endsWith("/")) return folder + name
  return folder + "/" + name
}

// basename is the last path segment of a remote file path.
function basename(path: string): string {
  const normalized = path.replace(/\\/g, "/")
  const i = normalized.lastIndexOf("/")
  return i >= 0 ? normalized.slice(i + 1) : normalized
}

// readAsBase64 reads a File as a data URL and strips the prefix, returning the
// raw base64. Using FileReader (not btoa over a spread Uint8Array) avoids a
// stack overflow on large files.
function readAsBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => {
      const result = reader.result
      if (typeof result !== "string") {
        reject(new Error("could not read file"))
        return
      }
      const comma = result.indexOf(",")
      resolve(comma >= 0 ? result.slice(comma + 1) : result)
    }
    reader.onerror = () => reject(reader.error ?? new Error("file read error"))
    reader.readAsDataURL(file)
  })
}

export function useFileBrowser(service: RcService): UseFileBrowserResult {
  const {t} = useLanguage()
  const [folders, setFolders] = useState<FileBrowserFolder[]>([])
  const [files, setFiles] = useState<FileBrowserEntry[]>([])
  const [currentFolder, setCurrentFolder] = useState("")
  const [messages, setMessages] = useState<string[]>([])
  const [config, setConfig] = useState<FileBrowserConfig>(normalizeFileBrowserConfig())
  const [maxUpload, setMaxUpload] = useState(0)
  const [loading, setLoading] = useState(true)
  // loaded flips true once the first folder/file snapshot arrives, so the UI can
  // show a skeleton instead of the "No folders." / "Empty folder." final-state
  // text during the initial async gap (loading goes false before data lands).
  const [loaded, setLoaded] = useState(false)
  // Keep the latest folders for move-destination picks without re-deriving state.
  const foldersRef = useRef<FileBrowserFolder[]>([])
  foldersRef.current = folders
  // currentFolderRef mirrors currentFolder so the rc:fbChanged reload handler
  // (registered once) always reloads the LATEST folder, not a stale closure.
  const currentFolderRef = useRef("")
  currentFolderRef.current = currentFolder
  const mutationRefreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  // Thumbnail downloads share the native File Browser transfer channel with
  // explicit user downloads. Keep their paths in a ref so the event listener
  // can hide only preview protocol messages without stale closures.
  const thumbnailPreviewPathsRef = useRef<readonly string[]>([])

  const setThumbnailPreviewPaths = useCallback((paths: readonly string[]) => {
    const nextPaths = [...paths]
    thumbnailPreviewPathsRef.current = nextPaths
    setMessages((previous) => {
      const next = previous.filter((message) => !isPreviewTransferMessage(message, nextPaths))
      return next.length === previous.length ? previous : next
    })
  }, [])

  const pushMessage = useCallback((msg: string) => {
    if (isPreviewTransferMessage(msg, thumbnailPreviewPathsRef.current)) return
    setMessages((prev) => mergeFileBrowserMessage(prev, msg))
  }, [])

  const snapshotFolders = useCallback(async () => {
    const list = await service.getFileBrowserFolders().catch(() => null)
    setFolders(list ?? [])
    setLoaded(true)
  }, [service])

  const snapshotFiles = useCallback(
    async (folder: string) => {
      const list = await service.getFileBrowserFiles().catch(() => null)
      setCurrentFolder(folder)
      setFiles(list ?? [])
      setLoaded(true)
    },
    [service],
  )

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      await service.fileBrowserStart()
    } catch (err) {
      toast.error(t("file.startFailed"), {description: String(err)})
    } finally {
      setLoading(false)
    }
  }, [service, t])

  const cd = useCallback(
    async (folder: string) => {
      try {
        await service.fileBrowserCd(folder)
      } catch (err) {
        toast.error(t("file.folderOpenFailed"), {description: String(err)})
      }
    },
    [service, t],
  )

  const download = useCallback(
    async (entry: FileBrowserEntry, saveAs = false, notify = true) => {
      if (entry.isDirectory) return false
      try {
        const saved = await service.downloadFile(entry.path, saveAs)
        if (saved && notify) toast.success(t("file.savedTo", {path: saved}))
        return Boolean(saved)
      } catch (err) {
        toast.error(t("file.downloadFailed"), {description: String(err)})
        return false
      }
    },
    [service, t],
  )

  const uploadFiles = useCallback(
    async (fileList: FileList | File[]) => {
      const selectedFiles = Array.from(fileList)
      if (selectedFiles.length === 0) return
      const uploadFolder = currentFolder
      const listedFiles = files
      let ok = 0
      for (const file of selectedFiles) {
        try {
          const remotePath = fileBrowserUploadPath(uploadFolder, file.name)
          const remoteFileExists = fileBrowserFileExists(listedFiles, file.name)
          const b64 = await readAsBase64(file)
          await service.uploadFileBytes(remotePath, b64, remoteFileExists)
          ok++
        } catch (err) {
          toast.error(t("file.uploadFailedFor", {name: file.name}), {description: String(err)})
        }
      }
      if (ok > 0) toast.success(t("file.uploadedCount", {count: ok}))
    },
    [service, currentFolder, files, t],
  )

  const uploadDroppedFiles = useCallback(
    async (paths: string[], remoteFolder: string) => {
      let ok = 0
      for (const localPath of paths) {
        const name = basename(localPath)
        try {
          await service.uploadDroppedFile(localPath, remoteFolder)
          ok++
        } catch (err) {
          toast.error(t("file.uploadFailedFor", {name}), {description: String(err)})
        }
      }
      if (ok > 0) toast.success(t("file.uploadedCount", {count: ok}))
    },
    [service, t],
  )

  const rename = useCallback(
    async (entry: FileBrowserEntry, newName: string) => {
      if (entry.isDirectory) return
      const dir = entry.path.includes("/")
        ? entry.path.slice(0, entry.path.lastIndexOf("/") + 1)
        : ""
      try {
        await service.fileBrowserRename(entry.path, joinPath(dir, newName))
      } catch (err) {
        toast.error(t("file.renameFailed"), {description: String(err)})
      }
    },
    [service, t],
  )

  const remove = useCallback(
    async (entry: FileBrowserEntry) => {
      try {
        await service.fileBrowserDelete(entry.path)
        return true
      } catch (err) {
        toast.error(t("file.deleteFailed"), {description: String(err)})
        return false
      }
    },
    [service, t],
  )

  const move = useCallback(
    async (entry: FileBrowserEntry, destFolder: string, newName?: string) => {
      const base = basename(entry.path)
      const dir = entry.path.includes("/") ? entry.path.slice(0, entry.path.lastIndexOf("/") + 1) : ""
      try {
        // Rename in place first (so the file lands in the destination with a
        // rights-matching name), then move. The server validates write rights on
        // the destination path, so a non-matching name must be fixed before move.
        if (newName && newName !== base) {
          const renamed = joinPath(dir, newName)
          await service.fileBrowserRename(entry.path, renamed)
          await service.fileBrowserMove(destFolder, renamed)
        } else {
          await service.fileBrowserMove(destFolder, entry.path)
        }
        return true
      } catch (err) {
        toast.error(t("file.moveFailed"), {description: String(err)})
        return false
      }
    },
    [service, t],
  )

  useEffect(() => {
    // Initial config + max upload size; then start the browser session.
    service
      .getFileBrowserConfig()
      .then((c) => setConfig(normalizeFileBrowserConfig(c)))
      .catch(() => {})
    service
      .fileBrowserMaxUploadSize()
      .then((n) => setMaxUpload(n ?? 0))
      .catch(() => {})
    refresh()

    // Folder/file readiness + server messages arrive on the uniform rc:evt channel.
    const offEvt = Events.On("rc:evt", (e: {data: string}) => {
      try {
        const m = JSON.parse(e.data) as Evt
        if (m.name === "rc:fbFolders") snapshotFolders()
        else if (m.name === "rc:fbFiles") {
          const folder = typeof m.data?.[0] === "string" ? (m.data[0] as string) : ""
          snapshotFiles(folder)
        } else if (m.name === "rc:fbReset") {
          if (mutationRefreshTimerRef.current !== null) {
            clearTimeout(mutationRefreshTimerRef.current)
            mutationRefreshTimerRef.current = null
          }
          thumbnailPreviewPathsRef.current = []
          foldersRef.current = []
          currentFolderRef.current = ""
          setFolders([])
          setFiles([])
          setCurrentFolder("")
          setMessages([])
          setMaxUpload(0)
          setLoaded(false)
          setLoading(true)
        } else if (m.name === "rc:connected") {
          // If a disconnect/reconnect happened while this WebView was still
          // tearing down, request a fresh root snapshot for the new server.
          void refresh()
        } else if (m.name === "rc:fbMessage") {
          pushMessage(typeof m.data?.[0] === "string" ? (m.data[0] as string) : "")
        } else if (m.name === "rc:fbMaxUpload") {
          setMaxUpload(typeof m.data?.[0] === "number" ? (m.data[0] as number) : 0)
        } else if (m.name === "rc:fbChanged") {
          // Match the reference client: wait for the server mutation to settle,
          // and coalesce multi-file uploads into one folder refresh.
          if (mutationRefreshTimerRef.current !== null) {
            clearTimeout(mutationRefreshTimerRef.current)
          }
          mutationRefreshTimerRef.current = setTimeout(() => {
            mutationRefreshTimerRef.current = null
            const folder = currentFolderRef.current
            if (folder) void cd(folder)
            else void refresh()
          }, 500)
        }
      } catch {
        // ignore malformed events
      }
    })

    // Downloads-folder changes broadcast raw (not on rc:evt).
    const offCfg = Events.On("rc:fbConfig", (e: {data: string}) => {
      try {
        setConfig(normalizeFileBrowserConfig(JSON.parse(e.data) as FileBrowserConfig))
      } catch {
        // ignore
      }
    })

    const offDroppedFiles = Events.On("filebrowser:files-dropped", (e: {data: unknown}) => {
      try {
        const data = typeof e.data === "string" ? JSON.parse(e.data) as unknown : e.data
        if (data === null || typeof data !== "object") return
        const payload = data as {files?: unknown; folder?: unknown}
        const paths = Array.isArray(payload.files)
          ? payload.files.filter((filePath): filePath is string => typeof filePath === "string" && filePath.length > 0)
          : []
        if (paths.length === 0) return
        const folder = typeof payload.folder === "string" ? payload.folder : currentFolderRef.current
        void uploadDroppedFiles(paths, folder)
      } catch {
        // ignore malformed native drop events
      }
    })

    return () => {
      offEvt()
      offCfg()
      offDroppedFiles()
      if (mutationRefreshTimerRef.current !== null) {
        clearTimeout(mutationRefreshTimerRef.current)
        mutationRefreshTimerRef.current = null
      }
    }
  }, [refresh, cd, snapshotFolders, snapshotFiles, pushMessage, service, uploadDroppedFiles])

  return {
    folders,
    files,
    currentFolder,
    messages,
    config,
    maxUpload,
    loading,
    loaded,
    refresh,
    cd,
    download,
    uploadFiles,
    uploadDroppedFiles,
    rename,
    remove,
    move,
    setThumbnailPreviewPaths,
  }
}
