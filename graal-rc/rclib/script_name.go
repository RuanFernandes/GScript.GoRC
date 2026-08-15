package rclib

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsUsableScriptName reports whether a server-provided script name is safe to
// use as a permission path, NC request key, or local filename component.
// Empty/control/format names are cache corruption or incomplete list entries,
// not actionable scripts.
func IsUsableScriptName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	placeholderOnly := true
	for _, r := range name {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
		if !isPlaceholderRune(r) {
			placeholderOnly = false
		}
	}
	return !placeholderOnly
}

func isPlaceholderRune(r rune) bool {
	switch r {
	case '\u25a0', '\u25a1', '\u25aa', '\u25ab', '\u25fb', '\u25fc':
		return true
	default:
		return false
	}
}
