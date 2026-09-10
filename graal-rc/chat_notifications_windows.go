//go:build windows

package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf16"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
	"github.com/wailsapp/wails/v3/pkg/w32"
	"golang.org/x/sys/windows/registry"
)

const (
	windowsChatNotificationAppID       = "graal-rc"
	windowsChatNotificationRegistryKey = "Software\\Classes\\AppUserModelId\\" + windowsChatNotificationAppID
	windowsChatNotificationIconName    = "graal-rc-chat.png"
)

var windowsChatNotificationIconMu sync.Mutex

type windowsMentionToast struct {
	XMLName xml.Name             `xml:"toast"`
	Visual  windowsMentionVisual `xml:"visual"`
	Audio   windowsMentionAudio  `xml:"audio"`
}

type windowsMentionVisual struct {
	Binding windowsMentionBinding `xml:"binding"`
}

type windowsMentionBinding struct {
	Template string                     `xml:"template,attr"`
	Texts    []windowsMentionToastText  `xml:"text"`
	Images   []windowsMentionToastImage `xml:"image,omitempty"`
}

type windowsMentionToastText struct {
	Value string `xml:",chardata"`
}

type windowsMentionToastImage struct {
	Src       string `xml:"src,attr"`
	Placement string `xml:"placement,attr"`
	HintCrop  string `xml:"hint-crop,attr,omitempty"`
}

type windowsMentionAudio struct {
	Src string `xml:"src,attr"`
}

// sendChatMentionNotification uses the Windows Runtime PowerShell bridge
// directly. It avoids the COM apartment chosen by the RC socket goroutine; the
// Wails service remains the fallback for other platforms or older Windows
// environments where PowerShell is unavailable.
func sendChatMentionNotification(options notifications.NotificationOptions, serverName string) error {
	iconPath, err := ensureWindowsChatNotificationIcon()
	if err != nil {
		log.Printf("[notifications] chat mention icon unavailable: %v", err)
	}
	if err := updateWindowsChatNotificationMetadata(serverName, iconPath); err != nil {
		log.Printf("[notifications] chat mention metadata update failed: %v", err)
	}

	var images []windowsMentionToastImage
	if iconPath != "" {
		images = []windowsMentionToastImage{{
			Src:       windowsToastFileURI(iconPath),
			Placement: "appLogoOverride",
			HintCrop:  "circle",
		}}
	}

	doc := windowsMentionToast{
		Visual: windowsMentionVisual{
			Binding: windowsMentionBinding{
				Template: "ToastGeneric",
				Texts: []windowsMentionToastText{
					{Value: options.Title},
					{Value: options.Body},
				},
				Images: images,
			},
		},
		Audio: windowsMentionAudio{Src: "ms-winsoundevent:Notification.Default"},
	}
	xmlBytes, err := xml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal toast XML: %w", err)
	}

	encodedXML := base64.StdEncoding.EncodeToString(xmlBytes)
	script := "$ErrorActionPreference = 'Stop'\n" +
		"[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null\n" +
		"[Windows.UI.Notifications.ToastNotification, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null\n" +
		"[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null\n" +
		"$xmlBytes = [Convert]::FromBase64String('" + encodedXML + "')\n" +
		"$xml = [Text.Encoding]::UTF8.GetString($xmlBytes)\n" +
		"$document = New-Object Windows.Data.Xml.Dom.XmlDocument\n" +
		"$document.LoadXml($xml)\n" +
		"$toast = New-Object Windows.UI.Notifications.ToastNotification $document\n" +
		"[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('" + windowsChatNotificationAppID + "').Show($toast)"
	encodedScript := base64.StdEncoding.EncodeToString(utf16LE(script))

	command := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encodedScript,
	)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("PowerShell toast: %w", err)
		}
		return fmt.Errorf("PowerShell toast: %w: %s", err, message)
	}
	return nil
}

func ensureWindowsChatNotificationIcon() (string, error) {
	windowsChatNotificationIconMu.Lock()
	defer windowsChatNotificationIconMu.Unlock()

	iconPath := filepath.Join(os.TempDir(), windowsChatNotificationIconName)
	if info, err := os.Stat(iconPath); err == nil && !info.IsDir() && info.Size() > 0 {
		return iconPath, nil
	}

	icon, err := application.NewIconFromResource(w32.GetModuleHandle(""), uint16(3))
	if err != nil {
		return "", fmt.Errorf("load application icon resource: %w", err)
	}
	if err := w32.SaveHIconAsPNG(icon, iconPath); err != nil {
		return "", fmt.Errorf("save application icon: %w", err)
	}
	return iconPath, nil
}

func updateWindowsChatNotificationMetadata(serverName, iconPath string) error {
	displayName := windowsToastDisplayName(serverName)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, windowsChatNotificationRegistryKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open app user model registry key: %w", err)
	}
	if err := key.SetStringValue("DisplayName", displayName); err != nil {
		key.Close()
		return fmt.Errorf("set toast display name: %w", err)
	}
	if iconPath != "" {
		if err := key.SetStringValue("IconUri", iconPath); err != nil {
			key.Close()
			return fmt.Errorf("set toast icon: %w", err)
		}
	}
	if err := key.Close(); err != nil {
		return fmt.Errorf("close app user model registry key: %w", err)
	}
	return nil
}

func windowsToastDisplayName(serverName string) string {
	clean := strings.Map(func(value rune) rune {
		if unicode.IsControl(value) {
			return -1
		}
		return value
	}, strings.TrimSpace(serverName))
	if clean == "" {
		return "Graal Remote Control"
	}
	runes := []rune(clean)
	if len(runes) > 128 {
		return string(runes[:127]) + "…"
	}
	return clean
}

func windowsToastFileURI(path string) string {
	normalized := filepath.ToSlash(filepath.Clean(path))
	return (&url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(normalized, "/")}).String()
}

func utf16LE(value string) []byte {
	encoded := utf16.Encode([]rune(value))
	result := make([]byte, len(encoded)*2)
	for index, codeUnit := range encoded {
		binary.LittleEndian.PutUint16(result[index*2:], codeUnit)
	}
	return result
}
