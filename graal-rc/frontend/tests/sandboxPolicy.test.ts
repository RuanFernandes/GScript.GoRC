import test from "node:test"
import assert from "node:assert/strict"
import {readFileSync} from "node:fs"

import {pluginSandboxPolicy, pluginPreviewPolicy, sandboxPolicyTag} from "../src/plugins/sandboxPolicy.ts"

test("the plugin sandbox may run scripts and nothing else", () => {
  assert.match(pluginSandboxPolicy, /default-src 'none'/)
  assert.match(pluginSandboxPolicy, /script-src 'unsafe-inline' 'unsafe-eval'(;|$)/)
  assert.match(pluginSandboxPolicy, /form-action 'none'/)
  assert.doesNotMatch(pluginSandboxPolicy, /https?:|wss?:|\*|connect-src/)
  assert.doesNotMatch(pluginPreviewPolicy, /https?:|wss?:|\*|connect-src/)
  assert.equal(sandboxPolicyTag("a 'b'"), `<meta http-equiv="Content-Security-Policy" content="a 'b'">`)
})

// Both documents execute plugin bundles, so both need the policy.
test("every document that runs a plugin bundle declares the policy", () => {
  for (const file of ["../src/plugins/runtime.ts", "../src/screens/PluginManagerWindowScreen.tsx"]) {
    const source = readFileSync(new URL(file, import.meta.url), "utf8")
    const documents = source.split("<!doctype html>").slice(1)
    assert.ok(documents.length > 0, file)
    for (const document of documents) {
      assert.ok(document.startsWith("<html><head>${sandboxPolicyTag("), file)
    }
  }
})
