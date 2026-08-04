package graalscript

import (
	"encoding/json"
	"fmt"
)

// Position and Range intentionally mirror the Language Server Protocol data
// types. Character offsets are UTF-16 code units, as required by LSP.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

type TextDocumentContentChangeEvent struct {
	Range       *Range `json:"range,omitempty"`
	RangeLength int    `json:"rangeLength,omitempty"`
	Text        string `json:"text"`
}

type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type DocumentDiagnosticParams struct {
	TextDocument     TextDocumentIdentifier `json:"textDocument"`
	PreviousResultID string                 `json:"previousResultId,omitempty"`
}

type InitializeParams struct {
	RootURI  string `json:"rootUri"`
	RootPath string `json:"rootPath,omitempty"`
}

type ServerCapabilities struct {
	CompletionProvider    *CompletionOptions    `json:"completionProvider,omitempty"`
	HoverProvider         bool                  `json:"hoverProvider,omitempty"`
	SignatureHelpProvider *SignatureHelpOptions `json:"signatureHelpProvider,omitempty"`
	DiagnosticProvider    *DiagnosticOptions    `json:"diagnosticProvider,omitempty"`
}

type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

type SignatureHelpOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
}

type DiagnosticOptions struct {
	InterFileDependencies bool `json:"interFileDependencies,omitempty"`
	WorkspaceDiagnostics  bool `json:"workspaceDiagnostics,omitempty"`
}

type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      *CompletionContext     `json:"context,omitempty"`
}

type CompletionContext struct {
	TriggerKind      int    `json:"triggerKind"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

type CompletionItem struct {
	Label         string    `json:"label"`
	Kind          int       `json:"kind,omitempty"`
	Detail        string    `json:"detail,omitempty"`
	Documentation any       `json:"documentation,omitempty"`
	SortText      string    `json:"sortText,omitempty"`
	InsertText    string    `json:"insertText,omitempty"`
	TextEdit      *TextEdit `json:"textEdit,omitempty"`
}

type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

type Hover struct {
	Contents any    `json:"contents"`
	Range    *Range `json:"range,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature,omitempty"`
	ActiveParameter int                    `json:"activeParameter,omitempty"`
}

type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation any                    `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

type ParameterInformation struct {
	Label         string `json:"label"`
	Documentation any    `json:"documentation,omitempty"`
}

// Diagnostic follows the pull-diagnostics shape from LSP. Severity uses the
// LSP values: 1=Error, 2=Warning, 3=Information, 4=Hint.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

type DocumentDiagnosticReport struct {
	Kind     string       `json:"kind"`
	ResultID string       `json:"resultId,omitempty"`
	Items    []Diagnostic `json:"items,omitempty"`
}

type jsonRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

const (
	jsonRPCInvalidRequest       = -32600
	jsonRPCMethodNotFound       = -32601
	jsonRPCInvalidParams        = -32602
	jsonRPCInternalError        = -32603
	jsonRPCServerNotInitialized = -32002
)

func makeRPCError(id json.RawMessage, code int, err error) []byte {
	response := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &jsonRPCError{Code: code, Message: err.Error()},
	}
	b, marshalErr := json.Marshal(response)
	if marshalErr != nil {
		return []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":null,"error":{"code":%d,"message":"internal error"}}`, jsonRPCInternalError))
	}
	return b
}

func makeRPCResult(id json.RawMessage, result any) []byte {
	encoded, err := json.Marshal(result)
	if err != nil {
		return makeRPCError(id, jsonRPCInternalError, err)
	}
	response := jsonRPCResponse{JSONRPC: "2.0", ID: id, Result: encoded}
	b, err := json.Marshal(response)
	if err != nil {
		return makeRPCError(id, jsonRPCInternalError, err)
	}
	return b
}
