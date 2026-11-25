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

	"graal-rc/internal/fileutil"
	"graal-rc/internal/mcp"
)

const defaultMCPAddress = "127.0.0.1:8765"

const (
	mcpInstructionsStart = "#Nullborne Go RC INSTRUCTIONS START#"
	mcpInstructionsEnd   = "#END"
	mcpInstructionsFence = "#-----------#"
)

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
RC desktop client is running. It uses the MCP HTTP JSON-RPC transport. Use
these tools only when the user asks you to inspect or operate their connected
Graal RC session.

This MCP is specifically for Graal development, including GraalScript and GS2.
Use it to read and change the server's weapons, classes and NPCs, to read
remote files through the Graal File Browser, and to read the RC Chat for the
server's confirmations and script errors.

How a script change reaches the server
--------------------------------------
There is no upload, deploy or "apply on the server" step. Never tell the user
to update, upload or reload a script on the server, and never ask them to do it
for you: saving the change is the deployment.

- Graal RC keeps a local folder synchronized with the server's scripts. Call
  sync_status to get that folder. It holds weapons/<name>.gs2,
  classes/<name>.gs2 and npcs/<name>.gs2.
- When one of those files is saved with at least one character changed, Graal
  RC uploads it within about a second and the server applies the new script
  immediately. Saving a file whose content did not change uploads nothing.
- A new .gs2 file in weapons/ or classes/ creates that weapon or class. A new
  file in npcs/ does not create an NPC: use npc_add.
- script_write, weapon_add, class_add and npc_add change the server directly
  and take effect at once as well; the local file is then refreshed from the
  server, so do not also edit the file for the same change.
- After every change, call get_rc_chat. The server prints a line such as
  "Weapon/GUI-script <name> added/updated by <account>", "Script <class>
  updated by <account>" or "The script of NPC <name> has been updated by
  <account>", followed by any script error. That line is the proof that the
  change is live. If it is missing, call sync_status: sync may be paused, in
  panic mode, have auto push turned off, or the NC connection may be down.
- Nothing here deletes scripts. Removing a local file does not remove the
  script from the server, and there is no delete tool; deleting a weapon,
  class or NPC is something the user does in Graal RC.

Tools
-----
RC Chat
- get_rc_chat: read the latest RC Chat messages with UTC timestamps. Optional
  {"limit"} from 1 to 50. Use it to confirm a script change and to read
  runtime errors.
- send_rc_chat: send a message or RC command with {"message"}. Ask for
  confirmation before sending anything consequential or public.

Scripts (type is "weapon", "class" or "npc"; names are case-sensitive; an NPC
is addressed by {"id"}, or by {"name"} when the name is unique)
- script_list: list the scripts this account can read. Optional {"type"} and
  {"query"} (part of the name).
- script_read: read one script from the server with {"type", "name"} or
  {"type": "npc", "id"}. The reply includes "localPath", the synchronized file,
  when sync is enabled.
- script_write: replace the content of an existing script with {"type",
  "name" or "id", "script"}. It does not create scripts.
- weapon_add: create a weapon with {"name"} and optional {"script"}.
- class_add: create a class with {"name"} and optional {"script"}.
- npc_add: create a database NPC with {"name"} and optional {"id", "npcType",
  "scripter", "level", "x", "y", "script"}. Without "id" it uses the first free
  id from 1000 up.
- npc_reset: restart an NPC.
- npc_warp: move an NPC with {"level", "x", "y"}.
- npc_flags_read / npc_flags_write: read or replace an NPC's flags ({"flags"}
  is the complete text, one flag per line).
- npc_attributes_read: read an NPC's attributes.
- script_refresh: ask the server for the weapon list again.
- sync_status: the sync folder, whether auto push and auto pull are on, and
  whether sync is paused or failing.

The add tools refuse a name that already exists, so they cannot overwrite a
script; use script_write for that. Before a large rewrite, read the script
first and keep the parts you are not changing.

File Browser
- filebrowser_list: list a remote folder with {"path"}, or the current folder.
  Before any folder is opened it returns the accessible folder patterns; drop
  the trailing "*" to get a path.
- filebrowser_cd: open a remote folder with {"path"}; returns its listing.
- filebrowser_search: match file names with {"query"} in {"path"} or in the
  current folder, without entering subfolders. Before any folder is opened it
  matches the accessible folder patterns.
- filebrowser_read: download and read a remote UTF-8 text file with {"path"}.

The server is local-only and may be unavailable when Graal RC is closed. Do not
expose it on a public interface. Never infer or invent file or script contents
when a tool call fails; report the failure and ask the user what to do next.
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
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return MCPSetupResult{}, readErr
	}
	created := readErr != nil
	updated, err := withMCPInstructions(string(content))
	if err != nil {
		return MCPSetupResult{}, fmt.Errorf("%s: %w", path, err)
	}
	if !created && updated == string(content) {
		return MCPSetupResult{Name: name, Path: path, Configured: true}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return MCPSetupResult{}, err
	}
	mode := os.FileMode(0o600)
	if !created {
		if info, statErr := os.Stat(path); statErr == nil {
			mode = info.Mode().Perm()
		}
	}
	// The file belongs to the user and usually holds their own instructions, so
	// it is replaced atomically and the previous version is kept as path+.bak.
	if err := fileutil.AtomicWriteFile(path, []byte(updated), mode); err != nil {
		return MCPSetupResult{}, err
	}
	return MCPSetupResult{Name: name, Path: path, Configured: true, Created: created}, nil
}

// withMCPInstructions returns content with the current instruction block
// installed. A block written by an earlier version is replaced in place, so
// agents that were already set up learn about new tools; otherwise the block
// is appended after the user's own instructions.
func withMCPInstructions(content string) (string, error) {
	block := strings.TrimLeft(mcpInstructionBlock(), "\n")
	start := strings.Index(content, mcpInstructionsStart)
	if start < 0 {
		if content == "" {
			return block, nil
		}
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + "\n" + block, nil
	}

	endMarker := "\n" + mcpInstructionsEnd
	relativeEnd := strings.Index(content[start:], endMarker)
	if relativeEnd < 0 {
		return "", errors.New("the existing Graal RC instruction block has no #END line; remove the block by hand and run the setup again")
	}
	end := start + relativeEnd + len(endMarker)

	// Take the fence lines around the block along with it.
	if fence := strings.LastIndex(content[:start], mcpInstructionsFence); fence >= 0 &&
		strings.TrimSpace(content[fence+len(mcpInstructionsFence):start]) == "" {
		start = fence
	}
	if rest := strings.TrimLeft(content[end:], "\r\n"); strings.HasPrefix(rest, mcpInstructionsFence) {
		end = len(content) - len(rest) + len(mcpInstructionsFence)
	}
	// The block ends with its own line break.
	rest := strings.TrimPrefix(strings.TrimPrefix(content[end:], "\r"), "\n")
	return content[:start] + block + rest, nil
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
	// MCP exposes the raw captured history. The repeated-text aggregation is
	// intentionally a frontend-only presentation concern.
	lines := b.app.sessions.ChatHistory(limit)
	result := make([]mcp.ChatLine, 0, len(lines))
	for _, line := range lines {
		result = append(result, mcp.ChatLine{Text: line.Text, Timestamp: line.Timestamp.Format(time.RFC3339Nano)})
	}
	return result
}
func (b appMCPBackend) SendRCChat(message string) error { return b.app.sessions.Execute(message) }

// mcpFileBrowserTimeout stays below the MCP server's WriteTimeout, so a server
// that never answers yields a tool error instead of a dropped connection.
const mcpFileBrowserTimeout = 20 * time.Second

func (b appMCPBackend) FileBrowserFolders(ctx context.Context) ([]mcp.Folder, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpFileBrowserTimeout)
	defer cancel()
	entries, err := b.app.sessions.WaitFileBrowserFolders(ctx)
	if err != nil {
		return nil, err
	}
	folders := make([]mcp.Folder, 0, len(entries))
	for _, entry := range entries {
		folders = append(folders, mcp.Folder{Pattern: entry.Pattern, Rights: entry.Rights})
	}
	return folders, nil
}

func (b appMCPBackend) FileBrowserCurrentFolder() (string, bool) {
	if folder := b.app.mcpFileBrowserFolder.Load(); folder != nil {
		return *folder, true
	}
	return "", false
}

func (b appMCPBackend) FileBrowserList(ctx context.Context, folder string) (mcp.Listing, error) {
	ctx, cancel := context.WithTimeout(ctx, mcpFileBrowserTimeout)
	defer cancel()
	opened, entries, err := b.app.sessions.ListFileBrowserFolder(ctx, folder)
	if err != nil {
		return mcp.Listing{}, err
	}
	b.app.mcpFileBrowserFolder.Store(&opened)
	files := make([]mcp.File, 0, len(entries))
	for _, entry := range entries {
		// Full paths can be passed straight to filebrowser_read.
		path := entry.Path
		if opened != "" && !strings.HasPrefix(path, opened+"/") {
			path = opened + "/" + path
		}
		files = append(files, mcp.File{Path: path, Size: entry.Size, IsDirectory: entry.IsDirectory})
	}
	return mcp.Listing{Folder: opened, Files: files}, nil
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
	var allowedHosts []string
	if a.mcpServer != nil {
		// RC_MCP_ADDR may name something other than loopback.
		allowedHosts = append(allowedHosts, a.mcpServer.Addr)
	}
	mcp.NewServer(appMCPBackend{app: a}, allowedHosts...).ServeHTTP(w, r)
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
