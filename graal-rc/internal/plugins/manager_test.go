package plugins

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func validManifest() Manifest {
	return Manifest{ID: "com.example.test", Name: "Test", Version: "1.0.0", APIVersion: APIVersion, Main: "dist/index.js", Permissions: Permissions{Events: []string{"rc.message"}, APIs: []string{"network.http"}, Network: []string{"https://example.com"}}}
}

func TestValidateManifest(t *testing.T) {
	if err := ValidateManifest(validManifest()); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	cases := []Manifest{
		func() Manifest { m := validManifest(); m.ID = "bad"; return m }(),
		func() Manifest { m := validManifest(); m.APIVersion = 99; return m }(),
		func() Manifest { m := validManifest(); m.Main = "../index.js"; return m }(),
		func() Manifest { m := validManifest(); m.Main = "dist/index.ts"; return m }(),
		func() Manifest { m := validManifest(); m.Permissions.Plugins = []string{"not valid"}; return m }(),
	}
	for _, candidate := range cases {
		if err := ValidateManifest(candidate); err == nil {
			t.Errorf("expected manifest to be rejected: %+v", candidate)
		}
	}
}

func TestSafePluginPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "plugin")
	if _, err := safePluginPath(root, "dist/index.js"); err != nil {
		t.Fatal(err)
	}
	if _, err := safePluginPath(root, "../outside.js"); err == nil {
		t.Fatal("path traversal was accepted")
	}
}

func TestStorageIsolatedByPlugin(t *testing.T) {
	root := t.TempDir()
	m := &Manager{root: root, plugins: map[string]PluginInfo{
		"com.example.one": {Manifest: validManifest()},
		"com.example.two": {Manifest: func() Manifest { m := validManifest(); m.ID = "com.example.two"; return m }()},
	}, settings: map[string]persistedState{}}
	if err := m.StorageSet("com.example.one", "key", "one"); err != nil {
		t.Fatal(err)
	}
	if value, ok, err := m.StorageGet("com.example.one", "key"); err != nil || !ok || value != "one" {
		t.Fatalf("unexpected stored value: %q %v %v", value, ok, err)
	}
	if _, ok, err := m.StorageGet("com.example.two", "key"); err != nil || ok {
		t.Fatalf("storage leaked across plugins: ok=%v err=%v", ok, err)
	}
	if err := m.StorageDelete("com.example.one", "key"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := m.StorageGet("com.example.one", "key"); err != nil || ok {
		t.Fatalf("storage key was not deleted: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".data", "com.example.one.json")); err != nil {
		t.Fatal(err)
	}
}

func TestApprovedPermissionsAreIntersected(t *testing.T) {
	m := &Manager{state: filepath.Join(t.TempDir(), "plugins.json"), plugins: map[string]PluginInfo{"com.example.test": {Manifest: validManifest()}}, settings: map[string]persistedState{}}
	if err := m.Approve("com.example.test", Permissions{Events: []string{"rc.message", "rc.connected"}, APIs: []string{"network.http", "pm.send"}, Network: []string{"https://example.com", "https://evil.example"}}); err != nil {
		t.Fatal(err)
	}
	info := m.plugins["com.example.test"]
	if len(info.ApprovedEvents) != 1 || len(info.ApprovedAPIs) != 1 || len(info.ApprovedHosts) != 1 {
		t.Fatalf("approval was not intersected: %+v", info)
	}
}

func TestPluginMessagingRequiresApprovedTargetAndBothPlugins(t *testing.T) {
	manifestOne := validManifest()
	manifestOne.ID = "com.example.one"
	manifestOne.Permissions = Permissions{APIs: []string{"plugins.messaging"}, Plugins: []string{"com.example.two"}}
	manifestTwo := validManifest()
	manifestTwo.ID = "com.example.two"
	manifestTwo.Permissions = Permissions{APIs: []string{"plugins.messaging"}}
	m := &Manager{
		state: filepath.Join(t.TempDir(), "plugins.json"),
		plugins: map[string]PluginInfo{
			manifestOne.ID: {Manifest: manifestOne, Enabled: true, Status: "ready"},
			manifestTwo.ID: {Manifest: manifestTwo, Enabled: true, Status: "ready"},
		},
		settings: map[string]persistedState{},
	}
	if err := m.Approve(manifestOne.ID, Permissions{APIs: []string{"plugins.messaging"}, Plugins: []string{"com.example.two"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.Approve(manifestTwo.ID, Permissions{APIs: []string{"plugins.messaging"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.AuthorizePluginMessage(manifestOne.ID, manifestTwo.ID); err != nil {
		t.Fatalf("approved plugin message was rejected: %v", err)
	}
	if err := m.AuthorizePluginMessage(manifestTwo.ID, manifestOne.ID); err == nil {
		t.Fatal("message to an undeclared target was accepted")
	}
}

func TestInternalAPIPathValidation(t *testing.T) {
	for _, path := range []string{"/health", "/hooks/events", "/"} {
		if !validExpressPath(path) {
			t.Fatalf("valid internal API path rejected: %q", path)
		}
	}
	for _, path := range []string{"", "health", "/../secret", "/hooks?x=1", `/hooks\\events`} {
		if validExpressPath(path) {
			t.Fatalf("invalid internal API path accepted: %q", path)
		}
	}
}

func TestInternalAPIUsesLoopbackAndPluginToken(t *testing.T) {
	manifest := validManifest()
	manifest.Permissions = Permissions{APIs: []string{"express.http"}}
	m := &Manager{
		state:    filepath.Join(t.TempDir(), "plugins.json"),
		plugins:  map[string]PluginInfo{"com.example.test": {Manifest: manifest, Enabled: true, Status: "ready"}},
		settings: map[string]persistedState{},
	}
	if err := m.Approve(manifest.ID, Permissions{APIs: []string{"express.http"}}); err != nil {
		t.Fatal(err)
	}
	m.SetRuntimeEmitter(func(name string, data any) {
		if name != "plugin:http" {
			return
		}
		event, ok := data.(ExpressRequestEvent)
		if !ok {
			t.Errorf("unexpected internal API event type %T", data)
			return
		}
		if err := m.RespondExpressRequest(manifest.ID, event.RequestID, ExpressResponse{Status: http.StatusAccepted, Headers: map[string]string{"Content-Type": "text/plain"}, Body: "accepted"}); err != nil {
			t.Errorf("responding to internal API request: %v", err)
		}
	})
	info, err := m.ExpressListen(manifest.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.httpServer.server.Close() }()
	route, err := m.RegisterExpressRoute(manifest.ID, http.MethodPost, "/events")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.BaseURL, "http://127.0.0.1:") {
		t.Fatalf("internal API is not loopback-only: %s", info.BaseURL)
	}
	request, err := http.NewRequest(http.MethodPost, route.URL, strings.NewReader(`{"event":"test"}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("X-GoRC-Plugin-Token", info.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusAccepted || string(body) != "accepted" {
		t.Fatalf("unexpected internal API response: %d %q", response.StatusCode, string(body))
	}
}

func TestSocketRequiresWSSAndApprovedOrigin(t *testing.T) {
	manifest := validManifest()
	manifest.Permissions = Permissions{APIs: []string{"network.socket"}, Network: []string{"wss://gateway.example.com"}}
	m := &Manager{
		plugins:  map[string]PluginInfo{"com.example.test": {Manifest: manifest, Enabled: true, Status: "ready", ApprovedAPIs: []string{"network.socket"}, ApprovedHosts: []string{"wss://gateway.example.com"}}},
		settings: map[string]persistedState{},
	}
	if _, err := m.OpenSocket(manifest.ID, SocketRequest{URL: "ws://gateway.example.com"}); err == nil {
		t.Fatal("insecure ws socket was accepted")
	}
	if _, err := m.OpenSocket(manifest.ID, SocketRequest{URL: "wss://other.example.com"}); err == nil {
		t.Fatal("unapproved socket origin was accepted")
	}
}

func TestHTTPRequestRateLimitAndBodyLimit(t *testing.T) {
	m := &Manager{requestLog: map[string][]time.Time{}}
	for i := 0; i < maxHTTPRequestsPerMinute; i++ {
		if err := m.allowHTTPRequest("com.example.test"); err != nil {
			t.Fatalf("request %d was unexpectedly rejected: %v", i, err)
		}
	}
	if err := m.allowHTTPRequest("com.example.test"); err == nil {
		t.Fatal("rate limit did not reject the next request")
	}
	if err := m.allowHTTPRequest("com.example.other"); err != nil {
		t.Fatalf("rate limit leaked across plugins: %v", err)
	}
	manifest := validManifest()
	manifest.Permissions = Permissions{APIs: []string{"network.http"}, Network: []string{"https://example.com"}}
	m.plugins = map[string]PluginInfo{manifest.ID: {Manifest: manifest, Enabled: true, Status: "ready", ApprovedAPIs: []string{"network.http"}, ApprovedHosts: []string{"https://example.com"}}}
	if _, err := m.Request(manifest.ID, HTTPRequest{URL: "https://example.com", Body: string(make([]byte, maxRequestBodyBytes+1))}); err == nil {
		t.Fatal("oversized request body was accepted")
	}
}

func TestPluginFailureCircuitBreaker(t *testing.T) {
	manifest := validManifest()
	root := t.TempDir()
	m := &Manager{
		root:     root,
		state:    filepath.Join(root, "plugins.json"),
		plugins:  map[string]PluginInfo{manifest.ID: {Manifest: manifest, Enabled: true, Status: "ready"}},
		settings: map[string]persistedState{},
	}
	for attempt := 1; attempt < maxPluginFailures; attempt++ {
		disabled, err := m.RecordPluginFailure(manifest.ID)
		if err != nil || disabled {
			t.Fatalf("attempt %d unexpectedly disabled plugin: disabled=%v err=%v", attempt, disabled, err)
		}
	}
	disabled, err := m.RecordPluginFailure(manifest.ID)
	if err != nil || !disabled {
		t.Fatalf("final failure did not trip circuit breaker: disabled=%v err=%v", disabled, err)
	}
	if plugin := m.plugins[manifest.ID]; plugin.Enabled || plugin.Status != "disabled" || plugin.FailureCount != maxPluginFailures {
		t.Fatalf("unexpected disabled plugin state: %+v", plugin)
	}
	if err := m.SetEnabled(manifest.ID, true); err != nil {
		t.Fatal(err)
	}
	if plugin := m.plugins[manifest.ID]; !plugin.Enabled || plugin.FailureCount != 0 {
		t.Fatalf("enabling plugin did not reset failure count: %+v", plugin)
	}
}

func TestCreateTemplate(t *testing.T) {
	m := &Manager{root: t.TempDir(), state: filepath.Join(t.TempDir(), "plugins.json"), plugins: map[string]PluginInfo{}, settings: map[string]persistedState{}}
	info, err := m.CreateTemplate("My Discord Logger")
	if err != nil {
		t.Fatal(err)
	}
	if info.Manifest.ID != "com.gorc.my-discord-logger" || info.Manifest.Main != "dist/index.js" {
		t.Fatalf("unexpected template: %+v", info.Manifest)
	}
	if len(info.Manifest.Permissions.APIs) != 1 || info.Manifest.Permissions.APIs[0] != "ui.tabs" {
		t.Fatalf("template does not request the tab UI permission: %+v", info.Manifest.Permissions)
	}
	b, err := os.ReadFile(filepath.Join(info.Directory, info.Manifest.Main))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) == "" || !strings.Contains(string(b), "extends Plugin") || !strings.Contains(string(b), "new GorcMyDiscordLogger()") || !strings.Contains(string(b), "this.ui.tabs.register") {
		t.Fatalf("template bundle is empty or invalid: %q", string(b))
	}
	source, err := os.ReadFile(filepath.Join(info.Directory, "src", "index.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "export default class GorcMyDiscordLogger extends Plugin") {
		t.Fatalf("template source does not use the Plugin base class: %q", string(source))
	}
	if _, err := os.Stat(filepath.Join(info.Directory, "src", "plugin.d.ts")); err != nil {
		t.Fatalf("template type declarations are missing: %v", err)
	}
}
