import type {Player} from "@/types"

export function displayPlayerValue(value: string | undefined | null): string {
  return value?.trim() || "—"
}

export function playerDisplayName(player: Player): string {
  return player.nick?.trim() || player.account?.trim() || player.communityName?.trim() || `#${player.id}`
}

export function playerInitials(name: string): string {
  const clean = name.replace(/[^\p{L}\p{N} ]/gu, "").trim()
  if (!clean) return "?"
  const parts = clean.split(/\s+/).slice(0, 2)
  return parts.map((part) => part[0]?.toUpperCase() ?? "").join("") || clean[0]!.toUpperCase()
}

// Deterministic avatar hue so the same player keeps the same accent everywhere.
export function playerHue(name: string): number {
  let hue = 0
  for (let index = 0; index < name.length; index++) hue = (hue * 31 + name.charCodeAt(index)) % 360
  return hue
}
