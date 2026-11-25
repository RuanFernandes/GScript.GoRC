package main

import (
	"strings"
	"testing"

	"graal-rc/internal/mcp"
	"graal-rc/rclib"
)

func TestWithMCPInstructionsAppendsAfterUserContent(t *testing.T) {
	block := strings.TrimLeft(mcpInstructionBlock(), "\n")

	if got, err := withMCPInstructions(""); err != nil || got != block {
		t.Fatalf("empty file: got %q, err %v", got, err)
	}

	got, err := withMCPInstructions("# My rules\nBe brief.")
	if err != nil {
		t.Fatal(err)
	}
	if want := "# My rules\nBe brief.\n\n" + block; got != want {
		t.Fatalf("append:\n got %q\nwant %q", got, want)
	}
}

func TestWithMCPInstructionsReplacesAnOlderBlockInPlace(t *testing.T) {
	block := strings.TrimLeft(mcpInstructionBlock(), "\n")
	old := "#-----------#\n#Nullborne Go RC INSTRUCTIONS START#\n\n- get_rc_chat: old text\n#END\n#-----------#\n"

	for name, lineBreak := range map[string]string{"LF": "\n", "CRLF": "\r\n"} {
		before := strings.ReplaceAll("# My rules\nBe brief.\n\n", "\n", lineBreak)
		after := strings.ReplaceAll("\n# Other tool\nKeep this.\n", "\n", lineBreak)
		content := before + strings.ReplaceAll(old, "\n", lineBreak) + after

		got, err := withMCPInstructions(content)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if want := before + block + after; got != want {
			t.Fatalf("%s:\n got %q\nwant %q", name, got, want)
		}
		if strings.Contains(got, "old text") {
			t.Fatalf("%s: the old block survived", name)
		}
		if strings.Count(got, mcpInstructionsStart) != 1 {
			t.Fatalf("%s: expected exactly one block", name)
		}
		// Running the setup again must not change an up-to-date file.
		again, err := withMCPInstructions(got)
		if err != nil || again != got {
			t.Fatalf("%s: second run changed the file (err %v)", name, err)
		}
	}
}

func TestWithMCPInstructionsRefusesABlockWithoutEnd(t *testing.T) {
	content := "# My rules\n#-----------#\n#Nullborne Go RC INSTRUCTIONS START#\nhalf a block\n\n# Something the user wrote later\n"
	if _, err := withMCPInstructions(content); err == nil {
		t.Fatal("a block without #END must not be replaced, since its end is unknown")
	}
}

func TestMCPInstructionsDocumentEveryToolAndTheSyncRule(t *testing.T) {
	block := mcpInstructionBlock()
	for _, tool := range mcp.ToolNames() {
		if !strings.Contains(block, tool) {
			t.Errorf("the instruction block does not mention the %s tool", tool)
		}
	}
	for _, phrase := range []string{"at least one character changed", "sync_status", "get_rc_chat", "no delete tool"} {
		if !strings.Contains(block, phrase) {
			t.Errorf("the instruction block lost the sync explanation: %q is missing", phrase)
		}
	}
}

func TestFindNPCPrefersIDAndRejectsAmbiguousNames(t *testing.T) {
	npcs := []rclib.NPC{{ID: 1000, Name: "Guard"}, {ID: 1001, Name: "Guard"}, {ID: 1002, Name: "Door"}}

	if npc, err := findNPC(npcs, mcp.ScriptRef{Type: "npc", ID: 1001, HasID: true}); err != nil || npc.ID != 1001 {
		t.Fatalf("by id: got %+v, err %v", npc, err)
	}
	if npc, err := findNPC(npcs, mcp.ScriptRef{Type: "npc", Name: "Door"}); err != nil || npc.ID != 1002 {
		t.Fatalf("by unique name: got %+v, err %v", npc, err)
	}
	if _, err := findNPC(npcs, mcp.ScriptRef{Type: "npc", Name: "Guard"}); err == nil || !strings.Contains(err.Error(), "pass id") {
		t.Fatalf("an ambiguous name must ask for the id, got %v", err)
	}
	if _, err := findNPC(npcs, mcp.ScriptRef{Type: "npc", Name: "door"}); err == nil {
		t.Fatal("NPC names are case-sensitive")
	}
	if _, err := findNPC(npcs, mcp.ScriptRef{Type: "npc", ID: 9, HasID: true}); err == nil {
		t.Fatal("an unknown id must fail")
	}
}
