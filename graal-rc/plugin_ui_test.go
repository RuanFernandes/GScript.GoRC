package main

import (
	"strings"
	"testing"
)

func TestValidatePluginUITabOptions(t *testing.T) {
	valid := PluginUITabOptions{
		ID:    "dashboard",
		Title: "Plugin dashboard",
		Icon:  "dashboard",
		View: map[string]any{
			"type": "stack",
			"children": []any{
				map[string]any{"type": "heading", "text": "Ready"},
				map[string]any{"type": "button", "id": "refresh", "label": "Refresh", "action": "refresh"},
			},
		},
	}
	if err := validatePluginUITabOptions(valid); err != nil {
		t.Fatalf("valid tab was rejected: %v", err)
	}

	for name, tab := range map[string]PluginUITabOptions{
		"missing id": {Title: "Tab", View: valid.View},
		"bad icon":   {ID: "tab", Title: "Tab", Icon: "unknown", View: valid.View},
		"bad tab id": {ID: "one:two", Title: "Tab", View: valid.View},
		"bad view":   {ID: "tab", Title: "Tab", View: map[string]any{"type": "unknown"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validatePluginUITabOptions(tab); err == nil {
				t.Fatal("invalid tab was accepted")
			}
		})
	}
}

func TestValidatePluginUIViewLimitsAndShapes(t *testing.T) {
	if err := validatePluginUIView(map[string]any{"type": "textarea", "id": "notes", "label": "Notes", "rows": 4}); err != nil {
		t.Fatalf("valid textarea was rejected: %v", err)
	}
	if err := validatePluginUIView(map[string]any{"type": "progress", "value": 25, "max": 100, "label": "Work"}); err != nil {
		t.Fatalf("valid progress was rejected: %v", err)
	}
	if err := validatePluginUIView(map[string]any{"type": "button", "id": "run", "label": "Run"}); err == nil {
		t.Fatal("button without action was accepted")
	}
	if err := validatePluginUIView(map[string]any{"type": "table", "columns": []any{}, "rows": []any{}}); err == nil {
		t.Fatal("table without columns was accepted")
	}
	if err := validatePluginUIView(strings.Repeat("x", maxPluginUIViewBytes+1)); err == nil {
		t.Fatal("oversized view was accepted")
	}
	for _, value := range []any{nil, "hello", true, 42.0} {
		if err := validatePluginUIValue(value); err != nil {
			t.Fatalf("primitive action value was rejected (%v): %v", value, err)
		}
	}
	if err := validatePluginUIValue(map[string]any{"unexpected": true}); err == nil {
		t.Fatal("object action value was accepted")
	}
}
