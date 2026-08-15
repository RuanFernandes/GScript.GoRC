package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

func kindFromDir(dir string) string {
	switch filepath.Base(dir) {
	case "weapons":
		return "weapon"
	case "classes":
		return "class"
	case "npcs":
		return "npc"
	default:
		return ""
	}
}

// sanitizeFileName replaces filesystem-unsafe characters. Script names may
// contain "/", ":", etc. (e.g. "heheh/denvnob"); the true name is kept in the
// manifest Entry.Name so the round-trip to the server uses the original.
func encodeName(name string) string {
	if name == "" {
		return "_"
	}
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|', '%':
			b.WriteByte('%')
			b.WriteString(fmt.Sprintf("%03d", r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func decodeName(name string) string {
	var b strings.Builder
	for i := 0; i < len(name); i++ {
		if name[i] == '%' && i+3 < len(name) {
			if n, err := strconv.Atoi(name[i+1 : i+4]); err == nil {
				b.WriteRune(rune(n))
				i += 3
				continue
			}
		}
		b.WriteByte(name[i])
	}
	return b.String()
}

// fileNameFor returns the encoded server name (without extension). The ref
// kept in memory supplies the stable NPC id when the file is uploaded.
func fileNameFor(kind, key, name string) string {
	if name != "" {
		return encodeName(name)
	}
	return encodeName(key)
}

// fullPath returns the on-disk path for an entry.
func fullPath(outputDir, kind, fileName string) string {
	return filepath.Join(outputDir, kindSubdir(kind), fileName+scriptExt)
}

// LocalScriptPath returns the managed local path for a script name. The name
// is encoded using the same convention as the sync engine, so scripts whose
// server names contain path separators still resolve to a single file inside
// the configured output directory.
func LocalScriptPath(outputDir, kind, name string) (string, error) {
	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		return "", fmt.Errorf("sync output directory is required")
	}

	kind = strings.ToLower(strings.TrimSpace(kind))
	switch kind {
	case "weapon", "class", "npc":
	default:
		return "", fmt.Errorf("unsupported script kind %q", kind)
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("script name is required")
	}

	directory, err := filepath.Abs(outputDir)
	if err != nil {
		return "", fmt.Errorf("resolve sync output directory: %w", err)
	}
	return fullPath(directory, kind, fileNameFor(kind, name, name)), nil
}

func entryKey(kind, key string) string { return kind + ":" + key }

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
