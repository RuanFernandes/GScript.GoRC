import assert from "node:assert/strict"
import test from "node:test"
import {
  appendChatMessage,
  clearChannelMessages,
  createChatLineLogger,
  initialChatSession,
  moveChatTab,
  reconcileChannelTabs,
  retainChannelHistory,
  type ChatSession,
} from "../src/lib/chatSession.ts"
import type {ChatMessage} from "../src/types/index.ts"

function message(id: number, channel = "", text = `line ${id}`): ChatMessage {
  return {id, channel, text, source: "rc", ts: 0}
}

const texts = (state: ChatSession, channel: string) =>
  state.tabs.find((tab) => tab.channel === channel)?.messages.map((m) => m.text)

const settle = () => new Promise((resolve) => setImmediate(resolve))

test("a message creates its channel tab once and is appended in order", () => {
  let state = initialChatSession("RC Chat")
  state = appendChatMessage(state, message(1, "#a"))
  state = appendChatMessage(state, message(2, "#a"))
  assert.deepEqual(state.tabs.map((tab) => tab.channel), ["", "#a"])
  assert.deepEqual(texts(state, "#a"), ["line 1", "line 2"])
})

test("a tab keeps only the newest lines", () => {
  let state = initialChatSession("RC Chat")
  for (let id = 1; id <= 1005; id++) state = appendChatMessage(state, message(id))
  assert.equal(state.tabs[0].messages.length, 1000)
  assert.equal(state.tabs[0].messages[0].id, 6)
})

test("leaving a channel drops its history", () => {
  let state = appendChatMessage(initialChatSession("RC Chat"), message(1, "#a"))
  state = reconcileChannelTabs(state, [])
  state = reconcileChannelTabs(state, ["#a"])
  assert.deepEqual(texts(state, "#a"), [])
})

test("channel history survives the empty snapshots of an automatic reconnect", () => {
  let state = appendChatMessage(initialChatSession("RC Chat"), message(1, "#a"))
  state = retainChannelHistory(state)
  state = reconcileChannelTabs(state, [])
  state = reconcileChannelTabs(state, [])
  assert.deepEqual(state.tabs.map((tab) => tab.channel), [""])
  state = reconcileChannelTabs(state, ["#a"])
  assert.deepEqual(texts(state, "#a"), ["line 1"])
  // Restored once: a later real leave is final.
  state = reconcileChannelTabs(reconcileChannelTabs(state, []), ["#a"])
  assert.deepEqual(texts(state, "#a"), [])
})

test("a line that arrives before the rejoin snapshot follows the retained history", () => {
  let state = appendChatMessage(initialChatSession("RC Chat"), message(1, "#a"))
  state = reconcileChannelTabs(retainChannelHistory(state), [])
  state = appendChatMessage(state, message(2, "#a"))
  assert.deepEqual(texts(state, "#a"), ["line 1", "line 2"])
})

test("a second disconnect keeps history that was not restored yet", () => {
  let state = appendChatMessage(initialChatSession("RC Chat"), message(1, "#a"))
  state = reconcileChannelTabs(retainChannelHistory(state), [])
  state = reconcileChannelTabs(retainChannelHistory(state), ["#a"])
  assert.deepEqual(texts(state, "#a"), ["line 1"])
})

test("clearing a channel and moving tabs", () => {
  let state = appendChatMessage(initialChatSession("RC Chat"), message(1, "#a"))
  state = moveChatTab(state, 1, 0)
  assert.deepEqual(state.tabs.map((tab) => tab.channel), ["#a", ""])
  state = clearChannelMessages(state, "#a")
  assert.deepEqual(texts(state, "#a"), [])
  assert.equal(moveChatTab(state, 0, 5), state)
})

test("the logger writes each line once, one at a time, in arrival order", async () => {
  const written: string[] = []
  const pending: (() => void)[] = []
  const log = createChatLineLogger(() => true, (line) => new Promise<void>((resolve) => {
    pending.push(() => { written.push(line); resolve() })
  }))
  log({...message(1), text: "first"})
  log({...message(2, "#never-opened"), text: "second"})
  await settle()
  assert.equal(pending.length, 1, "the second write waits for the first")
  pending[0]()
  await settle()
  pending[1]()
  await settle()
  assert.deepEqual(written.map((line) => line.replace(/^\[\d\d:\d\d\] /, "")), ["[RC] first", "[RC] second"])
})

test("the logger skips lines that arrive while logging is off and survives a failed write", async () => {
  let enabled = false
  const written: string[] = []
  const log = createChatLineLogger(() => enabled, async (line) => {
    if (line.endsWith("bad")) throw new Error("disk full")
    written.push(line.slice(line.lastIndexOf(" ") + 1))
  })
  log({...message(1), text: "off"})
  enabled = true
  log({...message(2), text: "bad"})
  log({...message(3), text: "good"})
  await settle()
  assert.deepEqual(written, ["good"])
})
