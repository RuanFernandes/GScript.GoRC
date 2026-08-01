package sync

import (
	"os"
	"path/filepath"
	"strings"
)

// kindSubdir maps a script kind to its folder name under OutputDir.
func kindSubdir(kind string) string {
	switch kind {
	case "weapon":
		return "weapons"
	case "class":
		return "classes"
	case "npc":
		return "npcs"
	}
	return kind + "s"
}

// scriptExt is the file extension for synced scripts.
const scriptExt = ".gs2"

// sanitizeFileName replaces filesystem-unsafe characters. Script names may
// contain "/", ":", etc. (e.g. "heheh/denvnob"); the true name is kept in the
// manifest Entry.Name so the round-trip to the server uses the original.
func sanitizeFileName(name string) string {
	if name == "" {
		return "_"
	}
	r := strings.NewReplacer(
		string(os.PathSeparator), "_",
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		`"`, "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	s := r.Replace(name)
	// collapse runs of underscores / spaces
	s = strings.ReplaceAll(s, " ", "_")
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	s = strings.Trim(s, "_.")
	if s == "" {
		s = "_"
	}
	return s
}

// fileNameFor returns the sanitized filename (no extension) for an entry.
// NPCs key by id (stable across renames); weapons/classes by sanitized name.
func fileNameFor(kind, key, name string) string {
	if kind == "npc" {
		return sanitizeFileName(key)
	}
	if name != "" {
		return sanitizeFileName(name)
	}
	return sanitizeFileName(key)
}

// fullPath returns the on-disk path for an entry.
func fullPath(outputDir, kind, fileName string) string {
	return filepath.Join(outputDir, kindSubdir(kind), fileName+scriptExt)
}

// writeFileAtomic writes data via a temp file + rename so a crash mid-write or
// a watcher observing a half-written file cannot corrupt state. The ".tmp"
// suffix is also what the fsnotify filter keys on to ignore the engine's own
// writes.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readScriptFile reads + normalizes a script file. Missing file => ("", false).
func readScriptFile(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return normalizeEOL(string(b)), true
}
