package graalscript

import (
	"strings"
	"testing"
)

func TestDiagnosticsRequireSemicolonsForStatements(t *testing.T) {
	text := `function onCreated() {
  temp.array = {};
  echo("ready");
  if (temp.a == 10 ||
      temp.b == 20) {
    temp.value = 1;
  }
  for (temp.i = 0; temp.i < 2; temp.i++) {
    echo(temp.i);
  }
  switch (temp.a) {
    case 10:
      echo("ten");
      break;
    default:
      echo("other");
  }
}`

	if diagnostics := parseDocument("memory://valid-semicolons", text, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("valid statements produced diagnostics: %#v", diagnostics)
	}
}

func TestDiagnosticsReportMissingSemicolonsWithoutFlaggingControlBlocks(t *testing.T) {
	text := `function onCreated() {
  temp.value = 10
  echo("missing")
  if (temp.a == 10 ||
      temp.b == 20) {
    temp.other = 20
  }
  temp.array = {}
}`

	diagnostics := parseDocument("memory://missing-semicolons", text, 1).diagnostics()
	if len(diagnostics) != 4 {
		t.Fatalf("diagnostics = %#v, want four missing semicolons", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if !strings.Contains(diagnostic.Message, "Expected ';'") {
			t.Fatalf("unexpected diagnostic: %#v", diagnostic)
		}
	}
}
