package main

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const maxPluginUIViewBytes = 1 << 20

type PluginUIWindowOptions struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	View   any    `json:"view"`
}

type PluginUIWindowInfo struct {
	ID       string `json:"id"`
	PluginID string `json:"pluginId"`
	Title    string `json:"title"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	View     any    `json:"view"`
}

type pluginUIWindowState struct {
	Info   PluginUIWindowInfo
	Window *application.WebviewWindow
}

func validatePluginUIView(view any) error {
	b, err := json.Marshal(view)
	if err != nil {
		return errors.New("plugin UI view must be JSON serializable")
	}
	if len(b) > maxPluginUIViewBytes {
		return errors.New("plugin UI view exceeds 1 MiB")
	}
	return nil
}

func validatePluginUIWindowOptions(options PluginUIWindowOptions) error {
	options.ID = strings.TrimSpace(options.ID)
	options.Title = strings.TrimSpace(options.Title)
	if options.ID == "" || len(options.ID) > 128 || strings.ContainsAny(options.ID, "\\/\r\n") {
		return errors.New("plugin UI window id is invalid")
	}
	if options.Title == "" || len(options.Title) > 200 || strings.ContainsAny(options.Title, "\r\n") {
		return errors.New("plugin UI window title is invalid")
	}
	if err := validatePluginUIView(options.View); err != nil {
		return err
	}
	return nil
}

func pluginUIWindowKey(pluginID, windowID string) string { return pluginID + "\x00" + windowID }

func sanitizePluginUIWindowName(pluginID, windowID string) string {
	var b strings.Builder
	b.WriteString("plugin-ui-")
	for _, value := range pluginID + "-" + windowID {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' {
			b.WriteRune(value)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func (a *App) PluginUIWindowOpen(pluginID string, options PluginUIWindowOptions) (PluginUIWindowInfo, error) {
	if a.plugins == nil {
		return PluginUIWindowInfo{}, errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "ui.window"); err != nil {
		return PluginUIWindowInfo{}, err
	}
	if err := validatePluginUIWindowOptions(options); err != nil {
		return PluginUIWindowInfo{}, err
	}
	if options.Width < 0 {
		options.Width = 0
	}
	if options.Height < 0 {
		options.Height = 0
	}
	if options.Width == 0 {
		options.Width = 900
	}
	if options.Height == 0 {
		options.Height = 680
	}
	options.Width = maxInt(520, minInt(options.Width, 2400))
	options.Height = maxInt(400, minInt(options.Height, 1800))
	info := PluginUIWindowInfo{ID: options.ID, PluginID: pluginID, Title: options.Title, Width: options.Width, Height: options.Height, View: options.View}
	key := pluginUIWindowKey(pluginID, options.ID)
	a.pluginUIWindowMu.Lock()
	if current := a.pluginUIWindows[key]; current != nil {
		current.Info = info
		window := current.Window
		a.pluginUIWindowMu.Unlock()
		a.emitPluginUIUpdate(info)
		if window != nil {
			window.Show()
			window.Focus()
		}
		return info, nil
	}
	state := &pluginUIWindowState{Info: info}
	a.pluginUIWindows[key] = state
	a.pluginUIWindowMu.Unlock()
	if a.app == nil {
		return info, nil
	}
	w := a.newWebviewWindow(application.WebviewWindowOptions{
		Name:             sanitizePluginUIWindowName(pluginID, options.ID),
		Title:            options.Title,
		URL:              "/#plugin-ui?plugin=" + url.QueryEscape(pluginID) + "&window=" + url.QueryEscape(options.ID),
		Width:            options.Width,
		Height:           options.Height,
		MinWidth:         420,
		MinHeight:        320,
		Frameless:        true,
		BackgroundColour: application.NewRGB(15, 17, 21),
	})
	a.pluginUIWindowMu.Lock()
	if current := a.pluginUIWindows[key]; current != nil {
		current.Window = w
	}
	a.pluginUIWindowMu.Unlock()
	w.Show()
	w.Focus()
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.pluginUIWindowMu.Lock()
		isCurrent := a.pluginUIWindows[key] != nil && a.pluginUIWindows[key].Window == w
		if isCurrent {
			delete(a.pluginUIWindows, key)
		}
		a.pluginUIWindowMu.Unlock()
		if isCurrent {
			a.emitPluginUIClosed(pluginID, options.ID)
		}
	})
	return info, nil
}

func (a *App) PluginUIWindowUpdate(pluginID, windowID string, view any) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "ui.window"); err != nil {
		return err
	}
	if err := validatePluginUIView(view); err != nil {
		return err
	}
	key := pluginUIWindowKey(pluginID, windowID)
	a.pluginUIWindowMu.Lock()
	state := a.pluginUIWindows[key]
	if state != nil {
		state.Info.View = view
	}
	a.pluginUIWindowMu.Unlock()
	if state == nil {
		return errors.New("plugin UI window not found")
	}
	a.emitPluginUIUpdate(state.Info)
	return nil
}

func (a *App) PluginUIWindowClose(pluginID, windowID string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "ui.window"); err != nil {
		return err
	}
	key := pluginUIWindowKey(pluginID, windowID)
	a.pluginUIWindowMu.Lock()
	state := a.pluginUIWindows[key]
	if state != nil {
		delete(a.pluginUIWindows, key)
	}
	a.pluginUIWindowMu.Unlock()
	if state == nil {
		return nil
	}
	if state.Window != nil {
		state.Window.Close()
	}
	a.emitPluginUIClosed(pluginID, windowID)
	return nil
}

func (a *App) PluginUIWindowCloseAll(pluginID string) error {
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "ui.window"); err != nil {
		return err
	}
	a.pluginUIWindowMu.Lock()
	states := []*pluginUIWindowState{}
	for key, state := range a.pluginUIWindows {
		if state.Info.PluginID == pluginID {
			delete(a.pluginUIWindows, key)
			states = append(states, state)
		}
	}
	a.pluginUIWindowMu.Unlock()
	for _, state := range states {
		if state.Window != nil {
			state.Window.Close()
		}
		a.emitPluginUIClosed(pluginID, state.Info.ID)
	}
	return nil
}

func (a *App) GetPluginUIWindow(pluginID, windowID string) (PluginUIWindowInfo, error) {
	if a.plugins == nil {
		return PluginUIWindowInfo{}, errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "ui.window"); err != nil {
		return PluginUIWindowInfo{}, err
	}
	a.pluginUIWindowMu.Lock()
	state := a.pluginUIWindows[pluginUIWindowKey(pluginID, windowID)]
	a.pluginUIWindowMu.Unlock()
	if state == nil {
		return PluginUIWindowInfo{}, errors.New("plugin UI window not found")
	}
	return state.Info, nil
}

func (a *App) PluginUIAction(pluginID, windowID, action string, value any) error {
	pluginID = strings.TrimSpace(pluginID)
	windowID = strings.TrimSpace(windowID)
	action = strings.TrimSpace(action)
	if pluginID == "" || windowID == "" || action == "" || len(action) > 128 || strings.ContainsAny(action, "\r\n") {
		return errors.New("invalid plugin UI action")
	}
	if err := validatePluginUIView(value); err != nil {
		return err
	}
	if a.plugins == nil {
		return errors.New("plugin manager is unavailable")
	}
	if err := a.plugins.RequireAPI(pluginID, "ui.window"); err != nil {
		return err
	}
	a.pluginUIWindowMu.Lock()
	state := a.pluginUIWindows[pluginUIWindowKey(pluginID, windowID)]
	a.pluginUIWindowMu.Unlock()
	if state == nil {
		return errors.New("plugin UI window not found")
	}
	payload := struct {
		PluginID string `json:"pluginId"`
		WindowID string `json:"windowId"`
		Action   string `json:"action"`
		Value    any    `json:"value,omitempty"`
	}{PluginID: pluginID, WindowID: windowID, Action: action, Value: value}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if a.app != nil {
		a.app.Event.Emit("plugin:ui-action", string(b))
	}
	return nil
}

func (a *App) emitPluginUIUpdate(info PluginUIWindowInfo) {
	if a.app == nil {
		return
	}
	b, err := json.Marshal(info)
	if err == nil {
		a.app.Event.Emit("plugin:ui-updated", string(b))
	}
}

func (a *App) emitPluginUIClosed(pluginID, windowID string) {
	if a.app == nil {
		return
	}
	b, err := json.Marshal(struct {
		PluginID string `json:"pluginId"`
		WindowID string `json:"windowId"`
	}{PluginID: pluginID, WindowID: windowID})
	if err == nil {
		a.app.Event.Emit("plugin:ui-closed", string(b))
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
