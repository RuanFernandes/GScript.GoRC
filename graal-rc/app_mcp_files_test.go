package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"graal-rc/internal/mcp"
	"graal-rc/rclib"
)

func mcpDownloadSandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AppData", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	return dir
}

func filesUnder(t *testing.T, dir string) (count int) {
	t.Helper()
	if err := filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return count
}

func TestMCPDownloadSavesTheFileFromItsOwnFolder(t *testing.T) {
	sandbox := mcpDownloadSandbox(t)
	a, remote := remoteApp(t, map[string]string{"levels/town.nw": "level data", "other/town.nw": "other"})
	backend := appMCPBackend{app: a}

	file, err := backend.DownloadFile(context.Background(), "/levels/town.nw")
	if err != nil {
		t.Fatal(err)
	}
	wantCalls(t, remote, "download levels/town.nw")
	saved, err := os.ReadFile(file.LocalPath)
	if err != nil || string(saved) != "level data" || string(file.Content) != "level data" || file.Path != "levels/town.nw" {
		t.Fatalf("file = %+v, saved %q, err %v", file, saved, err)
	}
	if !strings.HasPrefix(file.LocalPath, sandbox) || !strings.HasSuffix(file.LocalPath, "town.nw") {
		t.Fatalf("localPath = %q", file.LocalPath)
	}
	same, err := backend.DownloadFile(context.Background(), "other/town.nw")
	if err != nil || same.LocalPath == file.LocalPath {
		t.Fatalf("same-named files must not share a local copy: %q, err %v", same.LocalPath, err)
	}
}

func TestMCPDownloadOfAMissingOrUnsafeFileCreatesNothing(t *testing.T) {
	sandbox := mcpDownloadSandbox(t)
	a, remote := remoteApp(t, map[string]string{"levels/a:b": "x", "levels/town.nw": "x"})
	for _, path := range []string{"levels/missing.nw", "levels/a:b", "levels/..", "../x", "levels/town.nw/", "levels/*", ""} {
		if _, err := (appMCPBackend{app: a}).DownloadFile(context.Background(), path); err == nil {
			t.Errorf("%q: expected an error", path)
		}
	}
	wantCalls(t, remote)
	if n := filesUnder(t, sandbox); n != 0 {
		t.Fatalf("%d local files were created", n)
	}
}

func TestMCPUploadFromContentAndLocalFile(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"levels/town.nw": "old"})
	backend := appMCPBackend{app: a}
	text := "new"

	result, err := backend.UploadFile(context.Background(), mcp.UploadRequest{Path: "levels/town.nw", Content: &text})
	if err != nil {
		t.Fatal(err)
	}
	if result != (mcp.UploadResult{Path: "levels/town.nw", Size: 3, Replaced: true}) {
		t.Fatalf("result = %+v", result)
	}
	// The replaced version is read first, for the safety backup.
	wantCalls(t, remote, "download levels/town.nw", "upload levels/town.nw")

	// The local name does not matter: path is the destination.
	local := filepath.Join(t.TempDir(), "picture.bin")
	if err := os.WriteFile(local, []byte{0, 1, 2, 255}, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err = backend.UploadFile(context.Background(), mcp.UploadRequest{Path: `levels\new.png`, LocalPath: local})
	if err != nil {
		t.Fatal(err)
	}
	if result != (mcp.UploadResult{Path: "levels/new.png", Size: 4, Replaced: false}) {
		t.Fatalf("result = %+v", result)
	}
	if remote.files["levels/new.png"] != "\x00\x01\x02\xff" {
		t.Fatalf("uploaded %q", remote.files["levels/new.png"])
	}
}

func TestMCPUploadRefusesBadDestinationsAndSources(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{})
	text := "x"
	requests := []mcp.UploadRequest{
		{Path: "levels/", Content: &text},
		{Path: `levels\`, Content: &text},
		{Path: "../x", Content: &text},
		{Path: "levels/*", Content: &text},
		{Path: "levels/a.nw", LocalPath: "relative.nw"},
		{Path: "levels/a.nw", LocalPath: filepath.Join(t.TempDir(), "missing.nw")},
		{Path: "levels/a.nw", LocalPath: t.TempDir()},
	}
	for _, request := range requests {
		if _, err := (appMCPBackend{app: a}).UploadFile(context.Background(), request); err == nil {
			t.Errorf("%+v: expected an error", request)
		}
	}
	wantCalls(t, remote)
}

func TestCheckUploadSize(t *testing.T) {
	if err := checkUploadSize(10, 10); err != nil {
		t.Fatal(err)
	}
	if err := checkUploadSize(10, 0); err != nil {
		t.Fatalf("no limit reported yet: %v", err)
	}
	if err := checkUploadSize(11, 10); err == nil {
		t.Fatal("expected the limit to be enforced")
	}
}

// The server only knows bare names in its current folder: asking for the full
// path left the transfer waiting and blocked the File Browser.
func TestMCPReadOpensTheFolderOfTheFile(t *testing.T) {
	a, remote := remoteApp(t, map[string]string{"levels/town.nw": "level data"})
	content, err := (appMCPBackend{app: a}).ReadFile(context.Background(), "levels/town.nw")
	if err != nil || string(content) != "level data" {
		t.Fatalf("content %q, err %v", content, err)
	}
	wantCalls(t, remote, "download levels/town.nw")
}

// While it waits, an MCP download keeps the rest of the File Browser out.
func TestMCPDownloadsStopWaitingSoon(t *testing.T) {
	mcpDownloadSandbox(t)
	for name, read := range map[string]func(appMCPBackend, context.Context) error{
		"read": func(b appMCPBackend, ctx context.Context) error {
			_, err := b.ReadFile(ctx, "levels/town.nw")
			return err
		},
		"download": func(b appMCPBackend, ctx context.Context) error {
			_, err := b.DownloadFile(ctx, "levels/town.nw")
			return err
		},
	} {
		a, _ := remoteApp(t, map[string]string{"levels/town.nw": "x"})
		open := a.openRemoteFolder
		var seen context.Context
		a.openRemoteFolder = func(ctx context.Context, folder string, use func(fileBrowserUploadFolder, []rclib.FileBrowserEntry) error) error {
			seen = ctx
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > mcpDownloadTimeout {
				t.Errorf("%s: deadline = %v (set %v)", name, deadline, ok)
			}
			return open(ctx, folder, use)
		}
		caller, cancel := context.WithCancel(context.Background())
		if err := read(appMCPBackend{app: a}, caller); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		cancel()
		if seen == nil || seen.Err() == nil {
			t.Errorf("%s: the wait does not end with its context", name)
		}
	}
}
