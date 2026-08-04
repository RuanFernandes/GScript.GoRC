package graalscript

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

type Definition struct {
	Name              string
	Kind              string
	Params            []string
	Returns           string
	Scope             string
	Description       string
	Example           string
	Dynamic           bool
	DynamicExpression string
}

type Catalog struct {
	Definitions []Definition
	byName      map[string]Definition
	byMember    map[string][]Definition
}

func newCatalog() *Catalog {
	return &Catalog{byName: map[string]Definition{}, byMember: map[string][]Definition{}}
}

func loadCatalog() *Catalog {
	catalog := newCatalog()
	if entries, err := loadScriptHelpDefinitions(); err == nil {
		for _, entry := range entries {
			catalog.add(entry)
		}
	}
	for _, entry := range builtinDefinitions() {
		if _, exists := catalog.byName[normalizeName(entry.Name)]; !exists {
			catalog.add(entry)
		}
	}
	return catalog
}

func builtinCatalog() *Catalog {
	catalog := newCatalog()
	for _, entry := range builtinDefinitions() {
		catalog.add(entry)
	}
	return catalog
}

const scriptHelpAPIURL = "https://api.gscript.dev/"

var (
	scriptHelpHTTPClient = &http.Client{Timeout: 10 * time.Second}
	scriptHelpCache      struct {
		sync.Mutex
		loaded  bool
		entries []Definition
	}
)

func loadScriptHelpDefinitions() ([]Definition, error) {
	scriptHelpCache.Lock()
	defer scriptHelpCache.Unlock()
	if scriptHelpCache.loaded {
		return append([]Definition(nil), scriptHelpCache.entries...), nil
	}
	entries, err := fetchScriptHelpDefinitions()
	if err != nil {
		return nil, err
	}
	scriptHelpCache.entries = append([]Definition(nil), entries...)
	scriptHelpCache.loaded = true
	return append([]Definition(nil), entries...), nil
}

func refreshScriptHelpDefinitions() ([]Definition, error) {
	scriptHelpCache.Lock()
	defer scriptHelpCache.Unlock()
	entries, err := fetchScriptHelpDefinitions()
	if err != nil {
		return nil, err
	}
	scriptHelpCache.entries = append([]Definition(nil), entries...)
	scriptHelpCache.loaded = true
	return append([]Definition(nil), entries...), nil
}

func fetchScriptHelpDefinitions() ([]Definition, error) {
	response, err := scriptHelpHTTPClient.Get(scriptHelpAPIURL)
	if err != nil {
		return nil, fmt.Errorf("fetch GraalScript definitions: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch GraalScript definitions: HTTP %s", response.Status)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read GraalScript definitions: %w", err)
	}
	entries, err := parseDefinitions(data)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (c *Catalog) add(entry Definition) {
	entry.Name = strings.TrimSpace(entry.Name)
	if entry.Name == "" {
		return
	}
	if entry.Kind == "" {
		if len(entry.Params) > 0 || strings.TrimSpace(entry.Returns) != "" {
			entry.Kind = "function"
		} else {
			entry.Kind = "variable"
		}
	}
	entry.Scope = normalizeDefinitionScope(entry.Scope)
	if entry.Scope == "" {
		entry.Scope = "global"
	}
	key := normalizeName(entry.Name)
	if existing, ok := c.byName[key]; ok {
		// Some historical definitions use the same name with different casing or
		// omit the `type` field. Keep the richer entry deterministically.
		if len(existing.Params) >= len(entry.Params) && existing.Description != "" {
			return
		}
	}
	c.byName[key] = entry
	updated := false
	for i := range c.Definitions {
		if normalizeName(c.Definitions[i].Name) == key {
			c.Definitions[i] = entry
			updated = true
			break
		}
	}
	if !updated {
		c.Definitions = append(c.Definitions, entry)
	}
	if dot := strings.LastIndex(entry.Name, "."); dot > 0 && dot+1 < len(entry.Name) {
		receiver := normalizeName(entry.Name[:dot])
		member := entry
		member.Name = entry.Name[dot+1:]
		c.byMember[receiver] = appendOrReplace(c.byMember[receiver], member)
	}
}

func (c *Catalog) lookup(name string) (Definition, bool) {
	entry, ok := c.byName[normalizeName(name)]
	return entry, ok
}

func (c *Catalog) members(receiver string) []Definition {
	items := append([]Definition(nil), c.byMember[normalizeName(receiver)]...)
	return items
}

func (c *Catalog) guiTypeNames(controlType string) []string {
	names := []string{}
	seen := map[string]bool{}
	appendType := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || !isGUITypeName(name) || seen[normalizeName(name)] {
			return
		}
		seen[normalizeName(name)] = true
		names = append(names, name)
	}

	appendType(controlType)
	if definition, ok := c.lookup(controlType); ok {
		appendType(definition.Returns)
	}

	// Some GS2 code uses GuiButton while the API names the concrete type
	// GuiButtonCtrl. Use the API's concrete member namespace when it exists.
	if !isGUIProfileType(controlType) && strings.HasPrefix(normalizeName(controlType), "gui") && !strings.HasSuffix(normalizeName(controlType), "ctrl") {
		ctrlType := strings.TrimSpace(controlType) + "Ctrl"
		if len(c.members(ctrlType)) > 0 {
			appendType(ctrlType)
		}
	}
	return names
}

func parseDefinitions(data []byte) ([]Definition, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse GraalScript definitions: %w", err)
	}
	keys := make([]string, 0, len(raw))
	for key := range raw {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]Definition, 0, len(keys))
	for _, key := range keys {
		var value struct {
			Name        string   `json:"name"`
			Type        string   `json:"type"`
			Params      []string `json:"params"`
			Returns     string   `json:"returns"`
			Scope       string   `json:"scope"`
			Description string   `json:"description"`
			Example     string   `json:"example"`
		}
		if err := json.Unmarshal(raw[key], &value); err != nil {
			continue
		}
		if value.Name == "" {
			value.Name = key
		}
		entries = append(entries, Definition{
			Name: value.Name, Kind: value.Type, Params: value.Params,
			Returns: value.Returns, Scope: normalizeDefinitionScope(value.Scope),
			Description: value.Description, Example: value.Example,
		})
	}
	if len(entries) == 0 {
		return nil, errors.New("definitions file is empty")
	}
	return entries, nil
}

func normalizeDefinitionScope(scope string) string {
	scope = strings.TrimSpace(scope)
	switch strings.ToLower(scope) {
	case "client", "clientside", "client-side":
		return "clientside"
	case "server", "serverside", "server-side":
		return "serverside"
	default:
		return scope
	}
}

func appendOrReplace(items []Definition, entry Definition) []Definition {
	for i := range items {
		if normalizeName(items[i].Name) == normalizeName(entry.Name) {
			items[i] = entry
			return items
		}
	}
	return append(items, entry)
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func builtinDefinitions() []Definition {
	entries := []Definition{
		{Name: "temp", Kind: "variable", Scope: "global", Description: "Function-local temporary variable scope."},
		{Name: "player", Kind: "variable", Scope: "global", Description: "The triggering or current player object."},
		{Name: "this", Kind: "variable", Scope: "global", Description: "The current NPC, weapon or object."},
		{Name: "thiso", Kind: "variable", Scope: "global", Description: "The original object when code runs in another object scope."},
		{Name: "level", Kind: "variable", Scope: "global", Description: "The current level object."},
		{Name: "client", Kind: "variable", Scope: "global", Description: "Client-writable flag/property scope."},
		{Name: "clientr", Kind: "variable", Scope: "global", Description: "Server-writable, client-readable property scope."},
		{Name: "server", Kind: "variable", Scope: "serverside", Description: "Server-only property scope."},
		{Name: "serverr", Kind: "variable", Scope: "server/client", Description: "Server-written, client-readable property scope."},
		{Name: "serveroptions", Kind: "variable", Scope: "serverside", Description: "Read-only server options scope."},
		{Name: "pi", Kind: "variable", Scope: "global", Returns: "float", Description: "The mathematical constant pi."},
		{Name: "true", Kind: "variable", Scope: "global", Returns: "bool"},
		{Name: "false", Kind: "variable", Scope: "global", Returns: "bool"},
		{Name: "null", Kind: "variable", Scope: "global", Returns: "nil"},
		{Name: "GuiControl", Kind: "function", Params: []string{"name"}, Returns: "GuiControl", Scope: "clientside",
			Description: "Creates a clientside GUI control."},
		{Name: "GuiControlProfile", Kind: "variable", Returns: "object", Scope: "clientside",
			Description: "Clientside GUI control profile object."},
		{Name: "echo", Kind: "function", Params: []string{"text"}, Returns: "void", Scope: "global", Description: "Writes a message to the server log."},
		{Name: "setTimer", Kind: "function", Params: []string{"seconds"}, Returns: "void", Scope: "global", Description: "Schedules the onTimeout event."},
		{Name: "scheduleevent", Kind: "function", Params: []string{"delay", "event", "params..."}, Returns: "void", Scope: "global", Description: "Schedules an event on the current object."},
		{Name: "trigger", Kind: "function", Params: []string{"event", "params..."}, Returns: "void", Scope: "global", Description: "Triggers an event on an object."},
		{Name: "triggerclient", Kind: "function", Params: []string{"type", "weapon", "event", "params..."}, Returns: "void", Scope: "serverside", Description: "Triggers a client-side event."},
		{Name: "triggerserver", Kind: "function", Params: []string{"type", "weapon", "event", "params..."}, Returns: "void", Scope: "clientside", Description: "Triggers a server-side event."},
		{Name: "findplayer", Kind: "function", Params: []string{"account"}, Returns: "player", Scope: "serverside", Description: "Finds a player by account."},
		{Name: "findplayer2", Kind: "function", Params: []string{"name"}, Returns: "player", Scope: "serverside", Description: "Finds a player by account or community name."},
		{Name: "findplayerbyid", Kind: "function", Params: []string{"id"}, Returns: "player", Scope: "serverside"},
		{Name: "findnpc", Kind: "function", Params: []string{"name"}, Returns: "npc", Scope: "serverside"},
		{Name: "findnpcbyid", Kind: "function", Params: []string{"id"}, Returns: "npc", Scope: "serverside"},
		{Name: "findweapon", Kind: "function", Params: []string{"name"}, Returns: "weapon", Scope: "serverside"},
		{Name: "findlevel", Kind: "function", Params: []string{"name"}, Returns: "level", Scope: "serverside"},
		{Name: "arraylen", Kind: "function", Params: []string{"array"}, Returns: "int", Scope: "global"},
		{Name: "strlen", Kind: "function", Params: []string{"text"}, Returns: "int", Scope: "global"},
		{Name: "min", Kind: "function", Params: []string{"a", "b"}, Returns: "number", Scope: "global"},
		{Name: "max", Kind: "function", Params: []string{"a", "b"}, Returns: "number", Scope: "global"},
		{Name: "abs", Kind: "function", Params: []string{"value"}, Returns: "number", Scope: "global"},
		{Name: "floor", Kind: "function", Params: []string{"value"}, Returns: "int", Scope: "global"},
		{Name: "random", Kind: "function", Params: []string{"min", "max"}, Returns: "number", Scope: "global"},
	}
	for _, name := range []string{"account", "nick", "communityname", "x", "y", "z", "dir", "hearts", "maxhearts", "rupees", "bombs", "chat", "level"} {
		entries = append(entries, Definition{Name: "player." + name, Kind: "variable", Scope: "global", Description: "Common player property."})
	}
	for _, name := range []string{"name", "id", "x", "y", "z", "health", "maxhealth", "chat", "scheduleevent", "trigger"} {
		entries = append(entries, Definition{Name: "this." + name, Kind: "variable", Scope: "global", Description: "Common object member."})
	}
	for _, name := range []string{"add", "remove", "size", "index", "clear", "sortascending", "sortdescending", "delete"} {
		kind := "function"
		params := []string{"value"}
		if name == "size" {
			params = nil
		}
		entries = append(entries, Definition{Name: "array." + name, Kind: kind, Params: params, Scope: "global", Description: "Array method."})
	}
	for _, name := range []string{"length", "upper", "lower", "substring", "pos", "tokenize", "trim", "starts", "escape"} {
		entries = append(entries, Definition{Name: "string." + name, Kind: "function", Params: []string{"value"}, Scope: "global", Description: "String method."})
	}
	entries = append(entries, builtinGUIControlDefinitions()...)
	entries = append(entries, builtinGUIControlProfileDefinitions()...)
	for _, event := range []struct {
		name   string
		params []string
		scope  string
	}{
		{name: "onCreated"}, {name: "onDestroy"}, {name: "onRemoved"},
		{name: "onPlayerEnters"}, {name: "onPlayerLeaves", scope: "serverside"},
		{name: "onPlayerTouchsMe"}, {name: "onPlayerChats"},
		{name: "onPlayerDies", scope: "clientside"}, {name: "onPlayerHurt", scope: "clientside"},
		{name: "onPlayerLogin", params: []string{"account", "communityname"}, scope: "clientside"},
		{name: "onPlayerLogout", params: []string{"account", "communityname"}, scope: "clientside"},
		{name: "onWeaponFired", scope: "clientside"},
		{name: "onKeyPressed", params: []string{"keycode", "key", "scancode"}, scope: "clientside"},
		{name: "onKeyReleased", params: []string{"key", "scancode"}, scope: "clientside"},
		{name: "onMouseDown", params: []string{"mousevalue"}, scope: "clientside"},
		{name: "onMouseUp", params: []string{"mousevalue"}, scope: "clientside"},
		{name: "onTimeout"}, {name: "onLevelLoaded"}, {name: "onRCChat", params: []string{"cmd", "params"}, scope: "clientside"},
		{name: "onWasHit", params: []string{"obj"}},
	} {
		entries = append(entries, Definition{Name: event.name, Kind: "function", Params: event.params, Scope: event.scope, Description: "GraalScript event handler."})
	}
	return entries
}

func builtinGUIControlDefinitions() []Definition {
	entries := []Definition{}
	for _, name := range []string{
		"active", "awake", "canmove", "canresize", "clipchildren", "clipmove", "cliptobounds",
		"controls", "cursor", "editing", "extent", "flickering", "flickertime", "height", "hint",
		"horizsizing", "layer", "minextent", "minsize", "parent", "position", "profile", "resizeheight",
		"resizewidth", "scrolllinex", "scrollliney", "showhint", "useownprofile", "vertsizing", "visible",
		"width", "x", "y",
	} {
		returns := ""
		if name == "profile" {
			returns = "GuiControlProfile"
		}
		entries = append(entries, Definition{
			Name: "GuiControl." + name, Kind: "variable", Scope: "clientside",
			Returns:     returns,
			Description: "Common GUI control property.",
		})
	}
	for _, method := range []struct {
		name   string
		params []string
	}{
		{name: "addControl", params: []string{"control"}},
		{name: "removeControl", params: []string{"control"}},
		{name: "clearControls"},
		{name: "makeFirstResponder", params: []string{"bool"}},
		{name: "isFirstResponder"},
		{name: "bringToFront"},
		{name: "pushToBack"},
		{name: "show"},
		{name: "hide"},
		{name: "showTop"},
		{name: "destroy"},
		{name: "getParent"},
		{name: "getRoot"},
		{name: "globalToLocalCoord", params: []string{"coord"}},
		{name: "localToGlobalCoord", params: []string{"coord"}},
		{name: "resize", params: []string{"x", "y", "width", "height"}},
		{name: "tabFirst"},
	} {
		entries = append(entries, Definition{
			Name: "GuiControl." + method.name, Kind: "function", Params: method.params,
			Scope: "clientside", Description: "Common GUI control method.",
		})
	}
	return entries
}

func builtinGUIControlProfileDefinitions() []Definition {
	entries := []Definition{}
	for _, property := range []struct {
		name    string
		returns string
	}{
		{name: "align", returns: "string"},
		{name: "autosizeheight", returns: "bool"},
		{name: "autosizewidth", returns: "bool"},
		{name: "justify", returns: "string"},
		{name: "linespacing", returns: "int"},
		{name: "textoffset", returns: "string"},
		{name: "borderColor", returns: "array"},
		{name: "borderColorHL", returns: "array"},
		{name: "borderColorNA", returns: "array"},
		{name: "fillColor", returns: "array"},
		{name: "fillColorHL", returns: "array"},
		{name: "fillColorNA", returns: "array"},
		{name: "fontColor", returns: "array"},
		{name: "fontColorHL", returns: "array"},
		{name: "fontColorNA", returns: "array"},
		{name: "fontColorSEL", returns: "array"},
		{name: "fontColorLink", returns: "array"},
		{name: "fontColorLinkHL", returns: "array"},
		{name: "shadowColor", returns: "array"},
		{name: "cursorColor", returns: "array"},
		{name: "border", returns: "int"},
		{name: "borderThickness", returns: "int"},
		{name: "fontType", returns: "string"},
		{name: "fontSize", returns: "int"},
		{name: "fontStyle", returns: "string"},
		{name: "bitmap", returns: "string"},
		{name: "opaque", returns: "bool"},
		{name: "transparency", returns: "float"},
		{name: "textShadow", returns: "bool"},
		{name: "shadowOffset", returns: "array"},
		{name: "canKeyFocus", returns: "bool"},
		{name: "modal", returns: "bool"},
		{name: "mouseOverSelected", returns: "bool"},
		{name: "numbersOnly", returns: "bool"},
		{name: "returnTab", returns: "bool"},
		{name: "tab", returns: "bool"},
		{name: "soundButtonDown", returns: "string"},
		{name: "soundButtonOver", returns: "string"},
	} {
		entries = append(entries, Definition{
			Name: "GuiControlProfile." + property.name, Kind: "variable",
			Returns: property.returns, Scope: "clientside",
			Description: "GUI control profile property.",
		})
	}
	return entries
}
