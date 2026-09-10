package connection

import (
	"log"
	"strings"
)

type selfChatIdentity struct {
	kind  string
	actor string
	value string
}

// captureSelfChatIdentity learns identities from the server's human-readable
// RC responses, for example "Account's Community Name is Community". Some
// servers send this before the native player-property callback, so it is a
// useful fallback for mention detection.
func (s *Service) captureSelfChatIdentity(text string) bool {
	identity, ok := parseSelfChatIdentity(text)
	if !ok || !s.isSelfRightsAlias(identity.actor) {
		return false
	}

	s.rightsMu.Lock()
	changed := false
	switch identity.kind {
	case "account":
		if s.selfRightsAccount != identity.value {
			s.selfRightsAccount = identity.value
			changed = true
		}
	case "community":
		if s.selfRightsCommunityName != identity.value {
			s.selfRightsCommunityName = identity.value
			changed = true
		}
	}
	s.rightsMu.Unlock()

	if changed {
		logIdentity := identity.kind
		// Account/community names are not credentials and are already displayed
		// in the player list, but keep the log compact and deterministic.
		log.Printf("[identity] chat %s received actor=%q value=%q", logIdentity, identity.actor, identity.value)
		s.resolveSelfPlayerIdentity()
		s.emitEvent("rc:scriptIdentityChanged")
	}
	return true
}

func parseSelfChatIdentity(text string) (selfChatIdentity, bool) {
	content := chatMessageContent(text)
	lower := strings.ToLower(content)
	markers := []struct {
		kind   string
		marker string
	}{
		{kind: "account", marker: "'s account name is "},
		{kind: "community", marker: "'s community name is "},
	}
	for _, candidate := range markers {
		index := strings.Index(lower, candidate.marker)
		if index <= 0 {
			continue
		}
		actor := strings.TrimSpace(content[:index])
		value := strings.TrimSpace(content[index+len(candidate.marker):])
		if actor == "" || value == "" {
			continue
		}
		return selfChatIdentity{kind: candidate.kind, actor: actor, value: value}, true
	}
	return selfChatIdentity{}, false
}

func chatMessageContent(text string) string {
	text = normalizeMentionText(strings.TrimSpace(text))
	text = strings.TrimPrefix(text, "<> ")
	for {
		if !strings.HasPrefix(text, "[") {
			break
		}
		end := strings.IndexByte(text, ']')
		if end < 0 || !isChatDisplayMarker(strings.TrimSpace(text[1:end])) {
			break
		}
		text = strings.TrimSpace(text[end+1:])
	}
	if colon := strings.IndexByte(text, ':'); colon > 0 {
		text = text[colon+1:]
	}
	return strings.TrimSpace(text)
}
