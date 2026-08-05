package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type PMLine struct {
	Direction string `json:"direction"`
	Text      string `json:"text"`
	Timestamp int64  `json:"timestamp"`
}

type PMConversation struct {
	PlayerID int      `json:"playerId"`
	Account  string   `json:"account"`
	Nick     string   `json:"nick"`
	Unread   int      `json:"unread"`
	Lines    []PMLine `json:"lines"`
}

type PMState struct {
	Conversations []PMConversation `json:"conversations"`
	UnreadTotal   int              `json:"unreadTotal"`
}

func (a *App) hasUnreadPM() bool {
	a.pmMu.RLock()
	defer a.pmMu.RUnlock()
	for _, c := range a.pmConversations {
		if c.Unread > 0 {
			return true
		}
	}
	return false
}

func (a *App) recordIncomingPM(playerID int, account, nick, message string) {
	message = strings.TrimSpace(message)
	if message == "" || playerID < 0 {
		return
	}
	a.pmMu.Lock()
	c := a.pmConversations[playerID]
	c.PlayerID, c.Account, c.Nick = playerID, account, nick
	c.Lines = append(c.Lines, PMLine{"in", message, time.Now().UnixMilli()})
	if len(c.Lines) > 500 {
		c.Lines = c.Lines[len(c.Lines)-500:]
	}
	c.Unread++
	a.pmConversations[playerID] = c
	a.pmMu.Unlock()
	if account != "" {
		_ = a.AppendPmLog(account, fmt.Sprintf("[%s] <- %s", time.Now().Format("15:04"), message))
	}
	a.emitPMState()
}

func (a *App) appendOutgoingPM(playerID int, account, nick, message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	a.pmMu.Lock()
	c := a.pmConversations[playerID]
	c.PlayerID, c.Account, c.Nick = playerID, account, nick
	c.Lines = append(c.Lines, PMLine{"out", message, time.Now().UnixMilli()})
	if len(c.Lines) > 500 {
		c.Lines = c.Lines[len(c.Lines)-500:]
	}
	a.pmConversations[playerID] = c
	a.pmMu.Unlock()
	a.emitPluginEvent("pm.sent", playerID, account, nick, message)
	a.emitPMState()
}

func (a *App) pmSnapshot() PMState {
	a.pmMu.RLock()
	defer a.pmMu.RUnlock()
	s := PMState{Conversations: make([]PMConversation, 0, len(a.pmConversations))}
	for _, c := range a.pmConversations {
		c.Lines = append([]PMLine(nil), c.Lines...)
		s.Conversations = append(s.Conversations, c)
		s.UnreadTotal += c.Unread
	}
	return s
}

func (a *App) emitPMState() {
	if a.app == nil {
		return
	}
	if b, err := json.Marshal(a.pmSnapshot()); err == nil {
		a.app.Event.Emit("rc:pmState", string(b))
	}
	a.updateTrayPMBadge()
}

func (a *App) GetPMState() PMState { return a.pmSnapshot() }

func (a *App) MarkPMRead(playerID int) {
	a.pmMu.Lock()
	if c, ok := a.pmConversations[playerID]; ok {
		c.Unread = 0
		a.pmConversations[playerID] = c
	}
	a.pmMu.Unlock()
	a.emitPMState()
}

func (a *App) RecordOutgoingPM(playerID int, account, nick, message string) {
	a.appendOutgoingPM(playerID, account, nick, message)
}

func (a *App) clearPMState() {
	a.pmMu.Lock()
	a.pmConversations = make(map[int]PMConversation)
	a.pmMu.Unlock()
	a.emitPMState()
}
