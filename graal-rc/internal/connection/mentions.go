package connection

import (
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type mentionCandidate struct {
	value string
}

// IsSelfMention reports whether text contains an explicit @ mention of one of
// the authenticated player's account identities. The login nickname is not
// considered an account identity: a chat line must address the account or
// community name that the server knows for the current player.
func (s *Service) IsSelfMention(text string) bool {
	return s.SelfMentionTarget(text) != ""
}

// SelfMentionTarget returns the account/community alias addressed by an
// explicit @ mention, or an empty string when the line is not for this player.
// Keeping the matched alias available lets callers make one scan decision and
// report it without duplicating the matcher or relying on stale UI state.
func (s *Service) SelfMentionTarget(text string) string {
	if s == nil {
		return ""
	}
	return findExplicitMention(text, s.selfMentionAliases())
}

func (s *Service) selfMentionAliases() []string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	account := s.creds.Account
	s.mu.Unlock()

	s.rightsMu.RLock()
	canonicalAccount := s.selfRightsAccount
	communityName := s.selfRightsCommunityName
	s.rightsMu.RUnlock()

	aliases := uniqueMentionAliases(account, canonicalAccount, communityName)
	accountAliases := uniqueMentionAliases(account, canonicalAccount)

	// Player properties are delivered independently from the rights response.
	// Join the community name for the local player's account when the rights
	// stream did not expose it yet (or when the server only exposes it there).
	s.playerIdentityMu.Lock()
	for playerID, playerAccount := range s.playerAccounts {
		if playerID != s.selfPlayerID && !equalFoldAny(playerAccount, accountAliases...) {
			continue
		}
		aliases = append(aliases, s.playerCommunities[playerID])
	}
	s.playerIdentityMu.Unlock()

	return uniqueMentionAliases(aliases...)
}

func uniqueMentionAliases(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	aliases := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(normalizeMentionText(value))
		if value == "" {
			continue
		}
		normalized := strings.ToLower(value)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		aliases = append(aliases, value)
	}
	return aliases
}

// findExplicitMention matches a complete identity immediately following @.
// The boundary checks keep @Account from matching an account suffix in an
// email, a longer account, or an identifier such as account_name.
func findExplicitMention(text string, aliases []string) string {
	text = normalizeMentionText(text)
	candidates := buildMentionCandidates(aliases)
	if text == "" || len(candidates) == 0 {
		return ""
	}

	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		if r == '@' && mentionBoundaryBefore(text, offset) {
			remainderStart := offset + size
			remainder := text[remainderStart:]
			for _, candidate := range candidates {
				if len(remainder) < len(candidate.value) ||
					!strings.EqualFold(remainder[:len(candidate.value)], candidate.value) {
					continue
				}
				end := remainderStart + len(candidate.value)
				if mentionBoundaryAfter(text, end) {
					return candidate.value
				}
			}
		}
		offset += size
	}
	return ""
}

func buildMentionCandidates(aliases []string) []mentionCandidate {
	seen := make(map[string]struct{}, len(aliases))
	candidates := make([]mentionCandidate, 0, len(aliases))
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		normalized := strings.ToLower(alias)
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, mentionCandidate{value: alias})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return utf8.RuneCountInString(candidates[i].value) > utf8.RuneCountInString(candidates[j].value)
	})
	return candidates
}

func mentionBoundaryBefore(text string, offset int) bool {
	if offset == 0 {
		return true
	}
	previous, _ := utf8.DecodeLastRuneInString(text[:offset])
	return previous != '@' && !isMentionWordRune(previous)
}

func mentionBoundaryAfter(text string, offset int) bool {
	if offset >= len(text) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(text[offset:])
	return !isMentionWordRune(next)
}

func isMentionWordRune(value rune) bool {
	return value == '_' || unicode.IsLetter(value) || unicode.IsDigit(value)
}

func normalizeMentionText(text string) string {
	return strings.Map(func(value rune) rune {
		if unicode.IsControl(value) || unicode.Is(unicode.Cf, value) {
			return -1
		}
		return value
	}, text)
}

func isChatDisplayMarker(value string) bool {
	if strings.EqualFold(value, "RC") || strings.EqualFold(value, "NC") || strings.EqualFold(value, "IRC") {
		return true
	}
	if len(value) != 5 || value[2] != ':' {
		return false
	}
	return isASCIIDigit(value[0]) && isASCIIDigit(value[1]) && isASCIIDigit(value[3]) && isASCIIDigit(value[4])
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}
