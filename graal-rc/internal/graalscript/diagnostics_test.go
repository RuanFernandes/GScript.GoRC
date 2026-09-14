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

func TestDiagnosticsAllowMultilineNLStringConcatenation(t *testing.T) {
	text := `text = "<b>Conditions:</b>\n\t\te.g. adminlevel>0\n" NL
	"<b>Variable usage example:</b> email='skyld@graalonline.com', adminlevel=1, adminworlds like '%all%', blocked=1";`

	if diagnostics := parseDocument("memory://multiline-nl-string-concatenation", text, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("multiline NL string concatenation produced diagnostics: %#v", diagnostics)
	}
}

func TestDiagnosticsRequireSemicolonAfterMultilineNLStringConcatenation(t *testing.T) {
	text := `"first" NL
"second"
next = 1;`

	diagnostics := parseDocument("memory://multiline-nl-string-concatenation-missing-semicolon", text, 1).diagnostics()
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "Expected ';'") {
		t.Fatalf("diagnostics = %#v, want one missing semicolon after the concatenated string", diagnostics)
	}
	if diagnostics[0].Range.Start.Line != 1 {
		t.Fatalf("diagnostic = %#v, want the final string line", diagnostics[0])
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

func TestDiagnosticsAllowNestedGUIObjectBlocksWithoutSemicolons(t *testing.T) {
	text := `new GuiControl("Name") {
  this.width = 10;
  this.join("Test");
  new GuiButton("Child") {
    this.text = "Hello";
    new GuiText("Nested") {
      this.text = "World";
    }
  }
}`

	if diagnostics := parseDocument("memory://nested-guis", text, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("GUI object blocks produced diagnostics: %#v", diagnostics)
	}
}

func TestDiagnosticsAllowOptionalCaseBlockSemicolon(t *testing.T) {
	withoutSemicolon := `function onCreated() {
  switch (temp.a) {
    case b: {
      break;
    }
  }
}`
	if diagnostics := parseDocument("memory://case-block", withoutSemicolon, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("case block without semicolon produced diagnostics: %#v", diagnostics)
	}

	withSemicolon := `function onCreated() {
  switch (temp.a) {
    case b: {
      break;
    };
  }
}`
	if diagnostics := parseDocument("memory://case-block-semicolon", withSemicolon, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("case block with semicolon produced diagnostics: %#v", diagnostics)
	}
}

func TestDiagnosticsAllowEnumDeclarationWithoutSemicolon(t *testing.T) {
	empty := `enum A { }`
	if diagnostics := parseDocument("memory://empty-enum-without-semicolon", empty, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("empty enum declaration without semicolon produced diagnostics: %#v", diagnostics)
	}

	withoutSemicolon := `enum A {
  First,
  Second = 2
}
temp.value = 1;`
	if diagnostics := parseDocument("memory://enum-without-semicolon", withoutSemicolon, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("enum declaration without semicolon produced diagnostics: %#v", diagnostics)
	}

	withSemicolon := `enum A {
  First,
  Second = 2
};`
	if diagnostics := parseDocument("memory://enum-with-semicolon", withSemicolon, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("enum declaration with semicolon produced diagnostics: %#v", diagnostics)
	}
}

func TestDiagnosticsAllowInlineFunctionExpressions(t *testing.T) {
	text := `function onCreated() {
  temp.var = function () {
    temp.value = 1;
    if (temp.value == 1) {
      echo("ready");
    }
  };
}`

	if diagnostics := parseDocument("memory://inline-function", text, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("inline function expression produced diagnostics: %#v", diagnostics)
	}
}

func TestDiagnosticsRequireSemicolonsInsideInlineFunctionExpressions(t *testing.T) {
	text := `temp.var = function () {
  temp.value = 1
};`

	diagnostics := parseDocument("memory://inline-function-missing-semicolon", text, 1).diagnostics()
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "Expected ';'") {
		t.Fatalf("diagnostics = %#v, want one missing semicolon inside inline function", diagnostics)
	}
	if diagnostics[0].Range.Start.Line != 1 {
		t.Fatalf("diagnostic = %#v, want the inline function body line", diagnostics[0])
	}
}

func TestDiagnosticsRequireSemicolonsInsideInlineFunctionCallbacks(t *testing.T) {
	text := `register(function () {
  temp.value = 1
});`

	diagnostics := parseDocument("memory://inline-function-callback", text, 1).diagnostics()
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0].Message, "Expected ';'") {
		t.Fatalf("diagnostics = %#v, want one missing semicolon inside inline callback", diagnostics)
	}
	if diagnostics[0].Range.Start.Line != 1 {
		t.Fatalf("diagnostic = %#v, want the callback body line", diagnostics[0])
	}
}
