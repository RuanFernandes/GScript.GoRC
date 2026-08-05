package plugins

import (
	"errors"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var fileScopeMeta = regexp.MustCompile(`[.+^${}()|[\]\\]`)

func validateFileScope(scope string) error {
	scope = strings.TrimSpace(strings.ReplaceAll(scope, "\\", "/"))
	if scope == "" || strings.HasPrefix(scope, "/") || strings.Contains(scope, "\x00") {
		return errors.New("file permission scope must be a relative path pattern")
	}
	for _, segment := range strings.Split(scope, "/") {
		if segment == ".." {
			return errors.New("file permission scope cannot contain parent traversal")
		}
	}
	return nil
}

func normalizeRemotePath(value string) (string, error) {
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
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", errors.New("invalid remote file path")
	}
	return cleaned, nil
}

func fileScopeMatches(scope, remotePath string) bool {
	if validateFileScope(scope) != nil {
		return false
	}
	scope = strings.Trim(strings.ReplaceAll(scope, "\\", "/"), "/")
	remotePath = strings.Trim(strings.ReplaceAll(remotePath, "\\", "/"), "/")
	var pattern strings.Builder
	pattern.WriteString("^")
	for i := 0; i < len(scope); {
		if strings.HasPrefix(scope[i:], "**/") {
			pattern.WriteString("(?:.*/)?")
			i += 3
			continue
		}
		if strings.HasPrefix(scope[i:], "**") {
			pattern.WriteString(".*")
			i += 2
			continue
		}
		if scope[i] == '*' {
			pattern.WriteString("[^/]*")
			i++
			continue
		}
		if scope[i] == '?' {
			pattern.WriteString("[^/]")
			i++
			continue
		}
		character := scope[i : i+1]
		if fileScopeMeta.MatchString(character) {
			pattern.WriteByte('\\')
		}
		pattern.WriteString(character)
		i++
	}
	pattern.WriteString("$")
	compiled, err := regexp.Compile(pattern.String())
	return err == nil && compiled.MatchString(remotePath)
}

func fileScopeAllowed(scopes []string, remotePath string) bool {
	for _, scope := range scopes {
		if fileScopeMatches(scope, remotePath) {
			return true
		}
	}
	return false
}

// RequireFileAccess checks both the explicit API approval and the narrower
// path allowlist approved for a plugin.
func (m *Manager) RequireFileAccess(id, remotePath string, write bool) error {
	cleaned, err := normalizeRemotePath(remotePath)
	if err != nil {
		return err
	}
	api := "filebrowser.read"
	if write {
		api = "filebrowser.write"
	}
	if err := m.RequireAPI(id, api); err != nil {
		return err
	}
	m.mu.RLock()
	plugin, ok := m.plugins[id]
	m.mu.RUnlock()
	if !ok {
		return ErrPluginNotFound
	}
	scopes := plugin.ApprovedFileRead
	if write {
		scopes = plugin.ApprovedFileWrite
	}
	if !fileScopeAllowed(scopes, cleaned) {
		return errors.New("plugin file path is outside its approved scope")
	}
	return nil
}

// RegisterFileEditor records routing metadata for an editor that remains
// inside the plugin iframe. The frontend runtime owns the actual callback.
func (m *Manager) RegisterFileEditor(id string, editor FileEditorRegistration) error {
	if err := m.RequireAPI(id, "filebrowser.editor"); err != nil {
		return err
	}
	editor.ID = strings.TrimSpace(editor.ID)
	editor.PluginID = id
	editor.Label = strings.TrimSpace(editor.Label)
	if editor.ID == "" || len(editor.ID) > 128 || editor.Label == "" || len(editor.Label) > 200 {
		return errors.New("file editor id and label are required")
	}
	if strings.ContainsAny(editor.ID, "\r\n") || strings.ContainsAny(editor.Label, "\r\n") {
		return errors.New("file editor metadata cannot contain line breaks")
	}
	if len(editor.Extensions) == 0 || len(editor.Extensions) > 32 {
		return errors.New("file editor must declare between one and 32 extensions")
	}
	normalized := make([]string, 0, len(editor.Extensions))
	for _, extension := range editor.Extensions {
		extension = strings.ToLower(strings.TrimSpace(extension))
		if extension == "" || len(extension) > 32 || !strings.HasPrefix(extension, ".") || strings.ContainsAny(extension, "/\\*?\r\n") {
			return errors.New("file editor extensions must be explicit dot-prefixed extensions")
		}
		if !contains(normalized, extension) {
			normalized = append(normalized, extension)
		}
	}
	editor.Extensions = normalized
	if editor.Priority < -1000 {
		editor.Priority = -1000
	}
	if editor.Priority > 1000 {
		editor.Priority = 1000
	}
	m.fileEditorsMu.Lock()
	if m.fileEditors == nil {
		m.fileEditors = map[string]FileEditorRegistration{}
	}
	m.fileEditors[id+"\x00"+editor.ID] = editor
	m.fileEditorsMu.Unlock()
	return nil
}

func (m *Manager) UnregisterFileEditor(id, editorID string) error {
	if err := m.RequireAPI(id, "filebrowser.editor"); err != nil {
		return err
	}
	m.fileEditorsMu.Lock()
	delete(m.fileEditors, id+"\x00"+strings.TrimSpace(editorID))
	m.fileEditorsMu.Unlock()
	return nil
}

func (m *Manager) ClosePluginEditors(id string) {
	m.fileEditorsMu.Lock()
	for key, editor := range m.fileEditors {
		if editor.PluginID == id {
			delete(m.fileEditors, key)
		}
	}
	m.fileEditorsMu.Unlock()
}

func (m *Manager) MatchFileEditor(remotePath string) (FileEditorRegistration, bool) {
	cleaned, err := normalizeRemotePath(remotePath)
	if err != nil {
		return FileEditorRegistration{}, false
	}
	extension := strings.ToLower(filepath.Ext(cleaned))
	m.mu.RLock()
	plugins := make(map[string]PluginInfo, len(m.plugins))
	for id, plugin := range m.plugins {
		plugins[id] = plugin
	}
	m.mu.RUnlock()
	m.fileEditorsMu.RLock()
	matches := make([]FileEditorRegistration, 0)
	for _, editor := range m.fileEditors {
		plugin, ok := plugins[editor.PluginID]
		if !ok || !plugin.Enabled || plugin.Status != "ready" || !contains(plugin.ApprovedAPIs, "filebrowser.editor") {
			continue
		}
		for _, candidate := range editor.Extensions {
			if candidate == extension {
				matches = append(matches, cloneFileEditor(editor))
				break
			}
		}
	}
	m.fileEditorsMu.RUnlock()
	if len(matches) == 0 {
		return FileEditorRegistration{}, false
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Priority != matches[j].Priority {
			return matches[i].Priority > matches[j].Priority
		}
		return matches[i].PluginID+matches[i].ID < matches[j].PluginID+matches[j].ID
	})
	return matches[0], true
}

func cloneFileEditor(editor FileEditorRegistration) FileEditorRegistration {
	editor.Extensions = append([]string(nil), editor.Extensions...)
	return editor
}
