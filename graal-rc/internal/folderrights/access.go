package folderrights

import (
	"encoding/csv"
	"fmt"
	"path"
	"strings"
)

// Access is the parsed folder-access list returned by openrights.
//
// An empty folder list has the same meaning as the server: unrestricted
// access. Non-empty lists are evaluated in order, with later entries able to
// remove rights granted by earlier entries.
type Access struct {
	unrestricted bool
	entries      []entry
}

type entry struct {
	pattern string
	r       bool
	w       bool
	deny    bool
}

// Parse parses the folder access representation used by Graal's openrights.
// It accepts both newline-separated entries and the quoted comma-separated
// representation used by some RC responses.
func Parse(raw string) (Access, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Access{unrestricted: true}, nil
	}

	entries, err := splitEntries(raw)
	if err != nil {
		return Access{}, err
	}

	access := Access{entries: make([]entry, 0, len(entries))}
	for _, line := range entries {
		parsed, ok := parseEntry(line)
		if ok {
			access.entries = append(access.entries, parsed)
		}
	}
	return access, nil
}

// CanRead reports whether the account can read the named script.
func (a Access) CanRead(scriptType, name string) bool {
	return a.has(scriptType, name, 'r')
}

// CanWrite reports whether the account can write the named script.
func (a Access) CanWrite(scriptType, name string) bool {
	return a.has(scriptType, name, 'w')
}

// ResourcePath converts a logical script type and name to the path checked by
// the RC server.
func ResourcePath(scriptType, name string) string {
	prefix := ""
	switch strings.ToLower(strings.TrimSpace(scriptType)) {
	case "weapon", "weapons":
		prefix = "WEAPONS/"
	case "class", "classes":
		prefix = "CLASSES/"
	case "npc", "npcs":
		prefix = "NPCS/"
	default:
		return ""
	}
	return prefix + strings.TrimSpace(name)
}

func (a Access) has(scriptType, name string, right rune) bool {
	if a.unrestricted {
		return true
	}

	resource := normalizePath(ResourcePath(scriptType, name))
	if resource == "" {
		return false
	}

	allowed := false
	for _, rule := range a.entries {
		matched, err := path.Match(rule.pattern, resource)
		if err != nil || !matched {
			continue
		}

		hasRight := (right == 'r' && rule.r) || (right == 'w' && rule.w)
		if rule.deny {
			if hasRight {
				allowed = false
			}
			continue
		}
		if hasRight {
			allowed = true
		}
	}
	return allowed
}

func splitEntries(raw string) ([]string, error) {
	if strings.ContainsAny(raw, "\r\n") {
		lines := strings.FieldsFunc(raw, func(r rune) bool {
			return r == '\r' || r == '\n'
		})
		return lines, nil
	}

	// Some responses serialize the folder list as CSV-like quoted values:
	// "rw WEAPONS/*","rw CLASSES/*".
	if strings.Contains(raw, ",") {
		reader := csv.NewReader(strings.NewReader(raw))
		reader.FieldsPerRecord = -1
		fields, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("parse folder access list: %w", err)
		}
		return fields, nil
	}

	return []string{raw}, nil
}

func parseEntry(raw string) (entry, bool) {
	line := strings.TrimSpace(raw)
	if line == "" {
		return entry{}, false
	}

	rights := "r"
	pattern := line
	if parts := strings.SplitN(line, " ", 2); len(parts) == 2 {
		rights = strings.ToLower(strings.TrimSpace(parts[0]))
		pattern = strings.TrimSpace(parts[1])
	}

	deny := strings.HasPrefix(rights, "-")
	rights = strings.TrimPrefix(rights, "-")
	pattern = normalizePath(pattern)
	if pattern == "" || strings.Contains(pattern, "..") || strings.Contains(pattern, ":") {
		return entry{}, false
	}
	if _, err := path.Match(pattern, ""); err != nil {
		return entry{}, false
	}

	parsed := entry{pattern: pattern, deny: deny}
	for _, right := range rights {
		switch right {
		case 'r':
			parsed.r = true
		case 'w':
			parsed.w = true
		}
	}
	if !parsed.r && !parsed.w {
		return entry{}, false
	}
	return parsed, true
}

func normalizePath(value string) string {
	return strings.ReplaceAll(strings.TrimLeft(strings.TrimSpace(value), "/"), "\\", "/")
}
