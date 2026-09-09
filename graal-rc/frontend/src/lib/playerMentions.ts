import type {Player} from "@/types"

export type PlayerIdentity = "account" | "communityName"

interface PlayerNameCandidate {
  normalized: string
  value: string
  player: Player
  identity: PlayerIdentity
  order: number
}

export interface PlayerMentionMatch {
  start: number
  end: number
  player: Player
  identity: PlayerIdentity
}

export interface PlayerMentionMatcher {
  find(text: string): PlayerMentionMatch[]
}

const WORD_CHARACTER = /[\p{L}\p{N}_]/u

function isWordCharacter(value: string | undefined): boolean {
  return Boolean(value && WORD_CHARACTER.test(value))
}

function normalize(value: string): string {
  return value.trim().toLowerCase()
}

function candidatesFor(players: Player[]): PlayerNameCandidate[] {
  const byName = new Map<string, PlayerNameCandidate>()
  let order = 0

  for (const player of players) {
    const identities: Array<[PlayerIdentity, string]> = [
      ["account", player.account],
      ["communityName", player.communityName],
    ]

    for (const [identity, rawValue] of identities) {
      const value = rawValue?.trim() ?? ""
      const normalized = normalize(value)
      if (!normalized) continue

      const candidate: PlayerNameCandidate = {normalized, value, player, identity, order}
      order += 1
      const existing = byName.get(normalized)

      // Account names are the more stable server identity. Prefer them when a
      // community name happens to collide with another player's account.
      if (!existing || (identity === "account" && existing.identity === "communityName")) {
        byName.set(normalized, candidate)
      }
    }
  }

  return [...byName.values()].sort((left, right) => right.normalized.length - left.normalized.length || left.order - right.order)
}

export function buildPlayerMentionMatcher(players: Player[]): PlayerMentionMatcher {
  const candidates = candidatesFor(players)

  return {
    find(text: string): PlayerMentionMatch[] {
      if (!text || candidates.length === 0) return []

      const normalizedText = text.toLowerCase()
      const matches: PlayerMentionMatch[] = []
      let cursor = 0

      while (cursor < text.length) {
        let matched = false
        if (!isWordCharacter(text[cursor - 1])) {
          for (const candidate of candidates) {
            const end = cursor + candidate.value.length
            if (
              normalizedText.startsWith(candidate.normalized, cursor)
              && !isWordCharacter(text[end])
            ) {
              matches.push({start: cursor, end, player: candidate.player, identity: candidate.identity})
              cursor = end
              matched = true
              break
            }
          }
        }

        // A match advances to its end; otherwise inspect the next character.
        if (matched) continue
        cursor += 1
      }

      return matches
    },
  }
}
