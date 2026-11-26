import assert from "node:assert/strict"
import test from "node:test"

import {isAttrOn, withReplyWorld} from "../src/lib/playerAttrs.ts"

// grclib writes the status lines as true/false; reading them as off made Apply
// clear the options on the character.
test("a status line written by grclib is read", () => {
  assert.equal(isAttrOn("true"), true)
  assert.equal(isAttrOn("1"), true)
  assert.equal(isAttrOn("false"), false)
  assert.equal(isAttrOn("0"), false)
  assert.equal(isAttrOn(undefined), false)
})

// The editor text has no world; the Alpaxis RC sends back the one it received.
test("saving keeps the world of the reply", () => {
  const saved = withReplyWorld('{"10":"idle"}', '{"account":"bob","world":"main"}')
  assert.deepEqual(JSON.parse(saved), {"10": "idle", world: "main"})
  assert.equal(withReplyWorld('{"10":"idle"}', "not json"), '{"10":"idle"}')
})
