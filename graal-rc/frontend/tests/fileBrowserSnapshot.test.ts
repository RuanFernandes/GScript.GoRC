import assert from "node:assert/strict"
import test from "node:test"

import {createLatestRequest} from "../src/lib/fileBrowserSnapshot.ts"

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => {
    resolve = done
  })
  return {promise, resolve}
}

test("a slower older snapshot never replaces a newer one", async () => {
  const latest = createLatestRequest()
  const older = deferred<string>()
  const newer = deferred<string>()
  const applied: string[] = []
  const first = latest.run(() => older.promise, (value) => applied.push(value))
  const second = latest.run(() => newer.promise, (value) => applied.push(value))
  newer.resolve("B")
  await second
  older.resolve("A")
  await first
  assert.deepEqual(applied, ["B"])
})

test("an in-order older snapshot is also superseded", async () => {
  const latest = createLatestRequest()
  const older = deferred<string>()
  const newer = deferred<string>()
  const applied: string[] = []
  const first = latest.run(() => older.promise, (value) => applied.push(value))
  const second = latest.run(() => newer.promise, (value) => applied.push(value))
  older.resolve("A")
  await first
  newer.resolve("B")
  await second
  assert.deepEqual(applied, ["B"])
})

test("invalidate drops a snapshot taken before a reset", async () => {
  const latest = createLatestRequest()
  const pending = deferred<string>()
  const applied: string[] = []
  const run = latest.run(() => pending.promise, (value) => applied.push(value))
  latest.invalidate()
  pending.resolve("old server")
  await run
  assert.deepEqual(applied, [])
})
