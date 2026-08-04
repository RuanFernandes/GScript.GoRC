package graalscript

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
	Imports   []string
	Joins     []string
}

type classSymbolMode uint8

const (
	classSymbolExports classSymbolMode = iota
	classSymbolMerged
)

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

	classes := map[string]ScriptSummary{}
	npcs := map[string]ScriptSummary{}
	weapons := map[string]ScriptSummary{}
	paths := []string{}
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
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return err
	}

	// Parsing every synchronized script can be expensive on a large server.
	// The parser is document-local, so process the closed-file summaries in a
	// bounded worker pool and only merge the small summaries on this goroutine.
	type scanResult struct {
		path    string
		kind    string
		keys    []string
		summary ScriptSummary
	}
	workers := runtime.GOMAXPROCS(0)
	if workers > 8 {
		workers = 8
	}
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan string)
	results := make(chan scanResult, workers)
	var scanWG sync.WaitGroup
	for i := 0; i < workers; i++ {
		scanWG.Add(1)
		go func() {
			defer scanWG.Done()
			for path := range jobs {
				text, readErr := os.ReadFile(path)
				if readErr != nil {
					continue
				}
				kind, keys := workspaceSymbolKeys(abs, path)
				if kind == "" || len(keys) == 0 {
					continue
				}
				uri := pathToURI(path)
				doc := parseDocument(uri, string(text), 0)
				results <- scanResult{
					path: path,
					kind: kind,
					keys: keys,
					summary: ScriptSummary{
						Name:      filepath.Base(path),
						Kind:      kind,
						URI:       uri,
						Functions: append([]FunctionSymbol(nil), doc.Functions...),
						Imports:   append([]string(nil), doc.Imports...),
						Joins:     classLevelJoins(doc),
					},
				}
			}
		}()
	}
	go func() {
		for _, path := range paths {
			jobs <- path
		}
		close(jobs)
		scanWG.Wait()
		close(results)
	}()
	scanned := []scanResult{}
	for result := range results {
		scanned = append(scanned, result)
	}
	sort.SliceStable(scanned, func(i, j int) bool { return scanned[i].path < scanned[j].path })
	for _, result := range scanned {
		for _, key := range result.keys {
			switch result.kind {
			case "class":
				classes[key] = result.summary
			case "npc":
				npcs[key] = result.summary
			case "weapon":
				weapons[key] = result.summary
			}
		}
	}
	w.mu.Lock()
	w.root = abs
	w.files = map[string]*Document{}
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
	return w.classSymbolsWithMode(names, side, classSymbolExports)
}

func (w *Workspace) joinedClassSymbols(names []string, side string) []FunctionSymbol {
	return w.classSymbolsWithMode(names, side, classSymbolMerged)
}

func (w *Workspace) classSymbolsWithMode(names []string, side string, mode classSymbolMode) []FunctionSymbol {
	w.mu.RLock()
	defer w.mu.RUnlock()
	seenFunctions := map[string]bool{}
	seenClasses := map[string]classSymbolMode{}
	result := []FunctionSymbol{}
	for _, name := range names {
		w.appendClassSymbolsLocked(name, side, mode, seenClasses, seenFunctions, &result)
	}
	sort.SliceStable(result, func(i, j int) bool { return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name) })
	return result
}

func (w *Workspace) appendClassSymbolsLocked(name, side string, mode classSymbolMode, seenClasses map[string]classSymbolMode, seenFunctions map[string]bool, result *[]FunctionSymbol) {
	for _, key := range w.classLookupKeysLocked(name, mode) {
		summary, ok := w.classes[key]
		if !ok {
			continue
		}
		identity := summary.URI
		if identity == "" {
			identity = key
		}
		if previousMode, seen := seenClasses[identity]; seen && previousMode >= mode {
			continue
		}
		seenClasses[identity] = mode

		for _, fn := range summary.Functions {
			if (mode == classSymbolExports && !fn.Public) || (side != "" && fn.Side != side) {
				continue
			}
			functionKey := normalizeName(fn.Name)
			if seenFunctions[functionKey] {
				continue
			}
			seenFunctions[functionKey] = true
			*result = append(*result, fn)
		}

		for _, dependency := range summary.Imports {
			w.appendClassSymbolsLocked(dependency, side, classSymbolExports, seenClasses, seenFunctions, result)
		}
		for _, dependency := range summary.Joins {
			w.appendClassSymbolsLocked(dependency, side, classSymbolMerged, seenClasses, seenFunctions, result)
		}
		return
	}
}

func (w *Workspace) classLookupKeysLocked(name string, mode classSymbolMode) []string {
	keys := symbolLookupKeys(name)
	for _, key := range keys {
		if _, ok := w.classes[key]; ok {
			return []string{key}
		}
	}

	// Constructors are functions in GS2 class scripts. Resolve `new ORM()` to
	// the class file that exports `public function ORM(...)` when the caller
	// only has the constructor/type name available.
	constructor := normalizeName(strings.TrimSpace(name))
	for key, summary := range w.classes {
		for _, fn := range summary.Functions {
			if (mode == classSymbolExports && !fn.Public) || !strings.EqualFold(fn.Name, constructor) {
				continue
			}
			keys = appendUnique(keys, key)
			break
		}
	}
	return keys
}

func classLevelJoins(doc *Document) []string {
	joins := []string{}
	for _, binding := range doc.JoinBindings {
		if binding.OwnerKey != scriptScopeKey || binding.ClassName == "" {
			continue
		}
		joins = appendUnique(joins, binding.ClassName)
	}
	return joins
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
	value = trimScriptExtension(value)
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

func trimScriptExtension(value string) string {
	lower := strings.ToLower(value)
	for _, extension := range []string{".gs2", ".gscript"} {
		if strings.HasSuffix(lower, extension) {
			return value[:len(value)-len(extension)]
		}
	}
	return value
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
