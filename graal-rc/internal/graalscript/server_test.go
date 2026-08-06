package graalscript

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func TestParseDocumentExtractsJSDocForFunctions(t *testing.T) {
	doc := parseDocument("memory://jsdoc", `/**
 * Registers an entity.
 * @param {string} name Entity name.
 * @return {Entity} The registered entity.
 * @throws Error when the entity is invalid.
 */
public function registerEntity(name) {}

// This is not API documentation.
function internalHelper() {}`, 1)
	if got := doc.Functions[0].Documentation; got != "Registers an entity.\n\n**Parameters**\n\n- `name`: Entity name.\n\n**Returns** (`Entity`): The registered entity.\n\n**Throws**: Error when the entity is invalid." {
		t.Fatalf("documentation = %q", got)
	}
	if got := doc.Functions[0].ParameterDocs["name"]; got != "Entity name." {
		t.Fatalf("parameter documentation = %q", got)
	}
	if got := doc.Functions[0].ReturnDoc; got != "The registered entity." {
		t.Fatalf("return documentation = %q", got)
	}
	if got := doc.Functions[1].Documentation; got != "" {
		t.Fatalf("ordinary comment was treated as documentation: %q", got)
	}
}

func TestParseJSDocSupportsInlineTagsAndMultilineDescriptions(t *testing.T) {
	doc := parseDocument("memory://inline-jsdoc", `/** Creates a query. @param eName Entity key.
 * @param [options] Optional settings.
 * @returns {Query} Query builder.
 * @see Query.where
 */
function table(eName, options) {}`, 1)
	fn := doc.Functions[0]
	if fn.Documentation != "Creates a query.\n\n**Parameters**\n\n- `eName`: Entity key.\n- `options`: Optional settings.\n\n**Returns** (`Query`): Query builder.\n\n**See**: Query.where" {
		t.Fatalf("inline documentation = %q", fn.Documentation)
	}
	if fn.ParameterDocs["options"] != "Optional settings." {
		t.Fatalf("optional parameter documentation = %q", fn.ParameterDocs["options"])
	}
}

func TestHoverReturnsJSDocForImportedAndJoinedFunctions(t *testing.T) {
	root := t.TempDir()
	classDir := filepath.Join(root, "classes")
	if err := os.MkdirAll(classDir, 0o755); err != nil {
		t.Fatal(err)
	}
	class := `/** Imported and joined helper. */
public function helper(value) {}`
	if err := os.WriteFile(filepath.Join(classDir, "Helpers.gs2"), []byte(class), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"import Helpers;\nhelper(value);", "join(\"Helpers\");\nhelper(value);"} {
		doc := parseDocument("memory://hover-jsdoc", source, 1)
		server.workspace.upsert(doc.URI, doc)
		hover := server.hover(TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: doc.URI},
			Position:     Position{Line: 1, Character: 3},
		})
		contents, ok := hoverContents(hover)
		if !ok || !strings.Contains(contents.Value, "Imported and joined helper.") {
			t.Fatalf("hover for %q did not contain JSDoc: %#v", source, hover)
		}
	}
}

func hoverContents(hover *Hover) (MarkupContent, bool) {
	if hover == nil {
		return MarkupContent{}, false
	}
	contents, ok := hover.Contents.(MarkupContent)
	return contents, ok
}

func TestParseDocumentTracksQualifiedImportsAndConstructors(t *testing.T) {
	doc := parseDocument("memory://qualified-import", `import class_gsorm.entity;
function onCreated() {
  this.entity = new Entity("players");
}`, 1)
	if len(doc.Imports) != 1 || doc.Imports[0] != "class_gsorm.entity" {
		t.Fatalf("imports = %#v, want class_gsorm.entity", doc.Imports)
	}
	if len(doc.Members) != 1 || doc.Members[0].Type != "Entity" {
		t.Fatalf("members = %#v, want Entity constructor type", doc.Members)
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

func TestCatalogNormalizesClientAndServerScopes(t *testing.T) {
	catalog := newCatalog()
	catalog.add(Definition{Name: "clientAlias", Kind: "function", Scope: "Client"})
	catalog.add(Definition{Name: "serverAlias", Kind: "function", Scope: "SERVER-SIDE"})

	client, ok := catalog.lookup("clientAlias")
	if !ok || client.Scope != "clientside" {
		t.Fatalf("client alias = %#v, found=%v; want clientside", client, ok)
	}
	server, ok := catalog.lookup("serverAlias")
	if !ok || server.Scope != "serverside" {
		t.Fatalf("server alias = %#v, found=%v; want serverside", server, ok)
	}
	if !definitionAvailableInSide(client, "Client") || definitionAvailableInSide(client, "SERVER") {
		t.Fatalf("client scope filtering is case-sensitive: %#v", client)
	}
	if !definitionAvailableInSide(server, "SERVER") || definitionAvailableInSide(server, "CLIENT") {
		t.Fatalf("server scope filtering is case-sensitive: %#v", server)
	}
}

func TestRefreshDefinitionsReplacesScriptHelpCache(t *testing.T) {
	oldClient := scriptHelpHTTPClient
	scriptHelpCache.Lock()
	oldLoaded := scriptHelpCache.loaded
	oldEntries := append([]Definition(nil), scriptHelpCache.entries...)
	scriptHelpCache.loaded = false
	scriptHelpCache.entries = nil
	scriptHelpCache.Unlock()
	t.Cleanup(func() {
		scriptHelpHTTPClient = oldClient
		scriptHelpCache.Lock()
		scriptHelpCache.loaded = oldLoaded
		scriptHelpCache.entries = oldEntries
		scriptHelpCache.Unlock()
	})

	responses := []string{
		`{"oldApiFunction":{"name":"oldApiFunction","type":"function","scope":"global"}}`,
		`{"newApiFunction":{"name":"newApiFunction","type":"function","scope":"global"}}`,
	}
	calls := 0
	scriptHelpHTTPClient = &http.Client{Transport: scriptHelpRoundTripper(func(request *http.Request) (*http.Response, error) {
		payload := responses[calls]
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Body:       io.NopCloser(strings.NewReader(payload)),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}

	server := NewLanguageServer()
	if err := server.RefreshDefinitions(); err != nil {
		t.Fatal(err)
	}
	if _, ok := server.catalog.lookup("oldApiFunction"); !ok {
		t.Fatal("first API response was not loaded")
	}
	if err := server.RefreshDefinitions(); err != nil {
		t.Fatal(err)
	}
	if _, ok := server.catalog.lookup("newApiFunction"); !ok {
		t.Fatal("refreshed API response was not loaded")
	}
	if _, ok := server.catalog.lookup("oldApiFunction"); ok {
		t.Fatal("old API response remained after refresh")
	}
	if calls != 2 {
		t.Fatalf("API calls = %d, want 2", calls)
	}
}

func TestCatalogPreservesGuiControlTypeAndScope(t *testing.T) {
	data := `{
        "GuiControl": {"name":"GuiControl", "type":"function", "params":["name"], "returns":"GuiControl", "scope":"clientside"},
        "GuiControlProfile": {"name":"GuiControlProfile", "type":"variable", "params":[], "returns":"object", "scope":"clientside"}
    }`
	entries, err := parseDefinitions([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	catalog := newCatalog()
	for _, entry := range entries {
		catalog.add(entry)
	}

	control, ok := catalog.lookup("GuiControl")
	if !ok || control.Kind != "function" || control.Scope != "clientside" || !sameStrings(control.Params, []string{"name"}) {
		t.Fatalf("GuiControl = %#v, found=%v; want clientside function(name)", control, ok)
	}
	profile, ok := catalog.lookup("GuiControlProfile")
	if !ok || profile.Kind != "variable" || profile.Scope != "clientside" {
		t.Fatalf("GuiControlProfile = %#v, found=%v; want clientside variable", profile, ok)
	}
}

func TestClientCompletionIncludesGuiControlConstructor(t *testing.T) {
	server := NewLanguageServer()
	list := completionAtText(server, "memory://gui-constructor", "//#CLIENTSIDE\nnew GuiC", "GuiC")
	if !hasCompletion(list.Items, "GuiControl") {
		t.Fatalf("clientside new completion is missing GuiControl: %#v", list.Items)
	}
	if !hasCompletion(list.Items, "GuiControlProfile") {
		t.Fatalf("clientside new completion is missing GuiControlProfile: %#v", list.Items)
	}

	list = completionAtText(server, "memory://gui-constructor", "//#CLIENTSIDE\nnew GuiContro", "GuiContro")
	if !hasCompletion(list.Items, "GuiControl") {
		t.Fatalf("clientside completion for the incomplete GuiControl prefix is missing GuiControl: %#v", list.Items)
	}
}

func TestCompletionMarksTruncatedListsIncomplete(t *testing.T) {
	server := NewLanguageServer()
	for i := 0; i < 200; i++ {
		server.catalog.add(Definition{
			Name:  fmt.Sprintf("localCompletion%03d", i),
			Kind:  "variable",
			Scope: "clientside",
		})
	}

	list := completionAtText(server, "memory://incomplete-completion", "//#CLIENTSIDE\nnew ", "")
	if !list.IsIncomplete {
		t.Fatalf("truncated completion list was not marked incomplete: %d items", len(list.Items))
	}

	list = completionAtText(server, "memory://incomplete-completion", "//#CLIENTSIDE\nnew GuiContro", "GuiContro")
	if list.IsIncomplete {
		t.Fatalf("narrow completion list was unexpectedly marked incomplete: %d items", len(list.Items))
	}
	if !hasCompletion(list.Items, "GuiControl") {
		t.Fatalf("narrow completion list is missing GuiControl: %#v", list.Items)
	}
}

func TestGUIInheritanceAndProfileCompletions(t *testing.T) {
	server := NewLanguageServer()
	controlURI := "memory://gui-inheritance"
	controlText := `//#CLIENTSIDE
new GuiButton("Button") {
  wid
}`
	list := completionAtText(server, controlURI, controlText, "wid")
	if !hasCompletion(list.Items, "width") {
		t.Fatalf("GuiButton did not inherit GuiControl members: %#v", list.Items)
	}

	profileText := `//#CLIENTSIDE
new GuiControl("Control") {
  profile.
}`
	list = completionAtText(server, controlURI, profileText, "profile.")
	if !hasCompletion(list.Items, "border") || !hasCompletion(list.Items, "fontColor") {
		t.Fatalf("profile members are missing from a GUI control: %#v", list.Items)
	}
	if hasCompletion(list.Items, "width") {
		t.Fatalf("profile completion leaked GuiControl members: %#v", list.Items)
	}

	thisProfileText := `//#CLIENTSIDE
new GuiControl("Control") {
  this.profile.
}`
	list = completionAtText(server, controlURI, thisProfileText, "this.profile.")
	if !hasCompletion(list.Items, "border") {
		t.Fatalf("this.profile did not resolve to GuiControlProfile: %#v", list.Items)
	}

	profileURI := "memory://gui-profile"
	profileBlockText := `//#CLIENTSIDE
new GuiControlProfile("Profile") {
  bor
}`
	list = completionAtText(server, profileURI, profileBlockText, "bor")
	if !hasCompletion(list.Items, "border") || hasCompletion(list.Items, "width") {
		t.Fatalf("GuiControlProfile inheritance is incorrect: %#v", list.Items)
	}
}

func TestGUICompletionUsesConstructorReturnTypeMembers(t *testing.T) {
	server := NewLanguageServer()
	server.catalog.add(Definition{
		Name: "GuiAlias", Kind: "function", Returns: "GuiButton", Scope: "clientside",
	})
	server.catalog.add(Definition{
		Name: "GuiButton.someAction", Kind: "function", Params: []string{"value"}, Scope: "clientside",
	})

	text := `//#CLIENTSIDE
new GuiAlias("Button") {
  someA
}`
	list := completionAtText(server, "memory://gui-return-type", text, "someA")
	if !hasCompletion(list.Items, "someAction") {
		t.Fatalf("GUI constructor return type did not provide concrete members: %#v", list.Items)
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

func TestWorkspacePropagatesFindNPCArgumentIntoFunctionParameter(t *testing.T) {
	root := t.TempDir()
	npcDir := filepath.Join(root, "npcs")
	if err := os.MkdirAll(npcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(npcDir, "EmotesDB.gs2"), []byte("public function GetDefaultEmotes() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://npc-parameter-context"
	text := `function loadDefaultEmotes(db) {
  db.GetDefaultEmotes();
}
function onCreated() {
  loadDefaultEmotes(findnpc("EmotesDB"));
}`
	doc := parseDocument(uri, text, 1)
	server.workspace.upsert(uri, doc)
	list := server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: 1, Character: utf16Length("  db.GetDefaultEmotes")},
	})
	if !hasCompletion(list.Items, "GetDefaultEmotes") {
		t.Fatalf("parameter NPC completion is missing GetDefaultEmotes: %#v", list.Items)
	}
	fn := doc.Functions[0]
	if fn.ParameterTypes["db"] != "npc" || fn.ParameterValues["db"] != "EmotesDB" {
		t.Fatalf("parameter inference = types:%#v values:%#v", fn.ParameterTypes, fn.ParameterValues)
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
	if got := completionDetail(list.Items, "publicServer"); got != "publicServer() · Class CombatHelpers" {
		t.Fatalf("class completion detail = %q, want %q", got, "publicServer() · Class CombatHelpers")
	}

	text = "join(\"CombatHelpers\");\n//#CLIENTSIDE\npublicC"
	server.workspace.upsert(uri, parseDocument(uri, text, 2))
	list = server.completion(CompletionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 2, Character: len("publicC")}})
	if !hasCompletion(list.Items, "publicClient") || hasCompletion(list.Items, "publicServer") {
		t.Fatalf("client-side class filtering is incorrect: %#v", list.Items)
	}
}

func TestImportExportsOnlyPublicWhileJoinMergesAllClassFunctions(t *testing.T) {
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
private function privateClient() {}`
	if err := os.WriteFile(filepath.Join(classDir, "CombatHelpers.gs2"), []byte(class), 0o644); err != nil {
		t.Fatal(err)
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://class-visibility"

	list := completionAtText(server, uri, "import CombatHelpers;\nimp", "imp")
	if hasCompletion(list.Items, "implicitServer") || hasCompletion(list.Items, "implicitClient") {
		t.Fatalf("import exposed implicit functions: %#v", list.Items)
	}
	list = completionAtText(server, uri, "import CombatHelpers;\npriv", "priv")
	if hasCompletion(list.Items, "privateServer") || hasCompletion(list.Items, "privateClient") {
		t.Fatalf("import exposed private functions: %#v", list.Items)
	}
	list = completionAtText(server, uri, "import CombatHelpers;\npub", "pub")
	if !hasCompletion(list.Items, "publicServer") || hasCompletion(list.Items, "publicClient") {
		t.Fatalf("import visibility or side filtering is incorrect: %#v", list.Items)
	}

	list = completionAtText(server, uri, "join(\"CombatHelpers\");\nimp", "imp")
	if !hasCompletion(list.Items, "implicitServer") || hasCompletion(list.Items, "implicitClient") {
		t.Fatalf("join did not merge implicit server functions: %#v", list.Items)
	}
	list = completionAtText(server, uri, "join(\"CombatHelpers\");\npriv", "priv")
	if !hasCompletion(list.Items, "privateServer") || hasCompletion(list.Items, "privateClient") {
		t.Fatalf("join did not merge private server functions: %#v", list.Items)
	}

	text := `import CombatHelpers;
function onCreated() {
  temp.obj = new CombatHelpers();
  temp.obj.imp
}`
	list = completionAtText(server, uri, text, "temp.obj.imp")
	if hasCompletion(list.Items, "implicitServer") {
		t.Fatalf("imported class instance exposed an implicit function: %#v", list.Items)
	}

	text = `function onCreated() {
  temp.obj = new CombatHelpers();
  temp.obj.join("CombatHelpers");
  temp.obj.imp
}`
	list = completionAtText(server, uri, text, "temp.obj.imp")
	if !hasCompletion(list.Items, "implicitServer") {
		t.Fatalf("joined class instance did not expose an implicit function: %#v", list.Items)
	}
	text = strings.Replace(text, "temp.obj.imp", "temp.obj.priv", 1)
	list = completionAtText(server, uri, text, "temp.obj.priv")
	if !hasCompletion(list.Items, "privateServer") {
		t.Fatalf("joined class instance did not expose a private function: %#v", list.Items)
	}
}

func TestWorkspaceResolvesImportedConstructorsAndTransitiveClassJoins(t *testing.T) {
	root := t.TempDir()
	classDir := filepath.Join(root, "classes")
	if err := os.MkdirAll(classDir, 0o755); err != nil {
		t.Fatal(err)
	}
	classes := map[string]string{
		"func_arrays.gs2": `public function _join(arr, delimiter) {}
public function _map(arr, callback) {}
private function arrayPrivate() {}`,
		"class_gsorm.entity.gs2": `public function Entity(tableName) {
  this.join("func_arrays");
}
public function addField(fieldName, fieldType, extra) {}
public function addIndex(indexName, fields, isUnique) {}
private function entityPrivate() {}`,
		"class_gsorm.orm.gs2": `public function ORM(dbName) {
  this.join("func_arrays");
}
public function registerEntity(name, entity) {}
public function syncEntity(name) {}
private function ormPrivate() {}`,
	}
	for name, source := range classes {
		if err := os.WriteFile(filepath.Join(classDir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://gsorm"
	text := `import class_gsorm.entity;
import class_gsorm.orm;

function syncEntity() {}
function onCreated() {
  this.ORM = new ORM("emotes");
  this.pemote_entity = new Entity("player_emotes");
  with (this.pemote_entity) {
    addF
    this.addI
  }
  this.ORM.reg
  this.ORM._ma
  this.syn
}`

	list := completionAtText(server, uri, text, "new Ent")
	if !hasCompletion(list.Items, "Entity") {
		t.Fatalf("qualified import did not expose Entity constructor: %#v", list.Items)
	}

	list = completionAtText(server, uri, text, "addF")
	if !hasCompletion(list.Items, "addField") {
		t.Fatalf("with(Entity) did not expose direct members: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "_ma")
	if !hasCompletion(list.Items, "_map") {
		t.Fatalf("with(Entity) did not expose members inherited from func_arrays: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "this.addI")
	if !hasCompletion(list.Items, "addIndex") {
		t.Fatalf("this inside with(Entity) did not resolve the Entity type: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "this.ORM.reg")
	if !hasCompletion(list.Items, "registerEntity") || hasCompletion(list.Items, "ormPrivate") {
		t.Fatalf("ORM instance completion is incorrect: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "this.ORM._ma")
	if !hasCompletion(list.Items, "_map") || hasCompletion(list.Items, "arrayPrivate") {
		t.Fatalf("transitive ORM join completion is incorrect: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "this.syn")
	if !hasCompletion(list.Items, "syncEntity") {
		t.Fatalf("this receiver did not expose the current script function: %#v", list.Items)
	}
}

func TestJoinedClassesRespectVariableWithAndGUIScopes(t *testing.T) {
	root := t.TempDir()
	classDir := filepath.Join(root, "classes")
	if err := os.MkdirAll(classDir, 0o755); err != nil {
		t.Fatal(err)
	}
	classes := map[string]string{
		"VariableClass": `public function onTestThis() {}`,
		"DynamicClass":  `public function onDynamicThis() {}`,
		"WithClass":     `public function onWithThis() {}`,
		"ScriptClass": `public function onScriptThis() {}
//#CLIENTSIDE
public function onScriptThis() {}`,
		"GuiClass": `//#CLIENTSIDE
public function onGuiThis() {}`,
		"OuterGuiClass": `//#CLIENTSIDE
public function onOuterGuiThis() {}`,
		"ChildGuiClass": `//#CLIENTSIDE
public function onChildGuiThis() {}`,
	}
	for name, source := range classes {
		if err := os.WriteFile(filepath.Join(classDir, name+".gs2"), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	server := NewLanguageServer()
	if err := server.workspace.setRoot(root); err != nil {
		t.Fatal(err)
	}
	uri := "memory://scoped-joins"
	variableText := `function onCreated() {
  temp.obj = new IStaticVar();
  temp.obj.join("VariableClass");
  onTest
}`
	list := completionAtText(server, uri, variableText, "onTest")
	if hasCompletion(list.Items, "onTestThis") {
		t.Fatalf("variable join leaked into the script scope: %#v", list.Items)
	}
	variableText = `function onCreated() {
  temp.obj = new IStaticVar();
  temp.obj.join("VariableClass");
  temp.obj.onTest
}`
	list = completionAtText(server, uri, variableText, "temp.obj.onTest")
	if !hasCompletion(list.Items, "onTestThis") || completionDetail(list.Items, "onTestThis") != "onTestThis() · Class VariableClass" {
		t.Fatalf("variable join was not scoped to the variable: %#v", list.Items)
	}
	dynamicText := `function onCreated() {
  temp.("obj") = new IStaticVar();
  temp.("obj").join("DynamicClass");
  temp.("obj").onDynamic
}`
	list = completionAtText(server, uri, dynamicText, "temp.(\"obj\").onDynamic")
	if !hasCompletion(list.Items, "onDynamicThis") {
		t.Fatalf("dynamic variable join was not scoped to the dynamic variable: %#v", list.Items)
	}

	text := `this.join("ScriptClass");
function onCreated() {
  with (temp.obj) {
    this.join("WithClass");
    onWith
    this.onWith
    thiso.onScript
  }
}
//#CLIENTSIDE
new GuiControl("Name") {
  this.join("GuiClass");
  onGui
  onScript
  thiso.onScript
  wid
}
onScript`
	list = completionAtText(server, uri, text, "onWith")
	if !hasCompletion(list.Items, "onWithThis") || hasCompletion(list.Items, "onScriptThis") {
		t.Fatalf("with join scope is incorrect: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "this.onWith")
	if !hasCompletion(list.Items, "onWithThis") {
		t.Fatalf("this inside with did not use the with object scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "thiso.onScript")
	if !hasCompletion(list.Items, "onScriptThis") {
		t.Fatalf("thiso inside with did not use the parent script scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "onGui")
	if !hasCompletion(list.Items, "onGuiThis") || hasCompletion(list.Items, "onScriptThis") {
		t.Fatalf("GUI join leaked or was not available in the GUI scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "onScript")
	if !hasCompletion(list.Items, "onScriptThis") {
		t.Fatalf("script join was not available after leaving the GUI scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "wid")
	if !hasCompletion(list.Items, "width") {
		t.Fatalf("GUI members were not available without this.: %#v", list.Items)
	}

	nestedText := text + `
new GuiControl("Outer") {
  this.join("OuterGuiClass");
  new GuiButton("Child") {
    this.join("ChildGuiClass");
    onChildGui
    onOuterGui
    thiso.onScript
    thiso.onOuterGui
  }
}`
	list = completionAtText(server, uri, nestedText, "onChildGui")
	if !hasCompletion(list.Items, "onChildGuiThis") || hasCompletion(list.Items, "onOuterGuiThis") || hasCompletion(list.Items, "onScriptThis") {
		t.Fatalf("nested GUI did not isolate the child join: %#v", list.Items)
	}
	list = completionAtText(server, uri, nestedText, "onOuterGui")
	if hasCompletion(list.Items, "onOuterGuiThis") {
		t.Fatalf("nested GUI leaked the outer GUI join into the child scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, nestedText, "thiso.onScript")
	if !hasCompletion(list.Items, "onScriptThis") || hasCompletion(list.Items, "onOuterGuiThis") {
		t.Fatalf("thiso did not resolve directly to the script owner: %#v", list.Items)
	}
	list = completionAtText(server, uri, nestedText, "thiso.onOuterGui")
	if hasCompletion(list.Items, "onOuterGuiThis") {
		t.Fatalf("thiso exposed the enclosing GUI instead of the script owner: %#v", list.Items)
	}
}

func TestMemberVariablesRespectThisThisoAndGUIScopes(t *testing.T) {
	server := NewLanguageServer()
	uri := "memory://member-scopes"
	text := `function onCreated() {
  this.serverParentValue = 1;
  with (temp.obj) {
    this.withScopeValue = 2;
    this.withScope
    serverParent
    this.serverParent
    thiso.serverParent
  }
  serverParent
}
//#CLIENTSIDE
this.clientParentValue = 3;
new GuiControl("Name") {
  this.guiScopeValue = 4;
  guiScope
  clientParent
  this.clientParent
  thiso.clientParent
}`

	list := completionAtText(server, uri, text, "this.withScope")
	if !hasCompletion(list.Items, "withScopeValue") || hasCompletion(list.Items, "serverParentValue") {
		t.Fatalf("this inside with did not stay in the with scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "    serverParent")
	if hasCompletion(list.Items, "serverParentValue") {
		t.Fatalf("bare members leaked into the with scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "this.serverParent")
	if hasCompletion(list.Items, "serverParentValue") {
		t.Fatalf("this inside with resolved the parent script scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "thiso.serverParent")
	if !hasCompletion(list.Items, "serverParentValue") {
		t.Fatalf("thiso inside with did not resolve the parent script scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "  guiScope")
	if !hasCompletion(list.Items, "guiScopeValue") || hasCompletion(list.Items, "clientParentValue") {
		t.Fatalf("bare members did not stay in the GUI scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "this.clientParent")
	if hasCompletion(list.Items, "clientParentValue") {
		t.Fatalf("this inside GUI resolved the parent script scope: %#v", list.Items)
	}
	list = completionAtText(server, uri, text, "thiso.clientParent")
	if !hasCompletion(list.Items, "clientParentValue") {
		t.Fatalf("thiso inside GUI did not resolve the parent script scope: %#v", list.Items)
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

func TestServerContextCompletionRespectsScopesAndConfigSections(t *testing.T) {
	server := NewLanguageServer()
	server.SetServerContext(ServerScriptContext{
		ServerFlags:   "# ignored\nserver.eventActive=true\nserverr.globalTime=120\n[Ignored]\nserver.notAFlag=false\n",
		ServerOptions: "# ignored\njaillevels=jail.nw,guest.nw\n[Tags]\nnotAnOption=true\nguild_Graal Police=staff.nw\n",
	})
	uri := "memory://server-context"
	serverText := `function onCreated() {
  server.event
  serverr.global
  serveroptions.jail
}
`

	list := completionAtText(server, uri, serverText, "server.event")
	if !hasCompletion(list.Items, "eventActive") {
		t.Fatalf("server flag completion missing on serverside: %#v", list.Items)
	}
	list = completionAtText(server, uri, serverText, "serverr.global")
	if !hasCompletion(list.Items, "globalTime") {
		t.Fatalf("serverr flag completion missing on serverside: %#v", list.Items)
	}
	list = completionAtText(server, uri, serverText, "serveroptions.jail")
	if !hasCompletion(list.Items, "jaillevels") || hasCompletion(list.Items, "Tags") || hasCompletion(list.Items, "notAnOption") {
		t.Fatalf("server option completion parsed comments/sections incorrectly: %#v", list.Items)
	}

	clientText := `//#CLIENTSIDE
	server.eventActive
	serverr.globalTime
	serveroptions.jaillevels
`
	list = completionAtText(server, uri+"-client", clientText, "server.eventActive")
	if hasCompletion(list.Items, "eventActive") {
		t.Fatalf("server-only flag leaked into clientside completion: %#v", list.Items)
	}
	list = completionAtText(server, uri+"-client", clientText, "serverr.globalTime")
	if !hasCompletion(list.Items, "globalTime") {
		t.Fatalf("serverr flag was not readable clientside: %#v", list.Items)
	}
	list = completionAtText(server, uri+"-client", clientText, "serveroptions.jaillevels")
	if hasCompletion(list.Items, "jaillevels") {
		t.Fatalf("server option leaked into clientside completion: %#v", list.Items)
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

func completionDetail(items []CompletionItem, name string) string {
	for _, item := range items {
		if strings.EqualFold(item.Label, name) {
			return item.Detail
		}
	}
	return ""
}

func completionAtText(server *LanguageServer, uri, text, needle string) CompletionList {
	server.workspace.upsert(uri, parseDocument(uri, text, 1))
	offset := strings.LastIndex(text, needle)
	if offset < 0 {
		return CompletionList{}
	}
	offset += len(needle)
	doc := server.workspace.document(uri)
	return server.completion(CompletionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     positionAt(doc.Text, offset),
	})
}

type scriptHelpRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper scriptHelpRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
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
