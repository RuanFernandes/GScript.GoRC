package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

const maxPluginRemoteTextBytes = 8 << 20

type pluginRemoteFile struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
	Revision  string `json:"revision"`
	Content   string `json:"content"`
}

type pluginFileWriteRequest struct {
	Path             string `json:"path"`
	Content          string `json:"content"`
	ExpectedRevision string `json:"expectedRevision,omitempty"`
}

type pluginFileOpenRequest struct {
	RequestID string
	PluginID  string
	EditorID  string
	Result    chan bool
}

type pluginFileOpenEvent struct {
	RequestID string                 `json:"requestId"`
	PluginID  string                 `json:"pluginId"`
	EditorID  string                 `json:"editorId"`
	File      pluginRemoteFileHeader `json:"file"`
}

type pluginRemoteFileHeader struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Extension string `json:"extension"`
}

type pluginFileEvent struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
	Revision  string `json:"revision,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

func normalizePluginRemotePath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(value, "/")
	if value == "" || strings.Contains(value, "\x00") {
		return "", errors.New("remote file path is required")
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", errors.New("remote file path cannot contain parent traversal")
		}
	}
	cleaned := filepath.ToSlash(filepath.Clean(value))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", errors.New("invalid remote file path")
	}
	return cleaned, nil
}

func pluginRemoteFileMetadata(path string, content []byte, revision string) pluginRemoteFile {
	name := filepath.Base(path)
	extension := strings.ToLower(filepath.Ext(name))
	return pluginRemoteFile{
		Path: path, Name: name, Extension: extension, Size: int64(len(content)), Revision: revision, Content: string(content),
	}
}

func pluginFileRevision(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

func (a *App) pluginReadRemoteText(id, remotePath string) (pluginRemoteFile, error) {
	if a.plugins == nil {
		return pluginRemoteFile{}, errors.New("plugin manager is unavailable")
	}
	path, err := normalizePluginRemotePath(remotePath)
	if err != nil {
		return pluginRemoteFile{}, err
	}
	if err := a.plugins.RequireFileAccess(id, path, false); err != nil {
		return pluginRemoteFile{}, err
	}
	content, err := a.sessions.DownloadFile(path)
	if err != nil {
		return pluginRemoteFile{}, err
	}
	if len(content) > maxPluginRemoteTextBytes {
		return pluginRemoteFile{}, errors.New("remote plugin file exceeds 8 MiB")
	}
	if !utf8.Valid(content) {
		return pluginRemoteFile{}, errors.New("remote file is not valid UTF-8 text")
	}
	revision := pluginFileRevision(content)
	result := pluginRemoteFileMetadata(path, content, revision)
	a.emitPluginEvent("filebrowser.file.read", pluginFileEvent{Path: path, Name: result.Name, Extension: result.Extension, Size: result.Size, Revision: revision})
	return result, nil
}

func (a *App) pluginWriteRemoteText(id string, request pluginFileWriteRequest) (pluginRemoteFile, error) {
	if a.plugins == nil {
		return pluginRemoteFile{}, errors.New("plugin manager is unavailable")
	}
	path, err := normalizePluginRemotePath(request.Path)
	if err != nil {
		return pluginRemoteFile{}, err
	}
	if err := a.plugins.RequireFileAccess(id, path, true); err != nil {
		return pluginRemoteFile{}, err
	}
	content := []byte(request.Content)
	if len(content) > maxPluginRemoteTextBytes {
		return pluginRemoteFile{}, errors.New("remote plugin file exceeds 8 MiB")
	}
	if !utf8.Valid(content) {
		return pluginRemoteFile{}, errors.New("remote file content is not valid UTF-8")
	}
	if request.ExpectedRevision != "" {
		current, downloadErr := a.sessions.DownloadFile(path)
		if downloadErr != nil {
			return pluginRemoteFile{}, downloadErr
		}
		if currentRevision := pluginFileRevision(current); currentRevision != request.ExpectedRevision {
			return pluginRemoteFile{}, fmt.Errorf("remote file changed since it was read (expected %s, current %s)", request.ExpectedRevision, currentRevision)
		}
	}
	if err := a.sessions.UploadFile(path, content); err != nil {
		return pluginRemoteFile{}, err
	}
	revision := pluginFileRevision(content)
	result := pluginRemoteFileMetadata(path, content, revision)
	a.emitPluginEvent("filebrowser.file.saved", pluginFileEvent{Path: path, Name: result.Name, Extension: result.Extension, Size: result.Size, Revision: revision})
	return result, nil
}

func (a *App) requestPluginFileOpen(remotePath string) (bool, error) {
	if a.plugins == nil || a.app == nil {
		return false, nil
	}
	editor, ok := a.plugins.MatchFileEditor(remotePath)
	if !ok {
		return false, nil
	}
	path, err := normalizePluginRemotePath(remotePath)
	if err != nil {
		return false, err
	}
	requestID := fmt.Sprintf("file-open-%d", atomic.AddUint64(&a.pluginFileOpenSeq, 1))
	waiter := pluginFileOpenRequest{RequestID: requestID, PluginID: editor.PluginID, EditorID: editor.ID, Result: make(chan bool, 1)}
	a.pluginFileOpenMu.Lock()
	if a.pluginFileOpenWaiters == nil {
		a.pluginFileOpenWaiters = map[string]pluginFileOpenRequest{}
	}
	a.pluginFileOpenWaiters[requestID] = waiter
	a.pluginFileOpenMu.Unlock()
	payload := pluginFileOpenEvent{
		RequestID: requestID,
		PluginID:  editor.PluginID,
		EditorID:  editor.ID,
		File:      pluginRemoteFileHeader{Path: path, Name: filepath.Base(path), Extension: strings.ToLower(filepath.Ext(path))},
	}
	a.emitPluginEvent("filebrowser.file.opening", pluginFileEvent{Path: path, Name: filepath.Base(path), Extension: strings.ToLower(filepath.Ext(path))})
	b, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		a.removePluginFileOpenWaiter(requestID)
		return false, marshalErr
	}
	a.app.Event.Emit("plugin:file-opening", string(b))
	timer := time.NewTimer(1800 * time.Millisecond)
	defer timer.Stop()
	select {
	case handled := <-waiter.Result:
		return handled, nil
	case <-timer.C:
		a.removePluginFileOpenWaiter(requestID)
		return false, nil
	}
}

func (a *App) removePluginFileOpenWaiter(requestID string) (pluginFileOpenRequest, bool) {
	a.pluginFileOpenMu.Lock()
	defer a.pluginFileOpenMu.Unlock()
	waiter, ok := a.pluginFileOpenWaiters[requestID]
	if ok {
		delete(a.pluginFileOpenWaiters, requestID)
	}
	return waiter, ok
}

func (a *App) pluginFileOpenResult(id, requestID string, handled bool) error {
	waiter, ok := a.removePluginFileOpenWaiter(requestID)
	if !ok || waiter.PluginID != id {
		return errors.New("file open request is no longer active")
	}
	waiter.Result <- handled
	return nil
}

// PluginFileOpenResult is public for the generated Wails surface and is also
// routed through PluginCall by the sandbox runtime.
func (a *App) PluginFileOpenResult(id, requestID string, handled bool) error {
	return a.pluginFileOpenResult(id, requestID, handled)
}
