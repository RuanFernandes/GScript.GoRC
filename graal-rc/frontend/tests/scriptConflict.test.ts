import assert from "node:assert/strict"
import test from "node:test"
import {localConflictText, mergeBoxValue, resolveScriptConflict} from "../src/lib/scriptConflict.ts"
import type {SyncReviewItem} from "../src/types/index.ts"

function item(server: string, local = "disk"): SyncReviewItem {
  return {kind: "weapon", key: "w", name: "w", state: "conflict", local, server} as SyncReviewItem
}

function editor(buffer: string, baseline: string, conflict = item("server")) {
  const state = {buffer, baseline, conflict: conflict as SyncReviewItem | null}
  const calls: [string, string][] = []
  const run = (choice: "local" | "server" | "merge", options: {
    mergeEdit?: string | null
    during?: () => void
    pair?: {local: string; server: string}
  } = {}) => resolveScriptConflict({
    choice,
    mergeEdit: options.mergeEdit ?? null,
    getConflict: () => state.conflict,
    getBuffer: () => state.buffer,
    getBaseline: () => state.baseline,
    resolve: async (sentChoice, mergeContent) => { calls.push([sentChoice, mergeContent]); options.during?.() },
    getPair: async () => options.pair ?? {local: "", server: ""},
  })
  return {state, calls, run}
}

test("local means the editor buffer when the editor is dirty", () => {
  assert.equal(localConflictText(item("server"), "typed", "old"), "typed")
  assert.equal(localConflictText(item("server"), "old", "old"), "disk")
})

test("keep local uploads the unsaved buffer instead of the disk file", async () => {
  const instance = editor("typed", "old")
  const result = await instance.run("local")
  assert.deepEqual(instance.calls, [["merge", "typed"]])
  assert.deepEqual(result, {status: "resolved", applied: "typed", replaceBuffer: true, remaining: null})
})

test("keep local on a clean editor keeps the synced file", async () => {
  const instance = editor("old", "old")
  const result = await instance.run("local")
  assert.deepEqual(instance.calls, [["local", ""]])
  assert.equal(result.status === "resolved" && result.applied, "disk")
})

test("an empty dirty buffer is refused instead of falling back to disk", async () => {
  const instance = editor("", "old")
  assert.deepEqual(await instance.run("local"), {status: "empty"})
  assert.deepEqual(instance.calls, [])
})

test("the merge box follows the buffer until the user types in it", () => {
  assert.equal(mergeBoxValue(null, item("server"), "typed", "old"), "typed")
  assert.equal(mergeBoxValue(null, item("server"), "old", "old"), "disk")
  assert.equal(mergeBoxValue("my merge", item("newer server"), "typed", "old"), "my merge")
  assert.equal(mergeBoxValue("", item("server"), "typed", "old"), "")
})

test("apply merge sends the untouched seed from the buffer", async () => {
  const instance = editor("typed", "old")
  await instance.run("merge")
  assert.deepEqual(instance.calls, [["merge", "typed"]])
})

test("apply merge sends what the user typed in the merge box", async () => {
  const instance = editor("typed", "old")
  await instance.run("merge", {mergeEdit: "merged"})
  assert.deepEqual(instance.calls, [["merge", "merged"]])
})

test("text typed while resolving is not replaced", async () => {
  for (const choice of ["local", "server", "merge"] as const) {
    const instance = editor("typed", "old")
    const result = await instance.run(choice, {during: () => { instance.state.buffer = "typed more" }})
    assert.equal(result.status === "resolved" && result.replaceBuffer, false, choice)
  }
})

test("use server applies the newest server text from a repeated event", async () => {
  const instance = editor("old", "old")
  instance.state.conflict = item("newest server")
  const result = await instance.run("server")
  assert.deepEqual(instance.calls, [["server", ""]])
  assert.equal(result.status === "resolved" && result.applied, "newest server")
})

test("a repeat event consumed by the resolve determines the applied server text", async () => {
  const instance = editor("old", "old")
  const result = await instance.run("server", {during: () => { instance.state.conflict = item("newest server") }})
  assert.deepEqual(result, {status: "resolved", applied: "newest server", replaceBuffer: true, remaining: null})
})

test("a new conflict raised during the resolve stays open", async () => {
  const instance = editor("old", "old")
  const result = await instance.run("server", {
    during: () => { instance.state.conflict = item("later server") },
    pair: {local: "server", server: "later server"},
  })
  assert.equal(result.status === "resolved" && result.applied, "server")
  assert.deepEqual(result.status === "resolved" && result.remaining, item("later server", "server"))
})
