package main

import (
	"net/http"
	"strings"
	"testing"
)

// Without both values nothing proves the download is the published installer.
func TestInstallerMetadataMustAllowVerification(t *testing.T) {
	good := strings.Repeat("a1", 32)
	for name, installer := range map[string]*UpdateInstaller{
		"missing":    nil,
		"no hash":    {Size: 10},
		"short hash": {Size: 10, SHA256: "abc"},
		"not hex":    {Size: 10, SHA256: strings.Repeat("zz", 32)},
		"no size":    {SHA256: good},
	} {
		if _, err := installerChecksum(installer); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if sum, err := installerChecksum(&UpdateInstaller{Size: 10, SHA256: " " + strings.ToUpper(good) + " "}); err != nil || sum != good {
		t.Errorf("valid metadata: %q %v", sum, err)
	}
}

func TestUpdateRedirectsMayNotLeaveThePortal(t *testing.T) {
	for target, allowed := range map[string]bool{
		"https://nullborne.com/windows": true,
		"http://nullborne.com/windows":  false,
		"https://evil.example/windows":  false,
	} {
		request, _ := http.NewRequest(http.MethodGet, target, nil)
		if err := requirePortalRedirect(request, nil); (err == nil) != allowed {
			t.Errorf("%s: err = %v", target, err)
		}
	}
}

// The helper runs from a script file any process of this user can edit, so it
// must not be elevated; that would also relaunch the RC as administrator.
func TestUpdateHelperIsNotElevated(t *testing.T) {
	if updateHelperVerb != "open" {
		t.Fatalf("helper verb = %q", updateHelperVerb)
	}
}

func TestUpdateScriptVerifiesTheInstallerWhileLocked(t *testing.T) {
	sum := strings.Repeat("a1", 32)
	script := buildWindowsUpdateScript("42", `C:\t\installer.exe`, `C:\p\graal-rc.exe`, `C:\t\u.log`, sum)
	lock := strings.Index(script, "[System.IO.File]::Open($installerPath, 'Open', 'Read', 'Read')")
	check := strings.Index(script, "$actualHash -ne $expectedHash")
	run := strings.Index(script, "Start-Process -FilePath $installerPath")
	release := strings.Index(script, "$installerLock.Dispose()")
	if lock < 0 || check < lock || run < check || release < run {
		t.Fatalf("want lock < check < run < release, got %d %d %d %d", lock, check, run, release)
	}
	if !strings.Contains(script, "$expectedHash = '"+sum+"'") {
		t.Fatal("the expected hash is not in the script")
	}
}
