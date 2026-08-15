package sync

// SyncConfig is the persisted sync configuration. It lives in
// <UserConfigDir>/graal-rc/sync.json (loaded/saved by app.go, mirroring the
// coding.json / filebrowser.json pattern).
type SyncConfig struct {
	// Enabled gates whether the engine runs goroutines at all.
	Enabled bool `json:"enabled"`
	// OutputDir is the root sync folder; scripts land in
	// OutputDir/{weapons,classes,npcs}/<name>.gs2. Empty => disabled.
	OutputDir string `json:"outputDir"`
	// PollingMinutes is the full-reconcile poll interval (min 1).
	PollingMinutes int `json:"pollingMinutes"`
	// AutoPushLocal auto-pushes unilateral local changes to the server.
	AutoPushLocal bool `json:"autoPushLocal"`
	// AutoPullServer auto-pulls unilateral server changes to local.
	AutoPullServer bool `json:"autoPullServer"`
	// PauseUntil is a unix timestamp until which reconcile is skipped
	// (0 = not paused). Set by PauseSync (now+1h), cleared by ResumeSync.
	PauseUntil int64 `json:"pauseUntil,omitempty"`
}

const DefaultPollingMinutes = 60

// DefaultSyncConfig returns sane defaults.
func DefaultSyncConfig() SyncConfig {
	return SyncConfig{
		PollingMinutes: DefaultPollingMinutes,
		AutoPushLocal:  true,
		AutoPullServer: true,
	}
}
