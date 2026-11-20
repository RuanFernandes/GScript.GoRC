package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"graal-rc/internal/mcp"
	synclib "graal-rc/internal/sync"
	"graal-rc/rclib"
)

const (
	// mcpFirstNPCID matches the Script Manager's "new NPC" dialog, which offers
	// the first free id from 1000 up.
	mcpFirstNPCID = 1000
	// mcpNPCListWait bounds how long npc_add waits for the server to announce
	// the NPC it just created.
	mcpNPCListWait = 5 * time.Second
	mcpNPCListPoll = 150 * time.Millisecond
)

// mcpScript is a script reference resolved against the current NC lists.
type mcpScript struct {
	kind string
	key  string // weapon/class name, or the stringified NPC id
	name string
	id   *int
}

func (b appMCPBackend) result(target mcpScript, content string) mcp.Script {
	return mcp.Script{Type: target.kind, Name: target.name, ID: target.id, Script: content, LocalPath: b.localScriptPath(target.kind, target.name)}
}

// localScriptPath returns the file the sync engine keeps for a script, or ""
// when sync is not enabled for the current server.
func (b appMCPBackend) localScriptPath(kind, name string) string {
	cfg := b.app.GetSyncConfig()
	if !cfg.Enabled || strings.TrimSpace(cfg.OutputDir) == "" {
		return ""
	}
	path, err := synclib.LocalScriptPath(cfg.OutputDir, kind, name)
	if err != nil {
		return ""
	}
	return path
}

func (b appMCPBackend) ListScripts(kind string) ([]mcp.ScriptEntry, error) {
	lists, err := b.app.sessions.GetScriptLists(true)
	if err != nil {
		return nil, err
	}
	entries := make([]mcp.ScriptEntry, 0, len(lists.Weapons)+len(lists.Classes)+len(lists.NPCs))
	if kind == "" || kind == "weapon" {
		for _, weapon := range lists.Weapons {
			entries = append(entries, mcp.ScriptEntry{Type: "weapon", Name: weapon.Name})
		}
	}
	if kind == "" || kind == "class" {
		for _, class := range lists.Classes {
			entries = append(entries, mcp.ScriptEntry{Type: "class", Name: class.Name})
		}
	}
	if kind == "" || kind == "npc" {
		for _, npc := range lists.NPCs {
			id := npc.ID
			entries = append(entries, mcp.ScriptEntry{Type: "npc", Name: npc.Name, ID: &id, NPCType: npc.Type, Level: npc.Level})
		}
	}
	return entries, nil
}

// resolveScript maps an MCP reference to an existing script. Names are matched
// exactly: the NC server is case-sensitive, and the update packets create a
// script when the name is new, so a near match must never be written to.
func (b appMCPBackend) resolveScript(ref mcp.ScriptRef) (mcpScript, error) {
	lists, err := b.app.sessions.GetScriptLists(false)
	if err != nil {
		return mcpScript{}, err
	}
	switch ref.Type {
	case "weapon":
		for _, weapon := range lists.Weapons {
			if weapon.Name == ref.Name {
				return mcpScript{kind: "weapon", key: weapon.Name, name: weapon.Name}, nil
			}
		}
		return mcpScript{}, fmt.Errorf("weapon %q is not in the script list (names are case-sensitive); use script_list to find it or weapon_add to create it", ref.Name)
	case "class":
		for _, class := range lists.Classes {
			if class.Name == ref.Name {
				return mcpScript{kind: "class", key: class.Name, name: class.Name}, nil
			}
		}
		return mcpScript{}, fmt.Errorf("class %q is not in the script list (names are case-sensitive); use script_list to find it or class_add to create it", ref.Name)
	case "npc":
		npc, err := findNPC(lists.NPCs, ref)
		if err != nil {
			return mcpScript{}, err
		}
		id := npc.ID
		return mcpScript{kind: "npc", key: strconv.Itoa(npc.ID), name: npc.Name, id: &id}, nil
	}
	return mcpScript{}, fmt.Errorf("unknown script type %q", ref.Type)
}

func findNPC(npcs []rclib.NPC, ref mcp.ScriptRef) (rclib.NPC, error) {
	if ref.HasID {
		for _, npc := range npcs {
			if npc.ID == ref.ID {
				return npc, nil
			}
		}
		return rclib.NPC{}, fmt.Errorf("NPC %d is not in the script list; use script_list with type npc to find it", ref.ID)
	}
	var matches []rclib.NPC
	for _, npc := range npcs {
		if npc.Name == ref.Name {
			matches = append(matches, npc)
		}
	}
	switch len(matches) {
	case 0:
		return rclib.NPC{}, fmt.Errorf("NPC %q is not in the script list (names are case-sensitive); use script_list with type npc to find it", ref.Name)
	case 1:
		return matches[0], nil
	}
	return rclib.NPC{}, fmt.Errorf("%d NPCs are named %q; pass id instead", len(matches), ref.Name)
}

func (b appMCPBackend) resolveNPC(ref mcp.ScriptRef) (rclib.NPC, error) {
	npcs, err := b.app.sessions.GetNPCs()
	if err != nil {
		return rclib.NPC{}, err
	}
	return findNPC(npcs, ref)
}

func (b appMCPBackend) ReadScript(ref mcp.ScriptRef) (mcp.Script, error) {
	target, err := b.resolveScript(ref)
	if err != nil {
		return mcp.Script{}, err
	}
	reply, err := b.app.sessions.OpenScript(target.kind, target.key)
	if err != nil {
		return mcp.Script{}, err
	}
	return b.result(target, reply.Script), nil
}

func (b appMCPBackend) WriteScript(ref mcp.ScriptRef, script string) (mcp.Script, error) {
	target, err := b.resolveScript(ref)
	if err != nil {
		return mcp.Script{}, err
	}
	switch target.kind {
	case "weapon":
		err = b.app.SaveWeapon(target.name, script)
	case "class":
		err = b.app.SaveClass(target.name, script)
	case "npc":
		err = b.app.SaveNPC(*target.id, script)
	}
	if err != nil {
		return mcp.Script{}, err
	}
	return b.result(target, script), nil
}

func (b appMCPBackend) AddScript(spec mcp.NewScript) (mcp.Script, error) {
	if !rclib.IsUsableScriptName(spec.Name) {
		return mcp.Script{}, fmt.Errorf("%q is not a usable %s name", spec.Name, spec.Type)
	}
	if err := b.app.requireScriptSyncForEditor(spec.Type); err != nil {
		return mcp.Script{}, err
	}
	if !b.app.sessions.CanWriteScript(spec.Type, spec.Name) {
		return mcp.Script{}, fmt.Errorf("no write permission for %s %q", spec.Type, spec.Name)
	}
	lists, err := b.app.sessions.GetScriptLists(false)
	if err != nil {
		return mcp.Script{}, err
	}
	switch spec.Type {
	case "weapon":
		for _, weapon := range lists.Weapons {
			if strings.EqualFold(weapon.Name, spec.Name) {
				return mcp.Script{}, fmt.Errorf("weapon %q already exists; use script_write to change it", weapon.Name)
			}
		}
		if err := b.app.AddWeapon(spec.Name); err != nil {
			return mcp.Script{}, err
		}
		return b.finishAdd(mcpScript{kind: "weapon", key: spec.Name, name: spec.Name}, spec.Script, func() error {
			return b.app.sessions.SaveWeapon(spec.Name, spec.Script)
		})
	case "class":
		for _, class := range lists.Classes {
			if strings.EqualFold(class.Name, spec.Name) {
				return mcp.Script{}, fmt.Errorf("class %q already exists; use script_write to change it", class.Name)
			}
		}
		// AddClass also writes the "// Scripted by" first line, because some
		// servers reject a class whose body is empty.
		if err := b.app.AddClass(spec.Name); err != nil {
			return mcp.Script{}, err
		}
		return b.finishAdd(mcpScript{kind: "class", key: spec.Name, name: spec.Name}, spec.Script, func() error {
			return b.app.sessions.SaveClass(spec.Name, spec.Script)
		})
	case "npc":
		return b.addNPC(spec, lists.NPCs)
	}
	return mcp.Script{}, fmt.Errorf("unknown script type %q", spec.Type)
}

func (b appMCPBackend) addNPC(spec mcp.NewScript, npcs []rclib.NPC) (mcp.Script, error) {
	used := make(map[int]bool, len(npcs))
	for _, npc := range npcs {
		used[npc.ID] = true
		// The sync folder keys NPC files by name, so two NPCs with one name
		// would share a file.
		if strings.EqualFold(npc.Name, spec.Name) {
			return mcp.Script{}, fmt.Errorf("NPC %q already exists with id %d; use script_write to change it", npc.Name, npc.ID)
		}
	}
	id := spec.ID
	if spec.HasID {
		if used[id] {
			return mcp.Script{}, fmt.Errorf("NPC id %d is already in use", id)
		}
	} else {
		for id = mcpFirstNPCID; used[id]; id++ {
		}
	}
	if err := b.app.CreateNPC(spec.Name, id, spec.NPCType, spec.Scripter, spec.Level, spec.X, spec.Y); err != nil {
		return mcp.Script{}, err
	}
	// The server announces a created NPC asynchronously, and SaveNPC resolves
	// the id through that list.
	if !b.waitForNPC(id) {
		return mcp.Script{}, fmt.Errorf("the request to create NPC %q (id %d) was sent, but the server did not list it within %s; check get_rc_chat for a rejection before retrying", spec.Name, id, mcpNPCListWait)
	}
	return b.finishAdd(mcpScript{kind: "npc", key: strconv.Itoa(id), name: spec.Name, id: &id}, spec.Script, func() error {
		return b.app.sessions.SaveNPC(id, spec.Script)
	})
}

func (b appMCPBackend) waitForNPC(id int) bool {
	deadline := time.Now().Add(mcpNPCListWait)
	for {
		if npcs, err := b.app.sessions.GetNPCs(); err == nil {
			for _, npc := range npcs {
				if npc.ID == id {
					return true
				}
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(mcpNPCListPoll)
	}
}

// finishAdd writes the first content of a script that was just created and
// records the creation. There is no earlier remote version to back up, so the
// write skips the read-then-backup step of a normal save.
func (b appMCPBackend) finishAdd(target mcpScript, script string, save func() error) (mcp.Script, error) {
	auditTarget := target.kind + ":" + target.key
	if script == "" {
		b.app.recordAudit("create", "script", auditTarget, "success", "created through MCP")
		return b.result(target, ""), nil
	}
	eng := b.app.currentSyncEngine()
	if eng != nil {
		eng.ExpectServerUpdate(target.kind, target.key, script)
	}
	if err := save(); err != nil {
		if eng != nil {
			eng.CancelExpectedServerUpdate(target.kind, target.key)
		}
		b.app.recordAudit("create", "script", auditTarget, "failed", "created through MCP, but the script was not written: "+err.Error())
		return mcp.Script{}, fmt.Errorf("%s %q was created, but its script could not be written (use script_write to retry): %w", target.kind, target.name, err)
	}
	b.app.recordAudit("create", "script", auditTarget, "success", "created through MCP with its script")
	b.app.emitPluginEvent("script.saved", target.kind, target.key)
	return b.result(target, script), nil
}

func (b appMCPBackend) RefreshScripts() error { return b.app.RefreshWeapons() }

func (b appMCPBackend) ResetNPC(ref mcp.ScriptRef) error {
	npc, err := b.resolveNPC(ref)
	if err != nil {
		return err
	}
	return b.app.ResetNPC(npc.ID)
}

func (b appMCPBackend) WarpNPC(ref mcp.ScriptRef, level string, x, y float64) error {
	npc, err := b.resolveNPC(ref)
	if err != nil {
		return err
	}
	return b.app.WarpNPC(npc.ID, x, y, level)
}

func (b appMCPBackend) ReadNPCFlags(ref mcp.ScriptRef) (string, error) {
	npc, err := b.resolveNPC(ref)
	if err != nil {
		return "", err
	}
	reply, err := b.app.OpenNPCFlags(npc.ID)
	return reply.Script, err
}

func (b appMCPBackend) WriteNPCFlags(ref mcp.ScriptRef, flags string) error {
	npc, err := b.resolveNPC(ref)
	if err != nil {
		return err
	}
	return b.app.SaveNPCFlags(npc.ID, flags)
}

func (b appMCPBackend) ReadNPCAttributes(ref mcp.ScriptRef) (string, error) {
	npc, err := b.resolveNPC(ref)
	if err != nil {
		return "", err
	}
	reply, err := b.app.OpenNPCAttributes(npc.ID)
	return reply.Script, err
}

func (b appMCPBackend) SyncStatus() mcp.SyncInfo {
	status := b.app.GetSyncStatus()
	cfg := b.app.GetSyncConfig()
	outputDir := status.OutputDir
	if outputDir == "" {
		outputDir = cfg.OutputDir
	}
	return mcp.SyncInfo{
		Enabled:           status.Enabled,
		Server:            status.Server,
		OutputDir:         outputDir,
		AutoPushLocal:     cfg.AutoPushLocal,
		AutoPullServer:    cfg.AutoPullServer,
		Paused:            status.Paused,
		InitialSync:       status.InitialSync,
		PanicMode:         status.PanicMode,
		PanicReason:       status.PanicReason,
		NCDown:            status.NCDown,
		ScriptWriteAccess: status.ScriptWriteAccess,
		LastSyncAt:        status.LastSyncAt,
		LastError:         status.LastError,
		Layout:            mcp.SyncLayout,
	}
}
