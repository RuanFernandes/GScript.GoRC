package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"

	"graal-rc/internal/connection"
	"graal-rc/internal/drafts"
)

func (a *App) editorDraftStore() (*drafts.Store, error) {
	a.draftsMu.Lock()
	defer a.draftsMu.Unlock()
	if a.draftStore != nil {
		return a.draftStore, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	a.draftStore, err = drafts.New(filepath.Join(dir, "graal-rc", "editor-drafts"))
	return a.draftStore, err
}

func (a *App) prepareEditorDraft(identity connection.SessionIdentity, kind, key string) (drafts.Lease, error) {
	if !a.sessions.IsSessionCurrent(identity) {
		return drafts.Lease{}, errors.New("server session changed while opening editor")
	}
	store, err := a.editorDraftStore()
	if err != nil {
		return drafts.Lease{}, err
	}
	return store.Acquire(drafts.Identity{
		Listserver: identity.ListserverEndpoint, Endpoint: identity.ServerEndpoint,
		Account: identity.Account, Server: identity.ServerName, Kind: kind, Resource: key,
		Epoch: identity.Epoch,
	})
}

// SaveEditorDraft routes only explicitly requested editor uploads. The lease
// fixes the original session and resource; connection transitions hold the
// exclusive side of this lock until the existing upload finishes or cancels.
func (a *App) SaveEditorDraft(token, content string) error {
	a.sessionActionMu.RLock()
	defer a.sessionActionMu.RUnlock()
	identity, err := a.currentEditorDraftIdentity(token)
	if err != nil {
		return err
	}
	switch identity.Kind {
	case "weapon":
		return a.SaveWeapon(identity.Resource, content)
	case "class":
		return a.SaveClass(identity.Resource, content)
	case "npc", "npcflags":
		id, err := strconv.Atoi(identity.Resource)
		if err != nil || id < 0 {
			return errors.New("invalid NPC editor resource")
		}
		if identity.Kind == "npc" {
			return a.SaveNPC(id, content)
		}
		return a.SaveNPCFlags(id, content)
	case "options", "folder_config", "flags":
		return a.SaveServerText(identity.Kind, content)
	case "textfile":
		return a.SaveTextFile(identity.Resource, content)
	default:
		return errors.New("this editor resource is read-only")
	}
}

// The caller holds sessionActionMu against a concurrent server switch.
func (a *App) currentEditorDraftIdentity(token string) (drafts.Identity, error) {
	store, err := a.editorDraftStore()
	if err != nil {
		return drafts.Identity{}, err
	}
	identity, err := store.Identity(token)
	if err != nil {
		return drafts.Identity{}, err
	}
	if a.sessions == nil || !a.sessions.IsSessionCurrent(connection.SessionIdentity{
		Account: identity.Account, ServerName: identity.Server,
		ListserverEndpoint: identity.Listserver, ServerEndpoint: identity.Endpoint, Epoch: identity.Epoch,
	}) {
		return drafts.Identity{}, errors.New("editor belongs to a disconnected server session; reopen it to recover your draft")
	}
	return identity, nil
}

type EditorDraftContent struct {
	Text string `json:"text"`
	Name string `json:"name"`
}

func (a *App) GetEditorDraftContent(token string) (EditorDraftContent, error) {
	a.sessionActionMu.RLock()
	defer a.sessionActionMu.RUnlock()
	identity, err := a.currentEditorDraftIdentity(token)
	if err != nil {
		return EditorDraftContent{}, err
	}
	if identity.Kind == "textfile" {
		text, err := a.GetTextFile(identity.Resource)
		return EditorDraftContent{Text: text}, err
	}
	reply, err := a.GetLoadedScript(identity.Kind, identity.Resource)
	return EditorDraftContent{Text: reply.Script, Name: reply.Name}, err
}

func (a *App) ResolveEditorDraftConflict(token, choice, mergeContent string) error {
	a.sessionActionMu.RLock()
	defer a.sessionActionMu.RUnlock()
	identity, err := a.currentEditorDraftIdentity(token)
	if err != nil {
		return err
	}
	if identity.Kind != "weapon" && identity.Kind != "class" && identity.Kind != "npc" {
		return errors.New("this editor does not support script sync conflicts")
	}
	return a.ResolveConflict(identity.Kind, identity.Resource, choice, mergeContent)
}

func (a *App) SetEditorDraftDirty(token string, dirty bool) error {
	a.sessionActionMu.RLock()
	defer a.sessionActionMu.RUnlock()
	identity, err := a.currentEditorDraftIdentity(token)
	if err != nil {
		return err
	}
	a.SetEditorDirty(identity.Kind, identity.Resource, dirty)
	return nil
}

func (a *App) CloseEditorDraft(token string) error {
	a.sessionActionMu.RLock()
	defer a.sessionActionMu.RUnlock()
	identity, err := a.currentEditorDraftIdentity(token)
	if err != nil {
		return err
	}
	a.CloseScriptEditor(identity.Kind, identity.Resource)
	return nil
}

func (a *App) GetEditorDraft(token string) (*drafts.Record, error) {
	store, err := a.editorDraftStore()
	if err != nil {
		return nil, err
	}
	return store.Load(token)
}

func (a *App) WriteEditorDraft(token string, sequence uint64, record drafts.Record) error {
	store, err := a.editorDraftStore()
	if err != nil {
		return err
	}
	return store.Write(token, sequence, record)
}

func (a *App) ClearEditorDraft(token, revision string) (bool, error) {
	store, err := a.editorDraftStore()
	if err != nil {
		return false, err
	}
	return store.Clear(token, revision)
}
