package main

import "testing"

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
	if (&App{}).GetAppVersion() != "3.1.0" {
		t.Fatalf("GetAppVersion() = %q, want 3.1.0", (&App{}).GetAppVersion())
	}
}
