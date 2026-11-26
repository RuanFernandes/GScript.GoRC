package mcp

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	out := &bytes.Buffer{}
	log.SetOutput(out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return out
}

func TestToolCallsAreLoggedWithPathAndOutcome(t *testing.T) {
	out := captureLog(t)
	callTool(t, browserBackend(), "filebrowser_cd", map[string]any{"path": "missing"})
	for _, want := range []string{`[mcp] call tool=filebrowser_cd path="missing"`, `[mcp] done tool=filebrowser_cd`, `error="file-browser folder listing timed out"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("log is missing %q: %q", want, out.String())
		}
	}
}

// The log is a plain file: contents, scripts and messages stay out of it.
func TestToolCallLogLeavesContentOut(t *testing.T) {
	out := captureLog(t)
	callTool(t, &fakeBackend{}, "filebrowser_upload", map[string]any{"path": "a.txt", "content": "secret-content"})
	callTool(t, &fakeBackend{}, "send_rc_chat", map[string]any{"message": "secret-message"})
	if strings.Contains(out.String(), "secret") || !strings.Contains(out.String(), "tool=send_rc_chat") {
		t.Fatalf("log = %q", out.String())
	}
}
