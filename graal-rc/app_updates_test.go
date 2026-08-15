package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseURLUsesOnlyTheTrustedPortal(t *testing.T) {
	for _, route := range []string{"/update", "/changelog", "/windows", "/linux", "/mac"} {
		got, err := releaseURL(route)
		if err != nil {
			t.Fatalf("releaseURL(%q): %v", route, err)
		}
		if got.String() != "https://nullborne.com"+route {
			t.Fatalf("releaseURL(%q) = %q", route, got.String())
		}
	}
}

func TestDownloadInstallerRejectsUntrustedRoutesBeforeNetworkAccess(t *testing.T) {
	for _, downloadURL := range []string{
		"http://nullborne.com/windows",
		"https://evil.example/windows",
		"https://nullborne.com/other",
		"file:///C:/installer.exe",
	} {
		t.Run(downloadURL, func(t *testing.T) {
			_, err := downloadInstaller(UpdateInfo{
				DownloadURL: downloadURL,
				Installer:   &UpdateInstaller{Size: 1},
			})
			if err == nil {
				t.Fatalf("downloadInstaller(%q) accepted an untrusted route", downloadURL)
			}
		})
	}
}

func TestAppVersionIsReleaseVersion(t *testing.T) {
	if (&App{}).GetAppVersion() != "3.1.3" {
		t.Fatalf("GetAppVersion() = %q, want 3.1.3", (&App{}).GetAppVersion())
	}
}

func TestReleaseTargetMapsSupportedOperatingSystems(t *testing.T) {
	tests := []struct {
		goos         string
		wantPlatform string
	}{
		{goos: "windows", wantPlatform: "windows"},
		{goos: "linux", wantPlatform: "linux"},
		{goos: "darwin", wantPlatform: "mac"},
		{goos: "freebsd", wantPlatform: ""},
	}
	for _, test := range tests {
		platform, architecture := releaseTarget(test.goos, "amd64")
		if platform != test.wantPlatform {
			t.Fatalf("releaseTarget(%q) platform = %q, want %q", test.goos, platform, test.wantPlatform)
		}
		if test.wantPlatform == "" && architecture != "" {
			t.Fatalf("releaseTarget(%q) architecture = %q, want empty", test.goos, architecture)
		}
		if test.wantPlatform != "" && architecture != "amd64" {
			t.Fatalf("releaseTarget(%q) architecture = %q, want amd64", test.goos, architecture)
		}
	}
}

func TestUpdateDownloadRoutesAndTemporaryNames(t *testing.T) {
	tests := []struct {
		platform string
		path     string
		pattern  string
	}{
		{platform: "windows", path: "/windows", pattern: "nullbornes-rc-update-*.exe"},
		{platform: "linux", path: "/linux", pattern: "nullbornes-rc-update-*.AppImage"},
		{platform: "mac", path: "/mac", pattern: "nullbornes-rc-update-*.dmg"},
	}
	for _, test := range tests {
		if got := updateDownloadPath(test.platform); got != test.path {
			t.Fatalf("updateDownloadPath(%q) = %q, want %q", test.platform, got, test.path)
		}
		if got := updateTemporaryPattern(test.platform); got != test.pattern {
			t.Fatalf("updateTemporaryPattern(%q) = %q, want %q", test.platform, got, test.pattern)
		}
	}
	if got := updateDownloadPath("other"); got != "" {
		t.Fatalf("updateDownloadPath(other) = %q, want empty", got)
	}
}

func TestUpdateFilenameUsesSafeArtifactBasename(t *testing.T) {
	info := UpdateInfo{Installer: &UpdateInstaller{Name: "nested/path/nullbornes-rc-linux.AppImage"}}
	if got := updateFilename(info, "linux"); got != "nullbornes-rc-linux.AppImage" {
		t.Fatalf("updateFilename returned %q, want basename", got)
	}
	if got := updateFilename(UpdateInfo{}, "mac"); got != "nullbornes-rc-macos.dmg" {
		t.Fatalf("updateFilename fallback = %q, want macOS filename", got)
	}
}

func TestSaveDownloadedInstallerCopiesVerifiedArtifact(t *testing.T) {
	sourcePath := filepath.Join(t.TempDir(), "download.AppImage")
	destinationPath := filepath.Join(t.TempDir(), "saved.AppImage")
	content := []byte("verified release payload")
	if err := os.WriteFile(sourcePath, content, 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := saveDownloadedInstaller(sourcePath, destinationPath, "linux"); err != nil {
		t.Fatalf("saveDownloadedInstaller: %v", err)
	}
	got, err := os.ReadFile(destinationPath)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("saved content = %q, want %q", got, content)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(destinationPath), ".nullbornes-rc-update-*"))
	if err != nil {
		t.Fatalf("find temporary destination files: %v", err)
	}
	if len(leftovers) != 0 {
		t.Fatalf("temporary destination files remain: %v", leftovers)
	}
}

func TestNSISProcessProbeUsesCompatibleNsExecInvocation(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("build", "windows", "nsis", "wails_tools.nsh"))
	if err != nil {
		t.Fatalf("read NSIS tools file: %v", err)
	}

	const invocation = "nsExec::ExecToStack '\"$SYSDIR\\tasklist.exe\" /FI \"IMAGENAME eq ${PRODUCT_EXECUTABLE}\" /FO CSV /NH'"
	sourceText := string(source)
	if !strings.Contains(sourceText, invocation) {
		t.Fatalf("NSIS process probe does not use the compatible tasklist invocation")
	}
	invocationLine := ""
	for _, line := range strings.Split(sourceText, "\n") {
		if strings.Contains(line, "nsExec::ExecToStack") {
			invocationLine = line
			break
		}
	}
	if strings.Contains(invocationLine, "/OEM") || strings.Contains(invocationLine, "/TIMEOUT") {
		t.Fatalf("NSIS process probe contains nsExec options incompatible with the Unicode plug-in")
	}
}

func TestWindowsUpdateScriptRelaunchesTheApplication(t *testing.T) {
	script := buildWindowsUpdateScript(
		"42",
		"C:\\Users\\RuanF\\AppData\\Local\\Temp\\installer.exe",
		"C:\\Program Files\\Ruan's\\Graal Remote Control\\graal-rc.exe",
		"C:\\Users\\RuanF\\AppData\\Local\\Temp\\nullbornes-rc-update.log",
	)

	for _, fragment := range []string{
		"$waitDeadline = (Get-Date).AddSeconds(30)",
		"throw \"Timed out waiting for Nullborne RC (PID $parentPid) to exit.\"",
		"$parentExited = $true",
		"$installer = Start-Process -FilePath $installerPath -ArgumentList @('/S') -Wait -PassThru -WindowStyle Hidden",
		"if (-not $parentExited)",
		"Start-Process -FilePath $applicationPath -WorkingDirectory $workingDirectory -WindowStyle Normal",
		"Set-Content -LiteralPath $logPath",
		"Ruan''s",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("update helper script is missing %q", fragment)
		}
	}
}
