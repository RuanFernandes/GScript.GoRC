package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

type PluginUITabOptions struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon,omitempty"`
	Order int    `json:"order,omitempty"`
	Open  bool   `json:"open,omitempty"`
	View  any    `json:"view"`
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
	var root any
	if err := json.Unmarshal(b, &root); err != nil {
		return errors.New("plugin UI view must contain a JSON object")
	}
	nodes := 0
	if err := validatePluginUIViewNode(root, 0, &nodes); err != nil {
		return err
	}
	return nil
}

func validatePluginUIValue(value any) error {
	b, err := json.Marshal(value)
	if err != nil || len(b) > maxPluginUIViewBytes {
		return errors.New("plugin UI action value is invalid")
	}
	var decoded any
	if err := json.Unmarshal(b, &decoded); err != nil {
		return errors.New("plugin UI action value is invalid")
	}
	switch decoded.(type) {
	case nil, string, bool, float64:
		return nil
	default:
		return errors.New("plugin UI action value must be a primitive")
	}
}

const (
	maxPluginUIViewDepth = 16
	maxPluginUIViewNodes = 512
	maxPluginUITextBytes = 16 << 10
)

func validatePluginUIViewNode(raw any, depth int, nodes *int) error {
	if depth > maxPluginUIViewDepth {
		return errors.New("plugin UI view is too deeply nested")
	}
	*nodes = *nodes + 1
	if *nodes > maxPluginUIViewNodes {
		return errors.New("plugin UI view contains too many nodes")
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return errors.New("plugin UI view nodes must be JSON objects")
	}
	typeName, ok := object["type"].(string)
	if !ok || strings.TrimSpace(typeName) == "" {
		return errors.New("plugin UI view node type is required")
	}
	text := func(key string, required bool) error {
		value, exists := object[key]
		if !exists {
			if required {
				return fmt.Errorf("plugin UI %s is required", key)
			}
			return nil
		}
		stringValue, valid := value.(string)
		if !valid || len(stringValue) > maxPluginUITextBytes {
			return fmt.Errorf("plugin UI %s is invalid", key)
		}
		return nil
	}
	controlID := func() error {
		value, ok := object["id"].(string)
		if !ok || strings.TrimSpace(value) == "" || len(value) > 128 || strings.ContainsAny(value, "\r\n") {
			return errors.New("plugin UI control id is invalid")
		}
		return nil
	}
	children := func() error {
		values, ok := object["children"].([]any)
		if !ok || len(values) == 0 {
			return errors.New("plugin UI children are required")
		}
		for _, child := range values {
			if err := validatePluginUIViewNode(child, depth+1, nodes); err != nil {
				return err
			}
		}
		return nil
	}
	switch typeName {
	case "stack", "row", "card":
		if typeName == "card" {
			if err := text("title", false); err != nil {
				return err
			}
			if err := text("description", false); err != nil {
				return err
			}
		}
		return children()
	case "text", "heading":
		return text("text", true)
	case "badge":
		return text("text", true)
	case "divider":
		return nil
	case "button":
		if err := controlID(); err != nil {
			return err
		}
		if err := text("label", true); err != nil {
			return err
		}
		return text("action", true)
	case "input", "textarea":
		if err := controlID(); err != nil {
			return err
		}
		if err := text("label", true); err != nil {
			return err
		}
		if _, exists := object["value"]; exists {
			if err := text("value", false); err != nil {
				return err
			}
		}
		if action, exists := object["action"]; exists {
			if _, ok := action.(string); !ok {
				return errors.New("plugin UI action is invalid")
			}
		}
		return nil
	case "checkbox":
		if err := controlID(); err != nil {
			return err
		}
		if err := text("label", true); err != nil {
			return err
		}
		if value, exists := object["value"]; exists {
			if _, ok := value.(bool); !ok {
				return errors.New("plugin UI checkbox value is invalid")
			}
		}
		if action, exists := object["action"]; exists {
			if _, ok := action.(string); !ok {
				return errors.New("plugin UI action is invalid")
			}
		}
		return nil
	case "select":
		if err := controlID(); err != nil {
			return err
		}
		if err := text("label", true); err != nil {
			return err
		}
		options, ok := object["options"].([]any)
		if !ok || len(options) == 0 || len(options) > 128 {
			return errors.New("plugin UI select options are invalid")
		}
		for _, rawOption := range options {
			option, ok := rawOption.(map[string]any)
			if !ok {
				return errors.New("plugin UI select option is invalid")
			}
			if label, ok := option["label"].(string); !ok || strings.TrimSpace(label) == "" || len(label) > maxPluginUITextBytes {
				return errors.New("plugin UI select option label is invalid")
			}
			if value, ok := option["value"].(string); !ok || len(value) > maxPluginUITextBytes {
				return errors.New("plugin UI select option value is invalid")
			}
		}
		return nil
	case "progress":
		value, ok := object["value"].(float64)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return errors.New("plugin UI progress value is invalid")
		}
		maxValue := 100.0
		if rawMax, exists := object["max"]; exists {
			var valid bool
			maxValue, valid = rawMax.(float64)
			if !valid || math.IsNaN(maxValue) || math.IsInf(maxValue, 0) || maxValue <= 0 {
				return errors.New("plugin UI progress maximum is invalid")
			}
		}
		if value > maxValue {
			return errors.New("plugin UI progress value exceeds maximum")
		}
		return text("label", false)
	case "empty":
		if err := text("title", true); err != nil {
			return err
		}
		return text("description", false)
	case "code":
		return text("value", true)
	case "table":
		columns, ok := object["columns"].([]any)
		if !ok || len(columns) == 0 || len(columns) > 128 {
			return errors.New("plugin UI table columns are invalid")
		}
		for _, rawColumn := range columns {
			column, ok := rawColumn.(map[string]any)
			if !ok {
				return errors.New("plugin UI table column is invalid")
			}
			key, keyOK := column["key"].(string)
			label, labelOK := column["label"].(string)
			if !keyOK || strings.TrimSpace(key) == "" || len(key) > 128 || !labelOK || strings.TrimSpace(label) == "" || len(label) > maxPluginUITextBytes {
				return errors.New("plugin UI table column is invalid")
			}
		}
		rows, ok := object["rows"].([]any)
		if !ok || len(rows) > 512 {
			return errors.New("plugin UI table rows are invalid")
		}
		for _, rawRow := range rows {
			row, ok := rawRow.(map[string]any)
			if !ok {
				return errors.New("plugin UI table row is invalid")
			}
			for _, value := range row {
				switch value.(type) {
				case nil, string, bool, float64:
				default:
					return errors.New("plugin UI table cell is invalid")
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported plugin UI node type %q", typeName)
	}
}

func validatePluginUITabOptions(options PluginUITabOptions) error {
	options.ID = strings.TrimSpace(options.ID)
	options.Title = strings.TrimSpace(options.Title)
	if options.ID == "" || len(options.ID) > 128 || strings.ContainsAny(options.ID, "\\/:\r\n") {
		return errors.New("plugin UI tab id is invalid")
	}
	if options.Title == "" || len(options.Title) > 200 || strings.ContainsAny(options.Title, "\r\n") {
		return errors.New("plugin UI tab title is invalid")
	}
	switch options.Icon {
	case "", "dashboard", "terminal", "settings", "puzzle":
	default:
		return errors.New("plugin UI tab icon is invalid")
	}
	if options.Order < -100000 || options.Order > 100000 {
		return errors.New("plugin UI tab order is invalid")
	}
	return validatePluginUIView(options.View)
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
	windowTitle := serverWindowTitle(a.sessions.Status().ServerName, options.Title)
	key := pluginUIWindowKey(pluginID, options.ID)
	a.pluginUIWindowMu.Lock()
	if current := a.pluginUIWindows[key]; current != nil {
		current.Info = info
		window := current.Window
		a.pluginUIWindowMu.Unlock()
		a.emitPluginUIUpdate(info)
		if window != nil {
			window.SetTitle(windowTitle)
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
		Title:            windowTitle,
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
	if err := validatePluginUIValue(value); err != nil {
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
