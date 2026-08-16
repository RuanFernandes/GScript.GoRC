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
import {mergeFileBrowserMessage} from "@/lib/fileBrowserMessages"

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
  uploadViaDialog: () => Promise<void>
  rename: (entry: FileBrowserEntry, newName: string) => Promise<void>
  remove: (entry: FileBrowserEntry) => Promise<void>
  move: (entry: FileBrowserEntry, destFolder: string, newName?: string) => Promise<boolean>
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
  const i = path.lastIndexOf("/")
  return i >= 0 ? path.slice(i + 1) : path
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

  const pushMessage = useCallback((msg: string) => {
    setMessages((prev) => mergeFileBrowserMessage(prev, msg))
  }, [])

  const snapshotFolders = useCallback(async () => {
    const list = await service.getFileBrowserFolders().catch(() => null)
    setFolders(list ?? [])
    setLoaded(true)
  }, [service])

  const snapshotFiles = useCallback(
    async (folder: string) => {
      setCurrentFolder(folder)
      const list = await service.getFileBrowserFiles().catch(() => null)
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

  const uploadOne = useCallback(
    async (file: File) => {
      const b64 = await readAsBase64(file)
      const remote = joinPath(currentFolder, file.name)
      await service.uploadFileBytes(remote, b64)
    },
    [service, currentFolder],
  )

  const uploadFiles = useCallback(
    async (fileList: FileList | File[]) => {
      const files = Array.from(fileList)
      if (files.length === 0) return
      let ok = 0
      for (const f of files) {
        try {
          await uploadOne(f)
          ok++
        } catch (err) {
          toast.error(t("file.uploadFailedFor", {name: f.name}), {description: String(err)})
        }
      }
      if (ok > 0) toast.success(t("file.uploadedCount", {count: ok}))
    },
    [uploadOne, t],
  )

  const uploadViaDialog = useCallback(async () => {
    try {
      await service.uploadFileViaDialog()
      toast.success(t("file.uploaded"))
    } catch (err) {
      toast.error(t("file.uploadFailed"), {description: String(err)})
    }
  }, [service, t])

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
      } catch (err) {
        toast.error(t("file.deleteFailed"), {description: String(err)})
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
          // A mutation (upload/delete/rename/move/save) finished elsewhere —
          // reload the current folder so the listing stays fresh.
          const folder = currentFolderRef.current
          if (folder) cd(folder)
          else refresh()
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

    return () => {
      offEvt()
      offCfg()
    }
  }, [refresh, cd, snapshotFolders, snapshotFiles, pushMessage, service])

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
    uploadViaDialog,
    rename,
    remove,
    move,
  }
}
