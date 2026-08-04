package graalscript

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type Workspace struct {
	mu      sync.RWMutex
	root    string
	files   map[string]*Document
	classes map[string]ScriptSummary
	npcs    map[string]ScriptSummary
	weapons map[string]ScriptSummary
}

type ScriptSummary struct {
	Name      string
	Kind      string
	URI       string
	Functions []FunctionSymbol
}

func newWorkspace() *Workspace {
	return &Workspace{
		files:   map[string]*Document{},
		classes: map[string]ScriptSummary{},
		npcs:    map[string]ScriptSummary{},
		weapons: map[string]ScriptSummary{},
	}
}

func (w *Workspace) setRoot(root string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		w.mu.Lock()
		w.root = ""
		w.files = map[string]*Document{}
		w.classes = map[string]ScriptSummary{}
		w.npcs = map[string]ScriptSummary{}
		w.weapons = map[string]ScriptSummary{}
		w.mu.Unlock()
		return nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("workspace root is not a directory")
		}
		return fmt.Errorf("invalid GraalScript workspace %q: %w", root, err)
	}

	files := map[string]*Document{}
	classes := map[string]ScriptSummary{}
	npcs := map[string]ScriptSummary{}
	weapons := map[string]ScriptSummary{}
	err = filepath.WalkDir(abs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if shouldSkipDirectory(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !isScriptFile(path) {
			return nil
		}
		text, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		uri := pathToURI(path)
		doc := parseDocument(uri, string(text), 0)
		// Keep only semantic summaries for closed files. Full source text and
		// tokens stay in memory only for documents currently open in an editor;
		// a real workspace can contain thousands of scripts.
		kind, keys := workspaceSymbolKeys(abs, path)
		if kind == "" || len(keys) == 0 {
			return nil
		}
		summary := ScriptSummary{
			Name:      filepath.Base(path),
			Kind:      kind,
			URI:       uri,
			Functions: append([]FunctionSymbol(nil), doc.Functions...),
		}
		for _, key := range keys {
			switch kind {
			case "class":
				classes[key] = summary
			case "npc":
				npcs[key] = summary
			case "weapon":
				weapons[key] = summary
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	w.mu.Lock()
	w.root = abs
	w.files = files
	w.classes = classes
	w.npcs = npcs
	w.weapons = weapons
	w.mu.Unlock()
	return nil
}

func (w *Workspace) refreshRoot() error {
	w.mu.RLock()
	root := w.root
	openFiles := make(map[string]*Document, len(w.files))
	for uri, doc := range w.files {
		openFiles[uri] = doc
	}
	w.mu.RUnlock()
	if err := w.setRoot(root); err != nil {
		return err
	}
	w.mu.Lock()
	for uri, doc := range openFiles {
		w.files[uri] = doc
	}
	w.mu.Unlock()
	return nil
}

func (w *Workspace) upsert(uri string, doc *Document) {
	w.mu.Lock()
	w.files[uri] = doc
	w.mu.Unlock()
}

func (w *Workspace) remove(uri string) {
	w.mu.Lock()
	delete(w.files, uri)
	w.mu.Unlock()
}

func (w *Workspace) document(uri string) *Document {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.files[uri]
}

func (w *Workspace) classSymbols(names []string, side string) []FunctionSymbol {
	w.mu.RLock()
	defer w.mu.RUnlock()
	seen := map[string]bool{}
	result := []FunctionSymbol{}
	for _, name := range names {
		for _, key := range symbolLookupKeys(name) {
			summary, ok := w.classes[key]
			if !ok {
				continue
			}
			for _, fn := range summary.Functions {
				if fn.Private || (side != "" && fn.Side != side) {
					continue
				}
				if !seen[normalizeName(fn.Name)] {
					seen[normalizeName(fn.Name)] = true
					result = append(result, fn)
				}
			}
			break
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result
}

func (w *Workspace) objectSymbols(kind, name, side string) []FunctionSymbol {
	if kind == "npc" && side != scriptSideServer {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	var summaries map[string]ScriptSummary
	switch kind {
	case "npc":
		summaries = w.npcs
	case "weapon":
		summaries = w.weapons
	default:
		return nil
	}
	var summary ScriptSummary
	var found bool
	for _, key := range symbolLookupKeys(name) {
		if summary, found = summaries[key]; found {
			break
		}
	}
	if !found {
		return nil
	}
	result := make([]FunctionSymbol, 0, len(summary.Functions))
	seen := map[string]bool{}
	for _, fn := range summary.Functions {
		// Calls through findNpc/findWeapon are external calls. Only explicit
		// public functions are part of that contract; lifecycle/private methods
		// must not leak into completion.
		if !fn.Public || fn.Private || fn.Side != side {
			continue
		}
		key := normalizeName(fn.Name)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, fn)
	}
	sort.SliceStable(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result
}

func (w *Workspace) rootPath() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.root
}

func shouldSkipDirectory(name string) bool {
	switch strings.ToLower(name) {
	case ".git", ".idea", ".vscode", "node_modules", "vendor", "bin", "dist", "build":
		return true
	default:
		return false
	}
}

func isScriptFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gs2", ".gscript":
		return true
	default:
		return false
	}
}

func workspaceSymbolKeys(root, path string) (string, []string) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return "", nil
	}
	folder := strings.ToLower(parts[0])
	kind := ""
	switch folder {
	case "class", "classes":
		kind = "class"
	case "npc", "npcs":
		kind = "npc"
	case "weapon", "weapons":
		kind = "weapon"
	default:
		return "", nil
	}
	stem := strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel))
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	keys := []string{normalizeName(stem)}
	if normalizeName(base) != keys[0] {
		keys = append(keys, normalizeName(base))
	}
	return kind, keys
}

func symbolLookupKeys(name string) []string {
	value := strings.TrimSpace(strings.Trim(name, `"`))
	if value == "" {
		return nil
	}
	value = strings.TrimSuffix(value, filepath.Ext(value))
	value = strings.ReplaceAll(value, "\\", "/")
	keys := []string{normalizeName(value)}
	base := value
	if slash := strings.LastIndexByte(base, '/'); slash >= 0 {
		base = base[slash+1:]
	}
	if key := normalizeName(base); key != "" && key != keys[0] {
		keys = append(keys, key)
	}
	return keys
}

func pathToURI(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	path = filepath.ToSlash(path)
	if len(path) >= 2 && path[1] == ':' && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func uriToPath(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err == nil && parsed.Scheme == "file" {
		path, err := url.PathUnescape(parsed.Path)
		if err == nil {
			if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
				path = path[1:]
			}
			return filepath.FromSlash(path)
		}
	}
	return filepath.FromSlash(value)
}
