package filedrag

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestServer(t *testing.T, download Downloader) (*Server, string) {
	t.Helper()
	s := New(download)
	t.Cleanup(func() { _ = s.Close() })
	base, err := s.Base()
	if err != nil {
		t.Fatal(err)
	}
	return s, base
}

func get(t *testing.T, method, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestBaseIsLoopbackWithASecretAndStaysStable(t *testing.T) {
	s, base := newTestServer(t, nil)
	if !strings.HasPrefix(base, "http://127.0.0.1:") || !strings.HasSuffix(base, "/") {
		t.Fatalf("base = %q, want a loopback URL ending in a slash", base)
	}
	secret := strings.TrimSuffix(strings.SplitAfter(base, routePrefix)[1], "/")
	if len(secret) != 64 {
		t.Fatalf("secret %q has %d characters, want 64 (256 bits)", secret, len(secret))
	}
	again, err := s.Base()
	if err != nil || again != base {
		t.Fatalf("second Base() = %q, %v; every window must receive the same URL", again, err)
	}
	other, otherBase := newTestServer(t, nil)
	_ = other
	if otherBase == base {
		t.Fatal("two servers shared a URL or secret")
	}
}

func TestServesTheRemoteFileAsADownload(t *testing.T) {
	var requested string
	_, base := newTestServer(t, func(_ context.Context, remote string) ([]byte, error) {
		requested = remote
		return []byte("script contents"), nil
	})
	resp := get(t, http.MethodGet, base+"levels/folder%20one/hello%20world.nw")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "script contents" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if requested != "levels/folder one/hello world.nw" {
		t.Fatalf("downloader asked for %q; the URL must decode to the remote path", requested)
	}
	if got := resp.Header.Get("Content-Length"); got != "15" {
		t.Fatalf("Content-Length = %q", got)
	}
	if disposition := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(disposition, "attachment;") || !strings.Contains(disposition, "hello world.nw") {
		t.Fatalf("Content-Disposition = %q", disposition)
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing hardening headers: %v", resp.Header)
	}
}

func TestNonASCIIFileNamesSurviveInTheDisposition(t *testing.T) {
	_, base := newTestServer(t, func(context.Context, string) ([]byte, error) { return []byte("x"), nil })
	resp := get(t, http.MethodGet, base+"levels/"+"%C3%A1rvore.nw")
	disposition := resp.Header.Get("Content-Disposition")
	if resp.StatusCode != http.StatusOK || !strings.Contains(strings.ToLower(disposition), "utf-8") {
		t.Fatalf("status %d, disposition %q; a non-ASCII name needs the RFC 2231 form", resp.StatusCode, disposition)
	}
}

func TestHeadDoesNotDownload(t *testing.T) {
	var calls atomic.Int32
	_, base := newTestServer(t, func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		return []byte("x"), nil
	})
	resp := get(t, http.MethodHead, base+"a.txt")
	if resp.StatusCode != http.StatusOK || calls.Load() != 0 {
		t.Fatalf("HEAD: status %d, %d downloads; a probe must not cost a download", resp.StatusCode, calls.Load())
	}
}

func TestRefusesRequestsThatDoNotProveTheyAreTheApplication(t *testing.T) {
	var calls atomic.Int32
	_, base := newTestServer(t, func(context.Context, string) ([]byte, error) {
		calls.Add(1)
		return []byte("secret data"), nil
	})
	hostAndPrefix := strings.TrimPrefix(base, "http://")
	host := hostAndPrefix[:strings.Index(hostAndPrefix, "/")]
	secretPath := strings.TrimPrefix(base, "http://"+host)

	cases := []struct {
		name   string
		method string
		path   string
		host   string
		want   int
	}{
		{"wrong secret", http.MethodGet, routePrefix + strings.Repeat("0", 64) + "/a.txt", host, http.StatusNotFound},
		{"no secret", http.MethodGet, routePrefix + "a.txt", host, http.StatusNotFound},
		{"other route", http.MethodGet, "/a.txt", host, http.StatusNotFound},
		{"DNS rebinding host", http.MethodGet, secretPath + "a.txt", "attacker.example:" + host[strings.LastIndex(host, ":")+1:], http.StatusForbidden},
		{"localhost alias", http.MethodGet, secretPath + "a.txt", "localhost:" + host[strings.LastIndex(host, ":")+1:], http.StatusForbidden},
		{"POST", http.MethodPost, secretPath + "a.txt", host, http.StatusMethodNotAllowed},
		{"parent traversal", http.MethodGet, secretPath + "levels/../../etc/passwd", host, http.StatusBadRequest},
		{"empty path", http.MethodGet, secretPath, host, http.StatusBadRequest},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(tc.method, "http://"+host+tc.path, nil)
		req.Host = tc.host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s: status %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("%d downloads ran for refused requests", calls.Load())
	}
}

func TestDownloadFailureIsABadGatewayAndNeverLeaksTheSecret(t *testing.T) {
	_, base := newTestServer(t, func(context.Context, string) ([]byte, error) {
		return nil, errors.New("script request timed out")
	})
	resp := get(t, http.MethodGet, base+"a.txt")
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "timed out") {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	if resp.Header.Get("Content-Disposition") != "" {
		t.Fatal("an error page must not be offered as the file")
	}
	secret := strings.TrimSuffix(strings.SplitAfter(base, routePrefix)[1], "/")
	if strings.Contains(string(body), secret) {
		t.Fatal("the error body contains the secret")
	}
}

func TestADroppedClientCancelsTheDownload(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	_, base := newTestServer(t, func(ctx context.Context, _ string) ([]byte, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"big.bin", nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
	<-started
	cancel()
	<-done
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("the downloader kept running after the client left")
	}
}

func TestCloseStopsListeningAndABaseAfterwardsGetsANewSecret(t *testing.T) {
	s, base := newTestServer(t, func(context.Context, string) ([]byte, error) { return []byte("x"), nil })
	host := strings.TrimPrefix(base, "http://")
	host = host[:strings.Index(host, "/")]
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if conn, err := net.Dial("tcp", host); err == nil {
		conn.Close()
		t.Fatal("the port is still accepting connections after Close")
	}
	next, err := s.Base()
	if err != nil || next == base {
		t.Fatalf("restarted Base() = %q, %v; it must be a new URL", next, err)
	}
}

func TestCleanRemotePath(t *testing.T) {
	good := map[string]string{
		"levels/a.nw":        "levels/a.nw",
		"/levels/a.nw":       "levels/a.nw",
		`levels\sub\a.nw`:    "levels/sub/a.nw",
		"  levels//a.nw  ":   "levels/a.nw",
		"levels/./a.nw":      "levels/a.nw",
		"a.txt":              "a.txt",
		"weapons/Staff/News": "weapons/Staff/News",
	}
	for in, want := range good {
		if got, err := CleanRemotePath(in); err != nil || got != want {
			t.Errorf("CleanRemotePath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "/", "..", "../a", "a/../b", "a/..", "a\x00b", `..\a`} {
		if got, err := CleanRemotePath(in); err == nil {
			t.Errorf("CleanRemotePath(%q) = %q; want an error", in, got)
		}
	}
}
