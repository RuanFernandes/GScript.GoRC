package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseURLUsesOnlyTheTrustedPortal(t *testing.T) {
	for _, route := range []string{"/update", "/changelog", "/windows"} {
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
	if (&App{}).GetAppVersion() != "3.1.1" {
		t.Fatalf("GetAppVersion() = %q, want 3.1.1", (&App{}).GetAppVersion())
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
