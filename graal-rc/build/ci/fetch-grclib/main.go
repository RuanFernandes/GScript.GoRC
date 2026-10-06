package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	repository     = "MorenoLand/GScript.GRClib"
	maxLibrarySize = 128 << 20
)

type nativeTarget struct {
	Key            string
	GOOS           string
	GOARCH         string
	Asset          string
	AssetLibraries []string
	Library        string
	Path           string
}

type releaseResponse struct {
	Assets []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type manifest struct {
	Repository string                    `json:"repository"`
	Release    string                    `json:"release"`
	ReleaseURL string                    `json:"releaseUrl"`
	Targets    map[string]manifestTarget `json:"targets"`
}

type manifestTarget struct {
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
	Asset   string `json:"asset"`
	Library string `json:"library"`
	Path    string `json:"path"`
	SHA256  string `json:"sha256"`
}

var targets = []nativeTarget{
	{Key: "windows-amd64", GOOS: "windows", GOARCH: "amd64", Asset: "grclib-windows-x64.zip", AssetLibraries: []string{"grclib.dll", "grclib64.dll"}, Library: "grclib64.dll", Path: "native/windows-amd64/grclib64.dll"},
	{Key: "windows-386", GOOS: "windows", GOARCH: "386", Asset: "grclib-windows-x86.zip", AssetLibraries: []string{"grclib.dll"}, Library: "grclib.dll", Path: "native/windows-386/grclib.dll"},
	{Key: "linux-amd64", GOOS: "linux", GOARCH: "amd64", Asset: "grclib-linux-x64.zip", AssetLibraries: []string{"grclib.so"}, Library: "grclib.so", Path: "native/linux-amd64/grclib.so"},
	{Key: "linux-386", GOOS: "linux", GOARCH: "386", Asset: "grclib-linux-x86.zip", AssetLibraries: []string{"grclib.so"}, Library: "grclib.so", Path: "native/linux-386/grclib.so"},
	{Key: "darwin-amd64", GOOS: "darwin", GOARCH: "amd64", Asset: "grclib-macos-x64.zip", AssetLibraries: []string{"grclib.dylib"}, Library: "grclib.dylib", Path: "native/darwin-amd64/grclib.dylib"},
	{Key: "darwin-arm64", GOOS: "darwin", GOARCH: "arm64", Asset: "grclib-macos-arm64.zip", AssetLibraries: []string{"grclib.dylib"}, Library: "grclib.dylib", Path: "native/darwin-arm64/grclib.dylib"},
}

func main() {
	rootFlag := flag.String("root", ".", "GoRC repository root")
	releaseFlag := flag.String("release", "v1.0.51", "GScript.GRClib release tag")
	checkFlag := flag.Bool("check", false, "validate the checked-in native libraries without network access")
	forceFlag := flag.Bool("force", false, "redownload libraries even when the manifest already matches")
	flag.Parse()

	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		fatal(err)
	}
	if *checkFlag {
		if err := check(root); err != nil {
			fatal(err)
		}
		fmt.Println("grclib native library matrix is valid")
		return
	}

	tag := normalizeTag(*releaseFlag)
	manifestPath := filepath.Join(root, "rclib", "native", "manifest.json")
	if !*forceFlag {
		if existing, err := readManifest(manifestPath); err == nil && existing.Release == tag {
			if err := checkManifest(root, existing); err == nil {
				fmt.Printf("grclib %s is already present and valid\n", tag)
				return
			}
		}
	}

	release, err := fetchRelease(tag)
	if err != nil {
		fatal(err)
	}
	assetURLs := make(map[string]string, len(release.Assets))
	for _, asset := range release.Assets {
		assetURLs[asset.Name] = asset.BrowserDownloadURL
	}

	result := manifest{
		Repository: repository,
		Release:    tag,
		ReleaseURL: fmt.Sprintf("https://github.com/%s/releases/tag/%s", repository, tag),
		Targets:    make(map[string]manifestTarget, len(targets)),
	}
	for _, target := range targets {
		assetURL, ok := assetURLs[target.Asset]
		if !ok || assetURL == "" {
			fatal(fmt.Errorf("release %s does not contain asset %s", tag, target.Asset))
		}
		archiveBytes, err := download(assetURL)
		if err != nil {
			fatal(fmt.Errorf("download %s: %w", target.Asset, err))
		}
		libraryBytes, err := extractLibrary(archiveBytes, target.AssetLibraries...)
		if err != nil {
			fatal(fmt.Errorf("extract %s from %s: %w", target.Library, target.Asset, err))
		}
		destination := filepath.Join(root, "rclib", target.Path)
		if err := writeAtomic(destination, libraryBytes, target.GOOS == "windows"); err != nil {
			fatal(fmt.Errorf("write %s: %w", destination, err))
		}
		hash := sha256.Sum256(libraryBytes)
		result.Targets[target.Key] = manifestTarget{
			GOOS:    target.GOOS,
			GOARCH:  target.GOARCH,
			Asset:   target.Asset,
			Library: target.Library,
			Path:    target.Path,
			SHA256:  hex.EncodeToString(hash[:]),
		}
		fmt.Printf("fetched %s/%s from %s\n", target.GOOS, target.GOARCH, target.Asset)
	}

	if err := writeManifest(manifestPath, result); err != nil {
		fatal(err)
	}
}

func fetchRelease(tag string) (releaseResponse, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", repository, url.PathEscape(tag))
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return releaseResponse{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "graal-rc-grclib-fetcher")
	response, err := (&http.Client{Timeout: 45 * time.Second}).Do(request)
	if err != nil {
		return releaseResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return releaseResponse{}, fmt.Errorf("GitHub release API returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	var release releaseResponse
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return releaseResponse{}, fmt.Errorf("decode GitHub release response: %w", err)
	}
	return release, nil
}

func download(assetURL string) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "graal-rc-grclib-fetcher")
	response, err := (&http.Client{Timeout: 2 * time.Minute}).Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxLibrarySize))
	if err != nil {
		return nil, err
	}
	return data, nil
}

func extractLibrary(archiveBytes []byte, libraryNames ...string) ([]byte, error) {
	archive, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(libraryNames))
	for _, libraryName := range libraryNames {
		wanted[libraryName] = struct{}{}
	}
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if _, ok := wanted[filepath.Base(filepath.ToSlash(entry.Name))]; !ok {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, maxLibrarySize))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(data) == 0 {
			return nil, errors.New("library is empty")
		}
		return data, nil
	}
	return nil, fmt.Errorf("archive does not contain any of %s", strings.Join(libraryNames, ", "))
}

func check(root string) error {
	manifestPath := filepath.Join(root, "rclib", "native", "manifest.json")
	value, err := readManifest(manifestPath)
	if err != nil {
		return err
	}
	return checkManifest(root, value)
}

func checkManifest(root string, value manifest) error {
	if value.Repository != repository {
		return fmt.Errorf("manifest repository is %q, want %q", value.Repository, repository)
	}
	if len(value.Targets) != len(targets) {
		return fmt.Errorf("manifest contains %d targets, want %d", len(value.Targets), len(targets))
	}
	for _, target := range targets {
		entry, ok := value.Targets[target.Key]
		if !ok {
			return fmt.Errorf("manifest is missing target %s", target.Key)
		}
		if entry.GOOS != target.GOOS || entry.GOARCH != target.GOARCH || entry.Library != target.Library || entry.Path != target.Path {
			return fmt.Errorf("manifest target %s does not match the expected runtime mapping", target.Key)
		}
		path := filepath.Join(root, "rclib", entry.Path)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		hash := sha256.Sum256(data)
		if !strings.EqualFold(entry.SHA256, hex.EncodeToString(hash[:])) {
			return fmt.Errorf("sha256 mismatch for %s", path)
		}
	}
	return nil
}

func readManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, err
	}
	var value manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return manifest{}, err
	}
	return value, nil
}

func writeManifest(path string, value manifest) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, true)
}

func writeAtomic(path string, data []byte, windows bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".grclib-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if !windows {
		if err := os.Chmod(temporaryName, 0o755); err != nil {
			return err
		}
	}
	// os.Rename cannot replace an existing file on Windows. The destination is
	// an exact matrix path controlled by this tool, so remove only that file
	// before the final move.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(temporaryName, path)
}

func normalizeTag(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "v1.0.51"
	}
	if !strings.HasPrefix(strings.ToLower(value), "v") {
		return "v" + value
	}
	return value
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
