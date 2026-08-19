import assert from "node:assert/strict"
import test from "node:test"

import {SERVER_CONFIG_KEY_PATTERN} from "../src/lib/monacoServerConfig.ts"

test("server option keys keep embedded spaces in the keyword token", () => {
  assert.equal(SERVER_CONFIG_KEY_PATTERN.exec("guild_Global Admin=Hoyt,Merlin")?.[0], "guild_Global Admin")
  assert.equal(SERVER_CONFIG_KEY_PATTERN.exec("guild_Server Admin =Graal768730")?.[0], "guild_Server Admin")
  assert.equal(SERVER_CONFIG_KEY_PATTERN.exec("guild_Global=Hoyt")?.[0], "guild_Global")
})

test("folder-config type lines are not treated as server option keys", () => {
  assert.equal(SERVER_CONFIG_KEY_PATTERN.exec("file levels/example.nw"), null)
})
