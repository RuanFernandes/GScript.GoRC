// Package sync implements bidirectional synchronization of NC scripts
// (Weapons, Classes, NPCs) between a Graal server and a local folder.
//
// The package depends only on a ScriptBackend interface (satisfied by
// *connection.Service) so it does not import internal/connection, avoiding an
// import cycle. Delete operations are deliberately absent from this interface:
// local deletions never propagate to the server, while server deletions are
// reflected by removing the corresponding local files.
package sync

import (
	"context"

	"graal-rc/rclib"
)

// ScriptBackend is the subset of *connection.Service the engine needs.
type ScriptBackend interface {
	// EnsureNCConnected waits for the NC socket to be connected and
	// authenticated, retrying a transient disconnect within a bounded timeout.
	// Reconcile must call this before reading or writing the script snapshot.
	EnsureNCConnected(context.Context) error

	// IsNCConnected reports whether the NC (script) socket is up. Every
	// reconcile path gates on this — no server I/O happens while NC is down.
	IsNCConnected() bool
	// IsNCAuthenticated reports whether the NC handshake completed and its
	// script-list caches are safe to read.
	IsNCAuthenticated() bool

	GetWeapons() ([]rclib.Weapon, error)
	GetClasses() ([]rclib.Class, error)
	GetNPCs() ([]rclib.NPC, error)

	// RefreshSelfFolderRights refreshes the current account's folder rights for
	// the active server. CanReadScript/CanWriteScript use that cached snapshot
	// and fail closed until a refresh succeeds.
	RefreshSelfFolderRights() error
	CanReadScript(scriptType, name string) bool
	CanWriteScript(scriptType, name string) bool

	// OpenScript fetches one script body (correlated via the pending map).
	// For weapon/class key is the name; for npc key is the stringified id.
	OpenScript(scriptType, key string) (rclib.ScriptReply, error)

	// Push (update existing).
	SaveWeapon(name, script string) error
	SaveClass(name, script string) error
	SaveNPC(id int, script string) error

	// Add creates a brand-new weapon/class. The watcher may call these for a
	// genuinely new local file; NPC creation remains intentionally unsupported.
	AddWeapon(name string) error
	AddClass(name string) error
	CreateNPC(name string, id int, npcType, scripter, level, x, y string) error

	// RefreshWeapons re-requests the weapon list (packet 115). Classes/NPCs have
	// no listget — their caches are server-auto-pushed at NC auth + live
	// add/delete pushes, so the engine just reads GetClasses/GetNPCs.
	RefreshWeapons() error

	// FetchAllScripts pulls every script body (loops the lists + OpenScript).
	FetchAllScripts(ctx context.Context, allowed func(scriptType, name string) bool, progress func(done, total int)) ([]rclib.ScriptReply, error)
}
