package main

import (
	"fmt"
	"log"
	"strings"
	"sync/atomic"
	"unicode"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

const maxChatMentionNotificationRunes = 1000

func (a *App) notifyChatMention(text string) {
	if a == nil {
		return
	}

	body := notificationText(text)
	if body == "" {
		return
	}

	id := atomic.AddUint64(&a.mentionNotificationSeq, 1)
	serverName := a.chatMentionServerName()
	options := notifications.NotificationOptions{
		ID:                fmt.Sprintf("rc-chat-mention-%d", id),
		Title:             chatMentionNotificationTitle(a.GetLanguage()),
		Body:              body,
		InterruptionLevel: notifications.InterruptionLevelActive,
	}

	if err := sendChatMentionNotification(options, serverName); err == nil {
		log.Printf("[notifications] chat mention sent id=%q backend=native", options.ID)
		return
	} else {
		log.Printf("[notifications] native chat mention failed: %v", err)
	}

	if a.osNotifications == nil {
		log.Printf("[notifications] chat mention unavailable: Wails notification service is nil")
		return
	}
	if err := a.osNotifications.SendNotification(options); err != nil {
		// A missing desktop notification daemon or an OS-level policy must not
		// affect the RC socket/event pump or make the chat unusable.
		log.Printf("[notifications] chat mention failed: %v", err)
		return
	}
	log.Printf("[notifications] chat mention sent id=%q backend=wails-fallback", options.ID)
}

func (a *App) chatMentionServerName() string {
	if a != nil && a.sessions != nil {
		if identity, err := a.sessions.SessionIdentity(); err == nil {
			if serverName := strings.TrimSpace(identity.ServerName); serverName != "" {
				return serverName
			}
		}
	}
	return "Graal Remote Control"
}

func chatMentionNotificationTitle(language string) string {
	switch language {
	case "pt-BR":
		return "Você foi chamado no RC Chat"
	case "es":
		return "Te mencionaron en RC Chat"
	default:
		return "You were mentioned in RC Chat"
	}
}

func notificationText(text string) string {
	clean := strings.Map(func(value rune) rune {
		if value == '\n' || value == '\r' || value == '\t' || !unicode.IsControl(value) {
			return value
		}
		return -1
	}, strings.TrimSpace(text))
	if clean == "" {
		return ""
	}
	runes := []rune(clean)
	if len(runes) <= maxChatMentionNotificationRunes {
		return clean
	}
	return string(runes[:maxChatMentionNotificationRunes-1]) + "…"
}
