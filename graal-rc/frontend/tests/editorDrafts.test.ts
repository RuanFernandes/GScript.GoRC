import assert from "node:assert/strict"
import test from "node:test"
import {EditorDraftController, selectEditorDraft} from "../src/lib/editorDrafts.ts"
import type {DraftStorage, EditorDraftBackend, EditorDraftRecord} from "../src/lib/editorDrafts.ts"

const token = "a".repeat(64)
const key = "b".repeat(64)

function record(revision: string, content: string, updatedAt = 1): EditorDraftRecord {
  return {revision, content, original: "remote", updatedAt, cleared: false}
}

function setup(sharedStorage = new Map<string, string>()) {
  let persisted: EditorDraftRecord | null = null
  let counter = 0
  const errors: (string | null)[] = []
  const storage: DraftStorage = {
    getItem: (key) => sharedStorage.get(key) ?? null,
    setItem: (key, value) => { sharedStorage.set(key, value) },
  }
  const backend: EditorDraftBackend = {
    saveEditorDraft: async () => {},
    getEditorDraftContent: async () => ({text: "remote", name: "script"}),
    resolveEditorDraftConflict: async () => {},
    setEditorDraftDirty: async () => {},
    closeEditorDraft: async () => {},
    getEditorDraft: async () => persisted,
    writeEditorDraft: async (_token, _sequence, value) => { persisted = {...value} },
    clearEditorDraft: async (_token, revision) => {
      if (persisted?.revision !== revision) return false
      persisted = {...persisted, original: "", content: "", cleared: true}
      return true
    },
  }
  const controller = new EditorDraftController({token, key, backend, storage, onError: (error) => errors.push(error), now: () => 100, newRevision: () => `r${++counter}`})
  return {controller, backend, errors, storage, sharedStorage, persisted: () => persisted}
}

test("last keystrokes are recoverable before the debounced disk write", async () => {
  const first = setup()
  await first.controller.open("remote")
  first.controller.stage("last keystroke")
  assert.equal(first.persisted(), null)
  const restarted = setup(first.sharedStorage)
  const recovery = await restarted.controller.open("remote")
  assert.deepEqual(recovery, {text: "last keystroke", recovered: true, remoteChanged: false})
  await Promise.all([first.controller.flush(), restarted.controller.flush()])
  assert.equal(restarted.persisted()?.content, "last keystroke")
})

test("a changed remote baseline restores locally and requests comparison", async () => {
  const instance = setup()
  instance.backend.getEditorDraft = async () => record("existing", "my work")
  const recovery = await instance.controller.open("new server version")
  assert.deepEqual(recovery, {text: "my work", recovered: true, remoteChanged: true})
  await instance.controller.flush()
  assert.equal(instance.persisted()?.original, "remote")
})

test("saving a snapshot keeps and rebases typing made during the upload", async () => {
  const instance = setup()
  await instance.controller.open("remote")
  instance.controller.stage("submitted")
  const submittedRevision = instance.controller.revision
  instance.controller.stage("submitted plus new typing")
  await instance.controller.saved("submitted", submittedRevision)
  await instance.controller.flush()
  assert.equal(instance.persisted()?.content, "submitted plus new typing")
  assert.equal(instance.persisted()?.original, "submitted")
  assert.equal(instance.persisted()?.cleared, false)
})

test("a successful save leaves a tombstone that suppresses stale crash fallback", async () => {
  const instance = setup()
  await instance.controller.open("remote")
  instance.controller.stage("submitted")
  const fallback = JSON.parse(instance.sharedStorage.get("gorc-editor-draft:v1:" + key)!) as EditorDraftRecord
  await instance.controller.saved("submitted", instance.controller.revision)
  assert.equal(instance.persisted()?.cleared, true)
  assert.equal(instance.controller.revision, "")
  assert.equal(selectEditorDraft(instance.persisted(), fallback)?.cleared, true)
})

test("an old save completion cannot erase a new edit while disk clear is waiting", async () => {
  const instance = setup()
  await instance.controller.open("remote")
  instance.controller.stage("submitted")
  let release: () => void = () => {}
  const blocked = new Promise<void>((resolve) => { release = resolve })
  const actualClear = instance.backend.clearEditorDraft
  instance.backend.clearEditorDraft = async (token, revision) => { await blocked; return actualClear(token, revision) }
  const clear = instance.controller.saved("submitted", instance.controller.revision)
  await new Promise((resolve) => setImmediate(resolve))
  instance.controller.stage("new work")
  release()
  await clear
  await instance.controller.flush()
  assert.equal(instance.persisted()?.content, "new work")
  assert.equal(instance.persisted()?.cleared, false)
})

test("discard refuses to close over edits made during its async cleanup", async () => {
  const instance = setup()
  await instance.controller.open("remote")
  instance.controller.stage("old")
  let release: () => void = () => {}
  const blocked = new Promise<void>((resolve) => { release = resolve })
  const actualClear = instance.backend.clearEditorDraft
  instance.backend.clearEditorDraft = async (token, revision) => { await blocked; return actualClear(token, revision) }
  const discard = instance.controller.discard()
  await new Promise((resolve) => setImmediate(resolve))
  instance.controller.stage("new")
  release()
  assert.equal(await discard, false)
  await instance.controller.flush()
  assert.equal(instance.persisted()?.content, "new")
})

test("quota and backend failures remain visible and preserve the recovery journal", async () => {
  const instance = setup()
  await instance.controller.open("remote")
  instance.storage.setItem = () => { throw new Error("quota exceeded") }
  instance.controller.stage("work")
  assert.match(instance.errors.at(-1) ?? "", /quota/)
  await instance.controller.flush()
  assert.equal(instance.persisted()?.content, "work")
  assert.match(instance.errors.at(-1) ?? "", /quota/)
  const actualWrite = instance.backend.writeEditorDraft
  instance.backend.writeEditorDraft = async () => { throw new Error("disk full") }
  instance.controller.stage("newer work")
  await assert.rejects(instance.controller.flush(), /disk full/)
  assert.match(instance.errors.at(-1) ?? "", /disk full/)
  instance.backend.writeEditorDraft = actualWrite
  await instance.controller.flush()
  assert.equal(instance.persisted()?.content, "newer work")
})

test("a sync upload changes the baseline while preserving unsaved content", async () => {
  const instance = setup()
  await instance.controller.open("remote")
  instance.controller.stage("editor work")
  instance.controller.rebase("external upload")
  await instance.controller.flush()
  assert.equal(instance.persisted()?.content, "editor work")
  assert.equal(instance.persisted()?.original, "external upload")
})

test("recovery never reads another resource's local storage journal", async () => {
  const instance = setup()
  instance.sharedStorage.set("gorc-editor-draft:v1:" + "c".repeat(64), JSON.stringify(record("other", "private")))
  assert.deepEqual(await instance.controller.open("remote"), {text: "remote", recovered: false, remoteChanged: false})
})

test("a superseded window cannot overwrite the newer window's browser journal", async () => {
  const old = setup()
  await old.controller.open("remote")
  old.controller.stage("old window work")
  await old.controller.flush()
  const fresh = new EditorDraftController({
    token: "c".repeat(64), key, backend: old.backend, storage: old.storage,
    onError: () => {}, now: () => 200, newRevision: () => "new-window",
  })
  await fresh.open("remote")
  fresh.stage("new window work")
  old.controller.stage("delayed old callback")
  const journal = JSON.parse(old.sharedStorage.get("gorc-editor-draft:v1:" + key)!) as EditorDraftRecord
  assert.equal(journal.content, "new window work")
  assert.match(old.errors.at(-1) ?? "", /newer editor window/)
  await Promise.all([old.controller.flush(), fresh.flush()])
})

test("recovering a draft never invokes any server write", async () => {
  const instance = setup()
  instance.backend.getEditorDraft = async () => record("recovered", "my work")
  instance.backend.saveEditorDraft = async () => { assert.fail("automatic upload") }
  instance.backend.resolveEditorDraftConflict = async () => { assert.fail("automatic conflict resolution") }
  await instance.controller.open("remote")
  await instance.controller.flush()
  assert.equal(instance.persisted()?.content, "my work")
})

test("text limits report failure without overwriting the last valid draft", async () => {
  const instance = setup()
  await instance.controller.open("remote")
  instance.controller.stage("protected")
  await instance.controller.flush()
  instance.controller.stage("á".repeat(3 * 1024 * 1024))
  assert.match(instance.errors.at(-1) ?? "", /4 MiB/)
  await instance.controller.flush()
  assert.equal(instance.persisted()?.content, "protected")
})
