package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"graal-rc/internal/mcp"
)

const defaultMCPAddress = "127.0.0.1:8765"

const mcpInstructionsStart = "#Nullborne Go RC INSTRUCTIONS START#"

type MCPAgentStatus struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	Exists        bool   `json:"exists"`
	Configured    bool   `json:"configured"`
	PreferredPath string `json:"preferredPath"`
}

type MCPSetupResult struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Configured bool   `json:"configured"`
	Created    bool   `json:"created"`
}

func mcpInstructionBlock() string {
	return `
#-----------#
#Nullborne Go RC INSTRUCTIONS START#

You can use the Graal RC MCP server at http://127.0.0.1:8765/mcp when the Graal
RC desktop client is running. It uses the MCP HTTP JSON-RPC
transport. Use these tools only when the user asks you to inspect or operate
their connected Graal RC session:

This MCP is specifically for Graal development, including GraalScript and GS2.
When helping with GraalScript/GS2 code, use it to inspect the latest RC Chat
messages for runtime errors and to read relevant remote scripts through the
Graal File Browser when the user requests that context.

- get_rc_chat: read the latest RC Chat messages with UTC timestamps. It accepts
  an optional limit from 1 to 50 and is useful for diagnosing server commands
  and runtime errors.
- send_rc_chat: send a message or RC command. Ask for confirmation before
  sending anything consequential or public.
- filebrowser_list: refresh and list the current remote File Browser folder.
- filebrowser_cd: change the current remote File Browser folder with {"path"}.
- filebrowser_search: search the current File Browser snapshot with {"query"}.
- filebrowser_read: download and read a remote UTF-8 text file with {"path"}.

The server is local-only and may be unavailable when Graal RC is closed. Do not
expose it on a public interface. Never infer or invent file contents when a
tool call fails; report the failure and ask the user what to do next.
#END
#-----------#
`
}

func mcpAgentCandidates(home string) map[string][]string {
	return map[string][]string{
		"claude-code": {filepath.Join(home, ".claude", "CLAUDE.md")},
		"codex":       {filepath.Join(home, ".codex", "AGENTS.md"), filepath.Join(home, "AGENTS.md")},
		"opencode":    {filepath.Join(home, ".config", "opencode", "AGENTS.md"), filepath.Join(home, ".opencode", "AGENTS.md")},
	}
}

func (a *App) GetMCPAgentStatuses() ([]MCPAgentStatus, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	labels := []string{"claude-code", "codex", "opencode"}
	statuses := make([]MCPAgentStatus, 0, len(labels))
	for _, name := range labels {
		candidates := mcpAgentCandidates(home)[name]
		status := MCPAgentStatus{Name: name, Path: candidates[0], PreferredPath: candidates[0]}
		for _, candidate := range candidates {
			content, readErr := os.ReadFile(candidate)
			if readErr == nil {
				status.Path, status.Exists = candidate, true
				status.Configured = strings.Contains(string(content), mcpInstructionsStart)
				break
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (a *App) SetupMCP(name string) (MCPSetupResult, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return MCPSetupResult{}, err
	}
	candidates, ok := mcpAgentCandidates(home)[name]
	if !ok {
		return MCPSetupResult{}, errors.New("unknown MCP agent")
	}
	path := candidates[0]
	for _, candidate := range candidates {
		if _, statErr := os.Stat(candidate); statErr == nil {
			path = candidate
			break
		}
	}
	content, readErr := os.ReadFile(path)
	if readErr == nil && strings.Contains(string(content), mcpInstructionsStart) {
		return MCPSetupResult{Name: name, Path: path, Configured: true}, nil
	}
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return MCPSetupResult{}, readErr
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return MCPSetupResult{}, err
	}
	if len(content) > 0 && !strings.HasSuffix(string(content), "\n") {
		content = append(content, '\n')
	}
	mode := os.FileMode(0o600)
	created := readErr != nil
	if !created {
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, mode)
	if err != nil {
		return MCPSetupResult{}, err
	}
	if _, err := file.Write(append(content, []byte(mcpInstructionBlock())...)); err != nil {
		_ = file.Close()
		return MCPSetupResult{}, err
	}
	if err := file.Close(); err != nil {
		return MCPSetupResult{}, err
	}
	return MCPSetupResult{Name: name, Path: path, Configured: true, Created: created}, nil
}

// OpenMCPAgentFile opens the detected global instruction file with the
// operating system's default associated application.
func (a *App) OpenMCPAgentFile(name string) error {
	statuses, err := a.GetMCPAgentStatuses()
	if err != nil {
		return err
	}
	var path string
	for _, status := range statuses {
		if status.Name == name {
			if !status.Exists {
				return fmt.Errorf("MCP instruction file does not exist for %s", name)
			}
			path = status.Path
			break
		}
	}
	if path == "" {
		return errors.New("unknown MCP agent")
	}

	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("explorer.exe", path)
	case "darwin":
		command = exec.Command("open", path)
	default:
		command = exec.Command("xdg-open", path)
	}
	return command.Start()
}

type appMCPBackend struct{ app *App }

func (b appMCPBackend) GetRCChat(limit int) []mcp.ChatLine {
	lines := b.app.sessions.ChatHistory(limit)
	result := make([]mcp.ChatLine, 0, len(lines))
	for _, line := range lines {
		result = append(result, mcp.ChatLine{Text: line.Text, Timestamp: line.Timestamp.Format(time.RFC3339Nano)})
	}
	return result
}
func (b appMCPBackend) SendRCChat(message string) error { return b.app.sessions.Execute(message) }
func (b appMCPBackend) FileBrowserStart() error         { return b.app.sessions.StartFileBrowser() }
func (b appMCPBackend) FileBrowserCd(path string) error { return b.app.sessions.FileBrowserCd(path) }
func (b appMCPBackend) FileBrowserFiles() ([]mcp.File, error) {
	entries, err := b.app.sessions.GetFileBrowserFiles()
	if err != nil {
		return nil, err
	}
	files := make([]mcp.File, 0, len(entries))
	for _, entry := range entries {
		files = append(files, mcp.File{Path: entry.Path, Size: entry.Size, IsDirectory: entry.IsDirectory})
	}
	return files, nil
}
func (b appMCPBackend) ReadFile(path string) ([]byte, error) {
	normalized, err := normalizePluginRemotePath(path)
	if err != nil {
		return nil, err
	}
	return b.app.sessions.DownloadFile(normalized)
}

func (a *App) startMCPServer() {
	address := os.Getenv("RC_MCP_ADDR")
	if address == "" {
		address = defaultMCPAddress
	}
	server := &http.Server{
		Addr:              address,
		Handler:           http.HandlerFunc(a.mcpHandler),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	a.mcpServer = server
	go func() {
		log.Printf("MCP server listening on http://%s/mcp", address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("MCP server stopped: %v", err)
		}
	}()
}

func (a *App) mcpHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/mcp" {
		http.NotFound(w, r)
		return
	}
	mcp.NewServer(appMCPBackend{app: a}).ServeHTTP(w, r)
}

func (a *App) stopMCPServer() {
	if a.mcpServer == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := a.mcpServer.Shutdown(ctx); err != nil {
		log.Printf("MCP shutdown: %v", err)
	}
	a.mcpServer = nil
}
