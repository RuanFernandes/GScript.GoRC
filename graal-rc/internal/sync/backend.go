// Package sync implements bidirectional synchronization of NC scripts
// (Weapons, Classes, NPCs) between a Graal server and a local folder.
//
// The package depends only on a ScriptBackend interface (satisfied by
// *connection.Service) so it does not import internal/connection, avoiding an
// import cycle. Delete operations are deliberately absent from this interface:
// the safety rules forbid propagating deletes in either direction, so the
// engine can never reach the underlying Delete* calls.
package sync

import (
	"context"

	"graal-rc/rclib"
)

// ScriptBackend is the subset of *connection.Service the engine needs.
type ScriptBackend interface {
	// IsNCConnected reports whether the NC (script) socket is up. Every
	// reconcile path gates on this — no server I/O happens while NC is down.
	IsNCConnected() bool

	GetWeapons() ([]rclib.Weapon, error)
	GetClasses() ([]rclib.Class, error)
	GetNPCs() ([]rclib.NPC, error)

	// OpenScript fetches one script body (correlated via the pending map).
	// For weapon/class key is the name; for npc key is the stringified id.
	OpenScript(scriptType, key string) (rclib.ScriptReply, error)

	// Push (update existing).
	SaveWeapon(name, script string) error
	SaveClass(name, script string) error
	SaveNPC(id int, script string) error

	// Add (create brand-new — only reached after a user confirms a new-local
	// review item, never automatically).
	AddWeapon(name string) error
	AddClass(name string) error
	CreateNPC(name string, id int, npcType, scripter, level, x, y string) error

	// RefreshWeapons re-requests the weapon list (packet 115). Classes/NPCs have
	// no listget — their caches are server-auto-pushed at NC auth + live
	// add/delete pushes, so the engine just reads GetClasses/GetNPCs.
	RefreshWeapons() error

	// FetchAllScripts pulls every script body (loops the lists + OpenScript).
	FetchAllScripts(ctx context.Context, progress func(done, total int)) ([]rclib.ScriptReply, error)
}
