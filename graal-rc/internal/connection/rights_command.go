package connection

// /openrights as the Alpaxis Remote Control does it: the typed line goes to the
// server as RC chat (a bare command gets this account), the server resolves the
// name, and the editor opens when its reply arrives. Asking by packet (the
// player menu) needs a length byte before the account.

import (
	"errors"
	"strings"
	"time"
)

const (
	userRightsRequestTTL = scriptTimeout
	userRightsReplyTTL   = 30 * time.Second
	maxRightsAccountLen  = 223
)

// userRightsRequest is a /openrights typed in the chat and not answered yet.
type userRightsRequest struct {
	target  string
	expires time.Time
}

type userRightsReply struct {
	data    RightsData
	expires time.Time
}

// rightsChatCommand reports whether line is /openrights, and returns the line to
// send and the account it asks for.
func rightsChatCommand(line, selfAccount string) (command, target string, ok bool) {
	line = strings.TrimSpace(line)
	name, argument, _ := strings.Cut(line, " ")
	if !strings.EqualFold(name, "/openrights") {
		return "", "", false
	}
	target = strings.TrimSpace(argument)
	if target != "" {
		return line, target, true
	}
	selfAccount = strings.TrimSpace(selfAccount)
	if selfAccount == "" {
		return line, "", true
	}
	// The Alpaxis server does not answer the command without an account.
	return "/openrights " + selfAccount, selfAccount, true
}

// rightsRequestArgument is the payload of PLI_RC_PLAYERRIGHTSGET on Alpaxis.
// grclib sends these bytes as they are, so the length byte is added here.
func rightsRequestArgument(account string) (string, error) {
	if len(account) > maxRightsAccountLen {
		return "", errors.New("account name is too long")
	}
	return string([]byte{byte(len(account) + 32)}) + account, nil
}

// SetRightsWindowOpener wires what opens the rights editor of an account when
// the reply to a typed /openrights arrives.
func (s *Service) SetRightsWindowOpener(open func(account string)) {
	s.userRightsMu.Lock()
	s.openRightsEditor = open
	s.userRightsMu.Unlock()
}

func (s *Service) addUserRightsRequest(target string, now time.Time) {
	s.userRightsMu.Lock()
	s.userRightsRequests = append(s.userRightsRequests, userRightsRequest{target: target, expires: now.Add(userRightsRequestTTL)})
	s.userRightsMu.Unlock()
}

// claimUserRightsRequest consumes the typed command a reply answers: the one
// naming that account, or else the oldest one when no other request took the
// reply (the server resolves partial and community names).
func (s *Service) claimUserRightsRequest(account string, answeredOther bool, now time.Time) bool {
	s.userRightsMu.Lock()
	defer s.userRightsMu.Unlock()
	live := s.userRightsRequests[:0]
	for _, request := range s.userRightsRequests {
		if !now.After(request.expires) {
			live = append(live, request)
		}
	}
	s.userRightsRequests = live
	match := -1
	for i, request := range live {
		if strings.EqualFold(request.target, account) {
			match = i
			break
		}
	}
	if match < 0 && !answeredOther && len(live) > 0 {
		match = 0
	}
	if match < 0 {
		return false
	}
	s.userRightsRequests = append(live[:match], live[match+1:]...)
	return true
}

// deliverRightsReply routes a rights reply to the request waiting for it and to
// the typed command that asked for it.
func (s *Service) deliverRightsReply(data RightsData) {
	resolved := s.resolveEditorForAccount("rights", data.Account, data)
	if data.Account == "" || !s.claimUserRightsRequest(data.Account, resolved, time.Now()) {
		return
	}
	s.userRightsMu.Lock()
	if s.userRightsReplies == nil {
		s.userRightsReplies = map[string]userRightsReply{}
	}
	s.userRightsReplies[strings.ToLower(data.Account)] = userRightsReply{data: data, expires: time.Now().Add(userRightsReplyTTL)}
	open := s.openRightsEditor
	s.userRightsMu.Unlock()
	if open != nil {
		open(data.Account)
	}
}

// takeRightsReply hands the stored reply to the editor opened for it, so the
// editor does not ask the server a second time.
func (s *Service) takeRightsReply(account string, now time.Time) (RightsData, bool) {
	key := strings.ToLower(strings.TrimSpace(account))
	s.userRightsMu.Lock()
	defer s.userRightsMu.Unlock()
	reply, ok := s.userRightsReplies[key]
	delete(s.userRightsReplies, key)
	if !ok || now.After(reply.expires) {
		return RightsData{}, false
	}
	return reply.data, true
}

func (s *Service) clearUserRights() {
	s.userRightsMu.Lock()
	s.userRightsRequests, s.userRightsReplies = nil, nil
	s.userRightsMu.Unlock()
}
