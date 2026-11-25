package plugins

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func trustManager(t *testing.T) *Manager {
	t.Helper()
	root := t.TempDir()
	return &Manager{
		root:     filepath.Join(root, "plugins"),
		state:    filepath.Join(root, "plugins.json"),
		plugins:  map[string]PluginInfo{},
		settings: map[string]persistedState{},
	}
}

func installPlugin(t *testing.T, m *Manager, folder, id string) {
	t.Helper()
	dir := filepath.Join(m.root, folder)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"id":"` + id + `","name":"P","version":"1","apiVersion":` + apiVersionText() + `,"main":"main.js","permissions":{"apis":["network.http"]}}`
	for name, content := range map[string]string{"manifest.json": manifest, "main.js": "//"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// Only the first URL used to be checked, so an approved host could redirect a
// plugin anywhere, including the local MCP server.
func TestPluginRequestRefusesRedirectToUnapprovedTarget(t *testing.T) {
	reached := false
	local := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer local.Close()
	otherTLS := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	defer otherTLS.Close()
	var target string
	approved := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/same" {
			w.Write([]byte("ok"))
			return
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
	}))
	defer approved.Close()
	u, _ := url.Parse(approved.URL)

	m := trustManager(t)
	m.httpTransport = approved.Client().Transport
	m.plugins["com.a.b"] = PluginInfo{Enabled: true, ApprovedAPIs: []string{"network.http"}, ApprovedHosts: []string{origin(u)}}

	for _, to := range []string{local.URL, otherTLS.URL} {
		target = to
		if _, err := m.Request("com.a.b", HTTPRequest{URL: approved.URL, Method: "POST", Body: "x"}); !errors.Is(err, ErrPermissionDenied) {
			t.Errorf("redirect to %s: error = %v, want permission denied", to, err)
		}
	}
	if reached {
		t.Fatal("a redirect target outside the approval was contacted")
	}
	target = approved.URL + "/same"
	if res, err := m.Request("com.a.b", HTTPRequest{URL: approved.URL}); err != nil || res.Body != "ok" {
		t.Fatalf("approved redirect failed: %v %q", err, res.Body)
	}
}

func TestDisabledPluginCannotMakeRequests(t *testing.T) {
	m := trustManager(t)
	m.plugins["com.a.b"] = PluginInfo{ApprovedAPIs: []string{"network.http"}, ApprovedHosts: []string{"https://example.invalid"}}
	if _, err := m.Request("com.a.b", HTTPRequest{URL: "https://example.invalid/"}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("error = %v, want permission denied", err)
	}
}

func TestDuplicatePluginIDsGetNoTrust(t *testing.T) {
	m := trustManager(t)
	installPlugin(t, m, "com.a.b", "com.a.b")
	m.settings["com.a.b"] = persistedState{Enabled: true, ApprovedAPIs: []string{"network.http"}}
	installPlugin(t, m, "zzz-copy", "com.a.b")
	if err := m.Discover(); err != nil {
		t.Fatal(err)
	}
	p := m.plugins["com.a.b"]
	if p.Enabled || len(p.ApprovedAPIs) != 0 || p.Status != "invalid" {
		t.Fatalf("duplicate id kept trust: enabled=%v apis=%v status=%s", p.Enabled, p.ApprovedAPIs, p.Status)
	}
}

func TestApprovalDoesNotFollowTheIDToAnotherFolder(t *testing.T) {
	m := trustManager(t)
	installPlugin(t, m, "com.a.b", "com.a.b")
	if err := m.Discover(); err != nil {
		t.Fatal(err)
	}
	if err := m.Approve("com.a.b", Permissions{APIs: []string{"network.http"}}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetEnabled("com.a.b", true); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(m.root, "com.a.b"), filepath.Join(m.root, "impostor")); err != nil {
		t.Fatal(err)
	}
	if err := m.Discover(); err != nil {
		t.Fatal(err)
	}
	if p := m.plugins["com.a.b"]; p.Enabled || len(p.ApprovedAPIs) != 0 {
		t.Fatalf("another folder inherited trust: enabled=%v apis=%v", p.Enabled, p.ApprovedAPIs)
	}
}
