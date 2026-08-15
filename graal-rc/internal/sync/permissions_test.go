package sync

import (
	"context"
	"errors"
	"testing"

	"graal-rc/rclib"
)

type permissionBackendStub struct {
	permissionRefreshes int
	fetches             int
	fetchErr            error
	fetchReplies        []rclib.ScriptReply
}

func (b *permissionBackendStub) IsNCConnected() bool     { return true }
func (b *permissionBackendStub) IsNCAuthenticated() bool { return true }

func (b *permissionBackendStub) GetWeapons() ([]rclib.Weapon, error) { return nil, nil }
func (b *permissionBackendStub) GetClasses() ([]rclib.Class, error)  { return nil, nil }
func (b *permissionBackendStub) GetNPCs() ([]rclib.NPC, error)       { return nil, nil }

func (b *permissionBackendStub) RefreshSelfFolderRights() error {
	b.permissionRefreshes++
	return nil
}

func (b *permissionBackendStub) CanReadScript(string, string) bool  { return true }
func (b *permissionBackendStub) CanWriteScript(string, string) bool { return true }

func (b *permissionBackendStub) OpenScript(string, string) (rclib.ScriptReply, error) {
	return rclib.ScriptReply{}, nil
}

func (b *permissionBackendStub) SaveWeapon(string, string) error { return nil }
func (b *permissionBackendStub) SaveClass(string, string) error  { return nil }
func (b *permissionBackendStub) SaveNPC(int, string) error       { return nil }

func (b *permissionBackendStub) AddWeapon(string) error { return nil }
func (b *permissionBackendStub) AddClass(string) error  { return nil }
func (b *permissionBackendStub) CreateNPC(string, int, string, string, string, string, string) error {
	return nil
}

func (b *permissionBackendStub) RefreshWeapons() error { return nil }

func (b *permissionBackendStub) FetchAllScripts(context.Context, func(string, string) bool, func(int, int)) ([]rclib.ScriptReply, error) {
	b.fetches++
	return b.fetchReplies, b.fetchErr
}

func TestBootstrapRetryReusesLoadedPermissions(t *testing.T) {
	backend := &permissionBackendStub{fetchErr: errors.New("script lists are warming up")}
	engine := NewEngine(backend, "TestServer", nil)
	engine.ApplyConfig(SyncConfig{Enabled: true, OutputDir: t.TempDir(), PollingMinutes: 1})

	if err := engine.bootstrap(context.Background(), engine.config().OutputDir); err == nil {
		t.Fatal("first bootstrap should fail while script lists are unavailable")
	}
	if err := engine.bootstrap(context.Background(), engine.config().OutputDir); err == nil {
		t.Fatal("retry bootstrap should still report the list error")
	}
	if backend.permissionRefreshes != 1 {
		t.Fatalf("permission refreshes = %d, want 1 across an internal retry", backend.permissionRefreshes)
	}
}

func TestBootstrapReusesSessionPermissions(t *testing.T) {
	backend := &permissionBackendStub{fetchReplies: []rclib.ScriptReply{{Type: "weapon", Name: "Test", Script: ""}}}
	engine := NewEngine(backend, "TestServer", nil)
	engine.ApplyConfig(SyncConfig{Enabled: true, OutputDir: t.TempDir(), PollingMinutes: 1})
	engine.MarkPermissionsReady()

	if err := engine.bootstrap(context.Background(), engine.config().OutputDir); err != nil {
		t.Fatalf("bootstrap with a loaded session snapshot failed: %v", err)
	}
	if backend.permissionRefreshes != 0 {
		t.Fatalf("permission refreshes = %d, want 0 when the session snapshot is reused", backend.permissionRefreshes)
	}
}

func TestReconcileRefreshesSelfPermissionsEveryPoll(t *testing.T) {
	backend := &permissionBackendStub{fetchReplies: []rclib.ScriptReply{{Type: "weapon", Name: "Test", Script: ""}}}
	engine := NewEngine(backend, "TestServer", nil)
	engine.ApplyConfig(SyncConfig{
		Enabled:        true,
		OutputDir:      t.TempDir(),
		PollingMinutes: 1,
		AutoPushLocal:  true,
		AutoPullServer: true,
	})

	engine.ReconcileAll(context.Background())
	engine.ReconcileAll(context.Background())

	if backend.permissionRefreshes != 2 {
		t.Fatalf("permission refreshes = %d, want 2", backend.permissionRefreshes)
	}
	if backend.fetches != 2 {
		t.Fatalf("script fetches = %d, want 2", backend.fetches)
	}
	status := engine.Status()
	if !status.PermissionsReady || status.PermissionsError != "" {
		t.Fatalf("permission status = ready:%v error:%q", status.PermissionsReady, status.PermissionsError)
	}
}

func TestScheduledPollReusesLoadedPermissions(t *testing.T) {
	backend := &permissionBackendStub{fetchReplies: []rclib.ScriptReply{{Type: "weapon", Name: "Test", Script: ""}}}
	engine := NewEngine(backend, "TestServer", nil)
	engine.ApplyConfig(SyncConfig{
		Enabled:        true,
		OutputDir:      t.TempDir(),
		PollingMinutes: 1,
		AutoPushLocal:  true,
		AutoPullServer: true,
	})

	engine.ReconcileAll(context.Background())
	engine.poll(context.Background())

	if backend.permissionRefreshes != 1 {
		t.Fatalf("permission refreshes = %d, want 1 when scheduled poll reuses the cache", backend.permissionRefreshes)
	}
	if backend.fetches != 2 {
		t.Fatalf("script fetches = %d, want 2 across manual sync + scheduled poll", backend.fetches)
	}
}
