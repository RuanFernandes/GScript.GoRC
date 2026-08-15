package main

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	chatLinkWindowWidth  = 1100
	chatLinkWindowHeight = 760
)

// OpenChatLink opens a user-selected HTTP(S) URL in a dedicated Wails
// WebView. The URL is validated before it reaches the native webview so chat
// text cannot request local files, JavaScript URLs, or other non-web schemes.
func (a *App) OpenChatLink(rawURL string) error {
	link, err := sanitizeChatLinkURL(rawURL)
	if err != nil {
		return err
	}
	if a.app == nil {
		return errors.New("application is not initialized")
	}

	a.chatLinkMu.Lock()
	a.chatLinkSeq++
	windowName := fmt.Sprintf("chat-link-%d", a.chatLinkSeq)
	a.chatLinkMu.Unlock()

	parsed, _ := url.Parse(link)
	title := parsed.Hostname()
	if title == "" {
		title = "RC Link"
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             windowName,
		Title:            serverWindowTitle(a.sessions.Status().ServerName, title),
		// Load the RC shell first so the WebView gets the same custom title bar
		// as every other secondary window. The external page is rendered inside
		// the chat-link route's embedded browser surface.
		URL:              chatLinkRouteURL(link),
		Width:            chatLinkWindowWidth,
		Height:           chatLinkWindowHeight,
		MinWidth:         640,
		MinHeight:        420,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})

	a.chatLinkMu.Lock()
	if a.chatLinkWindows == nil {
		a.chatLinkWindows = make(map[string]*application.WebviewWindow)
	}
	a.chatLinkWindows[windowName] = w
	a.chatLinkMu.Unlock()

	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.chatLinkMu.Lock()
		if a.chatLinkWindows[windowName] == w {
			delete(a.chatLinkWindows, windowName)
		}
		a.chatLinkMu.Unlock()
	})
	w.Show()
	w.Focus()
	return nil
}

func chatLinkRouteURL(link string) string {
	return "/#chat-link?u=" + url.QueryEscape(link)
}

func sanitizeChatLinkURL(rawURL string) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", errors.New("URL is empty")
	}
	if len(trimmed) > 8192 {
		return "", errors.New("URL is too long")
	}

	sanitized, err := application.ValidateAndSanitizeURL(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid chat URL: %w", err)
	}
	parsed, err := url.Parse(sanitized)
	if err != nil || parsed.Host == "" {
		return "", errors.New("chat links must include a host")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("only HTTP and HTTPS chat links are allowed")
	}
	return parsed.String(), nil
}
