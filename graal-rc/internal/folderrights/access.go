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

// Equal reports whether two parsed snapshots have the same effective rules.
// Entry order is part of the server semantics because later rules can revoke
// or restore rights granted by earlier entries.
func (a Access) Equal(other Access) bool {
	if a.unrestricted != other.unrestricted || len(a.entries) != len(other.entries) {
		return false
	}
	for i, entry := range a.entries {
		otherEntry := other.entries[i]
		if entry != otherEntry {
			return false
		}
	}
	return true
}

// CanRead reports whether the account can read the named script.
func (a Access) CanRead(scriptType, name string) bool {
	return a.has(scriptType, name, 'r')
}

// CanWrite reports whether the account can write the named script.
func (a Access) CanWrite(scriptType, name string) bool {
	return a.has(scriptType, name, 'w')
}

// HasWriteAccessForScriptTypes reports whether this snapshot contains a write
// rule that can target one of the requested script directories. The check is
// intentionally conservative for wildcard rules: when an account may write
// anywhere under WEAPONS, CLASSES, or NPCS, Local Sync must be enabled even if
// the NC list has not finished warming up yet.
//
// This method is used to protect the local workspace before a concrete script
// name is known. It does not replace CanWrite, which remains the authoritative
// check for an individual server request.
func (a Access) HasWriteAccessForScriptTypes(scriptTypes ...string) bool {
	if a.unrestricted {
		return true
	}

	for _, rule := range a.entries {
		if rule.deny || !rule.w {
			continue
		}
		for _, scriptType := range scriptTypes {
			prefix := resourcePrefix(scriptType)
			if prefix == "" {
				continue
			}
			if writeRuleTargetsPrefix(rule.pattern, prefix) {
				return true
			}
		}
	}
	return false
}

// ResourcePath converts a logical script type and name to the path checked by
// the RC server.
func ResourcePath(scriptType, name string) string {
	prefix := resourcePrefix(scriptType)
	if prefix == "" {
		return ""
	}
	return prefix + strings.TrimSpace(name)
}

func resourcePrefix(scriptType string) string {
	switch strings.ToLower(strings.TrimSpace(scriptType)) {
	case "weapon", "weapons":
		return "WEAPONS/"
	case "class", "classes":
		return "CLASSES/"
	case "npc", "npcs":
		return "NPCS/"
	default:
		return ""
	}
}

func writeRuleTargetsPrefix(pattern, prefix string) bool {
	pattern = normalizePath(pattern)
	if pattern == "" {
		return false
	}

	// Most server responses use rules such as WEAPONS/* or
	// WEAPONS/MyWeapon. This fast path also catches exact writable scripts that
	// are not present in the NC list yet.
	if pattern == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(pattern, prefix) {
		return true
	}

	// A broad rule such as * or */WEAPONS/* can still match a resource below the
	// requested directory. Probe a safe synthetic name instead of treating an
	// arbitrary wildcard as a grant for every script type.
	probe := prefix + "__gorc_write_permission_probe__"
	matched, err := path.Match(pattern, probe)
	return err == nil && matched
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
