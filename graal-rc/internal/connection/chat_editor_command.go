package connection

// /open and /openrights as the Alpaxis Remote Control does them: the typed line
// goes to the server as RC chat (a bare command gets this account), the server
// resolves the name, and the editor opens when its reply arrives. Asking for
// rights by packet (the player menu) needs a length byte before the account.

import (
	"errors"
	"strings"
	"time"
)

const (
	userEditorRequestTTL = scriptTimeout
	userEditorReplyTTL   = 30 * time.Second
	maxRightsAccountLen  = 223
)

// chatEditorCommands maps a chat command to the kind of editor its reply opens.
var chatEditorCommands = map[string]string{
	"/open":       "attrs",
	"/openrights": "rights",
}

// userEditorRequest is a command typed in the chat and not answered yet.
type userEditorRequest struct {
	kind    string
	target  string
	expires time.Time
}

type userEditorReply struct {
	data    any
	expires time.Time
}

// editorChatCommand reports whether line is one of chatEditorCommands, and
// returns the line to send, the editor it opens and the account it asks for.
func editorChatCommand(line, selfAccount string) (command, kind, target string, ok bool) {
	line = strings.TrimSpace(line)
	name, argument, _ := strings.Cut(line, " ")
	name = strings.ToLower(name)
	kind, ok = chatEditorCommands[name]
	if !ok {
		return "", "", "", false
	}
	target = strings.TrimSpace(argument)
	if target != "" {
		return line, kind, target, true
	}
	selfAccount = strings.TrimSpace(selfAccount)
	if selfAccount == "" {
		return line, kind, "", true
	}
	// The Alpaxis server does not answer these commands without an account.
	return name + " " + selfAccount, kind, selfAccount, true
}

// selfEditorCommand is the chat line that asks for this account's own data.
func selfEditorCommand(name, selfAccount string) string {
	return name + " " + strings.TrimSpace(selfAccount)
}

// rightsRequestArgument is the payload of PLI_RC_PLAYERRIGHTSGET on Alpaxis.
// grclib sends these bytes as they are, so the length byte is added here.
func rightsRequestArgument(account string) (string, error) {
	if len(account) > maxRightsAccountLen {
		return "", errors.New("account name is too long")
	}
	return string([]byte{byte(len(account) + 32)}) + account, nil
}

// SetEditorWindowOpener wires what opens the editor of an account when the reply
// to a typed command arrives. kind is "attrs" or "rights".
func (s *Service) SetEditorWindowOpener(open func(kind, account string)) {
	s.userEditorMu.Lock()
	s.openUserEditor = open
	s.userEditorMu.Unlock()
}

func (s *Service) addUserEditorRequest(kind, target string, now time.Time) {
	s.userEditorMu.Lock()
	s.userEditorRequests = append(s.userEditorRequests, userEditorRequest{kind: kind, target: target, expires: now.Add(userEditorRequestTTL)})
	s.userEditorMu.Unlock()
}

// claimUserEditorRequest consumes the typed command a reply answers: the one
// naming that account, or else the oldest one of that kind when no other request
// took the reply (the server resolves partial and community names).
func (s *Service) claimUserEditorRequest(kind, account string, answeredOther bool, now time.Time) bool {
	s.userEditorMu.Lock()
	defer s.userEditorMu.Unlock()
	live := s.userEditorRequests[:0]
	for _, request := range s.userEditorRequests {
		if !now.After(request.expires) {
			live = append(live, request)
		}
	}
	s.userEditorRequests = live
	match, oldest := -1, -1
	for i, request := range live {
		if request.kind != kind {
			continue
		}
		if oldest < 0 {
			oldest = i
		}
		if strings.EqualFold(request.target, account) {
			match = i
			break
		}
	}
	if match < 0 && !answeredOther {
		match = oldest
	}
	if match < 0 {
		return false
	}
	s.userEditorRequests = append(live[:match], live[match+1:]...)
	return true
}

// deliverEditorReply routes a reply to the request waiting for it and to the
// typed command that asked for it. It reports whether either of them took it.
func (s *Service) deliverEditorReply(kind, account string, data any) bool {
	resolved := s.resolveEditorForAccount(kind, account, data)
	if account == "" || !s.claimUserEditorRequest(kind, account, resolved, time.Now()) {
		return resolved
	}
	s.userEditorMu.Lock()
	if s.userEditorReplies == nil {
		s.userEditorReplies = map[string]userEditorReply{}
	}
	s.userEditorReplies[editorKey(kind, account)] = userEditorReply{data: data, expires: time.Now().Add(userEditorReplyTTL)}
	open := s.openUserEditor
	s.userEditorMu.Unlock()
	if open != nil {
		open(kind, account)
	}
	return true
}

// takeEditorReply hands the stored reply to the editor opened for it, so the
// editor does not ask the server a second time.
func (s *Service) takeEditorReply(kind, account string, now time.Time) (any, bool) {
	key := editorKey(kind, account)
	s.userEditorMu.Lock()
	defer s.userEditorMu.Unlock()
	reply, ok := s.userEditorReplies[key]
	delete(s.userEditorReplies, key)
	if !ok || now.After(reply.expires) {
		return nil, false
	}
	return reply.data, true
}

func (s *Service) clearUserEditors() {
	s.userEditorMu.Lock()
	s.userEditorRequests, s.userEditorReplies = nil, nil
	s.userEditorMu.Unlock()
}
