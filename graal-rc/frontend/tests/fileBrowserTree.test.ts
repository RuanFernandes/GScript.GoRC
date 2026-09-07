import assert from "node:assert/strict"
import test from "node:test"

import {buildFileBrowserTree, flattenFileBrowserTree} from "../src/lib/fileBrowserTree.ts"

test("backup tree keeps nested folders when no root rule exists", () => {
  const tree = buildFileBrowserTree([
    {pattern: "accounts/*", rights: "r"},
    {pattern: "characterslots/g/*", rights: "rw"},
  ], true)

  assert.deepEqual(flattenFileBrowserTree(tree).map((node) => node.path), [
    "characterslots",
    "characterslots/g",
    "accounts",
  ])
})

test("backup tree exposes root-level rules through the synthetic root", () => {
  const tree = buildFileBrowserTree([
    {pattern: "levels/*", rights: "r"},
    {pattern: "*.txt", rights: "r"},
  ], true)

  assert.equal(tree.length, 1)
  assert.equal(tree[0].path, "")
  assert.deepEqual(flattenFileBrowserTree(tree).map((node) => node.path), ["", "levels"])
})
