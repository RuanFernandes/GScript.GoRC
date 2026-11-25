// Dragging a remote file out of the File Browser onto Explorer.
//
// WebView2 cannot start a native file drag, but it understands the Chromium
// "DownloadURL" drag format ("mime:file name:url"): when the drag is dropped
// on something that accepts files, the browser fetches the URL and writes the
// result there. The URL points at the local server in internal/filedrag, which
// downloads the remote file through the RC session at that moment.

export const DOWNLOAD_URL_FORMAT = "DownloadURL"

const FALLBACK_NAME = "download"

// The value has three fields split on ":", so the name must not contain one,
// and Windows would refuse most of these characters in a file name anyway.
const UNSAFE_NAME_CHARACTERS = /[\u0000-\u001f<>:"/\\|?*]/g

/** File name offered to the drop target: the last path segment, safe on Windows. */
export function dragFileName(remotePath: string): string {
  const segments = remotePath.split(/[\\/]/).filter((segment) => segment.length > 0)
  const last = segments.length > 0 ? segments[segments.length - 1] : ""
  const safe = last.replace(UNSAFE_NAME_CHARACTERS, "_").replace(/[. ]+$/, "").trim()
  return safe.length > 0 ? safe : FALLBACK_NAME
}

/** The URL the browser fetches on drop: the base from GetFileDragBase plus the escaped remote path. */
export function dragFileUrl(base: string, remotePath: string): string {
  const segments = remotePath.split(/[\\/]/).filter((segment) => segment.length > 0)
  return base + segments.map((segment) => encodeURIComponent(segment)).join("/")
}

/** The value for dataTransfer.setData("DownloadURL", ...). */
export function dragDownloadUrlValue(base: string, remotePath: string): string {
  return `application/octet-stream:${dragFileName(remotePath)}:${dragFileUrl(base, remotePath)}`
}

type DragData = Pick<DataTransfer, "setData"> & {effectAllowed: DataTransfer["effectAllowed"]}

/**
 * Prepares a drag of one remote file. Returns false, and leaves the drag alone,
 * when there is nothing to offer: no base yet, or a folder.
 */
export function startRemoteFileDrag(
  dataTransfer: DragData | null,
  base: string | null,
  entry: {path: string; isDirectory: boolean},
): boolean {
  if (!dataTransfer || !base || entry.isDirectory) return false
  dataTransfer.effectAllowed = "copy"
  dataTransfer.setData(DOWNLOAD_URL_FORMAT, dragDownloadUrlValue(base, entry.path))
  return true
}
