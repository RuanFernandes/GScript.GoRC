package connection

// Player-editor replies carry only an account name, and the server may spell it
// differently from the request (another case, or the canonical account when
// asked by community name). This file decides which request, if any, a reply
// answers. A reply that cannot be attributed is dropped: the request times out,
// which is recoverable, while showing one account's data in another account's
// window lets Save write it to the wrong account.

import (
	"strings"
	"time"
)

// unansweredRequestMemory is how long the reply of a timed-out request is still
// considered possible.
const unansweredRequestMemory = 2 * time.Minute

// playerNames is a snapshot of the identities in the player property stream.
type playerNames struct {
	accounts    map[int]string
	communities map[int]string
}

func (s *Service) snapshotPlayerNames() playerNames {
	s.playerIdentityMu.Lock()
	defer s.playerIdentityMu.Unlock()
	names := playerNames{accounts: map[int]string{}, communities: map[int]string{}}
	for id, account := range s.playerAccounts {
		names.accounts[id] = account
	}
	for id, community := range s.playerCommunities {
		names.communities[id] = community
	}
	return names
}

func (n playerNames) isAccount(name string) bool {
	for _, account := range n.accounts {
		if strings.EqualFold(account, name) {
			return true
		}
	}
	return false
}

func (n playerNames) isCommunityOf(community, account string) bool {
	for id, candidate := range n.accounts {
		if strings.EqualFold(candidate, account) && strings.EqualFold(n.communities[id], community) {
			return true
		}
	}
	return false
}

// resolveEditorForAccount delivers a player-editor reply to the request it
// answers, in order of certainty:
//
//  1. the request for that account name (any case);
//  2. the self request, when the account is a known alias of this session;
//  3. the one request the player list links to the account by community name;
//  4. the only request that could be it: a community name, or the self request
//     while the canonical self account is still unknown. This is refused while a
//     different timed-out request may still be answered.
func (s *Service) resolveEditorForAccount(kind, account string, reply any) bool {
	account = strings.TrimSpace(account)
	exactKey := editorKey(kind, account)
	selfKey := editorSelfKey(kind)
	prefix := kind + ":"

	// Gathered before editorMu: these take the session and identity locks.
	isSelf := account != "" && s.isSelfRightsAlias(account)
	s.rightsMu.RLock()
	selfKnown := s.selfRightsAccount != ""
	s.rightsMu.RUnlock()
	players := s.snapshotPlayerNames()

	s.editorMu.Lock()
	defer s.editorMu.Unlock()
	now := time.Now()
	for key, expires := range s.unansweredEditors {
		if now.After(expires) {
			delete(s.unansweredEditors, key)
		}
	}

	deliver := func(key string) bool {
		waiter := s.editor[key]
		delete(s.editor, key)
		return completeEditorWait(waiter, reply)
	}

	if _, ok := s.editor[exactKey]; ok {
		delete(s.unansweredEditors, exactKey)
		return deliver(exactKey)
	}
	if _, late := s.unansweredEditors[exactKey]; late {
		delete(s.unansweredEditors, exactKey)
		return false
	}
	if isSelf {
		delete(s.unansweredEditors, selfKey)
		if _, ok := s.editor[selfKey]; ok {
			return deliver(selfKey)
		}
		return false
	}

	var linked, possible []string
	pending := 0
	for key, waiter := range s.editor {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if waiter.closed {
			delete(s.editor, key)
			continue
		}
		pending++
		if key == selfKey {
			if !selfKnown {
				possible = append(possible, key)
			}
			continue
		}
		requested := strings.TrimPrefix(key, prefix)
		switch {
		case account == "":
			possible = append(possible, key)
		case players.isAccount(requested):
			// An online account was asked for by name, so another name
			// cannot be its canonical spelling.
		case players.isCommunityOf(requested, account):
			linked = append(linked, key)
		default:
			possible = append(possible, key)
		}
	}
	if len(linked) > 0 {
		return len(linked) == 1 && deliver(linked[0])
	}

	var unanswered []string
	for key := range s.unansweredEditors {
		// A late self reply is recognizable once the self account is known.
		if strings.HasPrefix(key, prefix) && !(key == selfKey && selfKnown) {
			unanswered = append(unanswered, key)
		}
	}
	if pending == 0 && len(unanswered) == 1 {
		delete(s.unansweredEditors, unanswered[0])
	}
	if len(possible) != 1 {
		return false
	}
	for _, key := range unanswered {
		if key != possible[0] {
			return false
		}
	}
	delete(s.unansweredEditors, possible[0])
	return deliver(possible[0])
}

// rememberUnansweredLocked records a request whose reply may still arrive. The
// caller holds editorMu.
func (s *Service) rememberUnansweredLocked(key string) {
	if s.unansweredEditors == nil {
		s.unansweredEditors = map[string]time.Time{}
	}
	s.unansweredEditors[key] = time.Now().Add(unansweredRequestMemory)
}

// cacheBanTypes stores the ban-types list unless it comes from a server session
// that has already been replaced.
func (s *Service) cacheBanTypes(epoch uint64, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.serverEpoch != epoch {
		return
	}
	s.editorMu.Lock()
	s.banTypes = content
	s.editorMu.Unlock()
}
