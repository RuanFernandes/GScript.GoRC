import assert from "node:assert/strict"
import test from "node:test"
import {
  DOWNLOAD_URL_FORMAT,
  dragDownloadUrlValue,
  dragFileName,
  dragFileUrl,
  startRemoteFileDrag,
} from "../src/lib/fileDrag.ts"

const base = "http://127.0.0.1:50321/drag/" + "ab".repeat(32) + "/"

test("the file name is the last path segment", () => {
  assert.equal(dragFileName("levels/town/house.nw"), "house.nw")
  assert.equal(dragFileName("house.nw"), "house.nw")
  assert.equal(dragFileName("levels\\town\\house.nw"), "house.nw")
  assert.equal(dragFileName("/levels/house.nw"), "house.nw")
})

test("characters that would break the DownloadURL value or a Windows name are replaced", () => {
  // The value is split on ":", so a colon in the name would corrupt the URL field.
  assert.equal(dragFileName("weapons/Staff:News.txt"), "Staff_News.txt")
  assert.equal(dragFileName('a<b>c"d|e?f*g.txt'), "a_b_c_d_e_f_g.txt")
  assert.equal(dragFileName("trailing.dots..."), "trailing.dots")
  assert.equal(dragFileName("trailing space "), "trailing space")
  assert.equal(dragFileName("tab\there.txt"), "tab_here.txt")
})

test("a name that ends up empty falls back to a usable one", () => {
  assert.equal(dragFileName(""), "download")
  assert.equal(dragFileName("/"), "download")
  assert.equal(dragFileName("..."), "download")
})

test("non-ASCII and spaces are kept in the name", () => {
  assert.equal(dragFileName("levels/árvore velha.nw"), "árvore velha.nw")
})

test("the URL is the base plus the escaped path, segment by segment", () => {
  assert.equal(dragFileUrl(base, "levels/house.nw"), base + "levels/house.nw")
  assert.equal(dragFileUrl(base, "/levels//house.nw"), base + "levels/house.nw")
  assert.equal(dragFileUrl(base, "levels/my house #1.nw"), base + "levels/my%20house%20%231.nw")
  assert.equal(dragFileUrl(base, "levels/árvore.nw"), base + "levels/%C3%A1rvore.nw")
  // A "?" or "#" in a name must stay part of the path, not start a query or fragment.
  assert.equal(dragFileUrl(base, "a/what?.txt"), base + "a/what%3F.txt")
  // A backslash from a Windows-style listing is a separator, not part of a name.
  assert.equal(dragFileUrl(base, "levels\\house.nw"), base + "levels/house.nw")
})

test("a colon in the path cannot add a field to the DownloadURL value", () => {
  const value = dragDownloadUrlValue(base, "weapons/Staff:News.txt")
  const [mime, name, ...url] = value.split(":")
  assert.equal(mime, "application/octet-stream")
  assert.equal(name, "Staff_News.txt")
  // The URL itself legitimately contains ":" (after the scheme and before the port).
  assert.equal(url.join(":"), base + "weapons/Staff%3ANews.txt")
})

test("the drag carries one DownloadURL value and allows copying only", () => {
  const stored: Record<string, string> = {}
  const dataTransfer = {
    effectAllowed: "uninitialized" as DataTransfer["effectAllowed"],
    setData: (format: string, value: string) => { stored[format] = value },
  }
  const started = startRemoteFileDrag(dataTransfer, base, {path: "levels/house.nw", isDirectory: false})
  assert.equal(started, true)
  assert.equal(dataTransfer.effectAllowed, "copy")
  assert.deepEqual(Object.keys(stored), [DOWNLOAD_URL_FORMAT])
  assert.equal(stored[DOWNLOAD_URL_FORMAT], `application/octet-stream:house.nw:${base}levels/house.nw`)
})

test("folders and a missing base are left alone", () => {
  const calls: string[] = []
  const dataTransfer = {effectAllowed: "uninitialized" as DataTransfer["effectAllowed"], setData: (format: string) => { calls.push(format) }}
  assert.equal(startRemoteFileDrag(dataTransfer, base, {path: "levels", isDirectory: true}), false)
  assert.equal(startRemoteFileDrag(dataTransfer, null, {path: "levels/house.nw", isDirectory: false}), false)
  assert.equal(startRemoteFileDrag(dataTransfer, "", {path: "levels/house.nw", isDirectory: false}), false)
  assert.equal(startRemoteFileDrag(null, base, {path: "levels/house.nw", isDirectory: false}), false)
  assert.deepEqual(calls, [])
  assert.equal(dataTransfer.effectAllowed, "uninitialized")
})
