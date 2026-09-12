import type {FileBrowserEntry} from "@/types"

function basename(path: string): string {
  const normalized = path.replace(/\\/g, "/")
  return normalized.slice(normalized.lastIndexOf("/") + 1)
}

export function fileBrowserUploadPath(folder: string, fileName: string): string {
  const name = basename(fileName)
  if (!name || name === "." || name === "..") {
    throw new Error("invalid upload file name")
  }

  const normalizedFolder = folder.replace(/\\/g, "/").replace(/^\/+|\/+$/g, "")
  return normalizedFolder ? `${normalizedFolder}/${name}` : name
}

export function fileBrowserFileExists(files: readonly FileBrowserEntry[], fileName: string): boolean {
  const name = basename(fileName)
  return files.some((entry) => !entry.isDirectory && basename(entry.path) === name)
}
