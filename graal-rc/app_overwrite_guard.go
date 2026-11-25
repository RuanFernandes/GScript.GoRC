package main

import (
	"errors"
	"fmt"
	"strings"
)

// ensureScriptNameFree protects existing scripts from the add actions: the
// native add packets replace a script that already has the name.
func ensureScriptNameFree(kind, name string, existing []string) error {
	wanted := strings.TrimSpace(name)
	for _, candidate := range existing {
		if strings.EqualFold(strings.TrimSpace(candidate), wanted) {
			return fmt.Errorf("%s %q already exists", kind, candidate)
		}
	}
	return nil
}

// serverChangedSinceOpen reports whether saving next would replace a server
// version the editor never showed. baseline is what the editor started from.
func serverChangedSinceOpen(baseline string, hasBaseline bool, current, next string) bool {
	if !hasBaseline {
		return false
	}
	// The server may not preserve line endings or the final newline.
	normalize := func(text string) string {
		return strings.TrimRight(strings.ReplaceAll(text, "\r", ""), "\n")
	}
	return normalize(current) != normalize(baseline) && normalize(current) != normalize(next)
}

var errServerChanged = errors.New("the server version changed since this editor loaded it; " +
	"it was saved to Change history. Save again to overwrite it")

// adoptLocalUpload moves a clean editor's baseline to a script the local sync
// just deployed. A dirty editor keeps its text, so its next save must warn.
func (a *App) adoptLocalUpload(kind, key, content string) {
	a.editorCacheMu.Lock()
	dirty := a.editorDirty[kind+":"+key]
	a.editorCacheMu.Unlock()
	if !dirty {
		a.updateEditorContent(kind, key, content)
	}
}

// ensureNewScriptName refuses a weapon or class name the server already lists.
func (a *App) ensureNewScriptName(kind, name string) error {
	lists, err := a.sessions.GetScriptLists(false)
	if err != nil {
		return fmt.Errorf("check existing scripts before adding %s %q: %w", kind, name, err)
	}
	var existing []string
	if kind == "weapon" {
		for _, weapon := range lists.Weapons {
			existing = append(existing, weapon.Name)
		}
	} else {
		for _, class := range lists.Classes {
			existing = append(existing, class.Name)
		}
	}
	return ensureScriptNameFree(kind, name, existing)
}
