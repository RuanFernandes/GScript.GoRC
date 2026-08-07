package main

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// DiagnosticsNCStatus deliberately mirrors only connection health fields. It
// keeps the exported report independent from the full live-session model.
type DiagnosticsNCStatus struct {
	HasNC         bool `json:"hasNc"`
	Connected     bool `json:"connected"`
	Authenticated bool `json:"authenticated"`
}

type SyncDiagnostics struct {
	Enabled     bool   `json:"enabled"`
	Paused      bool   `json:"paused"`
	NCDown      bool   `json:"ncDown"`
	Server      string `json:"server"`
	LastSyncAt  int64  `json:"lastSyncAt"`
	ReviewCount int    `json:"reviewCount"`
	Progress    string `json:"progress"`
}

// DiagnosticsSnapshot is safe to copy into a support ticket: credentials,
// passwords, chat content, player data, and native handles are excluded.
type DiagnosticsSnapshot struct {
	GeneratedAt   int64               `json:"generatedAt"`
	OS            string              `json:"os"`
	Arch          string              `json:"arch"`
	GoVersion     string              `json:"goVersion"`
	DLLLoaded     bool                `json:"dllLoaded"`
	DLLPath       string              `json:"dllPath,omitempty"`
	Connected     bool                `json:"connected"`
	Authenticated bool                `json:"authenticated"`
	ServerName    string              `json:"serverName"`
	NC            DiagnosticsNCStatus `json:"nc"`
	PumpError     string              `json:"pumpError,omitempty"`
	Reconnect     ReconnectStatus     `json:"reconnect"`
	Sync          SyncDiagnostics     `json:"sync"`
}

// GetDiagnostics returns current local/runtime health without exposing the
// account vault or live player/chat payloads.
func (a *App) GetDiagnostics() DiagnosticsSnapshot {
	status := a.sessions.Status()
	nc := a.sessions.NCStatus()
	syncStatus := a.GetSyncStatus()
	return DiagnosticsSnapshot{
		GeneratedAt:   time.Now().UnixMilli(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GoVersion:     runtime.Version(),
		DLLLoaded:     status.Loaded,
		DLLPath:       status.DLLPath,
		Connected:     status.Connected,
		Authenticated: status.Authenticated,
		ServerName:    status.ServerName,
		NC: DiagnosticsNCStatus{
			HasNC:         nc.HasNc,
			Connected:     nc.Connected,
			Authenticated: nc.Authenticated,
		},
		PumpError: a.sessions.PumpError(),
		Reconnect: a.GetReconnectStatus(),
		Sync: SyncDiagnostics{
			Enabled:     syncStatus.Enabled,
			Paused:      syncStatus.Paused,
			NCDown:      syncStatus.NCDown,
			Server:      syncStatus.Server,
			LastSyncAt:  syncStatus.LastSyncAt,
			ReviewCount: syncStatus.ReviewCount,
			Progress:    syncStatus.Progress.Phase,
		},
	}
}

// ExportDiagnostics writes a redacted JSON report through the native save
// dialog and returns the selected path. An empty path means the dialog was
// cancelled.
func (a *App) ExportDiagnostics() (string, error) {
	if a.app == nil {
		return "", errors.New("application is not initialized")
	}
	path, err := a.app.Dialog.SaveFile().
		SetMessage("Export diagnostics").
		SetFilename("graal-rc-diagnostics-" + time.Now().Format("20060102-150405") + ".json").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	content, err := json.MarshalIndent(a.GetDiagnostics(), "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// OpenDiagnostics opens the singleton support/health window.
func (a *App) OpenDiagnostics() {
	if a == nil || a.app == nil {
		return
	}
	a.diagnosticsMu.Lock()
	defer a.diagnosticsMu.Unlock()
	if a.diagnosticsWindow != nil {
		a.diagnosticsWindow.Show()
		a.diagnosticsWindow.Focus()
		return
	}
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "diagnostics",
		Title:            "Diagnostics",
		URL:              "/#diagnostics",
		Width:            760,
		Height:           620,
		MinWidth:         560,
		MinHeight:        420,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(18, 18, 20),
	})
	a.diagnosticsWindow = w
	w.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		a.diagnosticsMu.Lock()
		a.diagnosticsWindow = nil
		a.diagnosticsMu.Unlock()
	})
	w.Show()
	w.Focus()
}
