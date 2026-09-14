import assert from "node:assert/strict"
import test from "node:test"

import {GraalScriptLspClient} from "../src/lib/graalScriptLspClient.ts"

const lintDiagnostic = {
  range: {
    start: {line: 0, character: 0},
    end: {line: 0, character: 1},
  },
  severity: 1,
  message: "Expected ';' after statement.",
}

function createFakeServer(diagnosticDelayMs = 0, onDiagnosticStart?: () => void) {
  const messages: Array<Record<string, any>> = []
  let text = ""

  const transport = async (payload: string): Promise<string> => {
    const message = JSON.parse(payload) as Record<string, any>
    messages.push(message)

    if (message.method === "textDocument/didOpen") {
      text = message.params.textDocument.text
    } else if (message.method === "textDocument/didChange") {
      text = message.params.contentChanges[0].text
    }

    if (message.id === undefined) return "{}"

    let result: unknown = null
    if (message.method === "initialize") {
      result = {capabilities: {}}
    } else if (message.method === "textDocument/diagnostic") {
      const diagnosedText = text
      onDiagnosticStart?.()
      if (diagnosticDelayMs > 0) {
        await new Promise(resolve => setTimeout(resolve, diagnosticDelayMs))
      }
      result = {kind: "full", items: diagnosedText.includes("lint-error") ? [lintDiagnostic] : []}
    }

    return JSON.stringify({jsonrpc: "2.0", id: message.id, result})
  }

  return {messages, transport}
}

async function openClient(changeDebounceMs: number, diagnosticDelayMs = 0, onDiagnosticStart?: () => void) {
  const server = createFakeServer(diagnosticDelayMs, onDiagnosticStart)
  const client = new GraalScriptLspClient(server.transport, changeDebounceMs)
  await client.initialize("")
  await client.open({uri: {toString: () => "memory://editor"}, getValue: () => "clean"}, "memory://editor")
  return {...server, client}
}

test("debounces full-document changes and resolves waiters with the latest diagnostics", async () => {
  const {client, messages} = await openClient(10)

  const earlierChange = client.change("lint-error")
  const latestChange = client.change("clean")
  const [earlierDiagnostics, latestDiagnostics] = await Promise.all([earlierChange, latestChange])

  assert.deepEqual(earlierDiagnostics, [])
  assert.deepEqual(latestDiagnostics, [])
  const changes = messages.filter(message => message.method === "textDocument/didChange")
  assert.equal(changes.length, 1)
  assert.equal(changes[0].params.contentChanges[0].text, "clean")
  assert.equal(messages.filter(message => message.method === "textDocument/diagnostic").length, 2)

  await client.close()
})

test("diagnostics on save flush pending text and reuse its completed result", async () => {
  const {client, messages} = await openClient(10_000)

  const earlierChange = client.change("clean")
  const latestChange = client.change("lint-error")
  const diagnostics = await client.diagnostics()

  assert.deepEqual(diagnostics, [lintDiagnostic])
  assert.deepEqual(await earlierChange, diagnostics)
  assert.deepEqual(await latestChange, diagnostics)
  assert.deepEqual(await client.diagnostics(), diagnostics)
  assert.equal(messages.filter(message => message.method === "textDocument/didChange").length, 1)
  assert.equal(messages.filter(message => message.method === "textDocument/diagnostic").length, 2)

  await client.close()
})

test("language requests flush pending document changes before querying the server", async () => {
  const {client, messages} = await openClient(10_000)

  const pendingChange = client.change("lint-error")
  await client.completion({line: 0, character: 0})
  await pendingChange

  const changes = messages.filter(message => message.method === "textDocument/didChange")
  assert.equal(changes.length, 1)
  assert.equal(changes[0].params.contentChanges[0].text, "lint-error")
  assert.equal(messages.filter(message => message.method === "textDocument/diagnostic").length, 2)

  await client.close()
})

test("save validation follows edits made while an earlier diagnostic request is in flight", async () => {
  let diagnosticCount = 0
  let signalSecondDiagnostic!: () => void
  const secondDiagnosticStarted = new Promise<void>(resolve => {
    signalSecondDiagnostic = resolve
  })
  const {client, messages} = await openClient(10_000, 30, () => {
    diagnosticCount++
    if (diagnosticCount === 2) signalSecondDiagnostic()
  })
  const firstChange = client.change("lint-error")
  const saveValidation = client.diagnostics()
  await secondDiagnosticStarted

  const latestChange = client.change("clean")
  const diagnostics = await saveValidation

  assert.deepEqual(diagnostics, [])
  assert.deepEqual(await firstChange, [lintDiagnostic])
  assert.deepEqual(await latestChange, [])
  const changes = messages.filter(message => message.method === "textDocument/didChange")
  assert.deepEqual(changes.map(message => message.params.contentChanges[0].text), ["lint-error", "clean"])

  await client.close()
})
