// Pure helpers that mirror the reference C++ client's server-name/icon parsing
// (getServerListName / getServerListIcon in TServerList.cpp).
import type {Server} from "@/types"

export type ServerTier = "gold" | "classic" | "none"

// Raw listserver names carry a single-letter flag prefix + space (e.g.
// "P Testbed" paid/gold, "U Testbed" classic, "H Testbed" hosted/hidden). Strip
// any such prefix for display; only P and U carry a tier badge. Mirrors the
// reference client's getServerListName.
export function serverDisplay(name: string): {label: string; tier: ServerTier} {
  const m = /^([A-Z]) (.+)$/.exec(name)
  if (m) {
    const tier: ServerTier = m[1] === "P" ? "gold" : m[1] === "U" ? "classic" : "none"
    return {label: m[2], tier}
  }
  return {label: name, tier: "none"}
}

export function tierBadge(tier: ServerTier) {
  switch (tier) {
    case "gold":
      return "Gold"
    case "classic":
      return "Classic"
    default:
      return null
  }
}

export function totalPlayers(servers: Server[]): number {
  return servers.reduce((sum, s) => sum + (s.players || 0), 0)
}
