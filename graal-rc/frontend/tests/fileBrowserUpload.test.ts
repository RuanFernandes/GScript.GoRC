import assert from "node:assert/strict"
import test from "node:test"

import {fileBrowserFileExists, fileBrowserUploadPath} from "../src/lib/fileBrowserUpload.ts"

test("upload paths use the selected remote folder and normalize separators", () => {
  assert.equal(fileBrowserUploadPath("levels\\forest\\", "guard.png"), "levels/forest/guard.png")
  assert.equal(fileBrowserUploadPath("", "guard.png"), "guard.png")
  assert.equal(fileBrowserUploadPath("/", "guard.png"), "guard.png")
})

test("upload backup check matches files in the current listing, not folders", () => {
  const files = [
    {path: "guard.png", rights: "rw", size: 12, modified: 1, isDirectory: false},
    {path: "folder/", rights: "rw", size: 0, modified: 1, isDirectory: true},
  ]

  assert.equal(fileBrowserFileExists(files, "guard.png"), true)
  assert.equal(fileBrowserFileExists(files, "folder"), false)
  assert.equal(fileBrowserFileExists(files, "new.png"), false)
})
