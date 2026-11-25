package mcp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerRejectsBrowserRequests(t *testing.T) {
	const body = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_rc_chat","arguments":{"message":"hi"}}}`
	tests := []struct {
		name        string
		host        string
		origin      string
		contentType string
		extra       map[string]string
		allowed     bool
	}{
		{name: "agent on loopback ip", host: "127.0.0.1:8765", contentType: "application/json", allowed: true},
		{name: "agent on localhost", host: "localhost:8765", contentType: "application/json; charset=utf-8", allowed: true},
		{name: "agent on ipv6 loopback", host: "[::1]:8765", contentType: "application/json", allowed: true},
		{name: "web page", host: "127.0.0.1:8765", origin: "https://evil.example", contentType: "application/json"},
		{name: "opaque origin", host: "127.0.0.1:8765", origin: "null", contentType: "application/json"},
		{name: "cross-site fetch without origin", host: "127.0.0.1:8765", contentType: "application/json", extra: map[string]string{"Sec-Fetch-Site": "cross-site"}},
		{name: "simple request", host: "127.0.0.1:8765", contentType: "text/plain;charset=UTF-8"},
		{name: "no content type", host: "127.0.0.1:8765"},
		{name: "dns rebinding", host: "evil.example:8765", contentType: "application/json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := &fakeBackend{}
			req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.contentType != "" {
				req.Header.Set("Content-Type", tt.contentType)
			}
			for key, value := range tt.extra {
				req.Header.Set(key, value)
			}
			recorder := httptest.NewRecorder()
			NewServer(backend).ServeHTTP(recorder, req)
			ran := backend.sent != ""
			if ran != tt.allowed {
				t.Fatalf("tool ran = %v, want %v (status %d)", ran, tt.allowed, recorder.Code)
			}
		})
	}
}

// RC_MCP_ADDR may bind the server to another name; that name must keep working.
func TestServerAllowsConfiguredHost(t *testing.T) {
	backend := &fakeBackend{}
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"send_rc_chat","arguments":{"message":"hi"}}}`))
	req.Host = "devbox.lan:9000"
	req.Header.Set("Content-Type", "application/json")
	NewServer(backend, "devbox.lan:9000").ServeHTTP(httptest.NewRecorder(), req)
	if backend.sent != "hi" {
		t.Fatal("configured host was rejected")
	}
}
