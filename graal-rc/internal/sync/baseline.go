package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// A baseline is the hash of a script the last time the local file and the
// server were known to hold the same content. It is what tells an untouched
// local file, which a pull may replace, from a local edit that never reached
// the server, which it may not. Unlike Engine.hashes (the watcher's change
// detection) it survives restarts.
//
// The state file lives inside the output directory and is named after the
// server, so neither two directories nor two servers can share baselines.

const (
	stateDirName    = ".sync-state"
	baselineVersion = 1
)

type baselineFile struct {
	Version int    `json:"version"`
	Server  string `json:"server"`
	// Scripts maps "weapons/Name.gs2" to the content hash.
	Scripts map[string]string `json:"scripts"`
}

type baselineSet struct {
	scripts map[string]string
	dirty   bool
}

// scriptRoot splits a managed script path into the sync root and the path
// relative to it.
func scriptRoot(path string) (root, relative string) {
	dir := filepath.Dir(path)
	if kindFromDir(dir) == "" {
		return dir, filepath.Base(path)
	}
	return filepath.Dir(dir), filepath.Base(dir) + "/" + filepath.Base(path)
}

func (e *Engine) baselinePath(root string) string {
	sum := sha256.Sum256([]byte(e.server))
	return filepath.Join(root, stateDirName, "baseline-"+hex.EncodeToString(sum[:8])+".json")
}

// baselineSetLocked returns the baselines of one sync root, loading them on
// first use. The caller holds baselineMu.
func (e *Engine) baselineSetLocked(root string) *baselineSet {
	if set := e.baselines[root]; set != nil {
		return set
	}
	set := &baselineSet{scripts: map[string]string{}}
	data, err := os.ReadFile(e.baselinePath(root))
	switch {
	case err == nil:
		var file baselineFile
		if err := json.Unmarshal(data, &file); err != nil {
			log.Printf("sync state: ignoring unreadable baseline file: %v", err)
		} else if file.Version == baselineVersion && file.Server == e.server && file.Scripts != nil {
			set.scripts = file.Scripts
		}
	case !os.IsNotExist(err):
		log.Printf("sync state: read baselines: %v", err)
	}
	e.baselines[root] = set
	return set
}

func (e *Engine) baseline(path string) (string, bool) {
	root, relative := scriptRoot(path)
	e.baselineMu.Lock()
	defer e.baselineMu.Unlock()
	hash, ok := e.baselineSetLocked(root).scripts[relative]
	return hash, ok
}

func (e *Engine) setBaseline(path, hash string) {
	root, relative := scriptRoot(path)
	e.baselineMu.Lock()
	defer e.baselineMu.Unlock()
	set := e.baselineSetLocked(root)
	if current, ok := set.scripts[relative]; !ok || current != hash {
		set.scripts[relative] = hash
		set.dirty = true
	}
}

func (e *Engine) dropBaseline(path string) {
	root, relative := scriptRoot(path)
	e.baselineMu.Lock()
	defer e.baselineMu.Unlock()
	set := e.baselineSetLocked(root)
	if _, ok := set.scripts[relative]; ok {
		delete(set.scripts, relative)
		set.dirty = true
	}
}

// flushBaselines writes changed baselines to disk. It runs once at the end of
// each operation rather than per script, since a snapshot touches thousands.
// Losing an unflushed update can only cause an extra review, never an
// overwrite.
func (e *Engine) flushBaselines() {
	e.baselineMu.Lock()
	defer e.baselineMu.Unlock()
	for root, set := range e.baselines {
		if !set.dirty {
			continue
		}
		data, err := json.Marshal(baselineFile{Version: baselineVersion, Server: e.server, Scripts: set.scripts})
		if err == nil {
			err = writeFileAtomic(e.baselinePath(root), data)
		}
		if err != nil {
			log.Printf("sync state: save baselines: %v", err)
			continue
		}
		set.dirty = false
	}
}

func (e *Engine) forgetBaselines() {
	e.baselineMu.Lock()
	e.baselines = map[string]*baselineSet{}
	e.baselineMu.Unlock()
}
