package main

import (
	"encoding/csv"
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

// normalizePMText decodes the comma-text representation used by the Graal
// protocol for multiline PMs. Depending on the native library version, the
// payload can arrive as a JSON string array or as the equivalent unwrapped
// comma-text value, for example: `"Oi","Linha 2","Linha3",`.
//
// Keeping this normalization at the application boundary makes the PM state,
// event stream and log output consistent even when an older native DLL is
// still installed next to the executable.
func normalizePMText(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
	if text == "" {
		return ""
	}

	if lines, ok := decodePMStringArray(text); ok {
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return text
}

func decodePMStringArray(text string) ([]string, bool) {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		var lines []string
		if err := json.Unmarshal([]byte(trimmed), &lines); err == nil {
			return lines, true
		}
	}

	// The native protocol's comma-text format has a trailing separator. The
	// csv reader handles Graal's doubled-quote escaping and preserves commas
	// inside a quoted line.
	if !strings.HasPrefix(trimmed, `"`) {
		return nil, false
	}
	hasTrailingSeparator := strings.HasSuffix(trimmed, ",")
	if !hasTrailingSeparator && !strings.Contains(trimmed, `",`) {
		// A message such as `"Oi"` may already be decoded user text. Only
		// treat it as comma-text when there is an actual field separator.
		return nil, false
	}
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ","))
	reader := csv.NewReader(strings.NewReader(trimmed))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) != 1 || len(records[0]) == 0 {
		return nil, false
	}
	for i := range records[0] {
		// gtokenize escapes a literal backslash by doubling it. csv.Reader
		// already resolves doubled quotes, so mirror the remaining escape here.
		records[0][i] = strings.ReplaceAll(records[0][i], `\\`, `\`)
	}
	return records[0], true
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
	message = normalizePMText(message)
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
