// Content-Security-Policy of the documents that execute plugin bundles. The
// iframe sandbox does not restrict the network, so without this a plugin could
// reach any host and skip the network.http / network.socket approvals; plugins
// must go through the host API instead.
export const pluginSandboxPolicy = "default-src 'none'; script-src 'unsafe-inline' 'unsafe-eval'; form-action 'none'; base-uri 'none'"

// The preview also renders its own inline styles.
export const pluginPreviewPolicy = `${pluginSandboxPolicy}; style-src 'unsafe-inline'`

export function sandboxPolicyTag(policy: string): string {
  return `<meta http-equiv="Content-Security-Policy" content="${policy}">`
}
