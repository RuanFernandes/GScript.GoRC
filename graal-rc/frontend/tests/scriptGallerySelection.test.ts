import assert from "node:assert/strict"
import test from "node:test"
import {createSelectionTracker, loadSelectedScript, scriptForSelection} from "../src/lib/scriptGallerySelection.ts"

function harness() {
  const tracker = createSelectionTracker()
  const log: string[] = []
  const requests = new Map<string, {resolve: (id?: string) => void; reject: () => void}>()
  const select = (id: string) => loadSelectedScript({
    id,
    tracker,
    fetch: () => new Promise<{id: string}>((resolve, reject) => {
      requests.set(id, {resolve: (other) => resolve({id: other ?? id}), reject: () => reject(new Error("nope"))})
    }),
    onLoaded: (detail) => log.push(`loaded:${detail.id}`),
    onError: () => log.push(`error:${id}`),
    onSettled: () => log.push(`settled:${id}`),
  })
  return {tracker, log, requests, select}
}

test("a late response for the previous selection is ignored", async () => {
  const {log, requests, select} = harness()
  const a = select("a")
  const b = select("b")
  requests.get("b")!.resolve()
  await b
  requests.get("a")!.resolve()
  await a
  assert.deepEqual(log, ["loaded:b", "settled:b"])
})

test("a late error for the previous selection is ignored and does not end loading", async () => {
  const {log, requests, select} = harness()
  const a = select("a")
  const b = select("b")
  requests.get("a")!.reject()
  await a
  assert.deepEqual(log, [])
  requests.get("b")!.resolve()
  await b
  assert.deepEqual(log, ["loaded:b", "settled:b"])
})

test("clearing the selection discards the pending response", async () => {
  const {tracker, log, requests, select} = harness()
  const a = select("a")
  tracker.change()
  requests.get("a")!.resolve()
  await a
  assert.deepEqual(log, [])
})

test("an error or a mismatched detail for the current selection is reported", async () => {
  const {log, requests, select} = harness()
  const a = select("a")
  requests.get("a")!.reject()
  await a
  const b = select("b")
  requests.get("b")!.resolve("zzz")
  await b
  assert.deepEqual(log, ["error:a", "settled:a", "error:b", "settled:b"])
})

test("a watch stays valid until the selection changes", () => {
  const tracker = createSelectionTracker()
  const stillSame = tracker.watch()
  assert.equal(stillSame(), true)
  assert.equal(tracker.watch()(), true)
  assert.equal(stillSame(), true)
  tracker.change()
  assert.equal(stillSame(), false)
})

test("actions only see the script that matches the highlighted one", () => {
  const script = {id: "a"}
  assert.equal(scriptForSelection("a", script), script)
  assert.equal(scriptForSelection("b", script), null)
  assert.equal(scriptForSelection(null, script), null)
})
