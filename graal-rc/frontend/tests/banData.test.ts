import assert from "node:assert/strict"
import test from "node:test"

import {buildScopes, isLegacyBanDetails} from "../src/lib/banData.ts"

// Alpaxis answers /openaccess with the legacy ban packet, which grclib hands
// over as "banned=0|1" and the reason. Reading it as lister records showed a
// banned account as not banned.
test("a banned account in the legacy reply is shown as banned", () => {
  const scopes = buildScopes("bob", "", "banned=1\nbad behaviour", [])
  assert.equal(scopes[0].banned, true)
})

test("the legacy reply has the local ban only", () => {
  const scopes = buildScopes("bob", "123", "banned=0\n", [])
  assert.deepEqual(scopes.map((s) => [s.target, s.banned]), [["bob", false], ["", false], ["", false], ["", false]])
})

test("lister records are not taken for the legacy reply", () => {
  const details = "account=bob,world=all,reason=spam"
  assert.equal(isLegacyBanDetails(details), false)
  assert.equal(isLegacyBanDetails(""), false)
  const scopes = buildScopes("bob", "", details, [])
  assert.deepEqual([scopes[0].banned, scopes[1].banned, scopes[1].reason], [false, true, "spam"])
})
