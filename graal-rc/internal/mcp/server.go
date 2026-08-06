// Package mcp contains the small, local MCP HTTP transport used by Graal RC.
// It intentionally implements only the MCP JSON-RPC surface needed by the
// desktop integration, keeping the application free of an SDK/runtime server.
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"
)

type Backend interface {
	GetRCChat(limit int) []ChatLine
	SendRCChat(message string) error
	FileBrowserStart() error
	FileBrowserCd(path string) error
	FileBrowserFiles() ([]File, error)
	ReadFile(path string) ([]byte, error)
}

type ChatLine struct {
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

type File struct {
	Path        string `json:"path"`
	Size        int    `json:"size"`
	IsDirectory bool   `json:"isDirectory"`
}

type Server struct{ backend Backend }

func NewServer(backend Backend) *Server { return &Server{backend: backend} }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "MCP endpoint accepts POST only", http.StatusMethodNotAllowed)
		return
	}
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.write(w, response{JSONRPC: "2.0", ID: nil, Error: &rpcError{-32700, "invalid JSON"}})
		return
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		s.write(w, response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{-32600, "invalid JSON-RPC request"}})
		return
	}
	result, rpcErr := s.call(r.Context(), req.Method, req.Params)
	// Notifications do not receive a response per JSON-RPC.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp := response{JSONRPC: "2.0", ID: req.ID, Result: result, Error: rpcErr}
	s.write(w, resp)
}

func (s *Server) write(w http.ResponseWriter, value response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) call(ctx context.Context, method string, raw json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "graal-rc", "version": "1.0"},
		}, nil
	case "notifications/initialized":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions()}, nil
	case "tools/call":
		return s.callTool(ctx, raw)
	default:
		return nil, &rpcError{-32601, "method not found"}
	}
}

func toolDefinitions() []map[string]any {
	return []map[string]any{
		{"name": "get_rc_chat", "description": "Retorna as últimas mensagens do RC Chat com timestamp.", "inputSchema": objectSchema(map[string]any{
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 50, "default": 50},
		})},
		{"name": "send_rc_chat", "description": "Envia uma mensagem para o RC Chat.", "inputSchema": objectSchema(map[string]any{
			"message": map[string]any{"type": "string", "minLength": 1},
		})},
		{"name": "filebrowser_list", "description": "Lista os arquivos do diretório atual do File Browser.", "inputSchema": objectSchema(map[string]any{})},
		{"name": "filebrowser_cd", "description": "Navega para um diretório remoto do File Browser.", "inputSchema": objectSchema(map[string]any{
			"path": map[string]any{"type": "string", "minLength": 1},
		})},
		{"name": "filebrowser_search", "description": "Procura no snapshot do diretório atual por nome/caminho.", "inputSchema": objectSchema(map[string]any{
			"query": map[string]any{"type": "string", "minLength": 1},
		})},
		{"name": "filebrowser_read", "description": "Baixa e lê um arquivo remoto como texto UTF-8.", "inputSchema": objectSchema(map[string]any{
			"path": map[string]any{"type": "string", "minLength": 1},
		})},
	}
}

func objectSchema(properties map[string]any) map[string]any {
	return map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
}

func (s *Server) callTool(ctx context.Context, raw json.RawMessage) (any, *rpcError) {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || p.Name == "" {
		return nil, &rpcError{-32602, "tools/call requires a tool name"}
	}
	if err := ctx.Err(); err != nil {
		return nil, &rpcError{-32603, err.Error()}
	}
	textResult := func(value any) (any, *rpcError) {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, &rpcError{-32603, err.Error()}
		}
		return map[string]any{"content": []map[string]string{{"type": "text", "text": string(encoded)}}, "isError": false}, nil
	}
	fail := func(err error) (any, *rpcError) {
		if err == nil {
			err = errors.New("tool failed")
		}
		return map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true}, nil
	}
	switch p.Name {
	case "get_rc_chat":
		limit := intArg(p.Arguments, "limit", 50)
		if limit < 1 || limit > 50 {
			return nil, &rpcError{-32602, "limit must be between 1 and 50"}
		}
		return textResult(map[string]any{"lines": s.backend.GetRCChat(limit)})
	case "send_rc_chat":
		message, ok := stringArg(p.Arguments, "message")
		if !ok || strings.TrimSpace(message) == "" {
			return nil, &rpcError{-32602, "message is required"}
		}
		return fail(s.backend.SendRCChat(message))
	case "filebrowser_list":
		if err := s.backend.FileBrowserStart(); err != nil {
			return fail(err)
		}
		files, err := s.backend.FileBrowserFiles()
		if err != nil {
			return fail(err)
		}
		return textResult(map[string]any{"files": files})
	case "filebrowser_cd":
		path, ok := stringArg(p.Arguments, "path")
		if !ok {
			return nil, &rpcError{-32602, "path is required"}
		}
		return fail(s.backend.FileBrowserCd(path))
	case "filebrowser_search":
		query, ok := stringArg(p.Arguments, "query")
		if !ok || strings.TrimSpace(query) == "" {
			return nil, &rpcError{-32602, "query is required"}
		}
		if err := s.backend.FileBrowserStart(); err != nil {
			return fail(err)
		}
		files, err := s.backend.FileBrowserFiles()
		if err != nil {
			return fail(err)
		}
		query = strings.ToLower(query)
		matches := make([]File, 0)
		for _, file := range files {
			if strings.Contains(strings.ToLower(file.Path), query) {
				matches = append(matches, file)
			}
		}
		return textResult(map[string]any{"files": matches})
	case "filebrowser_read":
		path, ok := stringArg(p.Arguments, "path")
		if !ok {
			return nil, &rpcError{-32602, "path is required"}
		}
		content, err := s.backend.ReadFile(path)
		if err != nil {
			return fail(err)
		}
		if !utf8.Valid(content) {
			return fail(errors.New("remote file is not valid UTF-8 text"))
		}
		return textResult(map[string]any{"path": path, "content": string(content)})
	default:
		return nil, &rpcError{-32601, "unknown tool: " + p.Name}
	}
}

func intArg(args map[string]any, name string, fallback int) int {
	if n, ok := args[name].(float64); ok {
		return int(n)
	}
	return fallback
}
func stringArg(args map[string]any, name string) (string, bool) {
	v, ok := args[name].(string)
	return v, ok
}
