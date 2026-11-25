import assert from "node:assert/strict"
import test from "node:test"
import {
  addPendingRow, buildChanges, canSwitchTable, createLatestGate, createSingleFlight, editPendingCell,
  emptyStaged, isDirty, removePendingRow, stageCell, stageDelete, stagedFor,
} from "../src/lib/sqliteStaging.ts"

test("staged changes are bound to the table they were made on", () => {
  let staged = stageCell(emptyStaged(), "a", 1, "name", "x")
  staged = stageDelete(staged, "a", 2)
  staged = addPendingRow(staged, "a", "t1")
  staged = editPendingCell(staged, "a", "t1", "name", "7")
  assert.deepEqual(buildChanges(staged), {
    table: "a",
    inserts: [{name: 7}],
    updates: [{rowid: 1, column: "name", value: "x"}],
    deletes: [2],
  })
})

test("another table never sees or extends the staged changes", () => {
  const staged = addPendingRow(stageDelete(stageCell(emptyStaged(), "a", 1, "name", "x"), "a", 2), "a", "t1")
  const other = stagedFor(staged, "b")
  assert.equal(isDirty(other), false)
  assert.equal(stagedFor(staged, "a"), staged)
  assert.equal(stageCell(staged, "b", 3, "name", "y"), staged)
  assert.equal(stageDelete(staged, "b", 3), staged)
  assert.equal(addPendingRow(staged, "b", "t2"), staged)
  assert.equal(editPendingCell(staged, "b", "t1", "name", "y"), staged)
  assert.equal(removePendingRow(staged, "b", "t1"), staged)
})

test("a clean overlay has nothing to save and may be reused for any table", () => {
  assert.equal(buildChanges(emptyStaged()), null)
  const cleared = removePendingRow(addPendingRow(emptyStaged(), "a", "t1"), "a", "t1")
  assert.equal(isDirty(cleared), false)
  assert.equal(buildChanges(cleared), null)
  assert.equal(buildChanges(stageDelete(cleared, "b", 1))?.table, "b")
})

test("column names containing a colon survive the round trip", () => {
  const changes = buildChanges(stageCell(emptyStaged(), "a", 4, "ns:key", "v"))
  assert.deepEqual(changes?.updates, [{rowid: 4, column: "ns:key", value: "v"}])
})

test("table switching is blocked while dirty or saving", () => {
  const dirty = stageDelete(emptyStaged(), "a", 1)
  assert.equal(canSwitchTable(dirty, false, "a", "b"), false)
  assert.equal(canSwitchTable(dirty, false, "a", "a"), true)
  assert.equal(canSwitchTable(emptyStaged(), true, "a", "b"), false)
  assert.equal(canSwitchTable(emptyStaged(), false, "a", "b"), true)
})

test("only the latest data request is current", () => {
  const gate = createLatestGate()
  const first = gate()
  assert.equal(first(), true)
  const second = gate()
  assert.equal(first(), false)
  assert.equal(second(), true)
})

test("a second save while one is running joins it instead of committing again", async () => {
  const run = createSingleFlight<boolean>()
  let commits = 0
  let release: () => void = () => {}
  const blocked = new Promise<void>((resolve) => { release = resolve })
  const task = async () => { commits++; await blocked; return true }
  const first = run(task)
  const second = run(task)
  release()
  assert.deepEqual(await Promise.all([first, second]), [true, true])
  assert.equal(commits, 1)
  await run(task)
  assert.equal(commits, 2)
})

test("a failed save does not leave the single flight stuck", async () => {
  const run = createSingleFlight<boolean>()
  await assert.rejects(run(async () => { throw new Error("boom") }), /boom/)
  assert.equal(await run(async () => true), true)
})
