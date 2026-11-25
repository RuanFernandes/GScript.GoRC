package mcp

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")

func transferBackend(content []byte) *fakeBackend {
	return &fakeBackend{download: DownloadedFile{Path: "levels/a.bin", LocalPath: `C:\tmp\a.bin`, Content: content}}
}

func downloadResult(t *testing.T, content []byte) (string, []any) {
	t.Helper()
	response := callTool(t, transferBackend(content), "filebrowser_download", map[string]any{"path": "levels/a.bin"})
	text, isError := toolText(t, response)
	if isError {
		t.Fatalf("download failed: %s", text)
	}
	return text, response["result"].(map[string]any)["content"].([]any)
}

func TestFileBrowserDownloadReturnsLocalPathAndText(t *testing.T) {
	text, blocks := downloadResult(t, []byte("hello"))
	for _, want := range []string{`"path":"levels/a.bin"`, `"localPath":"C:\\tmp\\a.bin"`, `"size":5`, `"content":"hello"`} {
		if !strings.Contains(text, want) {
			t.Errorf("result is missing %s: %s", want, text)
		}
	}
	if len(blocks) != 1 {
		t.Fatalf("text download returned %d blocks", len(blocks))
	}
}

func TestFileBrowserDownloadInlinesOnlySmallText(t *testing.T) {
	cases := map[string][]byte{
		"not UTF-8":  {0xff, 0xfe, 0x01},
		"NUL bytes":  []byte("SQLite format 3\x00"),
		"large text": bytes.Repeat([]byte("a"), maxInlineTextBytes+1),
	}
	for name, content := range cases {
		text, blocks := downloadResult(t, content)
		if strings.Contains(text, `"content"`) || len(blocks) != 1 {
			t.Errorf("%s: the file must only be reported by its local path", name)
		}
		if !strings.Contains(text, `"localPath"`) {
			t.Errorf("%s: missing localPath", name)
		}
	}
}

func TestFileBrowserDownloadReturnsSmallImagesAsImageBlocks(t *testing.T) {
	_, blocks := downloadResult(t, pngBytes)
	if len(blocks) != 2 {
		t.Fatalf("expected metadata and image blocks, got %d", len(blocks))
	}
	image := blocks[1].(map[string]any)
	if image["type"] != "image" || image["mimeType"] != "image/png" || image["data"] == "" {
		t.Fatalf("unexpected image block: %v", image)
	}
	large := append(append([]byte{}, pngBytes...), make([]byte, maxInlineImageBytes)...)
	if _, blocks := downloadResult(t, large); len(blocks) != 1 {
		t.Fatal("a large image must not be inlined")
	}
}

func TestFileBrowserUploadPassesItsSource(t *testing.T) {
	backend := &fakeBackend{}
	_, isError := toolText(t, callTool(t, backend, "filebrowser_upload", map[string]any{"path": "levels/a.nw", "localPath": `C:\tmp\a.nw`}))
	if isError || backend.uploaded.Path != "levels/a.nw" || backend.uploaded.LocalPath != `C:\tmp\a.nw` || backend.uploaded.Content != nil {
		t.Fatalf("localPath upload: %+v (error %v)", backend.uploaded, isError)
	}
	// Empty text is a file too.
	_, isError = toolText(t, callTool(t, backend, "filebrowser_upload", map[string]any{"path": "levels/a.nw", "content": ""}))
	if isError || backend.uploaded.Content == nil || *backend.uploaded.Content != "" || backend.uploaded.LocalPath != "" {
		t.Fatalf("content upload: %+v (error %v)", backend.uploaded, isError)
	}
}

func TestFileBrowserTransfersRejectBadArguments(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"filebrowser_download", map[string]any{}},
		{"filebrowser_download", map[string]any{"path": " "}},
		{"filebrowser_upload", map[string]any{"content": "x"}},
		{"filebrowser_upload", map[string]any{"path": "a.txt"}},
		{"filebrowser_upload", map[string]any{"path": "a.txt", "content": "x", "localPath": `C:\a.txt`}},
		{"filebrowser_upload", map[string]any{"path": "a.txt", "localPath": ""}},
		{"filebrowser_upload", map[string]any{"path": "a.txt", "content": 3}},
	}
	for _, c := range cases {
		backend := &fakeBackend{}
		if response := callTool(t, backend, c.tool, c.args); response["error"] == nil {
			t.Errorf("%s %v: expected an RPC error", c.tool, c.args)
		}
		if backend.calls != 0 {
			t.Errorf("%s %v: the backend was called", c.tool, c.args)
		}
	}
}

type slowBackend struct{ *fakeBackend }

func (s slowBackend) DownloadFile(ctx context.Context, path string) (DownloadedFile, error) {
	time.Sleep(400 * time.Millisecond)
	return s.fakeBackend.DownloadFile(ctx, path)
}

func TestFileBrowserTransferOutlivesTheServerWriteTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		NewServer(slowBackend{transferBackend([]byte("hi"))}).ServeHTTP(w, r)
	})
	server := httptest.NewUnstartedServer(handler)
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	defer server.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"filebrowser_download","arguments":{"path":"levels/a.bin"}}}`
	response, err := http.Post(server.URL+"/mcp", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("the response was dropped: %v", err)
	}
	defer response.Body.Close()
	answer, err := io.ReadAll(response.Body)
	if err != nil || !strings.Contains(string(answer), "localPath") {
		t.Fatalf("answer = %q, err %v", answer, err)
	}
}
