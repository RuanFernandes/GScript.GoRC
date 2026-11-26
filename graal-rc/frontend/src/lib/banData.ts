// The ban reply as the access window shows it.

export interface ScopeState {
  target: string
  banned: boolean
  reset: boolean
  banType: string
  releaseTime: string
  reason: string
}

export function trimBanName(s: string): string {
  return s.replace(/^\s+|\s+$/g, "")
}

// Whether the reply is the legacy one ("banned=0|1" and the reason), which has a
// local ban only and is saved with the legacy packet.
export function isLegacyBanDetails(details: string): boolean {
  return details.split("\n")[0].trim().startsWith("banned=") && !details.includes("account=")
}

function legacyBanned(details: string): boolean {
  const value = details.split("\n")[0].trim().slice("banned=".length)
  return value === "1" || value === "true"
}

export function buildScopes(account: string, computerId: string, details: string, typeNames: string[]): ScopeState[] {
  const scopes: ScopeState[] = [0, 1, 2, 3].map((index) => ({
    target: index < 2 ? account : computerId ? "pc:" + computerId : "",
    banned: false,
    reset: false,
    banType: typeNames[0] ?? "",
    releaseTime: "",
    reason: "",
  }))
  if (isLegacyBanDetails(details)) {
    return scopes.map((scope, index) => index === 0
      ? {...scope, banned: legacyBanned(details)}
      : {...scope, target: ""})
  }
  for (const record of details.split(/\r?\n/)) {
    if (!record.trim()) continue
    const fields: Record<string, string> = {}
    for (const kv of record.split(",")) {
      const sep = kv.indexOf("=")
      if (sep >= 0) fields[kv.slice(0, sep)] = kv.slice(sep + 1)
    }
    const target = fields.account
    const world = fields.world
    if (target === undefined || world === undefined) continue
    const computer = target.startsWith("pc:")
    const index = (computer ? 2 : 0) + (world === "all" ? 1 : 0)
    if (!scopes[index].target) continue
    scopes[index].banned = true
    if (fields.bantype) {
      const bt = trimBanName(fields.bantype)
      if (typeNames.includes(bt)) scopes[index].banType = bt
    }
    if (fields.releasetime) scopes[index].releaseTime = fields.releasetime
    if (fields.reason) scopes[index].reason = fields.reason
  }
  return scopes
}
