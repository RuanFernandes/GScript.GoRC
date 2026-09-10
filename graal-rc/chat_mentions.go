package main

import (
	"log"
	"strings"
)

// handleChatMentionEvent inspects the same chat payloads that are rendered by
// the frontend. Keeping the source mapping here avoids silently missing IRC or
// NC lines that are shown in the chat tabs but do not use rc:message.
func (a *App) handleChatMentionEvent(name string, data []any) string {
	if a == nil || a.sessions == nil {
		return ""
	}

	text, ok := chatTextForMentionEvent(name, data)
	if !ok || !hasMentionMarker(text) {
		return ""
	}

	target := a.sessions.SelfMentionTarget(text)
	if target == "" {
		log.Printf("[notifications] chat @ mention ignored: no matching account/community")
		return ""
	}

	// Deliberately notify self-mentions too. The user may be testing from the
	// same account, and the sender nickname is not a reliable account identity.
	log.Printf("[notifications] chat mention detected alias=%q source=%s", target, name)
	go a.notifyChatMention(text)
	return target
}

func chatTextForMentionEvent(name string, data []any) (string, bool) {
	switch name {
	case "rc:message":
		return eventString(data, 0)
	case "rc:irc":
		return eventString(data, 1)
	case "rc:serverdata":
		kind, ok := eventString(data, 0)
		if !ok || !strings.EqualFold(kind, "nc_message") {
			return "", false
		}
		return eventString(data, 1)
	default:
		return "", false
	}
}

func eventString(data []any, index int) (string, bool) {
	if index < 0 || index >= len(data) {
		return "", false
	}
	value, ok := data[index].(string)
	return value, ok
}

func hasMentionMarker(text string) bool {
	return strings.ContainsRune(text, '@') || strings.ContainsRune(text, '\uFF20')
}
