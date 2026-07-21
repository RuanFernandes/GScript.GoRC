// Pure helpers that mirror the reference C++ client's server-name/icon parsing
// (getServerListName / getServerListIcon in TServerList.cpp).
import type {Server} from "@/types"

export type ServerTier = "gold" | "classic" | "none"

// Raw listserver names are prefixed "P " (paid/gold) or "U " (classic). Strip
// the prefix for display and derive the tier badge.
export function serverDisplay(name: string): {label: string; tier: ServerTier} {
  if (name.length >= 2 && name[1] === " ") {
    if (name[0] === "P") return {label: name.slice(2), tier: "gold"}
    if (name[0] === "U") return {label: name.slice(2), tier: "classic"}
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
