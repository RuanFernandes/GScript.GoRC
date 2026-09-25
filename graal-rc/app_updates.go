package main

import (
	"crypto/sha256"
	_ "embed"
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
	releaseAPIBaseURL  = "https://nullborne.com"
	updateRequestLimit = 512 * 1024
	maxInstallerBytes  = 512 * 1024 * 1024
	updateParentWait   = 30 * time.Second
	updateQuitWait     = 2 * time.Second
)

//go:embed VERSION
var embeddedRCVersion string

// RCVersion is a variable so local release builds can override the client
// version through Go's -ldflags -X option without changing production source
// metadata. Normal builds use the checked-in VERSION file.
var RCVersion = strings.TrimSpace(embeddedRCVersion)

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
// available. Windows and Debian-installed Linux builds use the automatic
// installer flow; AppImage and macOS builds save the verified artifact for
// manual installation.
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
// size and SHA-256 advertised by the server, then either starts the Windows
// helper or installs the Debian package through the user's polkit prompt.
func (a *App) InstallUpdate() error {
	if err := ensureAppRunning(a); err != nil {
		return err
	}

	platform, architecture := localReleaseTarget()
	if runtime.GOOS != "windows" && !(runtime.GOOS == "linux" && platform == "ubuntu") {
		return errors.New("automatic updates are only supported for Windows and Debian packages on Linux; use SaveUpdate for AppImage or macOS")
	}
	info, err := fetchUpdateInfo(platform, architecture)
	if err != nil {
		log.Printf("automatic update check failed: %v", err)
		return err
	}
	if !info.UpdateAvailable {
		return nil
	}
	if !info.DownloadAvailable || info.Installer == nil || info.DownloadURL == "" {
		err := fmt.Errorf("update %s is advertised without an available %s installer", info.LatestVersion, platform)
		log.Printf("automatic update skipped: %v", err)
		return err
	}

	installerPath, err := downloadInstaller(info)
	if err != nil {
		log.Printf("automatic update download failed: %v", err)
		return err
	}
	if platform == "ubuntu" {
		if err := a.installDebianUpdate(installerPath); err != nil {
			_ = os.Remove(installerPath)
			log.Printf("automatic Debian update failed: %v", err)
			return err
		}
		return nil
	}
	if err := launchWindowsInstallerAfterExit(installerPath); err != nil {
		_ = os.Remove(installerPath)
		log.Printf("automatic update launch failed: %v", err)
		return err
	}

	a.scheduleUpdateExit()
	return nil
}

func (a *App) scheduleUpdateExit() {
	// The installer waits for the current process to exit. A short delay lets
	// the binding call return before the Wails shutdown starts.
	a.quitting.Store(true)
	go func() {
		time.Sleep(350 * time.Millisecond)
		quitDone := make(chan struct{})
		go func() {
			if a.app != nil {
				a.app.Quit()
			}
			close(quitDone)
		}()
		select {
		case <-quitDone:
			// Give Wails a brief chance to release the WebView and file handles.
			time.Sleep(500 * time.Millisecond)
		case <-time.After(updateQuitWait):
			log.Printf("automatic update: Wails quit did not complete within %s; forcing process exit", updateQuitWait)
		}
		// The installer cannot replace a running executable. This is the final
		// fallback when the native Wails shutdown does not terminate the process.
		os.Exit(0)
	}()
}

func localReleaseTarget() (string, string) {
	if runtime.GOOS == "linux" {
		executablePath, _ := os.Executable()
		return linuxReleaseTarget(runtime.GOARCH, os.Getenv("APPIMAGE"), executablePath, isDpkgManagedExecutable(executablePath))
	}
	return releaseTarget(runtime.GOOS, runtime.GOARCH)
}

func linuxReleaseTarget(architecture, appImagePath, executablePath string, dpkgManaged bool) (string, string) {
	platform := linuxReleasePlatform(appImagePath, executablePath, dpkgManaged)
	if platform == "ubuntu" && architecture != "amd64" {
		platform = "linux"
	}
	return platform, architecture
}

func linuxReleasePlatform(appImagePath, executablePath string, dpkgManaged bool) string {
	if strings.TrimSpace(appImagePath) != "" || strings.HasSuffix(strings.ToLower(strings.TrimSpace(executablePath)), ".appimage") {
		return "linux"
	}
	if dpkgManaged {
		return "ubuntu"
	}
	return "linux"
}

func isDpkgManagedExecutable(executablePath string) bool {
	if strings.TrimSpace(executablePath) == "" {
		return false
	}
	dpkgQueryPath, err := exec.LookPath("dpkg-query")
	if err != nil {
		return false
	}

	candidates := make([]string, 0, 2)
	addCandidate := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if absolutePath, err := filepath.Abs(path); err == nil {
			path = absolutePath
		}
		for _, candidate := range candidates {
			if candidate == path {
				return
			}
		}
		candidates = append(candidates, path)
	}

	addCandidate(executablePath)
	if resolvedPath, err := filepath.EvalSymlinks(executablePath); err == nil {
		addCandidate(resolvedPath)
	}
	for _, candidate := range candidates {
		query := exec.Command(dpkgQueryPath, "--search", candidate)
		query.Stdout = io.Discard
		query.Stderr = io.Discard
		if err := query.Run(); err == nil {
			return true
		}
	}
	return false
}

func releaseTarget(goos, architecture string) (string, string) {
	switch goos {
	case "windows":
		return "windows", architecture
	case "linux":
		return "linux", architecture
	case "darwin":
		return "mac", architecture
	default:
		return "", ""
	}
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
	if info.Installer == nil {
		return "", errors.New("update metadata does not include an installer")
	}
	expectedPaths := updateDownloadPaths(info.Platform, info.Architecture)
	if len(expectedPaths) == 0 {
		return "", fmt.Errorf("unsupported update platform %q", info.Platform)
	}
	endpoint, err := url.Parse(info.DownloadURL)
	if err != nil {
		return "", err
	}
	if endpoint.Scheme != "https" || strings.ToLower(endpoint.Hostname()) != "nullborne.com" || !containsString(expectedPaths, endpoint.Path) || endpoint.RawQuery != "" || endpoint.Fragment != "" || endpoint.User != nil {
		return "", fmt.Errorf("update download URL is not the trusted %s release route", info.Platform)
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

	temporaryFile, err := os.CreateTemp("", updateTemporaryPattern(info.Platform))
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

func updateDownloadPath(platform, architecture string) string {
	platform = strings.ToLower(strings.TrimSpace(platform))
	architecture = strings.ToLower(strings.TrimSpace(architecture))

	switch platform {
	case "windows", "linux":
		if architecture != "amd64" && architecture != "386" {
			return ""
		}
	case "ubuntu":
		if architecture != "amd64" {
			return ""
		}
	case "mac":
		if architecture != "amd64" && architecture != "arm64" {
			return ""
		}
	default:
		return ""
	}

	return "/" + platform + "/" + architecture
}

func updateDownloadPaths(platform, architecture string) []string {
	canonicalPath := updateDownloadPath(platform, architecture)
	if canonicalPath == "" {
		return nil
	}

	paths := []string{canonicalPath}
	if strings.EqualFold(strings.TrimSpace(platform), "windows") && strings.EqualFold(strings.TrimSpace(architecture), "amd64") {
		// Builds before the architecture matrix only knew the legacy x64 alias.
		paths = append(paths, "/windows")
	}
	return paths
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func updateTemporaryPattern(platform string) string {
	switch platform {
	case "windows":
		return "nullbornes-rc-update-*.exe"
	case "linux":
		return "nullbornes-rc-update-*.AppImage"
	case "ubuntu":
		return "nullbornes-rc-update-*.deb"
	case "mac":
		return "nullbornes-rc-update-*.tar.gz"
	default:
		return "nullbornes-rc-update-*"
	}
}

func updateFilename(info UpdateInfo, platform string) string {
	if info.Installer != nil {
		name := filepath.Base(strings.TrimSpace(info.Installer.Name))
		if name != "." && name != string(filepath.Separator) && name != "" {
			return name
		}
	}
	switch platform {
	case "linux":
		return "nullbornes-rc-linux.AppImage"
	case "ubuntu":
		return "nullbornes-rc-linux.deb"
	case "mac":
		return "nullbornes-rc-macos-x64.tar.gz"
	default:
		return "nullbornes-rc-update"
	}
}

// saveDownloadedInstaller writes through a temporary file in the destination
// directory and renames it into place. A cancelled or interrupted copy can
// therefore never leave a partial release artifact at the user-selected path.
func saveDownloadedInstaller(sourcePath, destinationPath, platform string) error {
	destinationPath = filepath.Clean(strings.TrimSpace(destinationPath))
	if destinationPath == "" || destinationPath == "." {
		return errors.New("update destination is empty")
	}
	if existing, err := os.Stat(destinationPath); err == nil && existing.IsDir() {
		return errors.New("update destination is a directory")
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect update destination: %w", err)
	}

	temporaryFile, err := os.CreateTemp(filepath.Dir(destinationPath), ".nullbornes-rc-update-*")
	if err != nil {
		return fmt.Errorf("create update destination: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	source, err := os.Open(sourcePath)
	if err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("open downloaded update: %w", err)
	}
	_, copyErr := io.Copy(temporaryFile, source)
	closeSourceErr := source.Close()
	if copyErr != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("save downloaded update: %w", copyErr)
	}
	if closeSourceErr != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("close downloaded update: %w", closeSourceErr)
	}
	if err := temporaryFile.Sync(); err != nil {
		_ = temporaryFile.Close()
		return fmt.Errorf("flush downloaded update: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("close saved update: %w", err)
	}

	mode := os.FileMode(0o644)
	if platform == "linux" {
		mode = 0o755
	}
	if err := os.Chmod(temporaryPath, mode); err != nil {
		return fmt.Errorf("set update permissions: %w", err)
	}
	if err := os.Rename(temporaryPath, destinationPath); err != nil {
		return fmt.Errorf("finalize saved update: %w", err)
	}
	removeTemporary = false
	return nil
}

// SaveUpdate downloads and verifies a macOS/Linux artifact, then asks the user
// where to save it. It remains available as a manual fallback for Debian
// package installations when the automatic polkit flow cannot run.
func (a *App) SaveUpdate() (string, error) {
	if runtime.GOOS == "windows" {
		return "", errors.New("Windows updates use InstallUpdate")
	}
	if err := ensureAppRunning(a); err != nil {
		return "", err
	}

	platform, architecture := localReleaseTarget()
	if platform == "" {
		return "", errors.New("manual updates are not supported on this platform")
	}
	info, err := fetchUpdateInfo(platform, architecture)
	if err != nil {
		log.Printf("manual update check failed: %v", err)
		return "", err
	}
	if !info.UpdateAvailable {
		return "", nil
	}
	if !info.DownloadAvailable || info.Installer == nil || info.DownloadURL == "" {
		return "", fmt.Errorf("update %s is advertised without an available %s installer", info.LatestVersion, platform)
	}

	filename := updateFilename(info, platform)
	dialog := a.app.Dialog.SaveFile().
		AttachToWindow(a.dialogParentWindow()).
		SetMessage("Save Graal Remote Control update").
		SetFilename(filename)
	switch platform {
	case "linux":
		dialog.AddFilter("Linux AppImage", "*.AppImage")
	case "ubuntu":
		dialog.AddFilter("Debian package", "*.deb")
	case "mac":
		dialog.AddFilter("macOS app archive", "*.tar.gz")
	}
	chosenPath, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(chosenPath) == "" {
		return "", nil
	}

	temporaryPath, err := downloadInstaller(info)
	if err != nil {
		log.Printf("manual update download failed: %v", err)
		return "", err
	}
	defer os.Remove(temporaryPath)
	if err := saveDownloadedInstaller(temporaryPath, chosenPath, platform); err != nil {
		log.Printf("manual update save failed: %v", err)
		return "", err
	}
	return chosenPath, nil
}

func debianPackageInstallArgs(installerPath string) []string {
	return []string{"pkexec", "apt-get", "install", "--yes", installerPath}
}

func debianPackageInstallCommand(installerPath string) (*exec.Cmd, error) {
	if strings.TrimSpace(installerPath) == "" {
		return nil, errors.New("Debian installer path is empty")
	}
	args := debianPackageInstallArgs(installerPath)
	if _, err := exec.LookPath(args[0]); err != nil {
		return nil, errors.New("pkexec is required for automatic Debian updates")
	}
	if _, err := exec.LookPath(args[1]); err != nil {
		return nil, errors.New("apt-get is required for automatic Debian updates")
	}
	return exec.Command(args[0], args[1:]...), nil
}

func (a *App) installDebianUpdate(installerPath string) error {
	defer os.Remove(installerPath)

	command, err := debianPackageInstallCommand(installerPath)
	if err != nil {
		return err
	}
	output, err := command.CombinedOutput()
	if err != nil {
		if details := strings.TrimSpace(string(output)); details != "" {
			return fmt.Errorf("install Debian package: %w: %s", err, details)
		}
		return fmt.Errorf("install Debian package: %w", err)
	}

	applicationPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve application path after Debian update: %w", err)
	}
	application := exec.Command(applicationPath, os.Args[1:]...)
	application.Dir = filepath.Dir(applicationPath)
	if err := application.Start(); err != nil {
		return fmt.Errorf("relaunch application after Debian update: %w", err)
	}

	a.scheduleUpdateExit()
	return nil
}

func launchWindowsInstallerAfterExit(installerPath string) error {
	parentPID := strconv.Itoa(os.Getpid())
	applicationPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve application path for update: %w", err)
	}
	logPath := filepath.Join(os.TempDir(), "nullbornes-rc-update.log")
	script := buildWindowsUpdateScript(parentPID, installerPath, applicationPath, logPath)

	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("nullbornes-rc-update-%d.ps1", time.Now().UnixNano()))
	if err := os.WriteFile(scriptPath, []byte(script), 0600); err != nil {
		return err
	}

	if err := launchDetachedUpdateHelper(scriptPath); err != nil {
		_ = os.Remove(scriptPath)
		return err
	}
	return nil
}

func buildWindowsUpdateScript(parentPID, installerPath, applicationPath, logPath string) string {
	quotePowerShellLiteral := func(value string) string {
		return "'" + strings.ReplaceAll(value, "'", "''") + "'"
	}

	lines := []string{
		fmt.Sprintf("$parentPid = %s", quotePowerShellLiteral(parentPID)),
		fmt.Sprintf("$installerPath = %s", quotePowerShellLiteral(installerPath)),
		fmt.Sprintf("$applicationPath = %s", quotePowerShellLiteral(applicationPath)),
		fmt.Sprintf("$workingDirectory = %s", quotePowerShellLiteral(filepath.Dir(applicationPath))),
		fmt.Sprintf("$logPath = %s", quotePowerShellLiteral(logPath)),
		"$parentExited = $false",
		"$updateSucceeded = $false",
		"function Write-UpdateLog([string]$message) {",
		"  try { Add-Content -LiteralPath $logPath -Value ((Get-Date -Format o) + ' ' + $message) -Encoding UTF8 } catch { }",
		"}",
		"Write-UpdateLog 'Updater helper started.'",
		"try {",
		fmt.Sprintf("  $waitDeadline = (Get-Date).AddSeconds(%d)", int(updateParentWait.Seconds())),
		"  while (Get-Process -Id ([int]$parentPid) -ErrorAction SilentlyContinue) {",
		"    if ((Get-Date) -ge $waitDeadline) {",
		"      throw \"Timed out waiting for Nullborne RC (PID $parentPid) to exit.\"",
		"    }",
		"    Start-Sleep -Milliseconds 250",
		"  }",
		"  $parentExited = $true",
		"  Write-UpdateLog 'RC process exited.'",
		"  if (-not (Test-Path -LiteralPath $installerPath -PathType Leaf)) {",
		"    throw \"The downloaded installer was not found.\"",
		"  }",
		"  Write-UpdateLog ('Starting installer: ' + $installerPath)",
		// Release installers use the legacy machine-wide Program Files path so
		// upgrades replace old installations instead of creating a second copy.
		// RunAs is required because that path is protected by Windows/UAC.
		"  $installer = Start-Process -FilePath $installerPath -ArgumentList @('/S') -Verb RunAs -Wait -PassThru -WindowStyle Hidden",
		"  Write-UpdateLog ('Installer exited with code ' + $installer.ExitCode)",
		"  if ($installer.ExitCode -ne 0) {",
		"    throw \"The installer exited with code $($installer.ExitCode).\"",
		"  }",
		"  $updateSucceeded = $true",
		"} catch {",
		"  Write-UpdateLog (\"Update failed: \" + $_.Exception.Message)",
		"}",
		"if (-not $parentExited) {",
		"  Write-UpdateLog 'Updater stopped because the RC process did not exit.'",
		"  exit 1",
		"}",
		"if (-not $updateSucceeded) {",
		"  Write-UpdateLog 'Installer did not complete successfully; keeping the downloaded files for diagnosis.'",
		"  exit 1",
		"}",
		"try {",
		"  if (Test-Path -LiteralPath $applicationPath -PathType Leaf) {",
		"    $relaunch = Start-Process -FilePath $applicationPath -WorkingDirectory $workingDirectory -WindowStyle Normal -PassThru",
		"    Write-UpdateLog ('RC relaunch requested with PID ' + $relaunch.Id)",
		"  } else {",
		"    throw \"The installed RC executable was not found: $applicationPath\"",
		"  }",
		"} catch {",
		"  Write-UpdateLog (\"Could not relaunch Nullborne RC: \" + $_.Exception.Message)",
		"  exit 1",
		"}",
		"Remove-Item -LiteralPath $installerPath -Force -ErrorAction SilentlyContinue",
		"Remove-Item -LiteralPath $PSCommandPath -Force -ErrorAction SilentlyContinue",
		"Remove-Item -LiteralPath $logPath -Force -ErrorAction SilentlyContinue",
	}
	return strings.Join(lines, "\r\n") + "\r\n"
}
