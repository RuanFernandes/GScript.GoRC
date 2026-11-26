import assert from "node:assert/strict"
import test from "node:test"
import {
  applyCompletion,
  completionItems,
  completionKey,
  defaultSelection,
  highlightParts,
  keepsOpenAfterAccept,
  type CompletionKey,
  type CompletionPlayer,
} from "../src/lib/chatCompletion.ts"

const players: CompletionPlayer[] = [
  {account: "Zeca", nick: "Ruanzinho"},
  {account: "ruan2", nick: "Outro"},
  {account: "Ruan", nick: "Admin"},
  {account: "ruan", nick: "duplicado"},
  {account: "", nick: "sem conta"},
  {account: "Maria", nick: "Ma Silva"},
]

const items = (text: string, plugins: string[] = []) => completionItems(text, players, plugins)
const values = (text: string, plugins: string[] = []) => items(text, plugins).map((item) => item.value)

test("/refreshfilesystem is offered", () => {
  assert.deepEqual(values("/refreshf"), ["refreshfilesystem"])
  assert.ok(values("/").includes("refreshfilesystem"))
})

test("nothing is offered outside a slash command", () => {
  assert.deepEqual(values(""), [])
  assert.deepEqual(values("hello"), [])
  assert.deepEqual(values(" /open"), [])
})

test("commands are ranked exact, prefix, substring, then subsequence", () => {
  assert.deepEqual(values("/npc").slice(0, 3), ["npc", "npcstart", "npckill"])
  const rights = values("/rights")
  assert.equal(rights[0], "openrights")
  const ranked = values("/st")
  assert.ok(ranked.indexOf("stats") < ranked.indexOf("clientstats"), "prefix beats substring")
  assert.equal(values("/orig")[0], "openrights")
})

test("a subsequence must start at the first letter of the command", () => {
  assert.ok(!values("/pnr").includes("openrights"))
})

test("matched characters are reported for highlighting", () => {
  assert.deepEqual(items("/rights")[0].labelMatches, [5, 6, 7, 8, 9, 10])
  assert.deepEqual(items("/orig")[0].labelMatches, [1, 5, 6, 7])
  assert.equal(items("/rights")[0].label, "/openrights")
})

test("highlightParts groups consecutive characters", () => {
  assert.deepEqual(highlightParts("/openrights", [1, 5, 6]), [
    {text: "/", match: false},
    {text: "o", match: true},
    {text: "pen", match: false},
    {text: "ri", match: true},
    {text: "ghts", match: false},
  ])
  assert.deepEqual(highlightParts("abc", []), [{text: "abc", match: false}])
})

test("plugin commands are offered once", () => {
  assert.deepEqual(values("/hel", ["hello", "hello", "help"]).filter((v) => v === "hello" || v === "help"), ["help", "hello"])
})

test("players match by account or nick, once per account", () => {
  const found = items("/open ruan")
  assert.deepEqual(found.map((item) => item.value), ["Ruan", "ruan2", "Zeca"])
  assert.deepEqual(found[0].labelMatches, [0, 1, 2, 3])
  assert.equal(found[2].detail, "Ruanzinho")
  assert.deepEqual(found[2].detailMatches, [0, 1, 2, 3])
  assert.deepEqual(values("/open silva"), ["Maria"])
})

test("only account commands complete an argument", () => {
  assert.deepEqual(values("/stats ruan"), [])
  assert.deepEqual(values("/unknown ruan"), [])
})

test("text after a complete account is not completed again", () => {
  assert.deepEqual(values("/disconnect Ruan "), [])
  assert.deepEqual(values("/disconnect Ruan spam"), [])
})

test("accepting adds a space only to commands that take an account", () => {
  assert.equal(applyCompletion("/openr", items("/openr")[0]), "/openrights ")
  assert.equal(applyCompletion("/cle", items("/cle")[0]), "/clear")
  assert.equal(applyCompletion("/open  ru", items("/open  ru")[0]), "/open Ruan")
})

test("the list stays open after accepting a command that takes an account", () => {
  assert.equal(keepsOpenAfterAccept(items("/openr")[0]), true)
  assert.equal(keepsOpenAfterAccept(items("/cle")[0]), false)
  assert.equal(keepsOpenAfterAccept(items("/open ru")[0]), false)
})

// An empty argument must not preselect a player: Enter on "/disconnect " would
// otherwise pick whoever sorts first.
test("nothing is preselected while the account is still empty", () => {
  assert.equal(defaultSelection("/disconnect ", items("/disconnect ")), -1)
  assert.equal(defaultSelection("/disconnect r", items("/disconnect r")), 0)
  assert.equal(defaultSelection("/", items("/")), 0)
  assert.equal(defaultSelection("/zzzz", []), -1)
})

function press(text: string, key: string, extra: Partial<CompletionKey> = {}, open = true, selected?: number) {
  const list = items(text)
  return completionKey(
    {key, shift: false, ctrl: false, alt: false, meta: false, composing: false, ...extra},
    {open, text, items: list, selected: selected ?? defaultSelection(text, list)},
  )
}

test("arrows move through the list and wrap around", () => {
  const count = items("/open").length
  assert.deepEqual(press("/open", "ArrowDown"), {type: "move", index: 1})
  assert.deepEqual(press("/open", "ArrowUp"), {type: "move", index: count - 1})
  assert.deepEqual(press("/open", "ArrowDown", {}, true, count - 1), {type: "move", index: 0})
  assert.deepEqual(press("/open ", "ArrowDown"), {type: "move", index: 0})
  assert.deepEqual(press("/open ", "ArrowUp"), {type: "move", index: items("/open ").length - 1})
})

test("page keys jump without wrapping", () => {
  const count = items("/").length
  assert.deepEqual(press("/", "PageDown"), {type: "move", index: 8})
  assert.deepEqual(press("/", "PageUp"), {type: "move", index: 0})
  assert.deepEqual(press("/", "PageDown", {}, true, count - 2), {type: "move", index: count - 1})
})

test("Tab and Enter accept the selected suggestion", () => {
  assert.deepEqual(press("/openr", "Tab"), {type: "accept", index: 0})
  assert.deepEqual(press("/openr", "Enter"), {type: "accept", index: 0})
  assert.deepEqual(press("/open", "Enter", {}, true, 1), {type: "accept", index: 1})
})

test("Tab takes the first player when none is selected, Enter sends", () => {
  assert.deepEqual(press("/open ", "Tab"), {type: "accept", index: 0})
  assert.deepEqual(press("/open ", "Enter"), {type: "pass", close: true})
})

test("Enter sends when accepting would not change the text", () => {
  assert.deepEqual(press("/clear", "Enter"), {type: "pass", close: true})
  assert.deepEqual(press("/CLEAR", "Enter"), {type: "pass", close: true})
  assert.deepEqual(press("/npc", "Enter"), {type: "pass", close: true})
  assert.deepEqual(press("/open ruan", "Enter"), {type: "pass", close: true})
  assert.deepEqual(press("/clear", "Tab"), {type: "close"})
})

test("Escape closes the list", () => {
  assert.deepEqual(press("/open", "Escape"), {type: "close"})
  assert.deepEqual(press("/open", "Escape", {}, false), {type: "pass"})
})

test("a closed list leaves arrows and Enter alone, and Tab or Ctrl+Space reopen it", () => {
  assert.deepEqual(press("/openr", "ArrowUp", {}, false), {type: "pass"})
  assert.deepEqual(press("/openr", "Enter", {}, false), {type: "pass"})
  assert.deepEqual(press("/openr", "Tab", {}, false), {type: "open"})
  assert.deepEqual(press("/openr", " ", {ctrl: true}, false), {type: "open"})
  assert.deepEqual(press("hello", "Tab", {}, false), {type: "pass"})
})

test("modified keys and IME composition are left to the input", () => {
  assert.deepEqual(press("/openr", "Tab", {shift: true}), {type: "pass"})
  assert.deepEqual(press("/openr", "Enter", {shift: true}), {type: "pass"})
  assert.deepEqual(press("/openr", "ArrowUp", {alt: true}), {type: "pass"})
  assert.deepEqual(press("/openr", "Enter", {composing: true}), {type: "pass"})
  assert.deepEqual(press("/openr", "a"), {type: "pass"})
})
