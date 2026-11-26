package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// Larger files are only reported by their local path, to spare the
	// client's context.
	maxInlineTextBytes  = 256 << 10
	maxInlineImageBytes = 2 << 20
	// transferWriteTimeout replaces the server's WriteTimeout for transfers,
	// which can wait ten minutes for the queue or for the game server.
	transferWriteTimeout = 15 * time.Minute
)

// binaryExtensions are refused by filebrowser_read before the file is asked
// for. Content with any other name is checked once it arrives.
var binaryExtensions = map[string]bool{
	".png": true, ".gif": true, ".jpg": true, ".jpeg": true, ".bmp": true, ".webp": true, ".ico": true, ".mng": true,
	".wav": true, ".mp3": true, ".ogg": true, ".m4a": true, ".flac": true, ".mid": true,
	".mp4": true, ".webm": true, ".mov": true, ".avi": true, ".mkv": true,
	".zip": true, ".gz": true, ".7z": true, ".rar": true,
	".db": true, ".sqlite": true, ".sqlite3": true,
	".ttf": true, ".otf": true, ".pdf": true, ".exe": true, ".dll": true,
}

var errBinaryRead = errors.New("filebrowser_read only reads text and this file is binary: use filebrowser_download")

func hasBinaryExtension(remotePath string) bool {
	return binaryExtensions[strings.ToLower(path.Ext(strings.ReplaceAll(remotePath, "\\", "/")))]
}

func isText(content []byte) bool {
	return utf8.Valid(content) && bytes.IndexByte(content, 0) < 0
}

// DownloadedFile is a remote file saved on this machine.
type DownloadedFile struct {
	Path      string
	LocalPath string
	Content   []byte
}

// UploadRequest names the remote destination and exactly one source.
type UploadRequest struct {
	Path      string
	LocalPath string
	Content   *string
}

type UploadResult struct {
	Path     string `json:"path"`
	Size     int    `json:"size"`
	Replaced bool   `json:"replaced"`
}

// FileTransferBackend moves whole files between the server and this machine.
type FileTransferBackend interface {
	DownloadFile(ctx context.Context, path string) (DownloadedFile, error)
	UploadFile(ctx context.Context, upload UploadRequest) (UploadResult, error)
}

func fileToolDefinitions() []map[string]any {
	return []map[string]any{
		{"name": "filebrowser_download", "description": "Baixa um arquivo remoto de qualquer tipo, salva neste computador e retorna o caminho local. Texto pequeno vem também em content; PNG, GIF e JPG pequenos vêm como imagem.", "inputSchema": objectSchema(map[string]any{
			"path": map[string]any{"type": "string", "minLength": 1},
		}, "path")},
		{"name": "filebrowser_upload", "description": "Envia um arquivo para o servidor. path é o destino remoto completo, com o nome do arquivo. Informe localPath (caminho absoluto neste computador) ou content (texto), nunca os dois. Um arquivo existente é substituído.", "inputSchema": objectSchema(map[string]any{
			"path":      map[string]any{"type": "string", "minLength": 1},
			"localPath": map[string]any{"type": "string", "minLength": 1},
			"content":   map[string]any{"type": "string"},
		}, "path")},
	}
}

// extendTransferDeadline keeps the HTTP server from dropping the answer of a
// slow transfer.
func extendTransferDeadline(w http.ResponseWriter, method string, params json.RawMessage) {
	var p struct {
		Name string `json:"name"`
	}
	if method != "tools/call" || json.Unmarshal(params, &p) != nil {
		return
	}
	switch p.Name {
	case "filebrowser_read", "filebrowser_download", "filebrowser_upload":
		// A writer without deadlines has none to extend.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(transferWriteTimeout))
	}
}

func (s *Server) callFileTool(ctx context.Context, name string, args map[string]any) (any, *rpcError, bool) {
	if name != "filebrowser_download" && name != "filebrowser_upload" {
		return nil, nil, false
	}
	path, _ := stringArg(args, "path")
	if strings.TrimSpace(path) == "" {
		return nil, &rpcError{-32602, "path is required"}, true
	}
	if name == "filebrowser_download" {
		file, err := s.backend.DownloadFile(ctx, path)
		if err != nil {
			result, rpcErr := toolError(err)
			return result, rpcErr, true
		}
		result, rpcErr := downloadedResult(file)
		return result, rpcErr, true
	}

	upload := UploadRequest{Path: path}
	localPath, hasLocal := args["localPath"]
	content, hasContent := args["content"]
	if hasLocal == hasContent {
		return nil, &rpcError{-32602, "pass either localPath or content"}, true
	}
	if hasLocal {
		value, ok := localPath.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return nil, &rpcError{-32602, "localPath must be a file path"}, true
		}
		upload.LocalPath = value
	} else {
		value, ok := content.(string)
		if !ok {
			return nil, &rpcError{-32602, "content must be a string"}, true
		}
		upload.Content = &value
	}
	uploaded, err := s.backend.UploadFile(ctx, upload)
	if err != nil {
		result, rpcErr := toolError(err)
		return result, rpcErr, true
	}
	result, rpcErr := textResult(uploaded)
	return result, rpcErr, true
}

func downloadedResult(file DownloadedFile) (any, *rpcError) {
	metadata := map[string]any{"path": file.Path, "localPath": file.LocalPath, "size": len(file.Content)}
	mimeType := http.DetectContentType(file.Content)
	isImage := mimeType == "image/png" || mimeType == "image/gif" || mimeType == "image/jpeg"
	if !isImage && len(file.Content) <= maxInlineTextBytes && isText(file.Content) {
		metadata["content"] = string(file.Content)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, &rpcError{-32603, err.Error()}
	}
	blocks := []map[string]string{{"type": "text", "text": string(encoded)}}
	if isImage && len(file.Content) <= maxInlineImageBytes {
		blocks = append(blocks, map[string]string{"type": "image", "mimeType": mimeType, "data": base64.StdEncoding.EncodeToString(file.Content)})
	}
	return map[string]any{"content": blocks, "isError": false}, nil
}
