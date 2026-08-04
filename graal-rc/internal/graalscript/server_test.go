package graalscript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDocumentCollectsFunctionsJoinsVariablesAndWithBlocks(t *testing.T) {
	text := `join("inventory");
function onPlayerChats(message, count) {
    temp.value = message;
    with (findplayer2("Ruan")) {
        client.marker = true;
    }
}
`
	doc := parseDocument("memory://test", text, 4)
	if len(doc.Functions) != 1 {
		t.Fatalf("functions = %d, want 1", len(doc.Functions))
	}
	if doc.Functions[0].Name != "onPlayerChats" || len(doc.Functions[0].Params) != 2 {
		t.Fatalf("function = %#v", doc.Functions[0])
	}
	if len(doc.Joins) != 1 || doc.Joins[0] != "inventory" {
		t.Fatalf("joins = %#v", doc.Joins)
	}
	if len(doc.Variables) != 1 || doc.Variables[0].Name != "value" {
		t.Fatalf("variables = %#v", doc.Variables)
	}
	if len(doc.With) != 1 || !strings.Contains(doc.With[0].Receiver, "findplayer2") {
		t.Fatalf("with blocks = %#v", doc.With)
	}
}

func TestCatalogParsesScriptHelpDefinitionsWithEmptyFunctionType(t *testing.T) {
	data := `{
        "setTimer": {"name":"setTimer", "type":"", "params":[], "returns":"void", "scope":"global"},
        "player.account": {"name":"player.account", "type":"variable", "params":[], "returns":"string", "scope":"global"}
    }`
	entries, err := parseDefinitions([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	catalog := newCatalog()
	for _, entry := range entries {
		catalog.add(entry)
	}
	setTimer, ok := catalog.lookup("setTimer")
	if !ok || setTimer.Kind != "function" {
		t.Fatalf("setTimer = %#v, found=%v; want function", setTimer, ok)
	}
	account, ok := catalog.lookup("player.account")
	if !ok || account.Kind != "variable" {
		t.Fatalf("player.account = %#v, found=%v; want variable", account, ok)
	}
	if members := catalog.members("player"); !hasDefinition(members, "account") {
		t.Fatalf("player members do not contain account: %#v", members)
	}
}

func TestLanguageServerCompletionHoverAndSignature(t *testing.T) {
	server := NewLanguageServer()
	open := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"memory://test","languageId":"graalscript","version":1,"text":"function heal(target) {\n  echo(\"x\");\n  temp.va\n  player.ac\n}"}}}`
	if response, err := server.HandleJSON([]byte(open)); err != nil || response != nil {
		t.Fatalf("didOpen response=%s err=%v", response, err)
	}
	completion := `{"jsonrpc":"2.0","id":2,"method":"textDocument/completion","params":{"textDocument":{"uri":"memory://test"},"position":{"line":2,"character":10}}}`
	response, err := server.HandleJSON([]byte(completion))
	if err != nil {
		t.Fatal(err)
	}
	var completionResponse jsonRPCResponse
	if err := json.Unmarshal(response, &completionResponse); err != nil {
		t.Fatal(err)
	}
	var list CompletionList
	resultBytes, err := json.Marshal(completionResponse.Result)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(resultBytes, &list); err != nil {
		t.Fatal(err)
	}
	if !hasCompletion(list.Items, "va") {
		t.Fatalf("temp completion does not contain va: %#v", list.Items)
	}

	hoverRequest := `{"jsonrpc":"2.0","id":3,"method":"textDocument/hover","params":{"textDocument":{"uri":"memory://test"},"position":{"line":1,"character":3}}}`
	response, err = server.HandleJSON([]byte(hoverRequest))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), "echo") {
		t.Fatalf("hover response does not contain echo: %s", response)
	}

	signatureRequest := `{"jsonrpc":"2.0","id":4,"method":"textDocument/signatureHelp","params":{"textDocument":{"uri":"memory://test"},"position":{"line":1,"character":8}}}`
	response, err = server.HandleJSON([]byte(signatureRequest))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), "echo(text)") {
		t.Fatalf("signature response does not contain echo(text): %s", response)
	}
}

func TestFunctionDeclarationCompletionOnlyOffersEvents(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://function-declaration"
	text := "function "
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 0, Character: utf16Length(text)},
	})
	if !hasCompletion(list.Items, "onCreated") {
		t.Fatalf("function declaration completion does not contain onCreated: %#v", list.Items)
	}
	if hasCompletion(list.Items, "abs") || hasCompletion(list.Items, "floor") || hasCompletion(list.Items, "arraylen") {
		t.Fatalf("function declaration completion leaked global functions: %#v", list.Items)
	}
	for _, item := range list.Items {
		if !strings.HasPrefix(strings.ToLower(item.Label), "on") {
			t.Fatalf("function declaration completion contains non-event %q: %#v", item.Label, list.Items)
		}
	}
}

func TestTempCompletionIsScopedToCurrentFunction(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://temp-scope"
	text := `function onCreated() {
  temp.cu = hi;
  temp.
}
function onOther() {
  for (temp.command: values) {}
}`
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	line := "  temp."
	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 2, Character: utf16Length(line)},
	})
	if !hasCompletion(list.Items, "cu") {
		t.Fatalf("current function temp completion does not contain cu: %#v", list.Items)
	}
	if hasCompletion(list.Items, "command") {
		t.Fatalf("temp completion leaked variable from another function: %#v", list.Items)
	}
}

func TestThisMemberCompletionRespectsServerAndClientSections(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://this-side"
	text := `function onCreated() {
  this.serverOnly = true;
  this.shared = true;
  this.
}
//#CLIENTSIDE
function onCreatedClient() {
  this.clientOnly = true;
  this.shared = true;
  this.
}`
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	serverList := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: utf16Length("  this.")},
	})
	if !hasCompletion(serverList.Items, "serverOnly") || !hasCompletion(serverList.Items, "shared") {
		t.Fatalf("server-side this completion is missing members: %#v", serverList.Items)
	}
	if hasCompletion(serverList.Items, "clientOnly") {
		t.Fatalf("server-side this completion leaked client member: %#v", serverList.Items)
	}

	clientList := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 9, Character: utf16Length("  this.")},
	})
	if !hasCompletion(clientList.Items, "clientOnly") || !hasCompletion(clientList.Items, "shared") {
		t.Fatalf("client-side this completion is missing members: %#v", clientList.Items)
	}
	if hasCompletion(clientList.Items, "serverOnly") {
		t.Fatalf("client-side this completion leaked server member: %#v", clientList.Items)
	}
}

func TestLanguageServerReportsUnclosedDelimiters(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://diagnostics"
	text := "function onCreated() {\n  echo(\"ok\";\n"
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	request := `{"jsonrpc":"2.0","id":5,"method":"textDocument/diagnostic","params":{"textDocument":{"uri":"memory://diagnostics"}}}`
	response, err := server.HandleJSON([]byte(request))
	if err != nil {
		t.Fatal(err)
	}
	var envelope jsonRPCResponse
	if err := json.Unmarshal(response, &envelope); err != nil {
		t.Fatal(err)
	}
	var report DocumentDiagnosticReport
	resultBytes, err := json.Marshal(envelope.Result)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(resultBytes, &report); err != nil {
		t.Fatal(err)
	}
	if report.Kind != "full" || len(report.Items) != 2 {
		t.Fatalf("diagnostic report = %#v, want two unclosed delimiters", report)
	}
	if !strings.Contains(report.Items[0].Message, `expected "}"`) {
		t.Fatalf("first diagnostic = %#v", report.Items[0])
	}
	if !strings.Contains(report.Items[1].Message, `expected ")"`) {
		t.Fatalf("second diagnostic = %#v", report.Items[1])
	}
}

func TestDelimiterDiagnosticsIgnoreStringsAndComments(t *testing.T) {
	text := "function onCreated() {\n  echo(\"} ] )\"); // { [ (\n}\n"
	if diagnostics := parseDocument("memory://balanced", text, 1).diagnostics(); len(diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, delimiters inside strings/comments should be ignored", diagnostics)
	}
}

func TestLanguageServerAppliesRangedChanges(t *testing.T) {
	server := NewLanguageServer()
	open := `{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"memory://change","languageId":"graalscript","version":1,"text":"temp.old"}}}`
	if response, err := server.HandleJSON([]byte(open)); err != nil || response != nil {
		t.Fatalf("didOpen response=%s err=%v", response, err)
	}
	change := `{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":"memory://change","version":2},"contentChanges":[{"range":{"start":{"line":0,"character":5},"end":{"line":0,"character":8}},"text":"new"}]}}`
	if response, err := server.HandleJSON([]byte(change)); err != nil || response != nil {
		t.Fatalf("didChange response=%s err=%v", response, err)
	}
	doc := server.workspace.document("memory://change")
	if doc == nil {
		t.Fatal("document was removed")
	}
	if doc.Text != "temp.new" {
		t.Fatalf("document text = %q, want temp.new", doc.Text)
	}
}

func TestWorkspaceResolvesPublicNPCFunctionsFromFindNpcAndStringReceiver(t *testing.T) {
	root := t.TempDir()
	npcDir := filepath.Join(root, "npcs")
	if err := os.MkdirAll(npcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npc := `function onCreated() {}
public function sell(item, quantity) {}
private function secret() {}
//#CLIENTSIDE
public function clientOnly() {}
`
	if err := os.WriteFile(filepath.Join(npcDir, "Shop.gs2"), []byte(npc), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://npc-context"
	text := "findNpc(\"Shop\").se"
	server.workspace.upsert(uri, parseDocument(uri, text, 1))
	list := server.completion(CompletionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 0, Character: utf16Length(text)}})
	if !hasCompletion(list.Items, "sell") {
		t.Fatalf("findNpc completion does not contain public sell: %#v", list.Items)
	}
	if hasCompletion(list.Items, "secret") || hasCompletion(list.Items, "clientOnly") {
		t.Fatalf("findNpc completion leaked non-server functions: %#v", list.Items)
	}

	text = `("Shop").se`
	server.workspace.upsert(uri, parseDocument(uri, text, 2))
	list = server.completion(CompletionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 0, Character: utf16Length(text)}})
	if !hasCompletion(list.Items, "sell") {
		t.Fatalf("string receiver completion does not contain public sell: %#v", list.Items)
	}
}

func TestWorkspaceResolvesNPCFromTempAndThisVariables(t *testing.T) {
	root := t.TempDir()
	npcDir := filepath.Join(root, "npcs")
	if err := os.MkdirAll(npcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npc := `public function sell(item) {}
private function secret() {}
//#CLIENTSIDE
public function clientOnly() {}
`
	if err := os.WriteFile(filepath.Join(npcDir, "Ruan.gs2"), []byte(npc), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://npc-variable-context"
	text := `function onCreated() {
  temp.n = findnpc("Ruan");
  temp.nname = "Ruan";
  temp.npc = findnpc(temp.nname);
  temp.n.se
}`
	server.workspace.upsert(uri, parseDocument(uri, text, 1))
	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 4, Character: utf16Length("  temp.n.se")},
	})
	if !hasCompletion(list.Items, "sell") {
		t.Fatalf("temp NPC completion does not contain public sell: %#v", list.Items)
	}
	if hasCompletion(list.Items, "secret") || hasCompletion(list.Items, "clientOnly") {
		t.Fatalf("temp NPC completion leaked non-server functions: %#v", list.Items)
	}

	text = `function onCreated() {
  temp.nname = "Ruan";
  this.dbnpc = findnpc(temp.nname);
  this.dbnpc.se
}`
	server.workspace.upsert(uri, parseDocument(uri, text, 2))
	list = server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: utf16Length("  this.dbnpc.se")},
	})
	if !hasCompletion(list.Items, "sell") {
		t.Fatalf("this NPC completion does not contain public sell: %#v", list.Items)
	}
	if hasCompletion(list.Items, "secret") || hasCompletion(list.Items, "clientOnly") {
		t.Fatalf("this NPC completion leaked non-server functions: %#v", list.Items)
	}

	text = `function onCreated() {
  temp.nname = "Ruan";
  findnpc(temp.nname).se
}`
	server.workspace.upsert(uri, parseDocument(uri, text, 3))
	list = server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 2, Character: utf16Length("  findnpc(temp.nname).se")},
	})
	if !hasCompletion(list.Items, "sell") {
		t.Fatalf("variable argument NPC completion does not contain public sell: %#v", list.Items)
	}
}

func TestDynamicVariableCompletionIncludesKnownKeys(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://dynamic-completion"
	base := "function onCreated() {\n" +
		"  temp.(\"alpha\") = 10;\n" +
		"  temp.(@\"beta\") = true;\n" +
		"  this.(\"shared\") = \"text\";\n" +
		"  this.(@\"npc\") = findnpc(\"Ruan\");\n" +
		"}"
	text := base[:len(base)-1] + "  temp.(\""
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	tempPrefix := "  temp.(\""
	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 5, Character: utf16Length(tempPrefix)},
	})
	if !hasCompletion(list.Items, "alpha") || !hasCompletion(list.Items, "beta") {
		t.Fatalf("dynamic temp completion is missing known keys: %#v", list.Items)
	}
	if hasCompletion(list.Items, "shared") || hasCompletion(list.Items, "npc") {
		t.Fatalf("dynamic temp completion leaked this keys: %#v", list.Items)
	}

	text = base[:len(base)-1] + "  this.(@\""
	server.workspace.upsert(uri, parseDocument(uri, text, 2))
	thisPrefix := "  this.(@\""
	list = server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 5, Character: utf16Length(thisPrefix)},
	})
	if !hasCompletion(list.Items, "shared") || !hasCompletion(list.Items, "npc") {
		t.Fatalf("dynamic this completion is missing known keys: %#v", list.Items)
	}
	if hasCompletion(list.Items, "alpha") || hasCompletion(list.Items, "beta") {
		t.Fatalf("dynamic this completion leaked temp keys: %#v", list.Items)
	}
}

func TestDynamicVariableCompletionShowsFullAccessSyntax(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://dynamic-completion-syntax"
	text := "function onCreated() {\n" +
		"  this.(@\"var\") = 10;\n" +
		"  this.static = 20;\n" +
		"  this.\n" +
		"}"
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: utf16Length("  this.")},
	})

	var dynamicItem *CompletionItem
	var staticItem *CompletionItem
	for i := range list.Items {
		switch list.Items[i].Label {
		case `(@"var")`:
			dynamicItem = &list.Items[i]
		case "static":
			staticItem = &list.Items[i]
		}
	}
	if dynamicItem == nil {
		t.Fatalf("dynamic variable completion does not show full access syntax: %#v", list.Items)
	}
	if dynamicItem.InsertText != `(@"var")` || dynamicItem.TextEdit == nil || dynamicItem.TextEdit.NewText != `(@"var")` {
		t.Fatalf("dynamic variable completion insertion = %#v, want (@\"var\")", dynamicItem)
	}
	if staticItem == nil || staticItem.InsertText != "static" {
		t.Fatalf("static member completion was changed unexpectedly: %#v", list.Items)
	}
}

func TestDynamicVariableCompletionPreservesExpression(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://dynamic-completion-expression"
	text := "function onCreated() {\n" +
		"  temp.nname = \"Ruan\";\n" +
		"  temp.(@\"item_\"@temp.nname) = 1;\n" +
		"  temp.\n" +
		"}"
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 3, Character: utf16Length("  temp.")},
	})

	var expressionItem *CompletionItem
	for i := range list.Items {
		if list.Items[i].Label == `(@"item_"@temp.nname)` {
			expressionItem = &list.Items[i]
			break
		}
	}
	if expressionItem == nil {
		t.Fatalf("dynamic completion lost the original expression: %#v", list.Items)
	}
	if expressionItem.TextEdit == nil || expressionItem.TextEdit.NewText != `(@"item_"@temp.nname)` {
		t.Fatalf("dynamic completion insertion = %#v, want (@\"item_\"@temp.nname)", expressionItem)
	}
	if hasCompletion(list.Items, `(@"item_Ruan")`) {
		t.Fatalf("dynamic completion exposed the resolved value instead of the expression: %#v", list.Items)
	}
}

func TestCompletionPlacesVariablesBeforeFunctions(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://completion-order"
	server.workspace.upsert(uri, parseDocument(uri, "s", 1))

	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 0, Character: 1},
	})
	serverIndex := -1
	setTimerIndex := -1
	for index, item := range list.Items {
		switch item.Label {
		case "server":
			serverIndex = index
		case "setTimer":
			setTimerIndex = index
		}
	}
	if serverIndex < 0 || setTimerIndex < 0 {
		t.Fatalf("completion order test entries are missing: %#v", list.Items)
	}
	if serverIndex >= setTimerIndex {
		t.Fatalf("variable completion index = %d, function completion index = %d; want variable first: %#v", serverIndex, setTimerIndex, list.Items)
	}
}

func TestDynamicNPCVariablesResolveWorkspaceFunctions(t *testing.T) {
	root := t.TempDir()
	npcDir := filepath.Join(root, "npcs")
	if err := os.MkdirAll(npcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npc := "public function sell(item) {}\n" +
		"private function secret() {}\n" +
		"//#CLIENTSIDE\n" +
		"public function clientOnly() {}\n"
	if err := os.WriteFile(filepath.Join(npcDir, "Ruan.gs2"), []byte(npc), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://dynamic-npc"
	text := "function onCreated() {\n" +
		"  temp.(\"npc\") = findnpc(\"Ruan\");\n" +
		"  this.(@\"otherNpc\") = findnpc(\"Ruan\");\n" +
		"  // The receiver lookup must survive filtered comments.\n" +
		"  temp.(\"npc\").se\n" +
		"  this.(@\"otherNpc\").se\n" +
		"}"
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 4, Character: utf16Length("  temp.(\"npc\").se")},
	})
	if !hasCompletion(list.Items, "sell") {
		t.Fatalf("static dynamic NPC receiver does not contain sell: %#v", list.Items)
	}
	if hasCompletion(list.Items, "secret") || hasCompletion(list.Items, "clientOnly") {
		t.Fatalf("static dynamic NPC receiver leaked non-server functions: %#v", list.Items)
	}

	list = server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 5, Character: utf16Length("  this.(@\"otherNpc\").se")},
	})
	if !hasCompletion(list.Items, "sell") {
		t.Fatalf("@ dynamic NPC receiver does not contain sell: %#v", list.Items)
	}
	if hasCompletion(list.Items, "secret") || hasCompletion(list.Items, "clientOnly") {
		t.Fatalf("@ dynamic NPC receiver leaked non-server functions: %#v", list.Items)
	}
}

func TestDynamicVariableCandidatesFromForeachAndConcatenation(t *testing.T) {
	root := t.TempDir()
	npcDir := filepath.Join(root, "npcs")
	if err := os.MkdirAll(npcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	npc := "public function sell(item) {}\n"
	if err := os.WriteFile(filepath.Join(npcDir, "Ruan.gs2"), []byte(npc), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://dynamic-foreach"
	text := "function onCreated() {\n" +
		"  temp.stats = {\"Health\", \"Mana\"};\n" +
		"  for (temp.s : temp.stats) {\n" +
		"    this.(@temp.s) = findnpc(\"Ruan\");\n" +
		"    this.(@temp.s@\"_stat\") = findnpc(\"Ruan\");\n" +
		"  }\n" +
		"  this.(@temp.s).se\n" +
		"  this.(@temp.s@\"_stat\").se\n" +
		"}"
	server.workspace.upsert(uri, parseDocument(uri, text, 1))

	doc := server.workspace.document(uri)
	if doc == nil {
		t.Fatal("document was not stored")
	}
	stats := doc.symbolFor("temp", "stats", Position{Line: 6, Character: 10})
	if stats == nil || !sameStrings(stats.Values, []string{"Health", "Mana"}) {
		t.Fatalf("array values = %#v, want Health/Mana", stats)
	}
	loopVariable := doc.symbolFor("temp", "s", Position{Line: 6, Character: 10})
	if loopVariable == nil || !sameStrings(loopVariable.Values, []string{"Health", "Mana"}) {
		t.Fatalf("foreach values = %#v, want Health/Mana", loopVariable)
	}

	for _, line := range []struct {
		number int
		text   string
	}{
		{number: 6, text: "  this.(@temp.s).se"},
		{number: 7, text: "  this.(@temp.s@\"_stat\").se"},
	} {
		list := server.completion(CompletionParams{
			TextDocument: TextDocumentIdentifier{URI: uri},
			Position:     Position{Line: line.number, Character: utf16Length(line.text)},
		})
		if !hasCompletion(list.Items, "sell") {
			t.Fatalf("dynamic foreach NPC completion on line %d does not contain sell: %#v", line.number, list.Items)
		}
	}

	completionText := text + "\n  this.(@\""
	server.workspace.upsert(uri, parseDocument(uri, completionText, 2))
	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 8, Character: utf16Length("  this.(@\"")},
	})
	for _, name := range []string{"Health", "Mana", "Health_stat", "Mana_stat"} {
		if !hasCompletion(list.Items, name) {
			t.Fatalf("dynamic foreach key completion is missing %q: %#v", name, list.Items)
		}
	}
}

func TestDynamicExpressionsResolveVariablesFromEveryScope(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://dynamic-scopes"
	base := "function onCreated() {\n" +
		"  this.prefix = \"oi_\";\n" +
		"  client.var = \"Health\";\n" +
		"  server.suffix = \"_oi\";\n" +
		"  this.(@\"oi_\"@client.var) = true;\n" +
		"  this.(@this.prefix@client.var@server.suffix) = true;\n" +
		"}"
	server.workspace.upsert(uri, parseDocument(uri, base, 1))

	completionText := base[:len(base)-1] + "  this.(@\""
	server.workspace.upsert(uri, parseDocument(uri, completionText, 2))
	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 6, Character: utf16Length("  this.(@\"")},
	})
	for _, name := range []string{"oi_Health", "oi_Health_oi"} {
		if !hasCompletion(list.Items, name) {
			t.Fatalf("dynamic scope completion is missing %q: %#v", name, list.Items)
		}
	}
}

func TestWorkspaceRespectsJoinedClassImportsAndServerClientSections(t *testing.T) {
	root := t.TempDir()
	classDir := filepath.Join(root, "classes")
	if err := os.MkdirAll(classDir, 0o755); err != nil {
		t.Fatal(err)
	}
	class := `public function publicServer() {}
function implicitServer() {}
private function privateServer() {}
//#CLIENTSIDE
public function publicClient() {}
function implicitClient() {}
`
	if err := os.WriteFile(filepath.Join(classDir, "CombatHelpers.gs2"), []byte(class), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://class-context"
	text := "import CombatHelpers;\njoin(\"CombatHelpers\");\npub"
	server.workspace.upsert(uri, parseDocument(uri, text, 1))
	list := server.completion(CompletionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 2, Character: 3}})
	if !hasCompletion(list.Items, "publicServer") || hasCompletion(list.Items, "publicClient") {
		t.Fatalf("server-side joined class filtering is incorrect: %#v", list.Items)
	}
	if hasCompletion(list.Items, "privateServer") || hasCompletion(list.Items, "implicitServer") {
		t.Fatalf("unexpected class completion for prefix pub: %#v", list.Items)
	}

	text = "join(\"CombatHelpers\");\n//#CLIENTSIDE\npublicC"
	server.workspace.upsert(uri, parseDocument(uri, text, 2))
	list = server.completion(CompletionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 2, Character: len("publicC")}})
	if !hasCompletion(list.Items, "publicClient") || hasCompletion(list.Items, "publicServer") {
		t.Fatalf("client-side class filtering is incorrect: %#v", list.Items)
	}
}

func TestLanguageServerRequiresSyncWhenDisabled(t *testing.T) {
	server := NewLanguageServer()
	server.SetEnabled(false)
	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	response, err := server.HandleJSON(request)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), `"code":-32002`) {
		t.Fatalf("disabled server response = %s", response)
	}
}

func hasCompletion(items []CompletionItem, name string) bool {
	for _, item := range items {
		if strings.EqualFold(item.Label, name) {
			return true
		}
	}
	return false
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func hasDefinition(items []Definition, name string) bool {
	for _, item := range items {
		if strings.EqualFold(item.Name, name) {
			return true
		}
	}
	return false
}

func TestPositionRoundTripUsesUTF16Characters(t *testing.T) {
	text := "😀\nplayer"
	offset := strings.Index(text, "player")
	position := positionAt(text, offset)
	if position.Line != 1 || position.Character != 0 {
		t.Fatalf("position = %#v", position)
	}
	if got := offsetAt(text, Position{Line: 0, Character: 2}); got != len("😀") {
		t.Fatalf("offset = %d, want %d", got, len("😀"))
	}
}

func TestPathURIWindowsRoundTrip(t *testing.T) {
	path := `C:\workspace\classes\inventory.gs2`
	uri := pathToURI(path)
	if !strings.HasPrefix(uri, "file:///C:/") {
		t.Fatalf("uri = %q", uri)
	}
	if got := uriToPath(uri); got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}
