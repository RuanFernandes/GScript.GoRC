import assert from "node:assert/strict"
import test from "node:test"

import {buildPlayerMentionMatcher} from "../src/lib/playerMentions.ts"

const players = [
  {account: "Graal4970183", id: 7, nick: "ABlakBanana", level: "", communityName: "ruanf"},
  {account: "ThunderCowNinja", id: 8, nick: "Thunder", level: "10", communityName: ""},
]

test("matches account and community names case-insensitively", () => {
  const matches = buildPlayerMentionMatcher(players).find("ruanf linked Graal4970183")

  assert.deepEqual(matches.map((match) => ({value: "ruanf linked Graal4970183".slice(match.start, match.end), id: match.player.id, identity: match.identity})), [
    {value: "ruanf", id: 7, identity: "communityName"},
    {value: "Graal4970183", id: 7, identity: "account"},
  ])
})

test("does not match an identity embedded in another word", () => {
  const matcher = buildPlayerMentionMatcher([{account: "Ruan", id: 1, nick: "", level: "", communityName: ""}])

  assert.equal(matcher.find("ruanf Ruan_2 Ruan!").length, 1)
  assert.equal(matcher.find("ruanf Ruan_2 Ruan!")[0]?.start, 13)
})

test("prefers the longest identity when names overlap", () => {
  const matcher = buildPlayerMentionMatcher([
    {account: "Graal", id: 1, nick: "", level: "", communityName: ""},
    {account: "Graal4970183", id: 2, nick: "", level: "", communityName: ""},
  ])

  const matches = matcher.find("Graal4970183")
  assert.equal(matches.length, 1)
  assert.equal(matches[0]?.player.id, 2)
})
