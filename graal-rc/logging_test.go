package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedactLogMessagePreservesDiagnosticMetadata(t *testing.T) {
	input := "login password=\"p@ss\" access_token=access-secret Authorization: Bearer header-secret " +
		"headers={\"Authorization\":\"Bearer json-header-secret\",\"Content-Type\":\"application/json\",\"X-Request-ID\":\"req-1\"} " +
		"payload={\"account\":\"alice\",\"password\":\"payload-secret\"} " +
		"content \"{\\\"password\\\":\\\"json-secret\\\",\\\"kind\\\":\\\"player\\\"}\" github_pat_secret"

	got := redactLogMessage(input)
	for _, secret := range []string{
		"p@ss",
		"access-secret",
		"header-secret",
		"json-header-secret",
		"payload-secret",
		"json-secret",
		"github_pat_secret",
	} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted output contains secret %q: %s", secret, got)
		}
	}
	for _, diagnostic := range []string{
		"[REDACTED]",
		"[REDACTED token]",
		"[REDACTED header]",
		"[REDACTED payload len=",
		"Content-Type",
		"X-Request-ID",
		"kind",
	} {
		if !strings.Contains(got, diagnostic) {
			t.Fatalf("redacted output lost diagnostic marker %q: %s", diagnostic, got)
		}
	}
}

func TestRotatingLogWriterRotatesBySizeAndDate(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, time.August, 5, 23, 59, 0, 0, time.UTC)
	w, err := newRotatingLogWriterWithClock(dir, 32, 10, func() time.Time {
		return now
	})
	if err != nil {
		t.Fatal(err)
	}

	first := "first diagnostic line\n"
	second := "second diagnostic line\n"
	if n, err := w.Write([]byte(first)); err != nil || n != len(first) {
		t.Fatalf("first write = (%d, %v)", n, err)
	}
	if n, err := w.Write([]byte(second)); err != nil || n != len(second) {
		t.Fatalf("second write = (%d, %v)", n, err)
	}

	now = now.Add(24 * time.Hour)
	nextDay := "new day diagnostic\n"
	if n, err := w.Write([]byte(nextDay)); err != nil || n != len(nextDay) {
		t.Fatalf("next-day write = (%d, %v)", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	rotated, err := os.ReadFile(filepath.Join(dir, "app_2026_08_05_001.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(rotated) != first {
		t.Fatalf("rotated content = %q, want %q", rotated, first)
	}

	active, err := os.ReadFile(filepath.Join(dir, "app_2026_08_06.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(active) != nextDay {
		t.Fatalf("next-day content = %q, want %q", active, nextDay)
	}

	previousDay, err := os.ReadFile(filepath.Join(dir, "app_2026_08_05.log"))
	if err != nil {
		t.Fatal(err)
	}
	if string(previousDay) != second {
		t.Fatalf("new segment content = %q, want %q", previousDay, second)
	}
}

func TestRotatingLogWriterRedactsFileAndMirror(t *testing.T) {
	dir := t.TempDir()
	var mirror bytes.Buffer
	w, err := newRotatingLogWriterWithClock(dir, 1024, 5, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	w.mirror = &mirror

	input := []byte("authorization=Bearer file-secret token=another-secret\n")
	if _, err := w.Write(input); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	active, err := os.ReadFile(filepath.Join(dir, "app_"+time.Now().Format("2006_01_02")+".log"))
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]string{
		"file":   string(active),
		"mirror": mirror.String(),
	} {
		if strings.Contains(got, "file-secret") || strings.Contains(got, "another-secret") {
			t.Fatalf("%s contains an unredacted secret: %s", name, got)
		}
		if !strings.Contains(got, "[REDACTED") {
			t.Fatalf("%s does not contain a redaction marker: %s", name, got)
		}
	}
}

func TestCleanupManagedLogsIsBoundedAndScoped(t *testing.T) {
	dir := t.TempDir()
	files := []struct {
		name string
		hour int
	}{
		{name: "app_2026_08_01.log", hour: 0},
		{name: "app_2026_08_02.log", hour: 1},
		{name: "app_2026_08_03.log", hour: 2},
		{name: "app_2026_08_04_001.log", hour: 3},
	}
	base := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	for _, file := range files {
		path := filepath.Join(dir, file.name)
		if err := os.WriteFile(path, []byte(file.name), 0o600); err != nil {
			t.Fatal(err)
		}
		stamp := base.Add(time.Duration(file.hour) * time.Hour)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}

	unrelated := filepath.Join(dir, "app_notes.log")
	if err := os.WriteFile(unrelated, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	nonRegular := filepath.Join(dir, "app_2026_08_05_002.log")
	if err := os.Mkdir(nonRegular, 0o700); err != nil {
		t.Fatal(err)
	}

	current := filepath.Join(dir, "app_2026_08_01.log")
	if err := cleanupManagedLogs(dir, current, "", 2); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"app_2026_08_01.log",
		"app_2026_08_03.log",
		"app_2026_08_04_001.log",
		"app_notes.log",
		"app_2026_08_05_002.log",
	} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to remain: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "app_2026_08_02.log")); !os.IsNotExist(err) {
		t.Errorf("old managed log still exists, stat error = %v", err)
	}
}
