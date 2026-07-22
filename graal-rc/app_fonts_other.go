//go:build !windows

package main

import (
	"os/exec"
	"sort"
	"strings"
)

// ListFonts returns the display names of font families installed on the system
// for a font picker, mirroring app_fonts_windows.go. On Linux it shells out to
// fontconfig's `fc-list`; if fontconfig is absent it falls back to a small
// default set so the picker is never empty.
func (a *App) ListFonts() ([]string, error) {
	out, err := exec.Command("fc-list", ":", "family").Output()
	if err != nil || len(out) == 0 {
		return defaultFonts(), nil
	}
	seen := map[string]struct{}{}
	for _, line := range strings.Split(string(out), "\n") {
		// fc-list prints one "Family" per line with this query, but guard against
		// comma-separated multi-family output anyway.
		for _, f := range strings.Split(line, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				seen[f] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return defaultFonts(), nil
	}
	list := make([]string, 0, len(seen))
	for f := range seen {
		list = append(list, f)
	}
	sort.Slice(list, func(i, j int) bool { return strings.ToLower(list[i]) < strings.ToLower(list[j]) })
	return list, nil
}

// defaultFonts is the fallback picker set when fontconfig is unavailable.
func defaultFonts() []string {
	return []string{"DejaVu Sans Mono", "Liberation Mono", "Courier New", "monospace"}
}
