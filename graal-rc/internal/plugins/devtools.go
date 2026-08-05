package plugins

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"graal-rc/internal/fileutil"
)

const maxPluginFileBytes int64 = 8 << 20

type PluginFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type PluginBuildResult struct {
	Plugin  PluginInfo `json:"plugin"`
	Success bool       `json:"success"`
	Message string     `json:"message"`
}

type PluginLogEntry struct {
	Timestamp int64  `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
}

var pluginLogs = struct {
	sync.Mutex
	values map[string][]PluginLogEntry
}{values: map[string][]PluginLogEntry{}}

func (m *Manager) plugin(id string) (PluginInfo, error) {
	m.mu.RLock()
	p, ok := m.plugins[id]
	m.mu.RUnlock()
	if !ok {
		return PluginInfo{}, ErrPluginNotFound
	}
	return p, nil
}

func (m *Manager) filePath(id, relative string) (string, error) {
	p, err := m.plugin(id)
	if err != nil {
		return "", err
	}
	if relative == "" || filepath.IsAbs(relative) || strings.Contains(relative, "..") {
		return "", errors.New("invalid plugin file path")
	}
	path, err := safePluginPath(p.Directory, relative)
	if err != nil {
		return "", err
	}
	return path, nil
}

func (m *Manager) ReadFile(id, relative string) (PluginFile, error) {
	path, err := m.filePath(id, relative)
	if err != nil {
		return PluginFile{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return PluginFile{}, err
	}
	if int64(len(b)) > maxPluginFileBytes {
		return PluginFile{}, errors.New("plugin file exceeds 8 MiB")
	}
	return PluginFile{Path: filepath.ToSlash(relative), Content: string(b)}, nil
}

func (m *Manager) WriteFile(id, relative, content string) error {
	path, err := m.filePath(id, relative)
	if err != nil {
		return err
	}
	if int64(len(content)) > maxPluginFileBytes {
		return errors.New("plugin file exceeds 8 MiB")
	}
	if filepath.Base(path) == "manifest.json" {
		var manifest Manifest
		if err := json.Unmarshal([]byte(content), &manifest); err != nil {
			return fmt.Errorf("invalid manifest: %w", err)
		}
		if err := ValidateManifest(manifest); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var validator func([]byte) error
	if filepath.Base(path) == "manifest.json" {
		validator = func(payload []byte) error {
			var manifest Manifest
			if err := json.Unmarshal(payload, &manifest); err != nil {
				return err
			}
			return ValidateManifest(manifest)
		}
	}
	return fileutil.AtomicWriteFileWithValidator(path, []byte(content), 0o600, validator)
}

func (m *Manager) ListFiles(id string) ([]string, error) {
	p, err := m.plugin(id)
	if err != nil {
		return nil, err
	}
	files := []string{}
	err = filepath.WalkDir(p.Directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		rel, err := filepath.Rel(p.Directory, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func (m *Manager) Build(id string) (PluginBuildResult, error) {
	p, err := m.plugin(id)
	if err != nil {
		return PluginBuildResult{}, err
	}
	manifestBytes, err := os.ReadFile(filepath.Join(p.Directory, "manifest.json"))
	if err != nil {
		return PluginBuildResult{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return PluginBuildResult{Plugin: p, Message: "invalid manifest: " + err.Error()}, nil
	}
	if err := ValidateManifest(manifest); err != nil {
		return PluginBuildResult{Plugin: p, Message: err.Error()}, nil
	}
	mainPath, err := safePluginPath(p.Directory, manifest.Main)
	if err != nil {
		return PluginBuildResult{Plugin: p, Message: err.Error()}, nil
	}
	if info, statErr := os.Stat(mainPath); statErr != nil || info.IsDir() {
		return PluginBuildResult{Plugin: p, Message: "plugin main bundle does not exist"}, nil
	}
	if err := m.Discover(); err != nil {
		return PluginBuildResult{}, err
	}
	p, _ = m.plugin(id)
	m.Log(id, "info", "Build succeeded and bundle is ready to reload")
	return PluginBuildResult{Plugin: p, Success: true, Message: "Build succeeded"}, nil
}

func (m *Manager) Log(id, level, message string) {
	pluginLogs.Lock()
	defer pluginLogs.Unlock()
	entries := pluginLogs.values[id]
	entries = append(entries, PluginLogEntry{Timestamp: time.Now().UnixMilli(), Level: level, Message: message})
	if len(entries) > 500 {
		entries = entries[len(entries)-500:]
	}
	pluginLogs.values[id] = entries
}

func (m *Manager) Logs(id string) []PluginLogEntry {
	pluginLogs.Lock()
	defer pluginLogs.Unlock()
	return append([]PluginLogEntry(nil), pluginLogs.values[id]...)
}

func (m *Manager) ClearLogs(id string) {
	pluginLogs.Lock()
	defer pluginLogs.Unlock()
	delete(pluginLogs.values, id)
}

func (m *Manager) Export(id, destination string) (string, error) {
	p, err := m.plugin(id)
	if err != nil {
		return "", err
	}
	if destination == "" || filepath.Ext(destination) != ".zip" {
		return "", errors.New("destination must be a .zip file")
	}
	file, err := os.Create(destination)
	if err != nil {
		return "", err
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	defer archive.Close()
	err = filepath.WalkDir(p.Directory, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(p.Directory, path)
		if err != nil {
			return err
		}
		writer, err := archive.Create(filepath.ToSlash(filepath.Join(p.Manifest.ID, rel)))
		if err != nil {
			return err
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		_, err = io.Copy(writer, input)
		return err
	})
	if err != nil {
		return "", err
	}
	m.Log(id, "info", "Plugin exported to "+destination)
	return destination, nil
}
