package sync

import (
	"regexp"
	"strings"
)

// Activity is a parsed server-side script event from an RC chat line.
type Activity struct {
	Kind   string // "weapon" | "class" | "npc" | "delete"
	Key    string // weapon/class name or stringified npc id; for delete, the name
	Name   string // display name (same as Key for weapon/class; npc name)
	Actor  string
	Action string // "added" | "updated" | "deleted"
}

// These regexes are empirical — wording varies slightly by server protocol.
// False negatives only delay detection to the next poll; they never cause a
// wrong mutation (the safety rules are independent of parsing). Examples:
//
//	"Weapon/GUI-script -den added/updated by Repinho"
//	"Script bizdoor_outside updated by Repinho"            (class)
//	"The script of NPC kek has been updated by Repinho"
//	"NPC DenNob has been added by Repinho"
//	"Script hehedenvnob deleted by Repinho"
var (
	reWeapon      = regexp.MustCompile(`(?i)^Weapon(?:/GUI-script)?\s+(.+?)\s+(added/updated|added|updated|deleted)\s+by\s+(.+)$`)
	reClass       = regexp.MustCompile(`(?i)^Script\s+(.+?)\s+updated\s+by\s+(.+)$`)
	reClassDelete = regexp.MustCompile(`(?i)^Script\s+(.+?)\s+deleted\s+by\s+(.+)$`)
	reNPCUpdate   = regexp.MustCompile(`(?i)^The script of NPC\s+(.+?)\s+has been updated\s+by\s+(.+)$`)
	reNPCAdd      = regexp.MustCompile(`(?i)^NPC\s+(.+?)\s+has been added\s+by\s+(.+)$`)
	reNPCDelete   = regexp.MustCompile(`(?i)^The NPC\s+(.+?)\s+has been deleted\s+by\s+(.+)$`)
)

// ParseChatLine extracts a script Activity from an RC chat line. ok is false if
// the line is not a recognized script-activity message.
func ParseChatLine(line string) (Activity, bool) {
	line = strings.TrimSpace(line)
	if marker := strings.Index(line, "[RC]"); marker >= 0 {
		line = strings.TrimSpace(line[marker+len("[RC]"):])
	}
	if m := reWeapon.FindStringSubmatch(line); m != nil {
		name, action, actor := m[1], m[2], m[3]
		if strings.Contains(action, "/") {
			action = "updated"
		}
		if strings.EqualFold(action, "deleted") {
			return Activity{Kind: "weapon", Key: name, Name: name, Actor: actor, Action: "deleted"}, true
		}
		return Activity{Kind: "weapon", Key: name, Name: name, Actor: actor, Action: action}, true
	}
	if m := reNPCDelete.FindStringSubmatch(line); m != nil {
		return Activity{Kind: "npc", Key: "", Name: m[1], Actor: m[2], Action: "deleted"}, true
	}
	if m := reNPCUpdate.FindStringSubmatch(line); m != nil {
		name, actor := m[1], m[2]
		return Activity{Kind: "npc", Key: "", Name: name, Actor: actor, Action: "updated"}, true
	}
	if m := reNPCAdd.FindStringSubmatch(line); m != nil {
		name, actor := m[1], m[2]
		return Activity{Kind: "npc", Key: "", Name: name, Actor: actor, Action: "added"}, true
	}
	if m := reClass.FindStringSubmatch(line); m != nil {
		name, actor := m[1], m[2]
		return Activity{Kind: "class", Key: name, Name: name, Actor: actor, Action: "updated"}, true
	}
	if m := reClassDelete.FindStringSubmatch(line); m != nil {
		name, actor := m[1], m[2]
		return Activity{Kind: "class", Key: name, Name: name, Actor: actor, Action: "deleted"}, true
	}
	return Activity{}, false
}
