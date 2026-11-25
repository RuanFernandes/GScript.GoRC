import assert from "node:assert/strict"
import test from "node:test"
import {watchPmRead} from "../src/lib/pmRead.ts"

const settle = () => new Promise((resolve) => setImmediate(resolve))

function setup(options: {unread: number; visible?: boolean; focused?: boolean}) {
  const view = {visible: options.visible ?? true, focused: options.focused ?? true}
  const listeners = new Set<() => void>()
  let calls = 0
  let finishRequest: () => void = () => {}
  const stop = watchPmRead({
    unread: options.unread,
    isVisible: () => view.visible,
    isFocused: () => view.focused,
    subscribe: (check) => { listeners.add(check); return () => { listeners.delete(check) } },
    markRead: () => { calls++; return new Promise<void>((resolve) => { finishRequest = resolve }) },
  })
  return {
    view,
    stop,
    listeners,
    calls: () => calls,
    fire: () => listeners.forEach((check) => check()),
    finish: async () => { finishRequest(); await settle() },
  }
}

test("unread messages in an on-screen conversation are marked read", () => {
  assert.equal(setup({unread: 2}).calls(), 1)
})

test("nothing is marked when there is nothing unread", () => {
  const instance = setup({unread: 0})
  instance.fire()
  assert.equal(instance.calls(), 0)
  assert.equal(instance.listeners.size, 0)
})

test("hidden or unfocused windows keep messages unread until the user returns", () => {
  const hidden = setup({unread: 1, visible: false})
  hidden.fire()
  assert.equal(hidden.calls(), 0)
  hidden.view.visible = true
  hidden.fire()
  assert.equal(hidden.calls(), 1)

  const background = setup({unread: 1, focused: false})
  assert.equal(background.calls(), 0)
  background.view.focused = true
  background.fire()
  assert.equal(background.calls(), 1)
})

test("events during a pending request do not repeat it, a later event may retry", async () => {
  const instance = setup({unread: 1})
  instance.fire()
  instance.fire()
  assert.equal(instance.calls(), 1)
  await instance.finish()
  assert.equal(instance.calls(), 1, "settling alone never calls again")
  instance.fire()
  assert.equal(instance.calls(), 2)
})

test("stopping removes the listeners", () => {
  const instance = setup({unread: 1, focused: false})
  instance.stop()
  assert.equal(instance.listeners.size, 0)
})
