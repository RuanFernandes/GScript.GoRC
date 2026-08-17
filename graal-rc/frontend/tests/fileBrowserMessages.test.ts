import assert from "node:assert/strict"
import test from "node:test"

import {isPreviewTransferMessage, mergeFileBrowserMessage} from "../src/lib/fileBrowserMessages.ts"

test("identifies preview transfer protocol messages by path or basename", () => {
  const previewPaths = ["levels/users/Graal/images/damon_test.gif"]

  assert.equal(isPreviewTransferMessage("Bigfile transfer started: damon_test.gif", previewPaths), true)
  assert.equal(isPreviewTransferMessage("File downloaded: levels\\users\\Graal\\images\\damon_test.gif", previewPaths), true)
  assert.equal(
    isPreviewTransferMessage("Received chunk: 1024/2048 bytes for levels/users/Graal/images/damon_test.gif", previewPaths),
    true,
  )
  assert.equal(isPreviewTransferMessage("File downloaded: another.gif", previewPaths), false)
})

test("keeps only the newest received chunk for one file", () => {
  let messages: string[] = []
  messages = mergeFileBrowserMessage(messages, "Received chunk: 3840000/4042614 bytes for tutorial_Placing NPCs and Images.mp4")
  messages = mergeFileBrowserMessage(messages, "Received chunk: 4000000/4042614 bytes for tutorial_Placing NPCs and Images.mp4")
  messages = mergeFileBrowserMessage(messages, "Received chunk: 4042614/4042614 bytes for tutorial_Placing NPCs and Images.mp4")

  assert.deepEqual(messages, ["Received chunk: 4042614/4042614 bytes for tutorial_Placing NPCs and Images.mp4"])
})

test("replaces progress with completion and ignores a late chunk", () => {
  let messages = ["Connected"]
  messages = mergeFileBrowserMessage(messages, "Received chunk: 4000000/4042614 bytes for tutorial.mp4")
  messages = mergeFileBrowserMessage(messages, "File downloaded: tutorial.mp4")
  messages = mergeFileBrowserMessage(messages, "Received chunk: 4042614/4042614 bytes for tutorial.mp4")

  assert.deepEqual(messages, ["Connected", "File downloaded: tutorial.mp4"])
})

test("keeps independent downloads and ordinary messages separate", () => {
  let messages: string[] = []
  messages = mergeFileBrowserMessage(messages, "Received chunk: 10/20 bytes for first.bin")
  messages = mergeFileBrowserMessage(messages, "Received chunk: 10/30 bytes for second.bin")
  messages = mergeFileBrowserMessage(messages, "Received chunk: 20/20 bytes for first.bin")

  assert.deepEqual(messages, [
    "Received chunk: 20/20 bytes for first.bin",
    "Received chunk: 10/30 bytes for second.bin",
  ])

  messages = mergeFileBrowserMessage(messages, "Upload complete")
  assert.deepEqual(messages, [
    "Received chunk: 20/20 bytes for first.bin",
    "Received chunk: 10/30 bytes for second.bin",
    "Upload complete",
  ])
})

test("trims blank messages and enforces the history limit", () => {
  let messages: string[] = []
  messages = mergeFileBrowserMessage(messages, "  ")
  assert.deepEqual(messages, [])

  for (const message of ["one", "two", "three", "four"]) {
    messages = mergeFileBrowserMessage(messages, message, 3)
  }
  assert.deepEqual(messages, ["two", "three", "four"])
  assert.deepEqual(mergeFileBrowserMessage(["old"], "new", 1), ["new"])
})
