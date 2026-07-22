//go:build windows

package main

import (
	"sort"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// ListFonts returns the display names of font families installed on the system
// (machine-wide + per-user), suitable for a font picker. Names are read from the
// registry ("...\CurrentVersion\Fonts") value names with their trailing
// "(TrueType)"/"(OpenType)" style suffix stripped, deduped and sorted.
func (a *App) ListFonts() ([]string, error) {
	const regPath = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`
	seen := map[string]struct{}{}

	for _, root := range []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER} {
		k, err := registry.OpenKey(root, regPath, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, _ := k.ReadValueNames(-1)
		k.Close()
		for _, n := range names {
			if name := cleanFontName(n); name != "" {
				seen[name] = struct{}{}
			}
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	return out, nil
}

// cleanFontName strips the parenthesized font-format suffix the registry appends
// to each value name (e.g. "Arial (TrueType)" -> "Arial"). Returns "" for the
// unhelpful "@"-prefixed vertical-writing duplicates.
func cleanFontName(valueName string) string {
	name := valueName
	if i := strings.LastIndex(name, " ("); i > 0 {
		name = name[:i]
	}
	name = strings.TrimSpace(name)
	if name == "" || strings.HasPrefix(name, "@") {
		return ""
	}
	return name
}
