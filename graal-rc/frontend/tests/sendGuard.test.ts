import assert from "node:assert/strict"
import test from "node:test"
import {composerAfterSend, createSendGuard, draftAfterSend, emptyComposer} from "../src/lib/sendGuard.ts"

function deferred() {
  let resolve!: (value: boolean) => void
  const promise = new Promise<boolean>((ok) => { resolve = ok })
  return {promise, resolve}
}

test("a second submit while one is in flight does not send again", async () => {
  const guard = createSendGuard()
  const slow = deferred()
  let sends = 0
  const send = () => { sends++; return slow.promise }
  const first = guard.run("a", send)
  assert.equal(await guard.run("a", send), "busy")
  slow.resolve(true)
  assert.equal(await first, "sent")
  assert.equal(sends, 1)
})

test("channels are independent and the key is released afterwards", async () => {
  const changes: string[] = []
  const guard = createSendGuard((key, busy) => changes.push(`${key}:${busy}`))
  const slow = deferred()
  const first = guard.run("a", () => slow.promise)
  assert.equal(await guard.run("b", async () => true), "sent")
  slow.resolve(false)
  assert.equal(await first, "failed")
  assert.equal(await guard.run("a", async () => true), "sent")
  assert.deepEqual(changes, ["a:true", "b:true", "b:false", "a:false", "a:true", "a:false"])
})

test("a rejected or throwing send is a failure and releases the key", async () => {
  const guard = createSendGuard()
  assert.equal(await guard.run("a", async () => { throw new Error("offline") }), "failed")
  assert.equal(await guard.run("a", () => { throw new Error("sync") }), "failed")
  assert.equal(await guard.run("a", async () => true), "sent")
})

test("a draft is cleared only if it was not edited during the send", () => {
  assert.equal(draftAfterSend("hello", "hello"), "")
  assert.equal(draftAfterSend("next message", "hello"), "next message")
})

test("the composer of the sending channel is reset only if untouched", () => {
  const sent = {...emptyComposer(), text: "/stats"}
  const drafts = {a: sent, b: {...emptyComposer(), text: "other"}}
  assert.deepEqual(composerAfterSend(drafts, "a", sent), {a: emptyComposer(), b: drafts.b})
  const edited = {...drafts, a: {...sent, text: "typed later"}}
  assert.equal(composerAfterSend(edited, "a", sent), edited)
})
