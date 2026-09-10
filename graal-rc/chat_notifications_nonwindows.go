//go:build !windows

package main

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

func sendChatMentionNotification(notifications.NotificationOptions, string) error {
	return errors.New("direct Windows toast is unavailable on this platform")
}
