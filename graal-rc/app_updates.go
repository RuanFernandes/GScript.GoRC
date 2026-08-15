package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	RCVersion          = "3.1.1"
	releaseAPIBaseURL  = "https://nullborne.com"
	updateRequestLimit = 512 * 1024
	maxInstallerBytes  = 512 * 1024 * 1024
)

// UpdateInstaller describes the artifact advertised by the release service.
// The checksum is verified before the executable is handed to the installer
// helper, so a partial or unexpected download is never launched.
type UpdateInstaller struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// UpdateInfo is the small contract shared by the RC and nullborne.com/update.
type UpdateInfo struct {
	Product           string           `json:"product"`
	CurrentVersion    string           `json:"currentVersion"`
	LatestVersion     string           `json:"latestVersion"`
	UpdateAvailable   bool             `json:"updateAvailable"`
	Platform          string           `json:"platform"`
	Architecture      string           `json:"architecture"`
	ReleaseDate       string           `json:"releaseDate"`
	DownloadAvailable bool             `json:"downloadAvailable"`
	DownloadURL       string           `json:"downloadUrl"`
	ChangelogURL      string           `json:"changelogUrl"`
	Installer         *UpdateInstaller `json:"installer"`
}

// RemoteRelease mirrors one release from nullborne.com/changelog. The local
// changelog remains the offline fallback when this request cannot complete.
type RemoteRelease struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	Title   string   `json:"title"`
	Summary string   `json:"summary"`
	Changes []string `json:"changes"`
	Current bool     `json:"current"`
}

// RemoteChangelog is the release history displayed by the RC changelog panel.
type RemoteChangelog struct {
	Product       string          `json:"product"`
	ProductName   string          `json:"productName"`
	LatestVersion string          `json:"latestVersion"`
	UpdatedAt     string          `json:"updatedAt"`
	Releases      []RemoteRelease `json:"releases"`
}

// GetAppVersion exposes the build version to the frontend and keeps version
// reporting in one native source of truth.
func (a *App) GetAppVersion() string {
	return RCVersion
}

// CheckForUpdates asks the release service whether a newer supported build is
// available. Non-Windows builds return a stable no-op until their installers
// are published by the portal.
func (a *App) CheckForUpdates() (UpdateInfo, error) {
	if err := ensureAppRunning(a); err != nil {
		return UpdateInfo{CurrentVersion: RCVersion}, err
	}

	platform, architecture := localReleaseTarget()
	if platform == "" {
		return UpdateInfo{CurrentVersion: RCVersion}, nil
	}

	return fetchUpdateInfo(platform, architecture)
}

// FetchRemoteChangelog refreshes the in-app changelog from the public release
// service. The React component falls back to the bundled changelog on error.
func (a *App) FetchRemoteChangelog() (RemoteChangelog, error) {
	if err := ensureAppRunning(a); err != nil {
		return RemoteChangelog{}, err
	}

	endpoint, err := releaseURL("/changelog")
	if err != nil {
		return RemoteChangelog{}, err
	}

	var changelog RemoteChangelog
	if err := requestJSON(endpoint, &changelog); err != nil {
		return RemoteChangelog{}, fmt.Errorf("fetch changelog: %w", err)
	}
	if changelog.LatestVersion == "" || len(changelog.Releases) == 0 {
		return RemoteChangelog{}, errors.New("fetch changelog: response has no releases")
	}
	return changelog, nil
}

// InstallUpdate performs the requested automatic update flow. It checks the
// release metadata again immediately before downloading, verifies the exact
// size and SHA-256 advertised by the server, starts a hidden helper that waits
// for this process to exit, then closes the RC so NSIS can install over it.
func (a *App) InstallUpdate() error {
	if runtime.GOOS != "windows" {
		return nil
	}
	if err := ensureAppRunning(a); err != nil {
		return err
	}

	platform, architecture := localReleaseTarget()
	info, err := fetchUpdateInfo(platform, architecture)
	if err != nil {
		log.Printf("automatic update check failed: %v", err)
		return err
	}
	if !info.UpdateAvailable {
		return nil
	}
	if !info.DownloadAvailable || info.Installer == nil || info.DownloadURL == "" {
		err := fmt.Errorf("update %s is advertised without an available Windows installer", info.LatestVersion)
		log.Printf("automatic update skipped: %v", err)
		return err
	}

	installerPath, err := downloadInstaller(info)
	if err != nil {
		log.Printf("automatic update download failed: %v", err)
		return err
	}
	if err := launchWindowsInstallerAfterExit(installerPath); err != nil {
		_ = os.Remove(installerPath)
		log.Printf("automatic update launch failed: %v", err)
		return err
	}

	// The helper waits for the process name checked by the NSIS installer. A
	// short delay lets this binding call return before the Wails shutdown starts.
	a.quitting.Store(true)
	go func() {
		time.Sleep(350 * time.Millisecond)
		if a.app != nil {
			a.app.Quit()
		}
	}()
	return nil
}

func localReleaseTarget() (string, string) {
	if runtime.GOOS != "windows" {
		return "", ""
	}
	return "windows", runtime.GOARCH
}

func fetchUpdateInfo(platform, architecture string) (UpdateInfo, error) {
	endpoint, err := releaseURL("/update")
	if err != nil {
		return UpdateInfo{}, err
	}
	query := endpoint.Query()
	query.Set("version", RCVersion)
	query.Set("platform", platform)
	query.Set("arch", architecture)
	endpoint.RawQuery = query.Encode()

	var info UpdateInfo
	if err := requestJSON(endpoint, &info); err != nil {
		return UpdateInfo{}, fmt.Errorf("fetch update metadata: %w", err)
	}
	if info.CurrentVersion == "" || info.LatestVersion == "" {
		return UpdateInfo{}, errors.New("fetch update metadata: response has no version information")
	}
	return info, nil
}

func releaseURL(route string) (*url.URL, error) {
	base, err := url.Parse(releaseAPIBaseURL)
	if err != nil {
		return nil, err
	}
	if base.Scheme != "https" || strings.ToLower(base.Hostname()) != "nullborne.com" {
		return nil, errors.New("release service must use https://nullborne.com")
	}
	base.Path = strings.TrimRight(base.Path, "/") + route
	base.RawQuery = ""
	return base, nil
}

func requestJSON(endpoint *url.URL, target any) error {
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			if request.URL.Scheme != "https" || strings.ToLower(request.URL.Hostname()) != "nullborne.com" {
				return errors.New("release service redirect left https://nullborne.com")
			}
			return nil
		},
	}

	request, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("release service returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, updateRequestLimit))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func downloadInstaller(info UpdateInfo) (string, error) {
	endpoint, err := url.Parse(info.DownloadURL)
	if err != nil {
		return "", err
	}
	if endpoint.Scheme != "https" || strings.ToLower(endpoint.Hostname()) != "nullborne.com" || endpoint.Path != "/windows" {
		return "", errors.New("update download URL is not the trusted Windows release route")
	}

	client := &http.Client{Timeout: 20 * time.Minute}
	request, err := http.NewRequest(http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("installer download returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxInstallerBytes {
		return "", fmt.Errorf("installer is larger than the %d MB safety limit", maxInstallerBytes/(1024*1024))
	}

	temporaryFile, err := os.CreateTemp("", "nullbornes-rc-update-*.exe")
	if err != nil {
		return "", err
	}
	installerPath := temporaryFile.Name()
	removeOnFailure := true
	defer func() {
		if removeOnFailure {
			_ = os.Remove(installerPath)
		}
	}()

	hash := sha256.New()
	limitedBody := io.LimitReader(response.Body, maxInstallerBytes+1)
	written, copyErr := io.Copy(io.MultiWriter(temporaryFile, hash), limitedBody)
	if closeErr := temporaryFile.Close(); copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		return "", copyErr
	}
	if written > maxInstallerBytes {
		return "", fmt.Errorf("installer is larger than the %d MB safety limit", maxInstallerBytes/(1024*1024))
	}
	if info.Installer.Size > 0 && written != info.Installer.Size {
		return "", fmt.Errorf("installer size mismatch: expected %d bytes, received %d", info.Installer.Size, written)
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if expectedHash := strings.ToLower(strings.TrimSpace(info.Installer.SHA256)); expectedHash != "" && actualHash != expectedHash {
		return "", fmt.Errorf("installer checksum mismatch: expected %s, received %s", expectedHash, actualHash)
	}

	removeOnFailure = false
	return installerPath, nil
}

func launchWindowsInstallerAfterExit(installerPath string) error {
	parentPID := strconv.Itoa(os.Getpid())
	quotedInstallerPath := strings.ReplaceAll(installerPath, "'", "''")
	script := fmt.Sprintf(`$parentPid = %s
$installerPath = '%s'
while (Get-Process -Id $parentPid -ErrorAction SilentlyContinue) {
  Start-Sleep -Milliseconds 250
}
try {
  Start-Process -FilePath $installerPath -ArgumentList '/S' -Wait
} finally {
  Remove-Item -LiteralPath $installerPath -Force -ErrorAction SilentlyContinue
  Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue
}
`, parentPID, quotedInstallerPath)

	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("nullbornes-rc-update-%d.ps1", time.Now().UnixNano()))
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}

	command := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-WindowStyle",
		"Hidden",
		"-File",
		scriptPath,
	)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		_ = os.Remove(scriptPath)
		return err
	}
	return nil
}
