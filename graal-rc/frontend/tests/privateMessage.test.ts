import assert from "node:assert/strict"
import test from "node:test"

import {
  containsUnsafePrivateMessageMarkup,
  normalizePrivateMessageText,
} from "../src/lib/privateMessage.ts"

test("decodes Graal comma-text multiline PMs", () => {
  assert.equal(
    normalizePrivateMessageText('"Oi","Linha 2","Linha3",'),
    "Oi\nLinha 2\nLinha3",
  )
})

test("decodes JSON string arrays and keeps ordinary text intact", () => {
  assert.equal(normalizePrivateMessageText('["Oi","Linha 2"]'), "Oi\nLinha 2")
  assert.equal(normalizePrivateMessageText("Oi, tudo bem?"), "Oi, tudo bem?")
  assert.equal(normalizePrivateMessageText('"Oi"'), '"Oi"')
})

test("decodes commas and escaped quotes inside a PM line", () => {
  assert.equal(normalizePrivateMessageText('"Oi, tudo bem?","Linha 2"'), "Oi, tudo bem?\nLinha 2")
  assert.equal(normalizePrivateMessageText('"""Oi""","Linha 2"'), '"Oi"\nLinha 2')
})

test("detects executable markup without blocking ordinary text", () => {
  assert.equal(containsUnsafePrivateMessageMarkup("Oi <3"), false)
  assert.equal(containsUnsafePrivateMessageMarkup("2 < 3"), false)
  assert.equal(containsUnsafePrivateMessageMarkup("<script>alert(1)</script>"), true)
  assert.equal(containsUnsafePrivateMessageMarkup('<img src=x onerror="alert(1)">'), true)
  assert.equal(containsUnsafePrivateMessageMarkup("javascript:alert(1)"), true)
})
