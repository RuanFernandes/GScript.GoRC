package graalscript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

var functionDeclarationContextPattern = regexp.MustCompile(`(?i)^\s*(?:(?:public|private|protected|static|final|override)\s+)*function(?:\s+[a-z_][a-z0-9_]*)?\s*$`)

type LanguageServer struct {
	mu                     sync.Mutex
	workspace              *Workspace
	catalog                *Catalog
	serverContext          serverContextDefinitions
	serverContextInput     ServerScriptContext
	enabled                bool
	workspaceRefreshNeeded bool
}

func NewLanguageServer() *LanguageServer {
	return &LanguageServer{workspace: newWorkspace(), catalog: builtinCatalog(), enabled: true}
}

// SetEnabled gates the semantic server. The desktop app ties this to the local
// Sync configuration; keeping the gate in the server also prevents a stale
// editor window from querying an old synchronized workspace after Sync is off.
func (s *LanguageServer) SetEnabled(enabled bool) {
	s.mu.Lock()
	if enabled && !s.enabled {
		s.workspaceRefreshNeeded = true
	}
	s.enabled = enabled
	s.mu.Unlock()
}

// SetServerContext replaces the server options/flags snapshot used by
// completions and hover. The desktop app calls this before every LSP request,
// so a config loaded asynchronously after login becomes visible without
// restarting the editor.
func (s *LanguageServer) SetServerContext(context ServerScriptContext) {
	s.mu.Lock()
	if s.serverContextInput == context {
		s.mu.Unlock()
		return
	}
	s.serverContextInput = context
	s.serverContext = parseServerContext(context)
	s.mu.Unlock()
}

func (s *LanguageServer) RefreshWorkspace() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root := s.workspace.rootPath()
	if root == "" {
		return nil
	}
	if err := s.workspace.refreshRoot(); err != nil {
		return err
	}
	s.catalog = loadCatalog()
	s.workspaceRefreshNeeded = false
	return nil
}

func (s *LanguageServer) RefreshDefinitions() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := refreshScriptHelpDefinitions(); err != nil {
		return err
	}
	s.catalog = loadCatalog()
	return nil
}

// HandleJSON accepts a complete JSON-RPC 2.0 message and returns the complete
// response message. Wails transports the same payload; a future stdio adapter
// can reuse this method without changing the semantic engine.
func (s *LanguageServer) HandleJSON(message []byte) ([]byte, error) {
	var request jsonRPCRequest
	if err := json.Unmarshal(message, &request); err != nil {
		return nil, fmt.Errorf("decode GraalScript LSP request: %w", err)
	}
	if request.JSONRPC != "2.0" || strings.TrimSpace(request.Method) == "" {
		return makeRPCError(request.ID, jsonRPCInvalidRequest, fmt.Errorf("invalid JSON-RPC request")), nil
	}
	notification := len(request.ID) == 0 || bytes.Equal(bytes.TrimSpace(request.ID), []byte("null"))
	if !s.isEnabled() && !allowedWhenDisabled(request.Method) {
		if notification {
			return nil, nil
		}
		return makeRPCError(request.ID, jsonRPCServerNotInitialized, fmt.Errorf("GraalScript LSP requires Local Sync to be enabled and configured")), nil
	}
	result, code, err := s.handleMethod(request.Method, request.Params)
	if notification {
		return nil, err
	}
	if err != nil {
		return makeRPCError(request.ID, code, err), nil
	}
	return makeRPCResult(request.ID, result), nil
}

func (s *LanguageServer) isEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enabled
}

func allowedWhenDisabled(method string) bool {
	switch method {
	case "shutdown", "exit", "textDocument/didClose":
		return true
	default:
		return false
	}
}

func (s *LanguageServer) handleMethod(method string, params json.RawMessage) (any, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.enabled && method != "initialize" && s.workspaceRefreshNeeded && s.workspace.rootPath() != "" {
		if err := s.workspace.refreshRoot(); err != nil {
			return nil, jsonRPCInternalError, err
		}
		s.catalog = loadCatalog()
		s.workspaceRefreshNeeded = false
	}
	switch method {
	case "initialize":
		var value InitializeParams
		if err := decodeParams(params, &value); err != nil {
			return nil, jsonRPCInvalidParams, err
		}
		root := uriToPath(value.RootURI)
		if root == "" {
			root = value.RootPath
		}
		if root != s.workspace.rootPath() {
			if err := s.workspace.setRoot(root); err != nil {
				return nil, jsonRPCInvalidParams, err
			}
			s.catalog = loadCatalog()
			s.workspaceRefreshNeeded = false
		} else if s.workspaceRefreshNeeded && root != "" {
			if err := s.workspace.refreshRoot(); err != nil {
				return nil, jsonRPCInternalError, err
			}
			s.catalog = loadCatalog()
			s.workspaceRefreshNeeded = false
		}
		return InitializeResult{
			Capabilities: ServerCapabilities{
				CompletionProvider:    &CompletionOptions{TriggerCharacters: []string{".", "(", ","}},
				HoverProvider:         true,
				SignatureHelpProvider: &SignatureHelpOptions{TriggerCharacters: []string{"(", ","}},
				DiagnosticProvider:    &DiagnosticOptions{},
			},
			ServerInfo: ServerInfo{Name: "graalscript-lsp", Version: "5.2.0"},
		}, 0, nil
	case "initialized", "shutdown", "exit":
		return nil, 0, nil
	case "textDocument/didOpen":
		var value struct {
			TextDocument TextDocumentItem `json:"textDocument"`
		}
		if err := decodeParams(params, &value); err != nil {
			return nil, jsonRPCInvalidParams, err
		}
		s.workspace.upsert(value.TextDocument.URI, parseDocument(value.TextDocument.URI, value.TextDocument.Text, value.TextDocument.Version))
		return nil, 0, nil
	case "textDocument/didChange":
		var value struct {
			TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
			ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
		}
		if err := decodeParams(params, &value); err != nil {
			return nil, jsonRPCInvalidParams, err
		}
		if len(value.ContentChanges) == 0 {
			return nil, jsonRPCInvalidParams, fmt.Errorf("didChange has no content changes")
		}
		current := s.workspace.document(value.TextDocument.URI)
		if current != nil && value.TextDocument.Version > 0 && value.TextDocument.Version < current.Version {
			return nil, 0, nil
		}
		text := ""
		if current != nil {
			text = current.Text
		}
		for _, change := range value.ContentChanges {
			if change.Range == nil {
				text = change.Text
				continue
			}
			start := offsetAt(text, change.Range.Start)
			end := offsetAt(text, change.Range.End)
			if start > end {
				start, end = end, start
			}
			if start > len(text) {
				start = len(text)
			}
			if end > len(text) {
				end = len(text)
			}
			text = text[:start] + change.Text + text[end:]
		}
		s.workspace.upsert(value.TextDocument.URI, parseDocument(value.TextDocument.URI, text, value.TextDocument.Version))
		return nil, 0, nil
	case "textDocument/didClose":
		var value struct {
			TextDocument TextDocumentIdentifier `json:"textDocument"`
		}
		if err := decodeParams(params, &value); err != nil {
			return nil, jsonRPCInvalidParams, err
		}
		s.workspace.remove(value.TextDocument.URI)
		return nil, 0, nil
	case "textDocument/completion":
		var value CompletionParams
		if err := decodeParams(params, &value); err != nil {
			return nil, jsonRPCInvalidParams, err
		}
		return s.completion(value), 0, nil
	case "textDocument/hover":
		var value TextDocumentPositionParams
		if err := decodeParams(params, &value); err != nil {
			return nil, jsonRPCInvalidParams, err
		}
		return s.hover(value), 0, nil
	case "textDocument/signatureHelp":
		var value TextDocumentPositionParams
		if err := decodeParams(params, &value); err != nil {
			return nil, jsonRPCInvalidParams, err
		}
		return s.signatureHelp(value), 0, nil
	case "textDocument/diagnostic":
		var value DocumentDiagnosticParams
		if err := decodeParams(params, &value); err != nil {
			return nil, jsonRPCInvalidParams, err
		}
		doc := s.workspace.document(value.TextDocument.URI)
		if doc == nil {
			return DocumentDiagnosticReport{Kind: "full", Items: []Diagnostic{}}, 0, nil
		}
		return DocumentDiagnosticReport{Kind: "full", Items: doc.diagnostics()}, 0, nil
	default:
		return nil, jsonRPCMethodNotFound, fmt.Errorf("method not found: %s", method)
	}
}

func decodeParams(raw json.RawMessage, target any) error {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func (s *LanguageServer) completion(params CompletionParams) CompletionList {
	doc := s.workspace.document(params.TextDocument.URI)
	if doc == nil {
		return CompletionList{Items: []CompletionItem{}}
	}
	offset := offsetAt(doc.Text, params.Position)
	wordStart := identifierStart(doc.Text, offset)
	prefix := doc.Text[wordStart:offset]
	sig := significantBefore(doc.Tokens, wordStart)
	definitions := []Definition{}
	decorateDynamicVariables := false

	if isFunctionDeclarationContext(doc.Text, offset) {
		// After the function keyword the only useful completions are lifecycle
		// and engine event handlers. Global functions such as abs/floor are valid
		// expressions, but they are not valid names for this declaration intent.
		for _, entry := range s.catalog.Definitions {
			if entry.Kind == "function" && strings.HasPrefix(strings.ToLower(entry.Name), "on") && definitionAvailableInSide(entry, doc.sideAtOffset(offset)) {
				definitions = append(definitions, entry)
			}
		}
	} else if newObjectCompletionContext(doc.Text, wordStart, sig) {
		// `new ` starts an object-construction expression. Keep the result set
		// intentionally narrow: imported class constructors and built-in GUI
		// types are valid here, while ordinary functions, variables, and methods
		// would be misleading. Array allocation uses `new[size]` and does not
		// enter this context because the significant token before the word is `[`.
		definitions = append(definitions, s.newObjectDefinitions(doc, doc.sideAtOffset(offset))...)
	} else if receiver, ok := dynamicPropertyCompletionScope(doc, offset); ok {
		definitions = append(definitions, s.memberDefinitions(receiver, doc, doc.sideAtOffset(offset), params.Position)...)
		if strings.EqualFold(receiver, "serveroptions") {
			// Option names may contain spaces, so dynamic access should insert
			// serveroptions.("option name") instead of a bare identifier.
			decorateDynamicVariables = true
		}
	} else if len(sig) > 0 && sig[len(sig)-1].text == "." {
		receiver := receiverContextAtDot(doc, sig, len(sig)-1, params.Position)
		definitions = append(definitions, s.memberDefinitionsForContext(receiver, doc, doc.sideAtOffset(offset), params.Position)...)
		decorateDynamicVariables = true
	} else if len(sig) > 0 && sig[len(sig)-1].text == "::" {
		className := previousIdentifier(sig, len(sig)-2)
		for _, fn := range s.workspace.classSymbols([]string{className}, doc.sideAtOffset(offset)) {
			definitions = append(definitions, definitionFromFunctionInScope(fn, classScope(className)))
		}
	} else {
		definitions = append(definitions, s.localDefinitions(doc, params.Position)...)
		if block := withBlockAt(doc, params.Position); block != nil {
			definitions = append(definitions, s.withDefinitions(block.Receiver, doc, doc.sideAtOffset(offset), params.Position)...)
		}
		for _, entry := range s.catalog.Definitions {
			if !strings.Contains(entry.Name, ".") && definitionAvailableInSide(entry, doc.sideAtOffset(offset)) {
				definitions = append(definitions, entry)
			}
		}
	}

	items := make([]CompletionItem, 0, len(definitions))
	seen := map[string]bool{}
	for _, definition := range definitions {
		if definition.Name == "" || strings.Contains(definition.Name, ".") && len(sig) == 0 {
			continue
		}
		key := normalizeName(definition.Name)
		if seen[key] || (prefix != "" && !strings.HasPrefix(strings.ToLower(definition.Name), strings.ToLower(prefix))) {
			continue
		}
		seen[key] = true
		item := completionItem(definition)
		completionText := definition.Name
		if decorateDynamicVariables && definition.Dynamic {
			completionText = dynamicVariableCompletionText(definition.Name, definition.DynamicExpression)
			item.Label = completionText
			item.InsertText = completionText
		}
		item.TextEdit = &TextEdit{
			Range:   Range{Start: positionAt(doc.Text, wordStart), End: params.Position},
			NewText: completionText,
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].SortText != items[j].SortText {
			return items[i].SortText < items[j].SortText
		}
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	isIncomplete := len(items) > 150
	if len(items) > 150 {
		items = items[:150]
	}
	return CompletionList{IsIncomplete: isIncomplete, Items: items}
}

func newObjectCompletionContext(text string, wordStart int, sig []token) bool {
	if len(sig) == 0 || wordStart <= 0 || wordStart > len(text) {
		return false
	}
	previous := sig[len(sig)-1]
	if !isIdentifier(previous, "new") || previous.end >= wordStart {
		return false
	}
	// Require horizontal whitespace after `new`. In particular, `new[size]`
	// remains the array-allocation form and keeps normal expression completion.
	gap := text[previous.end:wordStart]
	return gap != "" && strings.Trim(gap, " \t") == ""
}

func (s *LanguageServer) newObjectDefinitions(doc *Document, side string) []Definition {
	definitions := []Definition{}
	seen := map[string]bool{}
	appendDefinition := func(definition Definition) {
		key := normalizeName(definition.Name)
		if key == "" || seen[key] || !definitionAvailableInSide(definition, side) {
			return
		}
		seen[key] = true
		definitions = append(definitions, definition)
	}

	for _, className := range doc.importedClassNames() {
		constructorName := importedClassCompletionName(className)
		if constructorName == "" {
			continue
		}
		functions := s.workspace.classSymbols([]string{className}, side)
		if len(functions) == 0 {
			continue
		}

		foundConstructor := false
		for _, fn := range functions {
			if !strings.EqualFold(fn.Name, constructorName) {
				continue
			}
			appendDefinition(definitionFromFunctionInScope(fn, classScope(className)))
			foundConstructor = true
			break
		}
		if !foundConstructor {
			appendDefinition(Definition{
				Name:        constructorName,
				Kind:        "class",
				Scope:       classScope(className),
				Description: "Imported GS2 class.",
			})
		}
	}

	for _, entry := range s.catalog.Definitions {
		if strings.Contains(entry.Name, ".") || !isGUITypeName(entry.Name) {
			continue
		}
		appendDefinition(entry)
	}
	return definitions
}

func importedClassCompletionName(name string) string {
	name = strings.TrimSpace(strings.Trim(name, `"`))
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimSuffix(name, "/")
	name = trimScriptExtension(name)
	if separator := strings.LastIndexAny(name, "/."); separator >= 0 {
		name = name[separator+1:]
	}
	return strings.TrimSpace(name)
}

func isFunctionDeclarationContext(text string, offset int) bool {
	if offset < 0 || offset > len(text) {
		return false
	}
	lineStart := strings.LastIndexByte(text[:offset], '\n') + 1
	line := text[lineStart:offset]
	if comment := strings.Index(line, "//"); comment >= 0 {
		line = line[:comment]
	}
	return functionDeclarationContextPattern.MatchString(line)
}

func (s *LanguageServer) memberDefinitions(receiver string, doc *Document, side string, position Position) []Definition {
	receiver = strings.TrimSpace(receiver)
	if !serverScopeVisible(receiver, side) {
		return nil
	}
	definitions := s.memberDefinitionsAtOwner(receiver, doc, side, position, doc.memberOwnerKey(receiver, position))
	definitions = append(definitions, s.serverScopeDefinitions(receiver, side)...)
	return definitions
}

func (s *LanguageServer) memberDefinitionsAtOwner(receiver string, doc *Document, side string, position Position, ownerKey string) []Definition {
	definitions := []Definition{}
	for _, entry := range s.catalog.members(receiver) {
		if definitionAvailableInSide(entry, side) {
			definitions = append(definitions, entry)
		}
	}
	if strings.EqualFold(receiver, "temp") {
		currentFunction := functionAt(doc, position)
		for _, variable := range doc.Variables {
			if !tempVariableVisible(variable, currentFunction) {
				continue
			}
			definitions = append(definitions, Definition{
				Name:              variable.Name,
				Kind:              "variable",
				Scope:             variable.Scope,
				Description:       variable.Detail,
				Dynamic:           len(variable.DynamicExpressions) > 0 || isDynamicVariableDetail(variable.Detail),
				DynamicExpression: firstString(variable.DynamicExpressions),
			})
		}
	}
	for _, member := range doc.Members {
		if !memberScopeMatches(member.Scope, receiver) || member.OwnerKey != ownerKey || !memberAvailableInSide(member, side) {
			continue
		}
		definitions = append(definitions, Definition{
			Name:              member.Name,
			Kind:              "variable",
			Scope:             member.Scope,
			Description:       member.Detail,
			Dynamic:           len(member.DynamicExpressions) > 0 || isDynamicVariableDetail(member.Detail),
			DynamicExpression: firstString(member.DynamicExpressions),
		})
	}
	return definitions
}

func isDynamicVariableDetail(detail string) bool {
	return strings.Contains(strings.ToLower(detail), "dynamic variable")
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func dynamicVariableCompletionText(name, expression string) string {
	if expression = strings.TrimSpace(expression); expression != "" {
		return "(" + expression + ")"
	}
	return `(@"` + strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), `"`, `\"`) + `")`
}

func (s *LanguageServer) memberDefinitionsForContext(receiver receiverContext, doc *Document, side string, position Position) []Definition {
	switch receiver.kind {
	case "joined":
		definitions := []Definition{}
		seen := map[string]bool{}
		for _, className := range receiver.objectNames() {
			for _, fn := range s.workspace.joinedClassSymbols([]string{className}, side) {
				key := normalizeName(fn.Name)
				if seen[key] {
					continue
				}
				seen[key] = true
				definitions = append(definitions, definitionFromFunctionInScope(fn, classScope(className)))
			}
		}
		if receiver.baseKind != "" {
			definitions = append(definitions, s.memberDefinitionsForContext(receiverContext{
				kind:     receiver.baseKind,
				name:     receiver.baseName,
				names:    receiver.baseNames,
				ownerKey: receiver.baseOwnerKey,
			}, doc, side, position)...)
		}
		return definitions
	case "gui":
		return s.guiDefinitions(receiver.name, receiver.ownerKey, doc, side)
	case "gui-profile":
		return s.memberDefinitions("GuiControlProfile", doc, side, position)
	case "server", "serverr", "serveroptions":
		if !serverScopeVisible(receiver.name, side) {
			return nil
		}
		definitions := s.memberDefinitionsAtOwner(receiver.name, doc, side, position, receiver.ownerKey)
		return append(definitions, s.serverScopeDefinitions(receiver.name, side)...)
	case "class":
		if isGUITypeName(receiver.name) {
			return s.guiDefinitions(receiver.name, receiver.ownerKey, doc, side)
		}
		if definitions := s.catalogObjectDefinitions(receiver.name, side); len(definitions) > 0 {
			return definitions
		}
		definitions := []Definition{}
		for _, fn := range s.workspace.classSymbols([]string{receiver.name}, side) {
			definitions = append(definitions, definitionFromFunctionInScope(fn, classScope(receiver.name)))
		}
		return definitions
	case "npc":
		definitions := []Definition{}
		seen := map[string]bool{}
		for _, name := range receiver.objectNames() {
			for _, fn := range s.workspace.objectSymbols("npc", name, side) {
				if seen[normalizeName(fn.Name)] {
					continue
				}
				seen[normalizeName(fn.Name)] = true
				definitions = append(definitions, definitionFromFunctionInScope(fn, "NPC "+name+" · "+scriptSideServer))
			}
		}
		return definitions
	case "weapon":
		definitions := []Definition{}
		seen := map[string]bool{}
		for _, name := range receiver.objectNames() {
			for _, fn := range s.workspace.objectSymbols("weapon", name, side) {
				if seen[normalizeName(fn.Name)] {
					continue
				}
				seen[normalizeName(fn.Name)] = true
				definitions = append(definitions, definitionFromFunctionInScope(fn, "Weapon "+name+" · "+side))
			}
		}
		return definitions
	case "player", "level":
		return s.memberDefinitions(receiver.kind, doc, side, position)
	case "string":
		if definitions := s.workspace.objectSymbols("npc", receiver.name, side); len(definitions) > 0 {
			result := make([]Definition, 0, len(definitions))
			for _, fn := range definitions {
				result = append(result, definitionFromFunctionInScope(fn, "NPC "+receiver.name+" · "+scriptSideServer))
			}
			return result
		}
		return s.memberDefinitions("string", doc, side, position)
	case "static":
		if definitions := s.enumMemberDefinitions(receiver.name, doc, side); len(definitions) > 0 {
			return definitions
		}
		if !serverScopeVisible(receiver.name, side) {
			return nil
		}
		definitions := s.memberDefinitionsAtOwner(receiver.name, doc, side, position, receiver.ownerKey)
		definitions = append(definitions, s.serverScopeDefinitions(receiver.name, side)...)
		if strings.EqualFold(receiver.name, "this") || strings.EqualFold(receiver.name, "thiso") {
			for _, fn := range doc.Functions {
				if fn.Side == "" || strings.EqualFold(fn.Side, side) {
					definitions = append(definitions, definitionFromFunction(fn))
				}
			}
		}
		return definitions
	default:
		return s.memberDefinitionsAtOwner(receiver.name, doc, side, position, receiver.ownerKey)
	}
}

func (s *LanguageServer) catalogObjectDefinitions(name, side string) []Definition {
	definitions := []Definition{}
	for _, entry := range s.catalog.members(name) {
		if definitionAvailableInSide(entry, side) {
			definitions = append(definitions, entry)
		}
	}
	return definitions
}

func (s *LanguageServer) localDefinitions(doc *Document, position Position) []Definition {
	definitions := []Definition{}
	side := doc.sideAtOffset(offsetAt(doc.Text, position))
	definitions = append(definitions, s.enumDefinitions(doc, side)...)
	currentFunction := functionAt(doc, position)
	for _, fn := range doc.Functions {
		if fn.Side != "" && !strings.EqualFold(fn.Side, side) {
			continue
		}
		definitions = append(definitions, definitionFromFunction(fn))
	}
	if fn := functionAt(doc, position); fn != nil {
		for _, param := range fn.Params {
			definitions = append(definitions, Definition{
				Name: param, Kind: "variable", Scope: "parameter",
				Description: "Parameter of " + fn.Name + ".",
			})
		}
	}
	for _, variable := range doc.Variables {
		if !tempVariableVisible(variable, currentFunction) {
			continue
		}
		definitions = append(definitions, Definition{Name: variable.Name, Kind: "variable", Scope: variable.Scope, Description: variable.Detail})
	}
	scope := doc.semanticScopeAt(position)
	if scope.kind == "gui" {
		definitions = append(definitions, s.guiDefinitions(scope.controlType, scope.key, doc, side)...)
	}
	for _, member := range doc.Members {
		if !strings.EqualFold(member.Scope, "this") || member.OwnerKey != scope.key || !memberAvailableInSide(member, side) {
			continue
		}
		definitions = append(definitions, Definition{Name: member.Name, Kind: "variable", Scope: member.Scope, Description: member.Detail})
	}
	for _, className := range doc.currentJoinedClassNames(position) {
		for _, fn := range s.workspace.joinedClassSymbols([]string{className}, side) {
			definitions = append(definitions, definitionFromFunctionInScope(fn, classScope(className)))
		}
	}
	for _, className := range doc.importedClassNames() {
		for _, fn := range s.workspace.classSymbols([]string{className}, side) {
			definitions = append(definitions, definitionFromFunctionInScope(fn, classScope(className)))
		}
	}
	return definitions
}

func (s *LanguageServer) enumDefinitions(doc *Document, side string) []Definition {
	definitions := []Definition{}
	seen := map[string]bool{}
	appendEnum := func(enum EnumSymbol) {
		key := normalizeName(enum.Name)
		if key == "" || seen[key] || !enumAvailableInSide(enum, side) {
			return
		}
		seen[key] = true
		definitions = append(definitions, Definition{
			Name:        enum.Name,
			Kind:        "enum",
			Scope:       "global",
			Description: "GS2 enum.",
		})
	}
	for _, enum := range doc.Enums {
		appendEnum(enum)
	}
	for _, enum := range s.workspace.enumSymbols() {
		appendEnum(enum)
	}
	return definitions
}

func (s *LanguageServer) enumMemberDefinitions(enumName string, doc *Document, side string) []Definition {
	var enum EnumSymbol
	found := false
	for _, candidate := range doc.Enums {
		if strings.EqualFold(candidate.Name, enumName) {
			enum = candidate
			found = true
			break
		}
	}
	if !found {
		enum, found = s.workspace.enumSymbol(enumName)
	}
	if !found || !enumAvailableInSide(enum, side) {
		return nil
	}

	definitions := make([]Definition, 0, len(enum.Members))
	for _, member := range enum.Members {
		definitions = append(definitions, Definition{
			Name:      member.Name,
			Kind:      "enum-member",
			Scope:     "enum " + enum.Name,
			EnumValue: member.Value,
		})
	}
	return definitions
}

func enumAvailableInSide(enum EnumSymbol, side string) bool {
	return enum.Side == "" || side == "" || strings.EqualFold(enum.Side, side)
}

func (s *LanguageServer) guiDefinitions(controlType, ownerKey string, doc *Document, side string) []Definition {
	definitions := []Definition{}
	seen := map[string]bool{}
	appendMembers := func(receiver string) {
		for _, definition := range s.catalog.members(receiver) {
			if !definitionAvailableInSide(definition, side) {
				continue
			}
			key := normalizeName(definition.Name)
			if seen[key] {
				continue
			}
			seen[key] = true
			definitions = append(definitions, definition)
		}
	}
	controlType = strings.TrimSpace(controlType)
	for _, guiType := range s.catalog.guiTypeNames(controlType) {
		appendMembers(guiType)
	}
	if isGUIProfileType(controlType) {
		appendMembers("GuiControlProfile")
	} else {
		// The API describes concrete GUI controls independently, but GS2 GUI
		// objects all inherit the common GuiControl surface.
		appendMembers("GuiControl")
	}
	for _, member := range doc.Members {
		if !strings.EqualFold(member.Scope, "this") || member.OwnerKey != ownerKey || !memberAvailableInSide(member, side) {
			continue
		}
		key := normalizeName(member.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		definitions = append(definitions, Definition{
			Name:              member.Name,
			Kind:              "variable",
			Scope:             member.Scope,
			Description:       member.Detail,
			Dynamic:           len(member.DynamicExpressions) > 0 || isDynamicVariableDetail(member.Detail),
			DynamicExpression: firstString(member.DynamicExpressions),
		})
	}
	return definitions
}

func (s *LanguageServer) withDefinitions(receiver string, doc *Document, side string, position Position) []Definition {
	context := withReceiverContext(doc, receiver, position)
	if context.kind == "static" {
		context.ownerKey = doc.semanticScopeAt(position).key
	}
	return s.memberDefinitionsForContext(context, doc, side, position)
}

func tempVariableVisible(variable VariableSymbol, currentFunction *FunctionSymbol) bool {
	if !strings.EqualFold(variable.Scope, "temp") {
		return true
	}
	return currentFunction != nil && variable.FunctionRange == currentFunction.BodyRange
}

func memberAvailableInSide(member VariableSymbol, side string) bool {
	return member.Side == "" || side == "" || strings.EqualFold(member.Side, side)
}

func (s *LanguageServer) hover(params TextDocumentPositionParams) *Hover {
	doc := s.workspace.document(params.TextDocument.URI)
	if doc == nil {
		return nil
	}
	offset := offsetAt(doc.Text, params.Position)
	start, end, word := identifierRange(doc.Text, offset)
	if word == "" {
		return nil
	}
	sig := significantBefore(doc.Tokens, start)
	fullName := word
	if len(sig) > 0 && sig[len(sig)-1].text == "." {
		receiver := receiverContextAtDot(doc, sig, len(sig)-1, params.Position)
		if receiver.kind == "npc" {
			if definition, ok := s.externalFunctionDefinition("npc", receiver.name, word, doc.sideAtOffset(offset)); ok {
				return &Hover{Contents: MarkupContent{Kind: "markdown", Value: formatDefinition(definition)}, Range: &Range{Start: positionAt(doc.Text, start), End: positionAt(doc.Text, end)}}
			}
		}
		if receiver.name != "" {
			fullName = receiver.name + "." + word
		}
		if receiver.kind == "static" {
			if serverDefinition, found := s.serverScopeDefinition(receiver.name, word, doc.sideAtOffset(offset)); found {
				return &Hover{Contents: MarkupContent{Kind: "markdown", Value: formatDefinition(serverDefinition)}, Range: &Range{Start: positionAt(doc.Text, start), End: positionAt(doc.Text, end)}}
			}
		}
	}
	definition, ok := s.catalog.lookup(fullName)
	if ok && !definitionAvailableInSide(definition, doc.sideAtOffset(offset)) {
		ok = false
	}
	if !ok {
		definition, ok = s.catalog.lookup(word)
		if ok && !definitionAvailableInSide(definition, doc.sideAtOffset(offset)) {
			ok = false
		}
	}
	if !ok {
		for _, fn := range doc.Functions {
			if strings.EqualFold(fn.Name, word) && fn.Side == doc.sideAtOffset(offset) {
				definition = definitionFromFunction(fn)
				ok = true
				break
			}
		}
	}
	if !ok {
		definition, ok = s.joinedClassFunctionDefinition(
			doc.currentJoinedClassNames(params.Position),
			word,
			doc.sideAtOffset(offset),
		)
	}
	if !ok {
		definition, ok = s.classFunctionDefinition(
			doc.importedClassNames(),
			word,
			doc.sideAtOffset(offset),
		)
	}
	if !ok {
		currentFunction := functionAt(doc, params.Position)
		for _, variable := range doc.Variables {
			if !tempVariableVisible(variable, currentFunction) {
				continue
			}
			if strings.EqualFold(variable.Name, word) {
				definition = Definition{Name: variable.Name, Kind: "variable", Scope: variable.Scope, Description: variable.Detail}
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil
	}
	return &Hover{
		Contents: MarkupContent{Kind: "markdown", Value: formatDefinition(definition)},
		Range:    &Range{Start: positionAt(doc.Text, start), End: positionAt(doc.Text, end)},
	}
}

func (s *LanguageServer) signatureHelp(params TextDocumentPositionParams) *SignatureHelp {
	doc := s.workspace.document(params.TextDocument.URI)
	if doc == nil {
		return nil
	}
	name, active, receiver, ok := callContext(doc, params.Position)
	if !ok {
		return nil
	}
	definition, found := Definition{}, false
	if receiver.kind == "npc" {
		definition, found = s.externalFunctionDefinition("npc", receiver.name, name, doc.sideAtOffset(offsetAt(doc.Text, params.Position)))
	} else if receiver.kind == "class" {
		definition, found = s.classFunctionDefinition(
			[]string{receiver.name},
			name,
			doc.sideAtOffset(offsetAt(doc.Text, params.Position)),
		)
	}
	if !found {
		definition, found = s.catalog.lookup(name)
		if found && !definitionAvailableInSide(definition, doc.sideAtOffset(offsetAt(doc.Text, params.Position))) {
			found = false
		}
	}
	if !found {
		side := doc.sideAtOffset(offsetAt(doc.Text, params.Position))
		for _, fn := range doc.Functions {
			if strings.EqualFold(fn.Name, name) && (fn.Side == "" || strings.EqualFold(fn.Side, side)) {
				definition = definitionFromFunction(fn)
				found = true
				break
			}
		}
	}
	if !found {
		definition, found = s.joinedClassFunctionDefinition(
			doc.currentJoinedClassNames(params.Position),
			name,
			doc.sideAtOffset(offsetAt(doc.Text, params.Position)),
		)
	}
	if !found {
		definition, found = s.classFunctionDefinition(
			doc.importedClassNames(),
			name,
			doc.sideAtOffset(offsetAt(doc.Text, params.Position)),
		)
	}
	if !found || definition.Kind != "function" {
		return nil
	}
	if len(definition.Params) > 0 && active >= len(definition.Params) {
		active = len(definition.Params) - 1
	}
	parameters := make([]ParameterInformation, 0, len(definition.Params))
	for _, param := range definition.Params {
		parameters = append(parameters, ParameterInformation{Label: param, Documentation: definition.ParameterDocs[param]})
	}
	return &SignatureHelp{
		Signatures: []SignatureInformation{{
			Label:         formatSignature(definition),
			Documentation: definition.Description,
			Parameters:    parameters,
		}},
		ActiveSignature: 0,
		ActiveParameter: active,
	}
}

func callContext(doc *Document, position Position) (string, int, receiverContext, bool) {
	offset := offsetAt(doc.Text, position)
	tokens := significantBefore(doc.Tokens, offset)
	depth := 0
	active := 0
	for i := len(tokens) - 1; i >= 0; i-- {
		switch tokens[i].text {
		case ")", "]", "}":
			depth++
		case "(":
			if depth == 0 {
				callee := previousIdentifier(tokens, i-1)
				if callee == "" {
					return "", 0, receiverContext{}, false
				}
				receiver := receiverContext{}
				if i >= 2 && tokens[i-2].text == "." {
					receiver = receiverContextAtDot(doc, tokens, i-2, position)
				} else if i >= 2 && tokens[i-2].text == "::" {
					receiver = receiverContext{kind: "class", name: previousIdentifier(tokens, i-3)}
				}
				return callee, active, receiver, true
			}
			depth--
		case ",":
			if depth == 0 {
				active++
			}
		}
	}
	return "", 0, receiverContext{}, false
}

func completionItem(definition Definition) CompletionItem {
	detail := definition.Scope
	if isEnumMemberKind(definition.Kind) {
		if value := strings.TrimSpace(definition.EnumValue); value != "" {
			detail += " = " + value
		}
	} else if signature := formatSignature(definition); signature != "" {
		detail = signature
		if scope := completionScope(definition); scope != "" {
			detail += " · " + scope
		}
	}
	if definition.Scope != "" && definition.Kind != "function" && !isEnumMemberKind(definition.Kind) {
		detail = definition.Kind + " · " + definition.Scope
	}
	return CompletionItem{
		Label:         definition.Name,
		Kind:          completionKind(definition.Kind),
		Detail:        detail,
		Documentation: MarkupContent{Kind: "markdown", Value: formatDefinition(definition)},
		SortText:      completionSortText(definition),
		InsertText:    definition.Name,
	}
}

func completionScope(definition Definition) string {
	scope := strings.TrimSpace(definition.Scope)
	switch strings.ToLower(scope) {
	case "", "global", "local", "parameter":
		return ""
	default:
		return scope
	}
}

func completionKind(kind string) int {
	switch strings.ToLower(kind) {
	case "function", "method":
		return 3
	case "variable", "constant":
		return 6
	case "class", "type":
		return 7
	case "enum":
		return 13
	case "enum-member", "enummember":
		return 20
	default:
		return 10
	}
}

func completionSortText(definition Definition) string {
	switch strings.ToLower(definition.Kind) {
	case "variable", "constant":
		return "1-" + strings.ToLower(definition.Name)
	case "enum-member", "enummember":
		return "1-" + strings.ToLower(definition.Name)
	case "function", "method":
		return "2-" + strings.ToLower(definition.Name)
	default:
		return "3-" + strings.ToLower(definition.Name)
	}
}

func formatSignature(definition Definition) string {
	if definition.Kind != "function" && definition.Kind != "method" {
		return ""
	}
	return definition.Name + "(" + strings.Join(definition.Params, ", ") + ")" + returnSuffix(definition.Returns)
}

func isEnumMemberKind(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "enum-member", "enummember":
		return true
	default:
		return false
	}
}

func returnSuffix(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return " -> " + value
}

func formatDefinition(definition Definition) string {
	var out strings.Builder
	if signature := formatSignature(definition); signature != "" {
		out.WriteString("```graalscript\n")
		out.WriteString(signature)
		out.WriteString("\n```")
	} else if isEnumMemberKind(definition.Kind) {
		out.WriteString("```graalscript\n")
		out.WriteString(definition.Name)
		if value := strings.TrimSpace(definition.EnumValue); value != "" {
			out.WriteString(" = ")
			out.WriteString(value)
		}
		out.WriteString("\n```")
	} else {
		out.WriteString("**")
		out.WriteString(definition.Name)
		out.WriteString("**")
	}
	if definition.Scope != "" {
		out.WriteString("\n\nScope: `")
		out.WriteString(definition.Scope)
		out.WriteString("`")
	}
	if definition.Description != "" {
		out.WriteString("\n\n")
		out.WriteString(definition.Description)
	}
	if definition.Example != "" {
		out.WriteString("\n\n```graalscript\n")
		out.WriteString(definition.Example)
		out.WriteString("\n```")
	}
	return out.String()
}

func definitionFromFunction(fn FunctionSymbol) Definition {
	return definitionFromFunctionInScope(fn, "local")
}

func classScope(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Class"
	}
	return "Class " + name
}

func (s *LanguageServer) classFunctionDefinition(classNames []string, functionName, side string) (Definition, bool) {
	for _, className := range classNames {
		for _, fn := range s.workspace.classSymbols([]string{className}, side) {
			if strings.EqualFold(fn.Name, functionName) {
				return definitionFromFunctionInScope(fn, classScope(className)), true
			}
		}
	}
	return Definition{}, false
}

func (s *LanguageServer) joinedClassFunctionDefinition(classNames []string, functionName, side string) (Definition, bool) {
	for _, className := range classNames {
		for _, fn := range s.workspace.joinedClassSymbols([]string{className}, side) {
			if strings.EqualFold(fn.Name, functionName) {
				return definitionFromFunctionInScope(fn, classScope(className)), true
			}
		}
	}
	return Definition{}, false
}

func definitionFromFunctionInScope(fn FunctionSymbol, scope string) Definition {
	return Definition{
		Name: fn.Name, Kind: "function", Params: append([]string(nil), fn.Params...),
		Returns: fn.ReturnType, Scope: scope, Description: fn.Documentation,
		ParameterDocs: cloneStringMap(fn.ParameterDocs), ReturnDoc: fn.ReturnDoc,
		DocTags: append([]JSDocTag(nil), fn.DocTags...),
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

type receiverContext struct {
	kind         string
	name         string
	names        []string
	ownerKey     string
	baseKind     string
	baseName     string
	baseNames    []string
	baseOwnerKey string
}

func (receiver receiverContext) objectNames() []string {
	if len(receiver.names) > 0 {
		return receiver.names
	}
	if receiver.name == "" {
		return nil
	}
	return []string{receiver.name}
}

func (s *LanguageServer) externalFunctionDefinition(kind, objectName, functionName, side string) (Definition, bool) {
	for _, fn := range s.workspace.objectSymbols(kind, objectName, side) {
		if strings.EqualFold(fn.Name, functionName) {
			owner := strings.ToUpper(kind[:1]) + kind[1:]
			return definitionFromFunctionInScope(fn, owner+" "+objectName+" · "+side), true
		}
	}
	return Definition{}, false
}

func definitionAvailableInSide(definition Definition, side string) bool {
	scope := strings.ToLower(normalizeDefinitionScope(definition.Scope))
	side = strings.ToLower(strings.TrimSpace(side))
	if scope == "" || side == "" || scope == "global" || scope == "local" || scope == "parameter" {
		return true
	}
	hasClient := strings.Contains(scope, "client")
	hasServer := strings.Contains(scope, "server")
	if hasClient && !hasServer {
		return side == scriptSideClient
	}
	if hasServer && !hasClient {
		return side == scriptSideServer
	}
	return true
}

func dynamicPropertyCompletionScope(doc *Document, offset int) (string, bool) {
	open, scope, ok := dynamicPropertyOpenBefore(doc, offset)
	if !ok {
		return "", false
	}
	prefix := doc.Text[doc.Tokens[open].end:offset]
	prefix = strings.TrimSpace(prefix)
	if strings.HasPrefix(prefix, "@") {
		prefix = strings.TrimSpace(prefix[1:])
	}
	if prefix == "" {
		return scope, true
	}
	if !strings.HasPrefix(prefix, "\"") {
		return "", false
	}
	prefix = prefix[1:]
	if strings.Contains(prefix, "\"") {
		return "", false
	}
	return scope, true
}

func dynamicPropertyOpenBefore(doc *Document, offset int) (int, string, bool) {
	depth := 0
	for i := len(doc.Tokens) - 1; i >= 0; i-- {
		tok := doc.Tokens[i]
		if tok.kind == tokenComment || tok.kind == tokenEOF || tok.end > offset {
			continue
		}
		switch tok.text {
		case ")", "]", "}":
			depth++
		case "(", "[", "{":
			if depth > 0 {
				depth--
				continue
			}
			if tok.text != "(" || i < 2 || doc.Tokens[i-1].text != "." {
				continue
			}
			scope := doc.Tokens[i-2]
			if scope.kind != tokenIdentifier || !isDynamicVariableScope(scope.text) {
				continue
			}
			return i, strings.ToLower(scope.text), true
		}
	}
	return -1, "", false
}

func receiverContextAtDot(doc *Document, tokens []token, dotIndex int, position Position) receiverContext {
	if dotIndex <= 0 || dotIndex >= len(tokens) || tokens[dotIndex].text != "." {
		return receiverContext{}
	}
	if receiver := variableReceiverContext(doc, tokens, dotIndex, position); receiver.kind != "" {
		return receiver
	}
	if receiver := dynamicVariableReceiverContext(doc, tokens, dotIndex, position); receiver.kind != "" {
		return receiver
	}
	if receiver := previousIdentifier(tokens, dotIndex-1); receiver != "" {
		if parameter := doc.parameterSymbol(receiver, position); parameter != nil {
			if context := receiverContextFromType(*parameter); context.kind != "" {
				return context
			}
		}
		if variable := doc.symbolFor("temp", receiver, position); variable != nil {
			if context := receiverContextFromType(*variable); context.kind != "" {
				return context
			}
		}
		return receiverContextForNamedReceiver(doc, receiver, position)
	}
	if tokens[dotIndex-1].text != ")" {
		return receiverContext{}
	}
	open := matchingOpenBefore(tokens, dotIndex-1, "(", ")")
	if open < 0 {
		return receiverContext{}
	}
	value := nextSignificant(tokens, open+1)
	callee := previousSignificant(tokens, open-1)
	if callee >= 0 && tokens[callee].kind == tokenIdentifier {
		calleeName := tokens[callee].text
		switch strings.ToLower(calleeName) {
		case "findplayer", "findplayer2", "findplayerbyid":
			return receiverContext{kind: "player", name: "player"}
		case "findnpc", "findnpcbyname", "findnpcbyid", "findweapon", "findlevel":
			if value < 0 || value >= dotIndex {
				return receiverContext{}
			}
			name := stringExpressionValue(doc, tokens, value, dotIndex, position)
			if name == "" {
				return receiverContext{}
			}
			switch strings.ToLower(calleeName) {
			case "findnpc", "findnpcbyname", "findnpcbyid":
				return receiverContext{kind: "npc", name: name}
			case "findweapon":
				return receiverContext{kind: "weapon", name: name}
			case "findlevel":
				return receiverContext{kind: "level", name: name}
			}
		}
	}
	if value < 0 || value >= dotIndex {
		return receiverContext{}
	}
	name := stringExpressionValue(doc, tokens, value, dotIndex, position)
	if name == "" {
		return receiverContext{}
	}
	if callee < 0 || tokens[callee].kind != tokenIdentifier {
		return receiverContext{kind: "string", name: name}
	}
	return receiverContext{}
}

func dynamicVariableReceiverContext(doc *Document, tokens []token, dotIndex int, position Position) receiverContext {
	if dotIndex <= 0 || tokens[dotIndex-1].text != ")" {
		return receiverContext{}
	}
	open := matchingOpenBefore(tokens, dotIndex-1, "(", ")")
	if open < 2 || tokens[open-1].text != "." {
		return receiverContext{}
	}
	scopeIndex := open - 2
	scope := tokens[scopeIndex]
	if scope.kind != tokenIdentifier || !isDynamicVariableScope(scope.text) {
		return receiverContext{}
	}
	names, ok := doc.dynamicPropertyNamesInTokens(tokens, open+1, dotIndex-1, position)
	if !ok {
		return receiverContext{}
	}
	symbols := make([]VariableSymbol, 0, len(names))
	for _, name := range names {
		symbol := doc.symbolFor(scope.text, name, position)
		if symbol == nil {
			continue
		}
		symbols = append(symbols, *symbol)
	}
	if len(symbols) == 0 {
		return receiverContext{}
	}
	receiverName := strings.TrimSpace(doc.Text[tokens[scopeIndex].start:tokens[dotIndex-1].end])
	if classes := doc.joinedClassesForReceiver(receiverName, position); len(classes) > 0 {
		return receiverContext{kind: "joined", names: classes}
	}
	return receiverContextFromSymbols(symbols)
}

func variableReceiverContext(doc *Document, tokens []token, dotIndex int, position Position) receiverContext {
	if dotIndex < 3 || tokens[dotIndex-2].text != "." {
		return receiverContext{}
	}
	scopeIndex := dotIndex - 3
	memberIndex := dotIndex - 1
	scope := tokens[scopeIndex]
	member := tokens[memberIndex]
	if scope.kind != tokenIdentifier || member.kind != tokenIdentifier {
		return receiverContext{}
	}
	scopeName := strings.ToLower(scope.text)
	if !isDynamicVariableScope(scopeName) {
		return receiverContext{}
	}
	if scopeName == "this" && strings.EqualFold(member.text, "profile") {
		if receiver := guiProfileReceiverContext(doc, position); receiver.kind != "" {
			return receiver
		}
	}
	symbol := doc.symbolFor(scopeName, member.text, position)
	if symbol == nil {
		return receiverContext{}
	}
	receiverName := strings.TrimSpace(doc.Text[tokens[scopeIndex].start:tokens[memberIndex].end])
	if classes := doc.joinedClassesForReceiver(receiverName, position); len(classes) > 0 {
		return receiverContext{kind: "joined", names: classes}
	}
	return receiverContextFromSymbol(*symbol)
}

func receiverContextForNamedReceiver(doc *Document, receiver string, position Position) receiverContext {
	lower := strings.ToLower(strings.TrimSpace(receiver))
	if lower == "profile" {
		if context := guiProfileReceiverContext(doc, position); context.kind != "" {
			return context
		}
	}
	if lower != "this" && lower != "thiso" {
		return receiverContext{kind: "static", name: receiver}
	}

	scope := doc.semanticScopeAt(position)
	if lower == "thiso" {
		scope = semanticScope{kind: "script", key: scriptScopeKey}
	}
	base := receiverContext{kind: "static", name: lower, ownerKey: scope.key}
	switch scope.kind {
	case "gui":
		base = receiverContext{kind: "gui", name: scope.controlType, ownerKey: scope.key}
	case "with":
		base = withReceiverContext(doc, scope.receiver, position)
		if base.kind == "static" {
			base.ownerKey = scope.key
		}
	}
	classes := doc.joinedClassesForNamedReceiver(receiver, position)
	if len(classes) > 0 {
		return receiverContext{
			kind:         "joined",
			names:        classes,
			baseKind:     base.kind,
			baseName:     base.name,
			baseNames:    append([]string(nil), base.names...),
			baseOwnerKey: base.ownerKey,
		}
	}
	return base
}

func guiProfileReceiverContext(doc *Document, position Position) receiverContext {
	scope := doc.semanticScopeAt(position)
	if scope.kind != "gui" || isGUIProfileType(scope.controlType) {
		return receiverContext{}
	}
	return receiverContext{
		kind:     "gui-profile",
		name:     "GuiControlProfile",
		ownerKey: scope.key,
	}
}

func withReceiverContext(doc *Document, receiver string, position Position) receiverContext {
	if context := receiverContextForExpression(doc, receiver, position); context.kind != "" {
		return context
	}
	lower := strings.ToLower(strings.TrimSpace(receiver))
	switch {
	case strings.Contains(lower, "findnpc"):
		return receiverContext{kind: "npc", name: callArgumentValue(receiver)}
	case strings.Contains(lower, "findweapon"):
		return receiverContext{kind: "weapon", name: callArgumentValue(receiver)}
	case strings.Contains(lower, "findplayer"):
		return receiverContext{kind: "player", name: "player"}
	case strings.Contains(lower, "findlevel"):
		return receiverContext{kind: "level", name: "level"}
	}
	if classes := doc.joinedClassesForReceiver(receiver, position); len(classes) > 0 {
		return receiverContext{kind: "joined", names: classes}
	}
	return receiverContext{kind: "static", name: "this"}
}

func receiverContextForExpression(doc *Document, expression string, position Position) receiverContext {
	tokens := lex(strings.TrimSpace(expression))
	start := nextSignificant(tokens, 0)
	if start < 0 || tokens[start].kind != tokenIdentifier {
		return receiverContext{}
	}
	dot := nextSignificant(tokens, start+1)
	member := nextSignificant(tokens, dot+1)
	if dot < 0 || member < 0 || tokens[dot].text != "." || tokens[member].kind != tokenIdentifier {
		return receiverContext{}
	}
	symbol := doc.receiverSymbol(tokens[start].text, tokens[member].text, position)
	if symbol == nil {
		return receiverContext{}
	}
	return receiverContextFromSymbol(*symbol)
}

func callArgumentValue(expression string) string {
	open := strings.IndexByte(expression, '(')
	close := strings.LastIndexByte(expression, ')')
	if open < 0 || close <= open {
		return ""
	}
	argument := strings.TrimSpace(expression[open+1 : close])
	if len(argument) >= 2 && argument[0] == '"' && argument[len(argument)-1] == '"' {
		return strings.Trim(argument[1:len(argument)-1], " ")
	}
	return ""
}

func receiverContextFromSymbol(symbol VariableSymbol) receiverContext {
	return receiverContextFromSymbols([]VariableSymbol{symbol})
}

func receiverContextFromSymbols(symbols []VariableSymbol) receiverContext {
	if len(symbols) == 0 {
		return receiverContext{}
	}
	kind := ""
	names := []string{}
	for _, symbol := range symbols {
		context := receiverContextFromType(symbol)
		if context.kind == "" {
			continue
		}
		if kind == "" {
			kind = context.kind
		}
		if kind != context.kind {
			return receiverContext{}
		}
		if context.name != "" && !containsFold(names, context.name) {
			names = append(names, context.name)
		}
	}
	if kind == "" {
		return receiverContext{}
	}
	name := ""
	if len(names) > 0 {
		name = names[0]
	}
	return receiverContext{kind: kind, name: name, names: names}
}

func receiverContextFromType(symbol VariableSymbol) receiverContext {
	typeName := strings.TrimSpace(symbol.Type)
	switch strings.ToLower(typeName) {
	case "npc":
		if symbol.Value != "" {
			return receiverContext{kind: "npc", name: symbol.Value}
		}
	case "weapon":
		if symbol.Value != "" {
			return receiverContext{kind: "weapon", name: symbol.Value}
		}
	case "player":
		return receiverContext{kind: "player", name: "player"}
	case "level":
		return receiverContext{kind: "level", name: "level"}
	case "", "bool", "boolean", "float", "int", "nil", "number":
		return receiverContext{}
	default:
		return receiverContext{kind: "class", name: typeName}
	}
	return receiverContext{}
}

func containsFold(items []string, value string) bool {
	for _, item := range items {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func stringExpressionValue(doc *Document, tokens []token, start, end int, position Position) string {
	if start < 0 || start >= len(tokens) || start >= end {
		return ""
	}
	if tokens[start].kind == tokenString {
		return tokenStringValue(tokens[start])
	}
	if tokens[start].kind != tokenIdentifier {
		return ""
	}
	dot := nextSignificant(tokens, start+1)
	if dot < 0 || dot >= end || tokens[dot].text != "." {
		return ""
	}
	member := nextSignificant(tokens, dot+1)
	if member < 0 || member >= end {
		return ""
	}
	scope := strings.ToLower(tokens[start].text)
	if !isDynamicVariableScope(scope) {
		return ""
	}
	name := ""
	if tokens[member].kind == tokenIdentifier {
		name = tokens[member].text
	} else if tokens[member].text == "(" {
		close := matchingToken(tokens, member, "(", ")")
		if close < 0 || close >= end {
			return ""
		}
		resolved, ok := doc.dynamicPropertyNameInTokens(tokens, member+1, close, position)
		if !ok {
			return ""
		}
		name = resolved
	} else {
		return ""
	}
	symbol := doc.symbolFor(scope, name, position)
	if symbol == nil || symbol.Type != "string" {
		return ""
	}
	return symbol.Value
}

func matchingOpenBefore(tokens []token, closeIndex int, open, close string) int {
	depth := 0
	for i := closeIndex; i >= 0; i-- {
		switch tokens[i].text {
		case close:
			depth++
		case open:
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func isNPCFinder(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "findnpc", "findnpcbyname", "findnpcbyid":
		return true
	default:
		return false
	}
}

func previousIdentifier(tokens []token, index int) string {
	for i := index; i >= 0; i-- {
		if tokens[i].kind == tokenIdentifier {
			return tokens[i].text
		}
		if tokens[i].text != "." && tokens[i].text != "::" {
			return ""
		}
	}
	return ""
}

func significantBefore(tokens []token, offset int) []token {
	result := make([]token, 0, len(tokens))
	for _, tok := range tokens {
		if tok.kind == tokenComment || tok.kind == tokenEOF || tok.end > offset {
			continue
		}
		result = append(result, tok)
	}
	return result
}

func identifierStart(text string, offset int) int {
	if offset > len(text) {
		offset = len(text)
	}
	for offset > 0 {
		r, size := lastRune(text[:offset])
		if !isIdentifierPart(r) {
			break
		}
		offset -= size
	}
	return offset
}

func identifierRange(text string, offset int) (int, int, string) {
	start := identifierStart(text, offset)
	end := offset
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if !isIdentifierPart(r) {
			break
		}
		end += size
	}
	return start, end, text[start:end]
}

func lastRune(text string) (rune, int) {
	if text == "" {
		return 0, 0
	}
	r, size := utf8.DecodeLastRuneInString(text)
	return r, size
}

func offsetAt(text string, position Position) int {
	if position.Line < 0 {
		return 0
	}
	line := 0
	offset := 0
	for offset < len(text) && line < position.Line {
		if text[offset] == '\n' {
			line++
		}
		_, size := utf8.DecodeRuneInString(text[offset:])
		offset += size
	}
	if line < position.Line {
		return len(text)
	}
	character := 0
	for offset < len(text) && text[offset] != '\n' {
		r, size := utf8.DecodeRuneInString(text[offset:])
		width := 1
		if r > 0xffff {
			width = 2
		}
		if character+width > position.Character {
			break
		}
		character += width
		offset += size
	}
	return offset
}

func positionAt(text string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}
	line := 0
	character := 0
	for i := 0; i < offset; {
		r, size := utf8.DecodeRuneInString(text[i:])
		if r == '\n' {
			line++
			character = 0
		} else if r > 0xffff {
			character += 2
		} else {
			character++
		}
		i += size
	}
	return Position{Line: line, Character: character}
}

func withBlockAt(doc *Document, position Position) *WithBlock {
	for i := range doc.With {
		block := &doc.With[i]
		if position.Line > block.Range.Start.Line && position.Line < block.Range.End.Line {
			return block
		}
		if position.Line == block.Range.Start.Line && position.Character >= block.Range.Start.Character {
			return block
		}
	}
	return nil
}

func functionAt(doc *Document, position Position) *FunctionSymbol {
	for i := range doc.Functions {
		fn := &doc.Functions[i]
		if positionInRange(position, fn.BodyRange) {
			return fn
		}
	}
	return nil
}

func positionInRange(position Position, value Range) bool {
	if position.Line < value.Start.Line || position.Line > value.End.Line {
		return false
	}
	if position.Line == value.Start.Line && position.Character < value.Start.Character {
		return false
	}
	if position.Line == value.End.Line && position.Character > value.End.Character {
		return false
	}
	return true
}
