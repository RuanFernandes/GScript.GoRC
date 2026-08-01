package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Entry is the baseline state for one synced script.
type Entry struct {
	// Kind is "weapon" | "class" | "npc".
	Kind string `json:"kind"`
	// Key is the weapon/class name or the stringified NPC id — the same key
	// used to talk to the server.
	Key string `json:"key"`
	// Name is the true display name (kept because the on-disk filename is
	// sanitized; script names may contain "/" or other unsafe chars).
	Name string `json:"name"`
	// FileName is the sanitized on-disk filename (without extension).
	FileName string `json:"fileName"`
	// BaseHash is the short sha256 hex of the normalized content at the last
	// sync point (where server == local). "" means no baseline yet.
	BaseHash string `json:"baseHash"`
	// BaseAt is a unix timestamp of the last baseline mutation.
	BaseAt int64 `json:"baseAt"`
}

// Manifest is the persisted baseline set. Stored at
// <OutputDir>/.sync-manifest.json so it travels with the synced files and one
// output folder maps to one server baseline.
type Manifest struct {
	Version int              `json:"version"`
	Server  string           `json:"server"`
	Entries map[string]Entry `json:"entries"` // key = Kind + ":" + Key
}

func manifestPath(outputDir string) string {
	return filepath.Join(outputDir, ".sync-manifest.json")
}

func loadManifest(outputDir string) Manifest {
	m := Manifest{Version: 1, Entries: map[string]Entry{}}
	if outputDir == "" {
		return m
	}
	b, err := os.ReadFile(manifestPath(outputDir))
	if err != nil {
		return m
	}
	_ = json.Unmarshal(b, &m) // corrupt file => fresh manifest (first-run)
	if m.Entries == nil {
		m.Entries = map[string]Entry{}
	}
	if m.Version == 0 {
		m.Version = 1
	}
	return m
}

func saveManifest(outputDir string, m Manifest) error {
	if outputDir == "" {
		return nil
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(manifestPath(outputDir), b)
}

// entryKey is the manifest map key.
func entryKey(kind, key string) string { return kind + ":" + key }
