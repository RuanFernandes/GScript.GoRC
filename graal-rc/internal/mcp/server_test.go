package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeBackend struct {
	chat  []ChatLine
	files []File
	sent  string
}

func (f *fakeBackend) GetRCChat(limit int) []ChatLine {
	if limit > len(f.chat) {
		limit = len(f.chat)
	}
	return f.chat[len(f.chat)-limit:]
}
func (f *fakeBackend) SendRCChat(message string) error   { f.sent = message; return nil }
func (f *fakeBackend) FileBrowserStart() error           { return nil }
func (f *fakeBackend) FileBrowserCd(string) error        { return nil }
func (f *fakeBackend) FileBrowserFiles() ([]File, error) { return f.files, nil }
func (f *fakeBackend) ReadFile(path string) ([]byte, error) {
	if path != "scripts/main.txt" {
		return nil, errors.New("not found")
	}
	return []byte("echo hello"), nil
}

func callTool(t *testing.T, backend *fakeBackend, name string, args map[string]any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	NewServer(backend).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestToolsCallChatAndRead(t *testing.T) {
	backend := &fakeBackend{chat: []ChatLine{{Text: "one", Timestamp: "2026-08-05T20:00:00Z"}, {Text: "two", Timestamp: "2026-08-05T20:00:01Z"}}}
	result := callTool(t, backend, "get_rc_chat", map[string]any{"limit": 2})
	if result["error"] != nil {
		t.Fatalf("unexpected error: %v", result["error"])
	}
	content := result["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(content, "timestamp") || !strings.Contains(content, "2026-08-05T20:00:00Z") {
		t.Fatalf("chat result does not include timestamps: %s", content)
	}
	result = callTool(t, backend, "send_rc_chat", map[string]any{"message": "hello"})
	if backend.sent != "hello" || result["error"] != nil {
		t.Fatalf("send failed: sent=%q result=%v", backend.sent, result)
	}
	result = callTool(t, backend, "filebrowser_read", map[string]any{"path": "scripts/main.txt"})
	if result["error"] != nil {
		t.Fatalf("read failed: %v", result["error"])
	}
}

func TestInvalidToolArguments(t *testing.T) {
	result := callTool(t, &fakeBackend{}, "get_rc_chat", map[string]any{"limit": 51})
	if result["error"] == nil {
		t.Fatal("expected invalid limit error")
	}
}
