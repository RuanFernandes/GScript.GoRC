package sync

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
)

// foldPath identifies the file a path names on a case-insensitive file system.
func foldPath(path string) string { return strings.ToLower(filepath.Clean(path)) }

const collisionReason = "another script uses the same local file name (names differ only by case, or two NPCs share a name)"

// setCollisions replaces the set of blocked file names with the outcome of a
// snapshot listing. Scripts that would share one file are all left alone: the
// file is not pulled, uploaded or removed, since there is no way to tell which
// script its content belongs to.
func (e *Engine) setCollisions(outputDir string, scripts []scriptRef) {
	groups := map[string][]scriptRef{}
	for _, script := range scripts {
		if script.kind == "" {
			continue
		}
		script.path = fullPath(outputDir, script.kind, fileNameFor(script.kind, script.key, script.name))
		folded := foldPath(script.path)
		known := false
		for _, other := range groups[folded] {
			known = known || (other.kind == script.kind && other.key == script.key)
		}
		if !known {
			groups[folded] = append(groups[folded], script)
		}
	}
	for folded, group := range groups {
		if len(group) < 2 {
			delete(groups, folded)
		}
	}
	e.mu.Lock()
	e.collisions = map[string][]scriptRef{}
	e.mu.Unlock()
	for _, group := range groups {
		e.blockCollision(group)
	}
}

// blockCollision blocks the file shared by a group of scripts.
func (e *Engine) blockCollision(group []scriptRef) {
	folded := foldPath(group[0].path)
	for _, script := range group {
		log.Printf("[sync] %s %q (%s) blocked: %s; the local file is left untouched and will not be uploaded", script.kind, script.name, script.key, collisionReason)
	}
	e.mu.Lock()
	e.collisions[folded] = group
	var tracked []string
	for path := range e.refs {
		if foldPath(path) == folded {
			tracked = append(tracked, path)
		}
	}
	for _, path := range tracked {
		delete(e.refs, path)
		delete(e.hashes, path)
	}
	e.mu.Unlock()
	for _, script := range group {
		e.dropReview(script.path)
	}
}

func (e *Engine) collidesAt(path string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, ok := e.collisions[foldPath(path)]
	return ok
}

// trackedTwin returns a tracked script whose file differs from path only by
// case.
func (e *Engine) trackedTwin(path string) (scriptRef, bool) {
	folded := foldPath(path)
	e.mu.RLock()
	defer e.mu.RUnlock()
	for other, ref := range e.refs {
		if other != path && foldPath(other) == folded {
			ref.path = other
			return ref, true
		}
	}
	return scriptRef{}, false
}

// runtimeCollision checks a script announced after the snapshot against the
// ones already known, and blocks the shared file if there is a second owner.
func (e *Engine) runtimeCollision(ref scriptRef, path string) bool {
	if e.collidesAt(path) {
		return true
	}
	ref.path = path
	if ref.kind == "npc" {
		// The NPC list is authoritative; a recreated NPC has a new id, so
		// tracked ids cannot be used to detect a second owner.
		npcs, err := e.backend.GetNPCs()
		if err != nil {
			return false
		}
		var group []scriptRef
		for _, npc := range npcs {
			if strings.EqualFold(npc.Name, ref.name) {
				group = append(group, scriptRef{kind: "npc", key: fmt.Sprint(npc.ID), name: npc.Name, path: path})
			}
		}
		if len(group) < 2 {
			return false
		}
		e.blockCollision(group)
		return true
	}
	other, ok := e.trackedTwin(path)
	if !ok {
		e.mu.RLock()
		other, ok = e.refs[path]
		e.mu.RUnlock()
		ok = ok && other.key != ref.key
		other.path = path
	}
	if !ok || (other.kind == ref.kind && other.key == ref.key) {
		return false
	}
	e.blockCollision([]scriptRef{other, ref})
	return true
}

// blockStale blocks uploads of a script whose server version could not be
// written locally, so the stale file cannot replace it.
func (e *Engine) blockStale(ref scriptRef, path string, cause error) {
	e.mu.Lock()
	e.unreadable[path] = UnreadableScript{Kind: ref.kind, Key: ref.key, Name: ref.name, Reason: fmt.Sprintf("the server version could not be written locally: %v", cause)}
	e.mu.Unlock()
}

// markSnapshotLoaded records that the server's version of the scripts under
// outputDir has been read. No upload happens before that.
func (e *Engine) markSnapshotLoaded(outputDir string) {
	e.mu.Lock()
	e.snapshotDir = outputDir
	e.mu.Unlock()
}

func (e *Engine) snapshotLoaded() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.snapshotDir != "" && e.snapshotDir == e.cfg.OutputDir
}

// setPushError surfaces a local change that did not reach the server through
// SyncStatus.LastError.
func (e *Engine) setPushError(path, reason string) {
	_, relative := scriptRoot(path)
	message := fmt.Sprintf("%s was not uploaded: %s", relative, reason)
	log.Printf("sync upload: %s", message)
	e.mu.Lock()
	e.lastError = message
	e.lastErrorPath = path
	e.mu.Unlock()
	e.emitStatus()
}

func (e *Engine) clearPushError(path string) {
	e.mu.Lock()
	cleared := e.lastErrorPath == path && path != ""
	if cleared {
		e.lastError = ""
		e.lastErrorPath = ""
	}
	e.mu.Unlock()
	if cleared {
		e.emitStatus()
	}
}

// needsReview reports whether writing the server version over the local file
// would lose something. That is the case when the local file differs from the
// incoming content and was changed since the last sync, or already is under
// review with no baseline showing it unchanged. With neither a baseline nor a
// review the server wins, and the pull backs the local file up.
func (e *Engine) needsReview(path, local, serverHash string) bool {
	localHash := HashScript(local)
	if localHash == serverHash {
		return false
	}
	if base, ok := e.baseline(path); ok {
		return localHash != base
	}
	_, reviewing := e.reviewAt(path)
	return reviewing
}

// reviewAt returns the pending review of the script stored at path.
func (e *Engine) reviewAt(path string) (ReviewItem, bool) {
	folded := foldPath(path)
	e.mu.RLock()
	defer e.mu.RUnlock()
	for key, other := range e.reviewPaths {
		if foldPath(other) == folded {
			item, ok := e.review[key]
			return item, ok
		}
	}
	return ReviewItem{}, false
}

func (e *Engine) dropReview(path string) {
	folded := foldPath(path)
	e.mu.Lock()
	defer e.mu.Unlock()
	for key, other := range e.reviewPaths {
		if foldPath(other) == folded {
			delete(e.review, key)
			delete(e.reviewPaths, key)
		}
	}
}

// markInSync records that the local file and the server hold the same content.
func (e *Engine) markInSync(path, hash string) {
	e.setHash(path, hash)
	e.setBaseline(path, hash)
	e.dropReview(path)
}
