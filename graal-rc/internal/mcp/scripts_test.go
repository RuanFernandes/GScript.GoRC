package mcp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// toolText returns the text payload and the isError flag of a tools/call reply.
func toolText(t *testing.T, response map[string]any) (string, bool) {
	t.Helper()
	if response["error"] != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", response["error"])
	}
	result := response["result"].(map[string]any)
	text := result["content"].([]any)[0].(map[string]any)["text"].(string)
	return text, result["isError"].(bool)
}

func TestToolsListIncludesScriptToolsAndNoDelete(t *testing.T) {
	names := map[string]bool{}
	for _, tool := range toolDefinitions() {
		name := tool["name"].(string)
		if names[name] {
			t.Fatalf("tool %q is defined twice", name)
		}
		names[name] = true
		if strings.Contains(name, "delete") {
			t.Fatalf("delete must not be exposed through MCP, found %q", name)
		}
	}
	for _, want := range []string{
		"script_list", "script_read", "script_write", "weapon_add", "class_add", "npc_add",
		"npc_reset", "npc_warp", "npc_flags_read", "npc_flags_write", "npc_attributes_read",
		"script_refresh", "sync_status",
	} {
		if !names[want] {
			t.Fatalf("tool %q is missing", want)
		}
	}
}

func TestScriptListFiltersByTypeAndQuery(t *testing.T) {
	id := 1004
	backend := &fakeBackend{scripts: []ScriptEntry{
		{Type: "weapon", Name: "Staff/News"},
		{Type: "weapon", Name: "-System"},
		{Type: "npc", Name: "NewsBoard", ID: &id},
	}}
	text, isError := toolText(t, callTool(t, backend, "script_list", map[string]any{"type": "weapon", "query": "NEWS"}))
	if isError {
		t.Fatalf("script_list failed: %s", text)
	}
	if backend.listedKind != "weapon" {
		t.Fatalf("backend was asked for kind %q, want weapon", backend.listedKind)
	}
	var payload struct {
		Count   int           `json:"count"`
		Scripts []ScriptEntry `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatal(err)
	}
	// The fake returns every entry regardless of kind, so the query is what
	// narrows the result here.
	if payload.Count != 2 || payload.Scripts[0].Name != "Staff/News" || payload.Scripts[1].Name != "NewsBoard" {
		t.Fatalf("unexpected filtered list: %s", text)
	}
}

func TestScriptWriteAddressesNPCByIDAndReportsServerApplied(t *testing.T) {
	backend := &fakeBackend{}
	text, isError := toolText(t, callTool(t, backend, "script_write", map[string]any{"type": "npc", "id": 1004, "script": "function onCreated() {}"}))
	if isError {
		t.Fatalf("script_write failed: %s", text)
	}
	if !backend.wroteRef.HasID || backend.wroteRef.ID != 1004 || backend.wroteRef.Type != "npc" {
		t.Fatalf("unexpected reference: %+v", backend.wroteRef)
	}
	if backend.wroteScript != "function onCreated() {}" {
		t.Fatalf("script was not passed through: %q", backend.wroteScript)
	}
	if !strings.Contains(text, `"action":"updated"`) || !strings.Contains(text, "get_rc_chat") {
		t.Fatalf("reply should confirm the update and point at get_rc_chat: %s", text)
	}
}

func TestAddToolsBuildTheRightSpec(t *testing.T) {
	backend := &fakeBackend{}
	if text, isError := toolText(t, callTool(t, backend, "weapon_add", map[string]any{"name": " -Shop ", "script": "//#CLIENTSIDE"})); isError {
		t.Fatalf("weapon_add failed: %s", text)
	}
	if backend.added.Type != "weapon" || backend.added.Name != "-Shop" || backend.added.Script != "//#CLIENTSIDE" {
		t.Fatalf("unexpected weapon spec: %+v", backend.added)
	}

	if text, isError := toolText(t, callTool(t, backend, "class_add", map[string]any{"name": "door"})); isError {
		t.Fatalf("class_add failed: %s", text)
	}
	if backend.added.Type != "class" || backend.added.Name != "door" || backend.added.Script != "" {
		t.Fatalf("unexpected class spec: %+v", backend.added)
	}

	if text, isError := toolText(t, callTool(t, backend, "npc_add", map[string]any{"name": "Guard", "level": "town.nw", "x": 30.5, "y": 12})); isError {
		t.Fatalf("npc_add failed: %s", text)
	}
	if backend.added.Type != "npc" || backend.added.HasID || backend.added.Level != "town.nw" || backend.added.X != "30.5" || backend.added.Y != "12" {
		t.Fatalf("unexpected NPC spec: %+v", backend.added)
	}

	if text, isError := toolText(t, callTool(t, backend, "npc_add", map[string]any{"name": "Guard2", "id": 1200})); isError {
		t.Fatalf("npc_add with id failed: %s", text)
	}
	if !backend.added.HasID || backend.added.ID != 1200 || backend.added.X != "0" || backend.added.Y != "0" {
		t.Fatalf("unexpected NPC spec with id: %+v", backend.added)
	}
}

func TestScriptToolsRejectBadArguments(t *testing.T) {
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"script_read", map[string]any{"name": "door"}},                   // no type
		{"script_read", map[string]any{"type": "level", "name": "door"}},  // unknown type
		{"script_read", map[string]any{"type": "weapon"}},                 // no name
		{"script_read", map[string]any{"type": "npc"}},                    // no id or name
		{"script_write", map[string]any{"type": "weapon", "name": "x"}},   // no script
		{"weapon_add", map[string]any{"name": "   "}},                     // blank name
		{"npc_add", map[string]any{"name": "Guard", "id": 1.5}},           // fractional id
		{"npc_add", map[string]any{"name": "Guard", "x": "left"}},         // non-numeric x
		{"npc_warp", map[string]any{"id": 1, "level": "town.nw", "x": 1}}, // no y
		{"npc_flags_write", map[string]any{"id": 1}},                      // no flags
		{"script_list", map[string]any{"type": "levels"}},                 // unknown type
		{"npc_reset", map[string]any{"id": -1}},                           // negative id
	}
	for _, tc := range cases {
		backend := &fakeBackend{}
		response := callTool(t, backend, tc.tool, tc.args)
		if response["error"] == nil {
			t.Errorf("%s %v: expected an invalid-arguments error, got %v", tc.tool, tc.args, response["result"])
		}
		if backend.calls != 0 {
			t.Errorf("%s %v: the backend was called despite bad arguments", tc.tool, tc.args)
		}
	}
}

func TestScriptToolReportsBackendFailureAsToolError(t *testing.T) {
	backend := &fakeBackend{scriptErr: errors.New("weapon \"x\" already exists; use script_write to change it")}
	text, isError := toolText(t, callTool(t, backend, "weapon_add", map[string]any{"name": "x"}))
	if !isError || !strings.Contains(text, "already exists") {
		t.Fatalf("backend failure should be a tool error carrying the message, got isError=%v text=%s", isError, text)
	}
}

func TestActionsWithoutPayloadReportSuccess(t *testing.T) {
	backend := &fakeBackend{}
	for tool, args := range map[string]map[string]any{
		"send_rc_chat":    {"message": "hello"},
		"filebrowser_cd":  {"path": "weapons/"},
		"npc_reset":       {"id": 7},
		"npc_warp":        {"name": "Guard", "level": "town.nw", "x": 1, "y": 2},
		"npc_flags_write": {"id": 7, "flags": "a=1"},
		"script_refresh":  {},
	} {
		text, isError := toolText(t, callTool(t, backend, tool, args))
		if isError || !strings.Contains(text, `"ok":true`) {
			t.Errorf("%s: a successful action must not be reported as an error, got isError=%v text=%s", tool, isError, text)
		}
	}
}
