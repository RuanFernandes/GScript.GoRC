export interface EditorDraftRecord {
  revision: string
  original: string
  content: string
  updatedAt: number
  cleared: boolean
}

export interface EditorDraftBackend {
  getEditorDraft(token: string): Promise<EditorDraftRecord | null>
  writeEditorDraft(token: string, sequence: number, record: EditorDraftRecord): Promise<void>
  clearEditorDraft(token: string, revision: string): Promise<boolean>
  saveEditorDraft(token: string, content: string): Promise<void>
  getEditorDraftContent(token: string): Promise<{text: string; name: string}>
  resolveEditorDraftConflict(token: string, choice: string, mergeContent: string): Promise<void>
  setEditorDraftDirty(token: string, dirty: boolean): Promise<void>
  closeEditorDraft(token: string): Promise<void>
}

export interface DraftStorage {
  getItem(key: string): string | null
  setItem(key: string, value: string): void
}

interface DraftOptions {
  token: string
  key: string
  backend: EditorDraftBackend
  storage: DraftStorage
  onError: (message: string | null) => void
  now?: () => number
  newRevision?: () => string
}

const MAX_TEXT_BYTES = 4 * 1024 * 1024
const STORAGE_PREFIX = "gorc-editor-draft:v1:"
const UTF8_ENCODER = new TextEncoder()

function exceedsTextLimit(text: string) {
  // Ordinary scripts need no UTF-8 allocation: one UTF-16 code unit requires
  // at most three UTF-8 bytes. Only texts near the quota need exact sizing.
  return text.length > MAX_TEXT_BYTES || text.length > MAX_TEXT_BYTES / 3 && UTF8_ENCODER.encode(text).byteLength > MAX_TEXT_BYTES
}

function isDraft(value: unknown): value is EditorDraftRecord {
  if (!value || typeof value !== "object") return false
  const record = value as Partial<EditorDraftRecord>
  return typeof record.revision === "string" && record.revision.length > 0 && record.revision.length <= 128
    && typeof record.original === "string" && typeof record.content === "string"
    && typeof record.updatedAt === "number" && Number.isSafeInteger(record.updatedAt) && record.updatedAt > 0
    && typeof record.cleared === "boolean"
}

export function selectEditorDraft(disk: EditorDraftRecord | null, fallback: EditorDraftRecord | null): EditorDraftRecord | null {
  if (!disk) return fallback
  if (!fallback) return disk
  // A acknowledged save/discard dominates the crash fallback of that revision.
  if (disk.revision === fallback.revision) return disk.cleared ? disk : fallback
  return fallback.updatedAt > disk.updatedAt ? fallback : disk
}

// The synchronous browser journal protects the last keystrokes when the native
// window disappears before a debounced backend call. Disk writes are serialized
// and all cleanup compares revisions, including edits made during an upload.
export class EditorDraftController {
  private readonly options: DraftOptions
  private current: EditorDraftRecord | null = null
  private base = ""
  private baseTooLarge = false
  private initialized = false
  private lastTimestamp = 0
  private pending: EditorDraftRecord | null = null
  private timer: ReturnType<typeof setTimeout> | null = null
  private queue: Promise<void> = Promise.resolve()
  private localError: string | null = null
  private backendError: string | null = null
  private lastError: string | null = null

  constructor(options: DraftOptions) {
    this.options = options
  }

  private error(source: "local" | "backend", error: unknown | null) {
    const message = error === null ? null : String(error)
    if (source === "local") this.localError = message
    else this.backendError = message
    const combined = [this.localError, this.backendError].filter(Boolean).join("; ") || null
    if (combined !== this.lastError) {
      this.lastError = combined
      this.options.onError(combined)
    }
  }

  private get storageKey() { return STORAGE_PREFIX + this.options.key }
  private get ownerKey() { return this.storageKey + ":owner" }

  private writeFallback(record: EditorDraftRecord) {
    try {
      if (this.options.storage.getItem(this.ownerKey) !== this.options.token) {
        throw new Error("Editor recovery journal belongs to a newer editor window")
      }
      this.options.storage.setItem(this.storageKey, JSON.stringify(record))
      this.error("local", null)
    } catch (error) {
      this.error("local", error)
    }
  }

  private readFallback(): EditorDraftRecord | null {
    try {
      const value = this.options.storage.getItem(this.storageKey)
      if (value === null) return null
      // Avoid parsing an unexpectedly large or unrelated local storage value.
      if (value.length > MAX_TEXT_BYTES * 4 + 4096) throw new Error("Editor recovery journal is too large")
      const record: unknown = JSON.parse(value)
      if (!isDraft(record)) throw new Error("Editor recovery journal is invalid")
      return record
    } catch (error) {
      this.error("local", error)
      return null
    }
  }

  async open(remote: string): Promise<{text: string; recovered: boolean; remoteChanged: boolean}> {
    if (!/^[a-f0-9]{64}$/.test(this.options.token) || !/^[a-f0-9]{64}$/.test(this.options.key)) {
      throw new Error("Editor session identity is missing; reopen the editor")
    }
    try {
      this.options.storage.setItem(this.ownerKey, this.options.token)
    } catch (error) {
      this.error("local", error)
    }
    const fallback = this.readFallback()
    let disk: EditorDraftRecord | null = null
    try {
      disk = await this.options.backend.getEditorDraft(this.options.token)
      this.error("backend", null)
    } catch (error) {
      this.error("backend", error)
    }
    const record = selectEditorDraft(disk, fallback)
    this.base = remote
    this.baseTooLarge = exceedsTextLimit(remote)
    this.initialized = true
    this.lastTimestamp = record?.updatedAt ?? 0
    if (!record || record.cleared) return {text: remote, recovered: false, remoteChanged: false}
    this.current = record
    if (record.content === remote) {
      this.pending = record
      await this.saved(remote, record.revision).catch(() => {})
      return {text: remote, recovered: false, remoteChanged: false}
    }
    this.writeFallback(record)
    // Import a newer browser journal into the persistent store. A fresh logical
    // timestamp also lets a reloaded webview continue the same native lease.
    this.pending = {...record, updatedAt: this.timestamp()}
    this.current = this.pending
    this.writeFallback(this.pending)
    this.schedule()
    return {text: record.content, recovered: true, remoteChanged: record.original !== remote}
  }

  private timestamp() {
    this.lastTimestamp = Math.max((this.options.now ?? Date.now)(), this.lastTimestamp + 1)
    return this.lastTimestamp
  }

  stage(content: string) {
    if (!this.initialized || this.current?.content === content && this.current.original === this.base) return
    if (exceedsTextLimit(content) || this.baseTooLarge) {
      this.error("local", new Error("Editor draft exceeds 4 MiB; export or save your changes before closing"))
      return
    }
    const record: EditorDraftRecord = {
      revision: (this.options.newRevision ?? (() => crypto.randomUUID()))(),
      original: this.base,
      content,
      updatedAt: this.timestamp(),
      cleared: false,
    }
    this.current = record
    this.pending = record
    this.writeFallback(record)
    this.schedule()
  }

  private schedule() {
    if (this.timer !== null) clearTimeout(this.timer)
    this.timer = setTimeout(() => { this.timer = null; void this.flush().catch(() => {}) }, 250)
  }

  get revision() { return this.current?.revision ?? "" }

  upload(content: string) { return this.options.backend.saveEditorDraft(this.options.token, content) }

  readContent() { return this.options.backend.getEditorDraftContent(this.options.token) }

  resolveConflict(choice: string, mergeContent: string) { return this.options.backend.resolveEditorDraftConflict(this.options.token, choice, mergeContent) }

  setDirty(dirty: boolean) { return this.options.backend.setEditorDraftDirty(this.options.token, dirty) }

  close() { return this.options.backend.closeEditorDraft(this.options.token) }

  rebase(remote: string) {
    this.base = remote
    this.baseTooLarge = exceedsTextLimit(remote)
    if (this.current) this.stage(this.current.content)
  }

  flush(): Promise<void> {
    if (this.timer !== null) { clearTimeout(this.timer); this.timer = null }
    const record = this.pending
    if (!record) return this.queue
    this.pending = null
    const work = this.queue.catch(() => {}).then(async () => {
      try {
        await this.options.backend.writeEditorDraft(this.options.token, record.updatedAt, record)
        this.error("backend", null)
      } catch (error) {
        if (this.current?.revision === record.revision && !this.pending) this.pending = record
        this.error("backend", error)
        throw error
      }
    })
    this.queue = work
    return work
  }

  async saved(content: string, revision: string): Promise<void> {
    this.base = content
    this.baseTooLarge = exceedsTextLimit(content)
    if (this.current && this.current.revision !== revision) {
      // The upload acknowledged an older snapshot. Preserve and rebase what
      // the user typed while it was in flight instead of marking it clean.
      this.stage(this.current.content)
      return
    }
    if (!this.current || !revision) return
    await this.clearRevision(revision)
  }

  async discard(): Promise<boolean> {
    const revision = this.revision
    if (revision) await this.clearRevision(revision)
    return !this.current
  }

  private async clearRevision(revision: string) {
    await this.flush()
    const clear = this.queue.catch(() => {}).then(async () => {
      try {
        const cleared = await this.options.backend.clearEditorDraft(this.options.token, revision)
        if (!cleared && this.current?.revision === revision) throw new Error("Editor draft changed before it could be cleared")
        if (this.current?.revision === revision) {
          this.writeFallback({...this.current, original: "", content: "", cleared: true})
          this.current = null
        }
        this.error("backend", null)
      } catch (error) {
        this.error("backend", error)
        throw error
      }
    })
    this.queue = clear
    await clear
  }
}
