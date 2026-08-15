package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCommandMacroFileRoundTripNormalizesServerKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "command-macros.json")
	macros := []CommandMacro{{
		ID:        "macro-1",
		Name:      " Jail ",
		Command:   " /npc jail ",
		CreatedAt: 10,
		UpdatedAt: 10,
	}}
	if err := persistCommandMacroFile(path, map[string][]CommandMacro{"zodiac": macros}); err != nil {
		t.Fatalf("persist command macros: %v", err)
	}

	got, err := loadCommandMacroFile(path)
	if err != nil {
		t.Fatalf("load command macros: %v", err)
	}
	if len(got["zodiac"]) != 1 || got["zodiac"][0].Name != "Jail" || got["zodiac"][0].Command != "/npc jail" {
		t.Fatalf("loaded macros = %+v", got)
	}
}

func TestCommandMacroFileRecoversCorruptPrimary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "command-macros.json")
	first := map[string][]CommandMacro{"zodiac": {{
		ID:        "macro-1",
		Name:      "Jail",
		Command:   "/npc jail",
		CreatedAt: 10,
		UpdatedAt: 10,
	}}}
	second := map[string][]CommandMacro{"era": {{
		ID:        "macro-2",
		Name:      "Ban",
		Command:   "/ban",
		CreatedAt: 20,
		UpdatedAt: 20,
	}}}
	if err := persistCommandMacroFile(path, first); err != nil {
		t.Fatalf("persist first command macros: %v", err)
	}
	if err := persistCommandMacroFile(path, second); err != nil {
		t.Fatalf("persist second command macros: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatalf("corrupt primary command macros: %v", err)
	}

	got, err := loadCommandMacroFile(path)
	if err != nil {
		t.Fatalf("recover command macros: %v", err)
	}
	if len(got["zodiac"]) != 1 || len(got["era"]) != 0 {
		t.Fatalf("recovered macros = %+v", got)
	}
}

func TestDecodeCommandMacroFileRejectsInvalidParameter(t *testing.T) {
	data, err := json.Marshal(commandMacroFile{Servers: map[string][]CommandMacro{
		"zodiac": {{
			ID:         "macro-1",
			Name:       "Jail",
			Command:    "/npc jail",
			Parameters: []CommandMacroParameter{{Name: "target", Type: "unknown"}},
			CreatedAt:  10,
			UpdatedAt:  10,
		}},
	}})
	if err != nil {
		t.Fatalf("marshal invalid command macros: %v", err)
	}
	if _, err := decodeCommandMacroFile(data); err == nil {
		t.Fatal("expected invalid parameter type to be rejected")
	}
}
