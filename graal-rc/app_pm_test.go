package main

import "testing"

func TestNormalizePMText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain text", input: "Oi", want: "Oi"},
		{name: "already decoded quoted text", input: `"Oi"`, want: `"Oi"`},
		{name: "comma text", input: `"Oi","Linha 2","Linha3",`, want: "Oi\nLinha 2\nLinha3"},
		{name: "json array", input: `["Oi","Linha 2","Linha3"]`, want: "Oi\nLinha 2\nLinha3"},
		{name: "comma inside line", input: `"Oi, tudo bem?","Linha 2"`, want: "Oi, tudo bem?\nLinha 2"},
		{name: "escaped quote", input: `"""Oi""","Linha 2"`, want: "\"Oi\"\nLinha 2"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizePMText(test.input); got != test.want {
				t.Fatalf("normalizePMText(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestRecordIncomingPMNormalizesText(t *testing.T) {
	app := &App{pmConversations: map[int]PMConversation{}}
	app.recordIncomingPM(42, "Graal6325743", "Testbed", `"Oi","Linha 2"`)

	state := app.pmSnapshot()
	if len(state.Conversations) != 1 || len(state.Conversations[0].Lines) != 1 {
		t.Fatalf("unexpected PM snapshot: %#v", state)
	}
	if got := state.Conversations[0].Lines[0].Text; got != "Oi\nLinha 2" {
		t.Fatalf("recorded PM text = %q, want %q", got, "Oi\nLinha 2")
	}
}
